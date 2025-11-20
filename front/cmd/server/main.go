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
	"github.com/cheolwanpark/meows/front/internal/config"
	"github.com/cheolwanpark/meows/front/internal/handlers"
	"github.com/cheolwanpark/meows/front/internal/middleware"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

func main() {
	// Load configuration from environment variables
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal(fmt.Sprintf("Failed to load config: %v", err))
	}

	// Setup structured logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Initialize collector client
	collectorClient := collector.NewClient(cfg.Frontend.CollectorURL, 10*time.Second)

	// Initialize CSRF middleware
	csrfMiddleware := middleware.NewCSRF()

	// Initialize profile middleware
	profileMiddleware := middleware.NewProfileMiddleware(collectorClient)

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
	r.Use(profileMiddleware.Middleware) // Profile context middleware

	// Static file server with cache headers
	fileServer := http.FileServer(http.Dir("static"))
	r.Handle("/static/*", http.StripPrefix("/static/",
		chimiddleware.SetHeader("Cache-Control", "public, max-age=3600")(fileServer)))

	// Routes
	r.Get("/", h.Home)
	r.Get("/articles/{id}", h.ArticleDetail)
	r.Get("/sources", h.SourcesPage)

	// Profile routes
	r.Get("/profiles/setup", h.ProfileSetup)
	r.Get("/profiles/switcher", h.ProfileSwitcherPartial)
	r.Get("/profile", h.ProfileEditPage)

	// CSRF token endpoint (not protected - used to fetch token)
	r.Get("/api/csrf-token", h.GetCSRFToken)

	// API endpoints (all under /api prefix with CSRF protection)
	r.Route("/api", func(r chi.Router) {
		r.Use(csrfMiddleware.Validate)
		r.Post("/sources", h.CreateSource)
		r.Delete("/sources/{id}", h.DeleteSource)
		r.Post("/sources/{id}/trigger", h.TriggerSource)
		r.Post("/profiles", h.CreateProfile)
		r.Get("/profiles/{id}/status", h.GetProfileStatus)
		r.Post("/profiles/switch/{id}", h.SwitchProfile)
		r.Patch("/profile", h.UpdateProfileHandler)
		r.Get("/profile/status", h.GetProfileStatusAPI)
		r.Post("/articles/{id}/like", h.LikeArticle)
		r.Delete("/likes/{id}", h.UnlikeArticle)
	})

	// HTTP server
	server := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.Frontend.Server.Port),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		slog.Info("Starting server", "port", cfg.Frontend.Server.Port, "collector_url", cfg.Frontend.CollectorURL)
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
