package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cheolwanpark/meows/front/internal/collector"
	"github.com/cheolwanpark/meows/front/internal/handlers"
	"github.com/cheolwanpark/meows/front/internal/middleware"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

func main() {
	// Load configuration from environment
	cfg := loadConfig()

	// Setup structured logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Initialize collector client
	collectorClient := collector.NewClient(cfg.CollectorURL, 10*time.Second)

	// Initialize CSRF middleware
	csrfMiddleware := middleware.NewCSRF(cfg.CSRFKey)

	// Initialize handlers
	h := handlers.NewHandler(collectorClient, csrfMiddleware)

	// Setup router
	r := chi.NewRouter()

	// Middleware stack
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.Timeout(30 * time.Second))

	// Static file server with cache headers
	fileServer := http.FileServer(http.Dir("static"))
	r.Handle("/static/*", http.StripPrefix("/static/",
		chimiddleware.SetHeader("Cache-Control", "public, max-age=3600")(fileServer)))

	// Routes
	r.Get("/", h.Home)
	r.Get("/config", h.ConfigPage)

	// HTMX endpoints (with CSRF protection)
	r.Group(func(r chi.Router) {
		r.Use(csrfMiddleware.Validate)
		r.Post("/config/sources", h.CreateSource)
		r.Delete("/config/sources/{id}", h.DeleteSource)
	})

	// HTTP server
	server := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.Port),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		slog.Info("Starting server", "port", cfg.Port, "collector_url", cfg.CollectorURL)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(fmt.Sprintf("Server failed to start: %v", err))
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
	}

	slog.Info("Server stopped")
}

// Config holds application configuration
type Config struct {
	Port         string
	CollectorURL string
	CSRFKey      string
	Environment  string
}

// loadConfig loads configuration from environment variables
func loadConfig() Config {
	return Config{
		Port:         getEnv("PORT", "3000"),
		CollectorURL: getEnv("COLLECTOR_URL", "http://localhost:8080"),
		CSRFKey:      getEnv("CSRF_KEY", "change-me-in-production-please-use-random-key"),
		Environment:  getEnv("ENV", "development"),
	}
}

// getEnv retrieves an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
