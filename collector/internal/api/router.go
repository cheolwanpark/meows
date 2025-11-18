package api

import (
	"net/http"
	"os"

	"github.com/cheolwanpark/meows/collector/internal/db"
	"github.com/cheolwanpark/meows/collector/internal/profile"
	"github.com/cheolwanpark/meows/collector/internal/scheduler"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	httpSwagger "github.com/swaggo/http-swagger"
)

// SetupRouter creates and configures the HTTP router
func SetupRouter(database *db.DB, sched *scheduler.Scheduler, profService *profile.UpdateService) http.Handler {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Recoverer)
	r.Use(Logger)
	r.Use(ContentType)
	r.Use(ProfileContext(database))

	// Create handler
	h := NewHandler(database, sched, profService)

	// Routes
	r.Route("/sources", func(r chi.Router) {
		r.Post("/", h.CreateSource)
		r.Get("/", h.ListSources)
		r.Get("/{id}", h.GetSource)
		r.Put("/{id}", h.UpdateSource)
		r.Delete("/{id}", h.DeleteSource)
		r.Delete("/{type}/{external_id}", h.DeleteSourceByTypeAndExternalID)
	})

	r.Route("/profiles", func(r chi.Router) {
		r.Post("/", h.CreateProfile)
		r.Get("/", h.ListProfiles)
		r.Get("/{id}/status", h.GetProfileStatus) // Must come before /{id}
		r.Get("/{id}", h.GetProfile)
		r.Patch("/{id}", h.UpdateProfile)
		r.Delete("/{id}", h.DeleteProfile)
	})

	r.Route("/articles", func(r chi.Router) {
		r.Get("/", h.ListArticles)
		r.Get("/{id}", h.GetArticle)
		r.Post("/{id}/like", h.LikeArticle)
	})

	r.Delete("/likes/{id}", h.UnlikeArticle)

	r.Get("/schedule", h.GetSchedule)
	r.Get("/health", h.Health)
	r.Get("/metrics", h.Metrics)
	// Note: Global config endpoints removed - config is now file-based (.config.yaml)

	// Swagger UI (environment-gated for development only)
	// Access at http://localhost:8080/docs when ENABLE_SWAGGER=true
	if os.Getenv("ENABLE_SWAGGER") == "true" {
		r.Get("/docs/*", httpSwagger.Handler(
			httpSwagger.URL("doc.json"), // Use the embedded swagger doc
		))
	}

	// 404 handler
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not found"}`))
	})

	return r
}
