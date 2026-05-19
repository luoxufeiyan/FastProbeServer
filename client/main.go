package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	gopsnet "github.com/shirou/gopsutil/v3/net"
)

type Config struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

type ReportPayload struct {
	OS            string  `json:"os"`
	KernelVersion string  `json:"kernel_version"`
	Uptime        uint64  `json:"uptime"`
	CPU           float64 `json:"cpu"`
	MemTotal      uint64  `json:"mem_total"`
	MemUsed       uint64  `json:"mem_used"`
	SwapTotal     uint64  `json:"swap_total"`
	SwapUsed      uint64  `json:"swap_used"`
	DiskTotal     uint64  `json:"disk_total"`
	DiskUsed      uint64  `json:"disk_used"`
	NetRx         uint64  `json:"net_rx"`
	NetTx         uint64  `json:"net_tx"`
	NetTotalRx    uint64  `json:"net_total_rx"`
	NetTotalTx    uint64  `json:"net_total_tx"`
	IP            string  `json:"ip"`
	IPStack       string  `json:"ip_stack"`
}

type ReportResponse struct {
	ReportInterval int `json:"report_interval"`
}

var lastNetTotalRx uint64
var lastNetTotalTx uint64
var lastReportTime time.Time

func main() {
	configPath := flag.String("config", "/etc/fastprobe-client/config.json", "Path to the configuration file")
	flag.Parse()

	config, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if config.URL == "" || config.Token == "" {
		log.Fatalf("URL and Token must be provided in the config file")
	}

	reportInterval := 30 * time.Second

	for {
		payload, err := gatherMetrics()
		if err != nil {
			log.Printf("Error gathering metrics: %v", err)
		} else {
			interval, err := sendReport(config, payload)
			if err != nil {
				log.Printf("Error sending report: %v", err)
			} else if interval > 0 {
				reportInterval = time.Duration(interval) * time.Second
			}
		}

		time.Sleep(reportInterval)
	}
}

func loadConfig(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	config := &Config{}
	err = decoder.Decode(config)
	if err != nil {
		return nil, err
	}

	return config, nil
}

func gatherMetrics() (*ReportPayload, error) {
	payload := &ReportPayload{}

	// Host Info
	hostInfo, err := host.Info()
	if err == nil {
		payload.OS = fmt.Sprintf("%s %s", hostInfo.Platform, hostInfo.PlatformVersion)
		if payload.OS == " " {
			payload.OS = runtime.GOOS
		}
		payload.KernelVersion = hostInfo.KernelVersion
		payload.Uptime = hostInfo.Uptime
	}

	// CPU Info
	cpuPercents, err := cpu.Percent(0, false)
	if err == nil && len(cpuPercents) > 0 {
		payload.CPU = cpuPercents[0]
	}

	// Memory Info
	memInfo, err := mem.VirtualMemory()
	if err == nil {
		payload.MemTotal = memInfo.Total
		payload.MemUsed = memInfo.Used
	}

	swapInfo, err := mem.SwapMemory()
	if err == nil {
		payload.SwapTotal = swapInfo.Total
		payload.SwapUsed = swapInfo.Used
	}

	// Disk Info (Main partition)
	path := "/"
	if runtime.GOOS == "windows" {
		path = "C:"
	}
	diskInfo, err := disk.Usage(path)
	if err == nil {
		payload.DiskTotal = diskInfo.Total
		payload.DiskUsed = diskInfo.Used
	}

	// Network Info
	netIOCounters, err := gopsnet.IOCounters(false)
	if err == nil && len(netIOCounters) > 0 {
		currentTotalRx := netIOCounters[0].BytesRecv
		currentTotalTx := netIOCounters[0].BytesSent
		now := time.Now()

		if !lastReportTime.IsZero() {
			timeDiff := now.Sub(lastReportTime).Seconds()
			if timeDiff > 0 {
				if currentTotalRx >= lastNetTotalRx {
					payload.NetRx = uint64(float64(currentTotalRx-lastNetTotalRx) / timeDiff)
				}
				if currentTotalTx >= lastNetTotalTx {
					payload.NetTx = uint64(float64(currentTotalTx-lastNetTotalTx) / timeDiff)
				}
			}
		}

		payload.NetTotalRx = currentTotalRx
		payload.NetTotalTx = currentTotalTx

		lastNetTotalRx = currentTotalRx
		lastNetTotalTx = currentTotalTx
		lastReportTime = now
	}

	// IP Info
	ip, ipStack := getLocalIPInfo()
	payload.IP = ip
	payload.IPStack = ipStack

	return payload, nil
}

func getLocalIPInfo() (string, string) {
	var mainIP string
	hasIPv4 := false
	hasIPv6 := false

	interfaces, err := net.Interfaces()
	if err != nil {
		return "", "unknown"
	}

	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue // interface down or loopback
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			if ip == nil || ip.IsLoopback() {
				continue
			}

			if ip.To4() != nil {
				hasIPv4 = true
				if mainIP == "" {
					mainIP = ip.String()
				}
			} else {
				hasIPv6 = true
				if mainIP == "" {
					mainIP = ip.String()
				}
			}
		}
	}

	ipStack := "unknown"
	if hasIPv4 && hasIPv6 {
		ipStack = "dual"
	} else if hasIPv4 {
		ipStack = "ipv4"
	} else if hasIPv6 {
		ipStack = "ipv6"
	}

	return mainIP, ipStack
}

func sendReport(config *Config, payload *ReportPayload) (int, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", config.URL, bytes.NewBuffer(data))
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Node-Secret", config.Token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("server returned non-200 status: %d, body: %s", resp.StatusCode, string(body))
	}

	var reportResp ReportResponse
	if err := json.NewDecoder(resp.Body).Decode(&reportResp); err != nil {
		return 0, fmt.Errorf("failed to decode response: %w", err)
	}

	return reportResp.ReportInterval, nil
}
