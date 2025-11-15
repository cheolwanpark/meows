package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/cheolwanpark/meows/front/internal/collector"
	"github.com/cheolwanpark/meows/front/internal/middleware"
	"github.com/cheolwanpark/meows/front/internal/models"
	"github.com/cheolwanpark/meows/front/templates/components"
	"github.com/cheolwanpark/meows/front/templates/layouts"
	"github.com/cheolwanpark/meows/front/templates/pages"
	"github.com/go-chi/chi/v5"
	"github.com/robfig/cron/v3"
)

// Handler holds dependencies for HTTP handlers
type Handler struct {
	collector *collector.Client
	csrf      *middleware.CSRF
}

// NewHandler creates a new Handler
func NewHandler(collectorClient *collector.Client, csrf *middleware.CSRF) *Handler {
	return &Handler{
		collector: collectorClient,
		csrf:      csrf,
	}
}

// Home renders the home page with articles
func (h *Handler) Home(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Get CSRF token
	csrfToken := h.csrf.GetToken(r)
	h.csrf.SetToken(w, csrfToken)

	// Fetch articles from collector
	collectorArticles, err := h.collector.GetArticles(ctx, 50, 0)
	if err != nil {
		slog.Error("Failed to fetch articles", "error", err)
		// Render error page with proper HTTP status
		w.WriteHeader(http.StatusServiceUnavailable)
		component := layouts.Base("Error", csrfToken, components.ErrorPage(
			"Service Unavailable",
			"Unable to fetch articles. The collector service may be down. Please try again later.",
		))
		component.Render(r.Context(), w)
		return
	}

	// Convert to view models (we need to get source types)
	// For now, assume all articles are from Reddit sources
	// TODO: Add source type mapping from articles
	articles := make([]models.Article, 0, len(collectorArticles))
	for _, a := range collectorArticles {
		// Determine source type (this is a simplification)
		sourceType := "reddit" // Default to reddit for now
		articles = append(articles, models.FromCollectorArticle(a, sourceType))
	}

	// Render page
	component := pages.HomePage(articles, csrfToken)
	component.Render(r.Context(), w)
}

// ConfigPage renders the source management page
func (h *Handler) ConfigPage(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Get CSRF token
	csrfToken := h.csrf.GetToken(r)
	h.csrf.SetToken(w, csrfToken)

	// Fetch sources from collector
	collectorSources, err := h.collector.GetSources(ctx)
	if err != nil {
		slog.Error("Failed to fetch sources", "error", err)
		// Render error page with proper HTTP status
		w.WriteHeader(http.StatusServiceUnavailable)
		component := layouts.Base("Error", csrfToken, components.ErrorPage(
			"Service Unavailable",
			"Unable to fetch sources. The collector service may be down. Please try again later.",
		))
		component.Render(r.Context(), w)
		return
	}

	// Convert to view models
	sources := make([]models.Source, 0, len(collectorSources))
	for _, s := range collectorSources {
		sources = append(sources, models.FromCollectorSource(s))
	}

	// Render page
	component := pages.ConfigPage(sources, csrfToken, models.FormErrors{})
	component.Render(r.Context(), w)
}

// CreateSource handles source creation (htmx endpoint)
func (h *Handler) CreateSource(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Parse form
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	subreddit := r.FormValue("subreddit")
	cronExpr := r.FormValue("cron")

	// Validate inputs
	errors := models.FormErrors{}

	if subreddit == "" {
		errors.Name = "Subreddit name is required"
	}

	if cronExpr == "" {
		errors.Cron = "Cron expression is required"
	} else {
		// Validate cron expression
		if _, err := cron.ParseStandard(cronExpr); err != nil {
			errors.Cron = "Invalid cron expression format"
		}
	}

	// If there are errors, return form with errors
	if errors.HasErrors() {
		w.WriteHeader(http.StatusUnprocessableEntity)
		csrfToken := h.csrf.GetToken(r)
		component := components.AddSourceForm(csrfToken, errors, map[string]string{
			"subreddit": subreddit,
			"cron":      cronExpr,
		})
		component.Render(r.Context(), w)
		return
	}

	// Build Reddit config
	config := map[string]interface{}{
		"subreddit":           subreddit,
		"sort":                "hot",
		"limit":               25,
		"min_score":           10,
		"min_comments":        5,
		"user_agent":          "meows/1.0",
		"rate_limit_delay_ms": 2000,
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		errors.General = "Failed to create source configuration"
		w.WriteHeader(http.StatusInternalServerError)
		csrfToken := h.csrf.GetToken(r)
		component := components.AddSourceForm(csrfToken, errors, map[string]string{
			"subreddit": subreddit,
			"cron":      cronExpr,
		})
		component.Render(r.Context(), w)
		return
	}

	// Create source via collector
	createReq := collector.CreateSourceRequest{
		Type:     "reddit",
		Config:   configJSON,
		CronExpr: cronExpr,
	}

	source, err := h.collector.CreateSource(ctx, createReq)
	if err != nil {
		slog.Error("Failed to create source", "error", err)
		errors.General = "Failed to create source: " + err.Error()
		w.WriteHeader(http.StatusInternalServerError)
		csrfToken := h.csrf.GetToken(r)
		component := components.AddSourceForm(csrfToken, errors, map[string]string{
			"subreddit": subreddit,
			"cron":      cronExpr,
		})
		component.Render(r.Context(), w)
		return
	}

	// Success: return source card for htmx to insert
	viewSource := models.FromCollectorSource(*source)
	csrfToken := h.csrf.GetToken(r)
	component := components.SourceCard(viewSource, csrfToken)
	component.Render(r.Context(), w)
}

// DeleteSource handles source deletion (htmx endpoint)
func (h *Handler) DeleteSource(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "Missing source ID", http.StatusBadRequest)
		return
	}

	// Delete source via collector
	if err := h.collector.DeleteSource(ctx, id); err != nil {
		slog.Error("Failed to delete source", "id", id, "error", err)
		http.Error(w, "Failed to delete source", http.StatusInternalServerError)
		return
	}

	// Success: return 204 No Content (htmx will remove the element)
	w.WriteHeader(http.StatusNoContent)
}
