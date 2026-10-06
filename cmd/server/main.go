package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"doc-manager/internal/api"
	"doc-manager/internal/config"
	"doc-manager/internal/database"
	"doc-manager/internal/repository"
	"doc-manager/internal/service"
	"doc-manager/internal/storage"
	"doc-manager/pkg/signer"
)

func main() {
	var envPath string
	flag.StringVar(&envPath, "env", ".env", "Path to .env configuration file")
	flag.Parse()

	log.Println("Starting Document Management Service (DMS API)...")

	// 1. Load configuration (loads from -env flag or default .env)
	cfg := config.LoadConfig(envPath)

	// 2. Initialize Database (Pure-Go SQLite with WAL mode)
	db, err := database.InitDB(cfg.DBPath)
	if err != nil {
		log.Fatalf("Fatal: Failed to initialize SQLite database: %v", err)
	}
	defer func() {
		log.Println("Closing database connection...")
		_ = db.Close()
	}()

	// 3. Initialize Local Blob Storage
	localStorage, err := storage.NewLocalStorage(cfg.StorageDir)
	if err != nil {
		log.Fatalf("Fatal: Failed to initialize local storage: %v", err)
	}

	// 4. Repositories
	userRepo := repository.NewUserRepository(db)
	docRepo := repository.NewDocumentRepository(db)
	qrRepo := repository.NewQRRepository(db)

	// 5. Packages & Services
	urlSigner := signer.NewURLSigner(cfg.HMACSecret)
	authService := service.NewAuthService(userRepo, qrRepo, cfg)
	docService := service.NewDocumentService(docRepo, localStorage, urlSigner, cfg)
	workerService := service.NewWorkerService(docRepo, qrRepo, localStorage, cfg)

	// 6. Start Background Maintenance Worker
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	defer cancelWorker()
	go workerService.Start(workerCtx)

	// 7. Setup Router & Server
	router := api.SetupRouter(api.RouterParams{
		AuthService: authService,
		DocService:  docService,
	})

	srv := &http.Server{
		Addr:         ":" + cfg.ServerPort,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Server goroutine
	go func() {
		log.Printf("DMS API server listening on http://localhost:%s", cfg.ServerPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// 8. Graceful Shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	<-quit

	log.Println("Shutdown signal received. Shutting down gracefully...")

	// Cancel background worker
	cancelWorker()

	// Shutdown HTTP server with 10s timeout
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server forced to shutdown: %v", err)
	}

	fmt.Println("Server gracefully stopped.")
}
