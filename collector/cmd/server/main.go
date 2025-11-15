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

	"github.com/cheolwanpark/meows/collector/internal/api"
	"github.com/cheolwanpark/meows/collector/internal/config"
	"github.com/cheolwanpark/meows/collector/internal/db"
	"github.com/cheolwanpark/meows/collector/internal/scheduler"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	log.Printf("Starting collector service...")
	log.Printf("Configuration: DB=%s, Port=%d, MaxCommentDepth=%d, LogLevel=%s",
		cfg.DBPath, cfg.Port, cfg.MaxCommentDepth, cfg.LogLevel)

	// Initialize database
	database, err := db.Init(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()
	log.Println("Database initialized")

	// Initialize scheduler
	sched := scheduler.New(database, cfg.MaxCommentDepth)

	// Load sources and register jobs
	if err := sched.LoadSourcesFromDB(); err != nil {
		log.Fatalf("Failed to load sources: %v", err)
	}

	// Start scheduler
	sched.Start()
	log.Println("Scheduler started")

	// Setup HTTP router
	router := api.SetupRouter(database, sched)

	// Create HTTP server
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Channel to listen for errors coming from the listener
	serverErrors := make(chan error, 1)

	// Start HTTP server in a goroutine
	go func() {
		log.Printf("HTTP server listening on %s", server.Addr)
		serverErrors <- server.ListenAndServe()
	}()

	// Channel to listen for interrupt signals
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	// Block until we receive a signal or an error
	select {
	case err := <-serverErrors:
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}

	case sig := <-shutdown:
		log.Printf("Received signal %v, starting graceful shutdown...", sig)

		// Create context with timeout for shutdown
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Shutdown HTTP server
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
			server.Close()
		}

		// Stop scheduler (wait for running jobs)
		schedulerCtx, schedulerCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer schedulerCancel()

		if err := sched.Stop(schedulerCtx); err != nil {
			log.Printf("Scheduler shutdown error: %v", err)
		}

		log.Println("Graceful shutdown complete")
	}
}
