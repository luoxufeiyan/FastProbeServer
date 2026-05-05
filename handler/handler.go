package handler

import (
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"

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

	// --- Report Route (From Nodes) ---
	mux.HandleFunc("POST /report", nodeAuthMiddleware(reportHandler))

	// --- Setup & Admin Routes ---
	mux.HandleFunc("GET /api/check-setup", checkSetupHandler)
	mux.HandleFunc("POST /api/setup", setupHandler)
	mux.HandleFunc("POST /api/login", loginHandler)
	
	// Admin authenticated routes
	mux.HandleFunc("GET /api/admin/nodes", adminAuthMiddleware(getNodesHandler))
	mux.HandleFunc("POST /api/admin/nodes", adminAuthMiddleware(addNodeHandler))
	mux.HandleFunc("DELETE /api/admin/nodes/{id}", adminAuthMiddleware(deleteNodeHandler))

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
		json.NewEncoder(w).Encode(map[string]bool{"is_setup": false})
		return
	}
	json.NewEncoder(w).Encode(map[string]bool{"is_setup": config.Current.IsSetup})
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
	CPU       float64 `json:"cpu"`
	MemUsed   int64   `json:"mem_used"`
	MemTotal  int64   `json:"mem_total"`
	NetRx     int64   `json:"net_rx"`
	NetTx     int64   `json:"net_tx"`
	DiskUsed  int64   `json:"disk_used"`
	DiskTotal int64   `json:"disk_total"`
	Uptime    int64   `json:"uptime"`
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

	manager.UpdateReport(nodeID, p.CPU, p.MemUsed, p.MemTotal, p.NetRx, p.NetTx, p.DiskUsed, p.DiskTotal, p.Uptime)

	w.WriteHeader(http.StatusOK)
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	statuses := manager.GetAllStatuses()
	
	// Hide secret from public status endpoint
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
	
	if err := db.AddNode(n.Name, n.Location, n.Secret, n.IsAdminOnly); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	// Refresh memory
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
