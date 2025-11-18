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

	_ "github.com/cheolwanpark/meows/collector/docs" // Swagger docs
	"github.com/cheolwanpark/meows/collector/internal/api"
	"github.com/cheolwanpark/meows/collector/internal/config"
	"github.com/cheolwanpark/meows/collector/internal/db"
	"github.com/cheolwanpark/meows/collector/internal/gemini"
	"github.com/cheolwanpark/meows/collector/internal/profile"
	"github.com/cheolwanpark/meows/collector/internal/scheduler"
)

// @title Meows Collector API
// @version 1.0
// @description A Go-based web service for scheduled crawling and collecting articles from Reddit and Semantic Scholar
// @description
// @description Features:
// @description - Multi-source support (Reddit, Semantic Scholar)
// @description - Scheduled crawling with cron expressions
// @description - REST API for source and article management
// @description - Health monitoring and metrics
// @description
// @description **Security Notice:** This API currently has no authentication. Not recommended for production use without adding authentication.
// @contact.name Meows Project
// @contact.email support@example.com
// @license.name MIT
// @host localhost:8080
// @BasePath /
// @schemes http
func main() {
	// Load configuration from environment variables
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	log.Printf("Starting collector service...")
	log.Printf("Configuration loaded from environment variables")
	log.Printf("Server: DB=%s, Port=%d, MaxCommentDepth=%d, LogLevel=%s",
		cfg.Collector.Server.DBPath, cfg.Collector.Server.Port,
		cfg.Collector.Server.MaxCommentDepth, cfg.Collector.Server.LogLevel)
	log.Printf("Schedule: %s", cfg.Collector.Schedule.CronExpr)
	log.Printf("Rate limits: Reddit=%dms, S2=%dms",
		cfg.Collector.RateLimits.RedditDelayMs,
		cfg.Collector.RateLimits.SemanticScholarDelayMs)

	// Initialize database
	database, err := db.Init(cfg.Collector.Server.DBPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()
	log.Println("Database initialized")

	// Initialize Gemini client
	ctx := context.Background()
	geminiAPIKey := cfg.Collector.Gemini.APIKey
	if geminiAPIKey == "" {
		log.Println("Warning: GEMINI_API_KEY not set, profile character generation will fail")
	}

	geminiClient, err := gemini.NewClient(ctx, geminiAPIKey)
	if err != nil {
		log.Fatalf("Failed to initialize Gemini client: %v", err)
	}
	defer geminiClient.Close()
	log.Println("Gemini client initialized")

	// Initialize profile service with milestone thresholds from config
	profileService := profile.NewUpdateService(
		database,
		geminiClient,
		cfg.Collector.Profile.MilestoneThreshold1,
		cfg.Collector.Profile.MilestoneThreshold2,
		cfg.Collector.Profile.MilestoneThreshold3,
	)
	log.Println("Profile service initialized")

	// Initialize scheduler with global configuration from environment variables
	sched, err := scheduler.New(&cfg.Collector, database, profileService)
	if err != nil {
		log.Fatalf("Failed to initialize scheduler: %v", err)
	}

	// Start scheduler (loads sources from DB, uses environment variables for schedule/credentials)
	sched.Start()
	log.Println("Scheduler started")

	// Setup HTTP router
	router := api.SetupRouter(database, sched, profileService)

	// Create HTTP server
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Collector.Server.Port),
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
