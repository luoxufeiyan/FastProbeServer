package main

import (
	"log"
	"net"
	"net/http/fcgi"
	"os"

	"FastProbeServer/config"
	"FastProbeServer/db"
	"FastProbeServer/handler"
	"FastProbeServer/manager"
)

func main() {
	// 1. Load config
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 2. Initialize DB if setup is already complete
	if cfg.IsSetup {
		if err := db.InitDB(); err != nil {
			log.Printf("Failed to initialize database: %v. You might need to check your credentials.", err)
			// Continue to allow accessing the UI to see potential setup issues or re-setup
		} else {
			// Start background processes
			manager.ReloadNodes()
			manager.StartBackgroundTasks()
		}
	}

	// 3. Register HTTP handlers
	mux := handler.RegisterRoutes()

	// 4. Start FastCGI server
	// In standard Web Hosting environments (like Netcup/Apache), FastCGI is spawned by the web server
	// and communicates via stdin/stdout, rather than a TCP port.
	// If FCGI_ADDR is not set, we default to standard stdin FastCGI serving.
	if envAddr := os.Getenv("FCGI_ADDR"); envAddr != "" {
		listener, err := net.Listen("tcp", envAddr)
		if err != nil {
			log.Fatalf("Failed to listen on %s: %v", envAddr, err)
		}
		log.Printf("Starting FastCGI server on %s", envAddr)
		if err := fcgi.Serve(listener, mux); err != nil {
			log.Fatalf("FastCGI serve error: %v", err)
		}
	} else {
		log.Printf("Starting FastCGI server on stdin (Web Hosting Mode)")
		if err := fcgi.Serve(nil, mux); err != nil {
			log.Fatalf("FastCGI serve error: %v", err)
		}
	}
}
