package handler

import (
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"time"

	"FastProbeServer/config"
	"FastProbeServer/db"
	"FastProbeServer/manager"

	"golang.org/x/crypto/bcrypt"
)

// FS contains our embedded frontend files
//
//go:embed all:frontend/*
var FS embed.FS

// RegisterRoutes sets up all standard and FastCGI HTTP routes using go1.22+ ServeMux
func RegisterRoutes() *http.ServeMux {
	mux := http.NewServeMux()

	// --- Public & Frontend Routes ---
	mux.HandleFunc("GET /", indexHandler)
	mux.HandleFunc("GET /api/status", statusHandler)
	mux.HandleFunc("GET /api/node/{id}/history", historyHandler)

	// --- Report Route (From Nodes) ---
	mux.HandleFunc("POST /report", nodeAuthMiddleware(reportHandler))

	// --- Setup & Admin Routes ---
	mux.HandleFunc("GET /api/check-setup", checkSetupHandler)
	mux.HandleFunc("POST /api/setup", setupHandler)
	mux.HandleFunc("POST /api/login", loginHandler)
	
	// Admin authenticated routes
	mux.HandleFunc("GET /api/admin/nodes", adminAuthMiddleware(getNodesHandler))
	mux.HandleFunc("POST /api/admin/nodes", adminAuthMiddleware(addNodeHandler))
	mux.HandleFunc("PUT /api/admin/nodes/{id}", adminAuthMiddleware(updateNodeHandler))
	mux.HandleFunc("DELETE /api/admin/nodes/{id}", adminAuthMiddleware(deleteNodeHandler))
	mux.HandleFunc("GET /api/admin/config", adminAuthMiddleware(getConfigHandler))
	mux.HandleFunc("POST /api/admin/config", adminAuthMiddleware(updateConfigHandler))
	mux.HandleFunc("GET /api/admin/status", adminAuthMiddleware(adminStatusHandler))

	mux.HandleFunc("GET /api/admin/pending", adminAuthMiddleware(getPendingNodesHandler))
	mux.HandleFunc("POST /api/admin/pending/{secret}/accept", adminAuthMiddleware(acceptPendingNodeHandler))
	mux.HandleFunc("DELETE /api/admin/pending/{secret}", adminAuthMiddleware(deletePendingNodeHandler))

	return mux
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	content, err := fs.ReadFile(FS, "frontend/index.html")
	if err != nil {
		http.Error(w, "Internal Server Error: index.html not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(content)
}

func checkSetupHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if config.Current == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"is_setup": false})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"is_setup": config.Current.IsSetup,
		"show_details": config.Current.ShowDetails,
		"site_title": config.Current.SiteTitle,
		"announcement": config.Current.Announcement,
	})
}

func setupHandler(w http.ResponseWriter, r *http.Request) {
	if config.Current != nil && config.Current.IsSetup {
		http.Error(w, "Already setup", http.StatusBadRequest)
		return
	}

	var req config.AppConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Hash admin password
	hash, err := bcrypt.GenerateFromPassword([]byte(req.AdminPass), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Error hashing password", http.StatusInternalServerError)
		return
	}
	req.AdminPass = string(hash)
	req.IsSetup = true
	if req.SnapshotMins <= 0 {
		req.SnapshotMins = 5
	}

	// Update config and try to init DB
	if err := config.UpdateAndSave(req); err != nil {
		http.Error(w, "Error saving config", http.StatusInternalServerError)
		return
	}

	if err := db.InitDB(); err != nil {
		// Revert if DB fails
		req.IsSetup = false
		config.UpdateAndSave(req)
		http.Error(w, fmt.Sprintf("Database error: %v", err), http.StatusInternalServerError)
		return
	}

	// Start manager after DB init
	manager.ReloadNodes()
	manager.StartBackgroundTasks()

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// --- Node Report Handling ---

func nodeAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		secret := r.Header.Get("X-Node-Secret")
		if secret == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		nodeID, ok := manager.Authenticate(secret)
		if !ok {
			// Record pending node request
			var p ReportPayload
			bodyBytes, _ := io.ReadAll(r.Body)
			json.Unmarshal(bodyBytes, &p)
			
			ip := p.IP
			if ip == "" {
				ip = r.RemoteAddr
			}
			manager.RecordPendingNode(secret, ip, p.Hostname, p.OS)

			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Store nodeID in context
		// For simplicity, we just pass it in header or simple context.
		// Since net/http 1.22 context works well, we will use it.
		// Here we just attach it to headers for easy read in next handler.
		r.Header.Set("X-Authenticated-Node-ID", fmt.Sprintf("%d", nodeID))
		
		next.ServeHTTP(w, r)
	}
}

