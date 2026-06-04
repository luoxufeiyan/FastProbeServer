package manager

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"net/url"
	"strings"
	"sync"
	"time"

	"FastProbeServer/db"
)

var (
	alertChannels = make(map[int]db.AlertChannel)
	alertRules    = make(map[int]db.AlertRule)
	alertLock     sync.RWMutex

	// ruleState tracks whether a rule is currently triggered for a specific node
	// map[RuleID]map[NodeID]bool
	ruleState = make(map[int]map[int]bool)
	stateMu   sync.RWMutex

	httpClient = &http.Client{Timeout: 10 * time.Second}
)

type Condition struct {
	Type      string  `json:"type"`      // "online", "offline", "high_load"
	Metric    string  `json:"metric"`    // "cpu", "ram", "disk", "network"
	Threshold float64 `json:"threshold"` // e.g. 80 for 80%, or Mbps for network
}

type GotifyConfig struct {
	URL      string `json:"url"`
	Token    string `json:"token"`
	Priority int    `json:"priority"`
}

type WebhookConfig struct {
	URL      string `json:"url"`
	Auth     string `json:"auth"`
	Template string `json:"template"`
}

type MailConfig struct {
	SMTPServer string `json:"smtp_server"`
	SMTPPort   int    `json:"smtp_port"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	From       string `json:"from"`
	To         string `json:"to"`
}

type TelegramConfig struct {
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

// ReloadAlerts loads rules and channels from DB into memory
func ReloadAlerts() error {
	channels, err := db.GetAlertChannels()
	if err != nil {
		return err
	}
	rules, err := db.GetAlertRules()
	if err != nil {
		return err
	}

	alertLock.Lock()
	defer alertLock.Unlock()

	alertChannels = make(map[int]db.AlertChannel)
	for _, c := range channels {
		alertChannels[c.ID] = c
	}

	alertRules = make(map[int]db.AlertRule)
	for _, r := range rules {
		alertRules[r.ID] = r
	}

	// Restore ruleState from database inference
	states, err := db.GetActiveAlertStates()
	if err == nil {
		stateMu.Lock()
		ruleState = states
		stateMu.Unlock()
	}

	return nil
}

// EvaluateNodeState checks all rules for a given node.
// It should be called when a node's state changes or periodically.
func EvaluateNodeState(nodeID int) {
	alertLock.RLock()
	defer alertLock.RUnlock()

	stateLock.RLock()
	status, exists := statuses[nodeID]
	stateLock.RUnlock()

	if !exists {
		return
	}

	for _, rule := range alertRules {
		if !rule.Enabled {
			continue
		}

		// Check if rule applies to this node
		if len(rule.Nodes) > 0 {
			var nodeIDs []int
			json.Unmarshal(rule.Nodes, &nodeIDs)
			applies := false
			if len(nodeIDs) == 0 {
				applies = true // Empty array means all nodes
			} else {
				for _, id := range nodeIDs {
					if id == nodeID {
						applies = true
						break
					}
				}
			}
			if !applies {
				continue
			}
		}

		// Parse conditions
		var conditions []Condition
		if err := json.Unmarshal(rule.Conditions, &conditions); err != nil {
			continue // Invalid conditions
		}

		for _, cond := range conditions {
			isTriggered := false
			triggerMessage := ""

			switch cond.Type {
			case "offline":
				isTriggered = !status.IsOnline
				if isTriggered {
					triggerMessage = fmt.Sprintf("Node %s is offline.", status.Name)
				}
			case "high_load":
				if status.IsOnline {
					switch cond.Metric {
					case "cpu":
						if status.CPU > cond.Threshold {
							isTriggered = true
							triggerMessage = fmt.Sprintf("Node %s CPU usage is %.1f%% (>%.1f%%)", status.Name, status.CPU, cond.Threshold)
						}
					case "ram":
						ramPct := float64(0)
						if status.MemTotal > 0 {
							ramPct = float64(status.MemUsed) / float64(status.MemTotal) * 100
						}
						if ramPct > cond.Threshold {
							isTriggered = true
							triggerMessage = fmt.Sprintf("Node %s RAM usage is %.1f%% (>%.1f%%)", status.Name, ramPct, cond.Threshold)
						}
					case "disk":
						diskPct := float64(0)
						if status.DiskTotal > 0 {
							diskPct = float64(status.DiskUsed) / float64(status.DiskTotal) * 100
						}
						if diskPct > cond.Threshold {
							isTriggered = true
							triggerMessage = fmt.Sprintf("Node %s Disk usage is %.1f%% (>%.1f%%)", status.Name, diskPct, cond.Threshold)
						}
					case "network":
						// Network threshold in Mbps
						netMbps := float64(status.NetRx+status.NetTx) * 8 / 1000000
						if netMbps > cond.Threshold {
							isTriggered = true
							triggerMessage = fmt.Sprintf("Node %s Network traffic is %.2f Mbps (>%.2f Mbps)", status.Name, netMbps, cond.Threshold)
						}
					}
				}
			}

			// Debounce using edge-triggering
			stateMu.Lock()
			if ruleState[rule.ID] == nil {
				ruleState[rule.ID] = make(map[int]bool)
			}
			prevState := ruleState[rule.ID][nodeID]
			
			if isTriggered && !prevState {
				// State changed from normal to triggered -> Send Alert
				ruleState[rule.ID][nodeID] = true
				go pushAlert(rule, nodeID, triggerMessage, false)
			} else if !isTriggered && prevState {
				// State changed from triggered to normal
				ruleState[rule.ID][nodeID] = false
				
				recoveryMessage := fmt.Sprintf("[Recovered] Node %s condition %s is back to normal.", status.Name, cond.Metric)
				if cond.Type == "offline" {
					recoveryMessage = fmt.Sprintf("[Recovered] Node %s is back online.", status.Name)
				}
				go pushAlert(rule, nodeID, recoveryMessage, true)
			}
			stateMu.Unlock()
		}
	}
}

// pushAlert sends the message to all channels configured for the rule
func pushAlert(rule db.AlertRule, nodeID int, message string, isRecovery bool) {
	alertLock.RLock()
	var channelIDs []int
	json.Unmarshal(rule.Channels, &channelIDs)
	
	var targets []db.AlertChannel
	for _, cid := range channelIDs {
		if c, ok := alertChannels[cid]; ok {
			targets = append(targets, c)
		}
	}
	alertLock.RUnlock()

	for _, ch := range targets {
		status := "success"
		errMsg := ""

		switch ch.Type {
		case "gotify":
			err := sendGotify(ch.Config, rule.Name, message)
			if err != nil {
				status = "failed"
				errMsg = err.Error()
			}
		case "webhook":
			err := sendWebhook(ch.Config, rule.Name, message)
			if err != nil {
				status = "failed"
				errMsg = err.Error()
			}
		case "mail":
			err := sendMail(ch.Config, rule.Name, message)
			if err != nil {
				status = "failed"
				errMsg = err.Error()
			}
		case "telegram":
			err := sendTelegram(ch.Config, rule.Name, message)
			if err != nil {
				status = "failed"
				errMsg = err.Error()
			}
		default:
			status = "failed"
			errMsg = "Unknown channel type: " + ch.Type
		}

		db.AddAlertLog(rule.ID, ch.ID, nodeID, message, status, errMsg)
	}
}

// TestAlertChannel sends a test message to a specific channel config
func TestAlertChannel(channelType string, configJSON json.RawMessage) error {
	switch channelType {
	case "gotify":
		return sendGotify(configJSON, "Test Alert", "This is a test notification from FastProbe.")
	case "webhook":
		return sendWebhook(configJSON, "Test Alert", "This is a test notification from FastProbe.")
	case "mail":
		return sendMail(configJSON, "Test Alert", "This is a test notification from FastProbe.")
	case "telegram":
		return sendTelegram(configJSON, "Test Alert", "This is a test notification from FastProbe.")
	default:
		return fmt.Errorf("unsupported channel type")
	}
}

func sendGotify(configJSON json.RawMessage, title, message string) error {
	var cfg GotifyConfig
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		return fmt.Errorf("invalid gotify config: %v", err)
	}

	if cfg.URL == "" || cfg.Token == "" {
		return fmt.Errorf("gotify url or token is empty")
	}

	u, err := url.Parse(cfg.URL)
	if err != nil {
		return fmt.Errorf("invalid gotify url: %v", err)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/message"
	q := u.Query()
	q.Set("token", cfg.Token)
	u.RawQuery = q.Encode()

	payload := map[string]interface{}{
		"title":    title,
		"message":  message,
		"priority": cfg.Priority,
	}
	bodyBytes, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", u.String(), bytes.NewBuffer(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gotify responded with %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func sendWebhook(configJSON json.RawMessage, title, message string) error {
	var cfg WebhookConfig
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		return fmt.Errorf("invalid webhook config: %v", err)
	}

	if cfg.URL == "" {
		return fmt.Errorf("webhook url is empty")
	}

	var bodyBytes []byte
	if cfg.Template != "" {
		// Very basic template replacement, using strings.ReplaceAll
		// In a production environment with complex escaping needs, a real templating engine might be better,
		// but since it's user-provided JSON, they should format it safely.
		// To be slightly safer with JSON breaking quotes, we could marshal the strings first,
		// but simple string replacement is the most flexible for raw templates.
		
		// To prevent JSON injection, we json.Marshal the title/message first and strip the surrounding quotes.
		titleEscaped, _ := json.Marshal(title)
		titleStr := strings.Trim(string(titleEscaped), "\"")
		
		msgEscaped, _ := json.Marshal(message)
		msgStr := strings.Trim(string(msgEscaped), "\"")

		payloadStr := strings.ReplaceAll(cfg.Template, "${TITLE}", titleStr)
		payloadStr = strings.ReplaceAll(payloadStr, "${MESSAGE}", msgStr)
		
		bodyBytes = []byte(payloadStr)
	} else {
		payload := map[string]interface{}{
			"title":   title,
			"message": message,
		}
		bodyBytes, _ = json.Marshal(payload)
	}

	req, err := http.NewRequest("POST", cfg.URL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.Auth != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Auth)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook responded with %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func sendMail(configJSON json.RawMessage, title, message string) error {
	var cfg MailConfig
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		return fmt.Errorf("invalid mail config: %v", err)
	}

	if cfg.SMTPServer == "" || cfg.From == "" || cfg.To == "" {
		return fmt.Errorf("mail server, from, or to is empty")
	}

	auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.SMTPServer)

	msg := fmt.Sprintf("From: %s\r\n"+
		"To: %s\r\n"+
		"Subject: %s\r\n"+
		"\r\n"+
		"%s\r\n", cfg.From, cfg.To, title, message)

	addr := fmt.Sprintf("%s:%d", cfg.SMTPServer, cfg.SMTPPort)
	return smtp.SendMail(addr, auth, cfg.From, []string{cfg.To}, []byte(msg))
}

func sendTelegram(configJSON json.RawMessage, title, message string) error {
	var cfg TelegramConfig
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		return fmt.Errorf("invalid telegram config: %v", err)
	}

	if cfg.BotToken == "" || cfg.ChatID == "" {
		return fmt.Errorf("telegram bot token or chat id is empty")
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", cfg.BotToken)
	
	// Bold title, and then message
	text := fmt.Sprintf("<b>%s</b>\n\n%s", title, message)

	payload := map[string]interface{}{
		"chat_id":    cfg.ChatID,
		"text":       text,
		"parse_mode": "HTML",
	}
	bodyBytes, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram responded with %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
