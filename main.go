package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"nodax-central/internal/api"
	"nodax-central/internal/poller"
	"nodax-central/internal/store"
	"os"
	"strings"
	"time"
)

//go:embed frontend/dist/*
var frontendFS embed.FS

func runServer(stop <-chan struct{}) error {
	// Initialize storage
	db, err := store.New()
	if err != nil {
		return fmt.Errorf("failed to init storage: %w", err)
	}
	defer db.Close()

	cfg, _ := db.GetConfig()
	port := strings.TrimSpace(os.Getenv("NODAX_CENTRAL_PORT"))
	if port == "" {
		port = strings.TrimSpace(cfg.Port)
	}
	if port == "" {
		port = "8080"
	}

	// Initialize poller (poll every 15 seconds)
	p := poller.New(db, 15*time.Second)
	p.Start()
	defer p.Stop()

	// JWT secret: load from config or generate
	if cfg.JWTSecret == "" {
		cfg.JWTSecret = api.GenerateRandomSecret()
		db.SaveConfig(cfg)
	}
	api.SetJWTSecret(cfg.JWTSecret)

	// Setup HTTP routes
	mux := http.NewServeMux()

	// API routes
	handler := api.NewHandler(db, p)
	licenseStop := make(chan struct{})
	defer close(licenseStop)
	handler.StartLicenseLoop(licenseStop)
	handler.RegisterAuthRoutes(mux)
	handler.RegisterRoutes(mux)

	// Serve embedded frontend
	distFS, err := fs.Sub(frontendFS, "frontend/dist")
	if err != nil {
		return fmt.Errorf("failed to get frontend fs: %w", err)
	}
	fileServer := http.FileServer(http.FS(distFS))

	// SPA fallback: serve index.html for any non-API, non-file route
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the file first
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}
		// Check if file exists in the embedded FS
		f, err := distFS.Open(path[1:]) // strip leading /
		if err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		// Fallback to index.html for SPA routing
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})

	// CORS + Auth middleware
	corsHandler := corsMiddleware(handler.AuthMiddleware(mux))

	fmt.Printf("=== NODAX Central Server ===\n")
	fmt.Printf("Dashboard: http://localhost:%s\n", port)
	fmt.Printf("API:       http://localhost:%s/api/\n", port)
	fmt.Printf("Polling agents every 15s\n")
	fmt.Println("Press Ctrl+C to stop")

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: corsHandler,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	if stop == nil {
		err = <-errCh
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}

	select {
	case err = <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-stop:
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		return nil
	}
}

func main() {
	isSvc, err := isWindowsService()
	if err != nil {
		log.Fatalf("Failed to detect Windows service mode: %v", err)
	}
	if isSvc {
		if err := runWindowsService("NODAXCentral", runServer); err != nil {
			log.Fatalf("Windows service failed: %v", err)
		}
		return
	}

	if err := runServer(nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		if r.Method == "OPTIONS" {
			w.WriteHeader(200)
			return
		}
		next.ServeHTTP(w, r)
	})
}
