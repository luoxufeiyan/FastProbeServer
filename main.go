package main

import (
	"log"
	"net"
	"net/http/fcgi"
	"os"
	"syscall"

	"vps-probe/config"
	"vps-probe/db"
	"vps-probe/handler"
	"vps-probe/manager"
)

// isStdinSocket checks if stdin is a socket (set by Apache mod_fcgid)
func isStdinSocket() bool {
	var stat syscall.Stat_t
	if err := syscall.Fstat(0, &stat); err != nil {
		return false
	}
	return stat.Mode&syscall.S_IFMT == syscall.S_IFSOCK
}

func main() {
	// Setup file logging so we can see output from mod_fcgid
	logFile, err := os.OpenFile("app.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		log.SetOutput(logFile)
		defer logFile.Close()
	}

	log.Println("--- Process started ---")
	log.Printf("FCGI_ROLE=%s", os.Getenv("FCGI_ROLE"))
	log.Printf("isStdinSocket=%v", isStdinSocket())

	// 1. Load config
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	log.Printf("Config loaded: IsSetup=%v", cfg.IsSetup)

	// 2. Initialize DB if setup is already complete
	if cfg.IsSetup {
		if err := db.InitDB(); err != nil {
			log.Printf("Failed to initialize database: %v. You might need to check your credentials.", err)
		} else {
			manager.ReloadNodes()
			manager.StartBackgroundTasks()
		}
	}

	// 3. Register HTTP handlers
	mux := handler.RegisterRoutes()
	log.Println("Routes registered")

	// 4. Start FastCGI server
	// Detect mod_fcgid: check if stdin is a socket (mod_fcgid passes the socket as stdin)
	// or check FCGI_ROLE env var. Otherwise, listen on TCP for standalone testing.
	if isStdinSocket() || os.Getenv("FCGI_ROLE") != "" {
		log.Println("Starting FastCGI server on stdin (mod_fcgid mode)")
		if err := fcgi.Serve(nil, mux); err != nil {
			log.Fatalf("FastCGI serve error: %v", err)
		}
	} else {
		listenAddr := "127.0.0.1:9000"
		if envAddr := os.Getenv("FCGI_ADDR"); envAddr != "" {
			listenAddr = envAddr
		}

		listener, err := net.Listen("tcp", listenAddr)
		if err != nil {
			log.Fatalf("Failed to listen on %s: %v", listenAddr, err)
		}

		log.Printf("Starting FastCGI server on %s (standalone mode)", listenAddr)
		if err := fcgi.Serve(listener, mux); err != nil {
			log.Fatalf("FastCGI serve error: %v", err)
		}
	}
}