type ReportPayload struct {
	Hostname    string  `json:"hostname"`
	OS          string  `json:"os"`
	KernelVer   string  `json:"kernel_version"`
	CPU         float64 `json:"cpu"`
	MemUsed     int64   `json:"mem_used"`
	MemTotal    int64   `json:"mem_total"`
	SwapUsed    int64   `json:"swap_used"`
	SwapTotal   int64   `json:"swap_total"`
	NetRx       int64   `json:"net_rx"`
	NetTx       int64   `json:"net_tx"`
	NetTotalRx  int64   `json:"net_total_rx"`
	NetTotalTx  int64   `json:"net_total_tx"`
	DiskUsed    int64   `json:"disk_used"`
	DiskTotal   int64   `json:"disk_total"`
	Uptime      int64   `json:"uptime"`
	IP          string  `json:"ip"`
	IPStack     string  `json:"ip_stack"`
	Version     string  `json:"version"`
}

func reportHandler(w http.ResponseWriter, r *http.Request) {
	nodeIDStr := r.Header.Get("X-Authenticated-Node-ID")
	var nodeID int
	fmt.Sscanf(nodeIDStr, "%d", &nodeID)

	var p ReportPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	manager.UpdateReport(nodeID, p.OS, p.KernelVer, p.CPU, p.MemUsed, p.MemTotal, p.SwapUsed, p.SwapTotal, p.NetRx, p.NetTx, p.NetTotalRx, p.NetTotalTx, p.DiskUsed, p.DiskTotal, p.Uptime, p.IP, p.IPStack, p.Version)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	resp := map[string]int{"report_interval": manager.GetReportInterval(nodeID)}
	json.NewEncoder(w).Encode(resp)
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	statuses := manager.GetAllStatuses()

	isAdmin := false
	cookie, err := r.Cookie("admin_token")
	if err == nil && cookie.Value == currentSessionToken && currentSessionToken != "" {
		isAdmin = true
	}

	// Hide secret and sanitize based on config
	for i := range statuses {
		statuses[i].Secret = "" 
		
		if !isAdmin {
			if !statuses[i].Config.ShowDetails {
				statuses[i].OS = ""
				statuses[i].KernelVer = ""
				statuses[i].SwapTotal = 0
				statuses[i].SwapUsed = 0
				statuses[i].NetTotalRx = 0
				statuses[i].NetTotalTx = 0
				statuses[i].IPStack = ""
			}
			
			if !statuses[i].Config.ShowIP {
				statuses[i].IP = ""
			}
		}
	}
	
	showDetails := false
	siteTitle := "FastProbe"
	announcement := ""
	if config.Current != nil {
		showDetails = config.Current.ShowDetails
		siteTitle = config.Current.SiteTitle
		announcement = config.Current.Announcement
	}
	
	if isAdmin {
		showDetails = true
	}
	
	resp := map[string]interface{}{
		"nodes": statuses,
		"show_details": showDetails,
		"is_admin": isAdmin,
		"site_title": siteTitle,
		"announcement": announcement,
		"version": config.Version,
	}
	
	json.NewEncoder(w).Encode(resp)
}

func adminStatusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	statuses := manager.GetAllStatuses()
	for i := range statuses {
		statuses[i].Secret = "" 
	}
	json.NewEncoder(w).Encode(statuses)
}

// --- Admin Endpoints ---

var currentSessionToken string

func generateSessionToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		User string `json:"user"`
		Pass string `json:"pass"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	if config.Current == nil {
		http.Error(w, "Not setup", http.StatusBadRequest)
		return
	}

	if req.User != config.Current.AdminUser {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(config.Current.AdminPass), []byte(req.Pass)); err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Generate secure token
	currentSessionToken = generateSessionToken()

	// Set secure cookie for admin
	http.SetCookie(w, &http.Cookie{
		Name:     "admin_session",
		Value:    currentSessionToken,
		Path:     "/",
		MaxAge:   3600,
		HttpOnly: true,
	})
	
	w.WriteHeader(http.StatusOK)
}

func adminAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("admin_session")
		if err != nil || cookie.Value == "" || cookie.Value != currentSessionToken {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	}
}

func getNodesHandler(w http.ResponseWriter, r *http.Request) {
	nodes, err := db.GetAllNodes()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(nodes)
}

func addNodeHandler(w http.ResponseWriter, r *http.Request) {
	var n db.Node
	if err := json.NewDecoder(r.Body).Decode(&n); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}
	
	if n.Secret == "" {
		b := make([]byte, 16)
		rand.Read(b)
		n.Secret = base64.URLEncoding.EncodeToString(b)
	}

	if err := db.AddNode(n.Name, n.Location, n.Secret, n.IsAdminOnly, n.Config); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	// Refresh memory
	manager.ReloadNodes()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(n)
}

func updateNodeHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	var id int
	fmt.Sscanf(idStr, "%d", &id)

	var n db.Node
	if err := json.NewDecoder(r.Body).Decode(&n); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}
	
	if err := db.UpdateNode(id, n.Name, n.Location, n.IsAdminOnly, n.Config); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	manager.ReloadNodes()
	w.WriteHeader(http.StatusOK)
}

func deleteNodeHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	var id int
	fmt.Sscanf(idStr, "%d", &id)

	if err := db.DeleteNode(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Refresh memory
	manager.ReloadNodes()
	w.WriteHeader(http.StatusOK)
}

func getConfigHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if config.Current == nil {
		http.Error(w, "Not setup", http.StatusBadRequest)
		return
	}
	safeCfg := *config.Current
	safeCfg.AdminPass = ""
	safeCfg.DBPassword = ""
	json.NewEncoder(w).Encode(safeCfg)
}

func updateConfigHandler(w http.ResponseWriter, r *http.Request) {
	var req config.AppConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	if config.Current == nil {
		http.Error(w, "Not setup", http.StatusBadRequest)
		return
	}
	
	newCfg := *config.Current
	newCfg.ShowDetails = req.ShowDetails
	if req.GlobalReportInterval > 0 {
		newCfg.GlobalReportInterval = req.GlobalReportInterval
	}
	if req.SiteTitle != "" {
		newCfg.SiteTitle = req.SiteTitle
	}
	newCfg.Announcement = req.Announcement
	
	requireLogin := false
	
	if req.AdminUser != "" && req.AdminUser != newCfg.AdminUser {
		newCfg.AdminUser = req.AdminUser
		requireLogin = true
	}
	
	if req.AdminPass != "" {
		hashed, err := bcrypt.GenerateFromPassword([]byte(req.AdminPass), bcrypt.DefaultCost)
		if err == nil {
			newCfg.AdminPass = string(hashed)
			requireLogin = true
		}
	}
	
	if err := config.UpdateAndSave(newCfg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	if requireLogin {
		currentSessionToken = generateSessionToken()
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"require_login": requireLogin})
}

func getPendingNodesHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(manager.GetPendingNodes())
}

func acceptPendingNodeHandler(w http.ResponseWriter, r *http.Request) {
	secret := r.PathValue("secret")
	
	pendingNodes := manager.GetPendingNodes()
	var p *manager.PendingNode
	for _, node := range pendingNodes {
		if node.Secret == secret {
			p = &node
			break
		}
	}
	if p == nil {
		http.Error(w, "Pending node not found", http.StatusNotFound)
		return
	}

	cfg := db.NodeConfig{
		ReportInterval: 0,
		ShowDetails:    false,
		ShowIP:         false,
	}

	if err := db.AddNode(p.Hostname, "", p.Secret, false, cfg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	manager.RemovePendingNode(p.Secret)
	manager.ReloadNodes()
	w.WriteHeader(http.StatusOK)
}

func deletePendingNodeHandler(w http.ResponseWriter, r *http.Request) {
	secret := r.PathValue("secret")
	manager.RemovePendingNode(secret)
	w.WriteHeader(http.StatusOK)
}

func historyHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	var nodeID int
	fmt.Sscanf(idStr, "%d", &nodeID)

	// Auth check
	isAdmin := false
	cookie, err := r.Cookie("admin_session")
	if err == nil && cookie.Value == currentSessionToken && currentSessionToken != "" {
		isAdmin = true
	}

	var hasAccess bool
	if isAdmin {
		hasAccess = true
	} else {
		if config.Current != nil && config.Current.ShowDetails {
			nodes, _ := db.GetAllNodes()
			for _, n := range nodes {
				if n.ID == nodeID {
					if n.Config.ShowDetails && !n.IsAdminOnly {
						hasAccess = true
					}
					break
				}
			}
		}
	}

	if !hasAccess {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	period := r.URL.Query().Get("period")
	var duration time.Duration
	switch period {
	case "30m":
		duration = 30 * time.Minute
	case "1d":
		duration = 24 * time.Hour
	case "3d":
		duration = 3 * 24 * time.Hour
	case "7d":
		duration = 7 * 24 * time.Hour
	default:
		duration = 30 * time.Minute
	}

	since := time.Now().Add(-duration)
	history, err := db.GetHistory(nodeID, since)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Downsample to max 120 points
	downsampled := downsampleHistory(history, 120)

	events, err := db.GetLatestEvents(nodeID, 5)
	if err != nil {
		events = []db.NodeEvent{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"history": downsampled,
		"events":  events,
	})
}

func downsampleHistory(points []db.HistoryPoint, target int) []db.HistoryPoint {
	if len(points) <= target || target <= 0 {
		return points
	}
	bucketSize := float64(len(points)) / float64(target)
	var res []db.HistoryPoint

	for i := 0; i < target; i++ {
		startIdx := int(float64(i) * bucketSize)
		endIdx := int(float64(i+1) * bucketSize)
		if endIdx > len(points) {
			endIdx = len(points)
		}
		if startIdx >= endIdx {
			continue
		}

		var sumCPU float64
		var sumMemUsed, sumMemTotal, sumNetRx, sumNetTx, sumDiskUsed, sumDiskTotal int64
		
		for j := startIdx; j < endIdx; j++ {
			sumCPU += points[j].CPU
			sumMemUsed += points[j].MemUsed
			sumMemTotal += points[j].MemTotal
			sumNetRx += points[j].NetRx
			sumNetTx += points[j].NetTx
			sumDiskUsed += points[j].DiskUsed
			sumDiskTotal += points[j].DiskTotal
		}
		
		count := float64(endIdx - startIdx)
		res = append(res, db.HistoryPoint{
			RecordedAt: points[endIdx-1].RecordedAt,
			CPU:        sumCPU / count,
			MemUsed:    int64(float64(sumMemUsed) / count),
			MemTotal:   int64(float64(sumMemTotal) / count),
			NetRx:      int64(float64(sumNetRx) / count),
			NetTx:      int64(float64(sumNetTx) / count),
			DiskUsed:   int64(float64(sumDiskUsed) / count),
			DiskTotal:  int64(float64(sumDiskTotal) / count),
		})
	}
	return res
}
