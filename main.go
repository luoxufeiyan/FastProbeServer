package main

import (
	"log"
	"net"
	"net/http/fcgi"
	"os"

	"vps-probe/config"
	"vps-probe/db"
	"vps-probe/handler"
	"vps-probe/manager"
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
	listenAddr := "127.0.0.1:9000"
	if envAddr := os.Getenv("FCGI_ADDR"); envAddr != "" {
		listenAddr = envAddr
	}

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", listenAddr, err)
	}

	log.Printf("Starting FastCGI server on %s", listenAddr)
	if err := fcgi.Serve(listener, mux); err != nil {
		log.Fatalf("FastCGI serve error: %v", err)
	}
}
