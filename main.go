package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"smartsyncserver/auth"
	"smartsyncserver/config"
	"smartsyncserver/git"
	"smartsyncserver/handlers"
	"smartsyncserver/ignore"
	"smartsyncserver/storage"
)

func main() {
	// Load configuration
	cfg, err := config.Load("/data/config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Create vault directory if it doesn't exist
	if err := os.MkdirAll(cfg.Vault.Path, 0755); err != nil {
		log.Fatalf("Failed to create vault directory: %v", err)
	}

	// Initialize ignore matcher
	ignorePatterns := cfg.GetDefaultIgnorePatterns()
	ignoreMatcher := ignore.New(ignorePatterns)

	// Initialize checksums manager
	// execDir, _ := os.Executable()
	checksumsPath := "/data/checksums.json" // filepath.Join(filepath.Dir(execDir), "checksums.json")
	checksums := storage.NewChecksums(checksumsPath, cfg.Vault.Path, ignoreMatcher)

	wd, _ := os.Getwd()
	log.Printf("Working directory: %s", wd)
	log.Printf("Checksums path: %s", checksumsPath)

	// Scan on startup if enabled
	if cfg.Scan.OnStartup {
		log.Println("Scanning vault for checksums...")
		if err := checksums.Scan(); err != nil {
			log.Printf("Warning: Failed to scan vault: %v", err)
		} else {
			log.Printf("Scanned %d files", checksums.GetFileCount())
		}
	}

	// Initialize vault
	vault := storage.NewVault(cfg.Vault.Path, checksums, ignoreMatcher)

	// Initialize git manager
	var gitMgr *git.Manager
	if cfg.IsGitEnabled() {
		gitMgr = git.NewManager(
			cfg.Git.RepositoryURL,
			cfg.Git.AccessToken,
			cfg.Vault.Path,
			cfg.Git.CommitMessage,
			cfg.Git.AutocommitMinutes,
		)

		// Initialize git repository
		if err := gitMgr.Init(); err != nil {
			log.Printf("Warning: Failed to initialize git repository: %v", err)
		} else {
			log.Println("Git repository initialized")

			// Ensure .gitignore exists
			gitMgr.EnsureIgnore(cfg.Vault.Path)

			// Start auto-commit routine
			if cfg.Git.AutocommitMinutes > 0 {
				gitMgr.StartAutoCommit()
				log.Printf("Git auto-commit started (interval: %d minutes)", cfg.Git.AutocommitMinutes)
			}
		}
	}

	// Initialize auth middleware
	authMiddleware := auth.NewMiddleware(cfg.Auth.Type, cfg.Auth.Token)
	if authMiddleware.IsEnabled() {
		log.Println("Authentication enabled")
	} else {
		log.Println("Authentication disabled (dev mode)")
	}

	// Initialize handlers
	scanConfig := handlers.ScanConfig{
		API: cfg.Scan.API,
	}
	server := handlers.New(vault, checksums, gitMgr, authMiddleware, scanConfig)

	// Setup HTTP server
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	// Add CORS support (optional, useful for development)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Set CORS headers
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Call the actual handler
		server.RegisterRoutes(http.NewServeMux())
	})

	// Start periodic scanner if enabled
	if cfg.Scan.PeriodicInterval > 0 {
		go startPeriodicScan(checksums, cfg.Scan.PeriodicInterval, gitMgr)
		// go startPeriodicCommit(checksums, cfg.Git.AutocommitMinutes, gitMgr)
		log.Printf("Periodic scan started (interval: %d seconds)", cfg.Scan.PeriodicInterval)
	}

	// Start server
	addr := fmt.Sprintf("%s:%d", cfg.Server.Bind, cfg.Server.Port)
	log.Printf("Starting server on %s", addr)

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigchan := make(chan os.Signal, 1)
		signal.Notify(sigchan, os.Interrupt, syscall.SIGTERM)
		<-sigchan

		log.Println("Shutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if gitMgr != nil {
			gitMgr.Stop()
			// Try to commit before shutdown
			//bad idea!!!
			// _ = gitMgr.Commit()
		}

		_ = srv.Shutdown(ctx)
		os.Exit(0)
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}

// startPeriodicScan runs periodic vault scans
func startPeriodicScan(checksums *storage.Checksums, intervalSeconds int, gitMgr *git.Manager) {
	ticker := time.NewTicker(time.Duration(intervalSeconds) * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		log.Println("Running periodic scan...")
		if err := checksums.Scan(); err != nil {
			log.Printf("Periodic scan error: %v", err)
		} else {
			log.Printf("Periodic scan complete: %d files", checksums.GetFileCount())
		}

	}
}

// // startPeriodicCommit runs periodic vault commits
// func startPeriodicCommit(checksums *storage.Checksums, intervalMinutes int, gitMgr *git.Manager) {
// 	ticker := time.NewTicker(time.Duration(intervalMinutes) * time.Minute)
// 	defer ticker.Stop()

// 	for range ticker.C {

// 		// Commit changes to git if enabled
// 		// we only want commits on defined period
// 		if gitMgr != nil {
// 			if err := gitMgr.Commit(); err.Success != true {
// 				log.Printf("Git commit error: %v", err.Message)
// 			}
// 		}
// 	}
// }
