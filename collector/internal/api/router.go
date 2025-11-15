package api

import (
	"net/http"
	"os"

	"github.com/cheolwanpark/meows/collector/internal/db"
	"github.com/cheolwanpark/meows/collector/internal/scheduler"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	httpSwagger "github.com/swaggo/http-swagger"
)

// SetupRouter creates and configures the HTTP router
func SetupRouter(database *db.DB, sched *scheduler.Scheduler) http.Handler {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Recoverer)
	r.Use(Logger)
	r.Use(ContentType)

	// Create handler
	h := NewHandler(database, sched)

	// Routes
	r.Route("/sources", func(r chi.Router) {
		r.Post("/", h.CreateSource)
		r.Get("/", h.ListSources)
		r.Get("/{id}", h.GetSource)
		r.Put("/{id}", h.UpdateSource)
		r.Delete("/{id}", h.DeleteSource)
		r.Delete("/{type}/{external_id}", h.DeleteSourceByTypeAndExternalID)
	})

	r.Get("/schedule", h.GetSchedule)
	r.Get("/articles", h.ListArticles)
	r.Get("/health", h.Health)
	r.Get("/metrics", h.Metrics)

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
