package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

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

// respondWithError handles errors by logging technical details and sending user-friendly messages
// For htmx requests, it sends an HX-Trigger header to show a toast notification
func respondWithError(w http.ResponseWriter, userMsg string, logMsg string, err error, status int) {
	// Log technical error with context
	if err != nil {
		slog.Error(logMsg, "error", err, "status", status)
	} else {
		slog.Warn(logMsg, "status", status)
	}

	// Set HX-Trigger header for toast notification (properly encoded to prevent injection)
	trigger := map[string]string{"show-error": userMsg}
	triggerJSON, _ := json.Marshal(trigger)
	w.Header().Set("HX-Trigger", string(triggerJSON))

	// Write HTTP status and generic error response
	http.Error(w, userMsg, status)
}

// Home renders the home page with articles
func (h *Handler) Home(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), DefaultRequestTimeout)
	defer cancel()

	// Get CSRF token
	csrfToken := h.csrf.GetToken(r)
	h.csrf.SetToken(w, r, csrfToken)

	// Fetch articles from collector
	collectorArticles, err := h.collector.GetArticles(ctx, DefaultArticleLimit, DefaultArticleOffset)
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

	// Fetch sources to build source type lookup map (avoids N+1 queries)
	collectorSources, err := h.collector.GetSources(ctx)
	if err != nil {
		slog.Error("Failed to fetch sources", "error", err)
		// Continue with empty map if sources fetch fails
		collectorSources = []collector.Source{}
	}

	// Build sourceID -> sourceType map for O(1) lookups
	sourceTypeMap := make(map[string]string)
	for _, s := range collectorSources {
		sourceTypeMap[s.ID] = s.Type
	}

	// Convert to view models using source type map
	articles := make([]models.Article, 0, len(collectorArticles))
	for _, a := range collectorArticles {
		// Look up source type from map, default to "reddit" if not found
		sourceType := sourceTypeMap[a.SourceID]
		if sourceType == "" {
			sourceType = "reddit" // Fallback for legacy/orphaned articles
		}
		articles = append(articles, models.FromCollectorArticle(a, sourceType))
	}

	// Render page
	component := pages.HomePage(articles, csrfToken)
	component.Render(r.Context(), w)
}

// ConfigPage renders the source management page
func (h *Handler) ConfigPage(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), DefaultRequestTimeout)
	defer cancel()

	// Get CSRF token
	csrfToken := h.csrf.GetToken(r)
	h.csrf.SetToken(w, r, csrfToken)

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
	ctx, cancel := context.WithTimeout(r.Context(), DefaultRequestTimeout)
	defer cancel()

	// Parse form
	if err := r.ParseForm(); err != nil {
		respondWithError(w, "Invalid form data. Please check your input and try again.", "Failed to parse form", err, http.StatusBadRequest)
		return
	}

	sourceType := r.FormValue("source_type")
	cronExpr := r.FormValue("cron")

	// Validate inputs
	errors := models.FormErrors{}

	// Validate source type
	if sourceType == "" {
		errors.General = "Source type is required"
	} else if sourceType != "reddit" && sourceType != "semantic_scholar" {
		errors.General = "Invalid source type. Must be 'reddit' or 'semantic_scholar'"
	}

	// Validate cron expression
	if cronExpr == "" {
		errors.Cron = "Cron expression is required"
	} else {
		if _, err := cron.ParseStandard(cronExpr); err != nil {
			errors.Cron = "Invalid cron expression format"
		}
	}

	// Build config based on source type
	var config map[string]interface{}
	var configErr error

	if sourceType == "reddit" {
		config, configErr = buildRedditConfig(r)
		if configErr != nil {
			errors.General = configErr.Error()
		}
	} else if sourceType == "semantic_scholar" {
		config, configErr = buildSemanticScholarConfig(r)
		if configErr != nil {
			errors.General = configErr.Error()
		}
	}

	// If there are errors, return form with errors
	if errors.HasErrors() {
		w.WriteHeader(http.StatusUnprocessableEntity)
		csrfToken := h.csrf.GetToken(r)
		// Preserve form values for user convenience
		formValues := make(map[string]string)
		for key, values := range r.Form {
			if len(values) > 0 {
				formValues[key] = values[0]
			}
		}
		component := components.AddSourceForm(csrfToken, errors, formValues)
		component.Render(r.Context(), w)
		return
	}

	// Marshal config to JSON
	configJSON, err := json.Marshal(config)
	if err != nil {
		slog.Error("Failed to marshal config", "error", err)
		errors.General = "Failed to create source configuration"
		w.WriteHeader(http.StatusInternalServerError)
		csrfToken := h.csrf.GetToken(r)
		component := components.AddSourceForm(csrfToken, errors, map[string]string{
			"source_type": sourceType,
			"cron":        cronExpr,
		})
		component.Render(r.Context(), w)
		return
	}

	// Create source via collector
	createReq := collector.CreateSourceRequest{
		Type:     sourceType,
		Config:   configJSON,
		CronExpr: cronExpr,
	}

	source, err := h.collector.CreateSource(ctx, createReq)
	if err != nil {
		slog.Error("Failed to create source", "error", err, "type", sourceType)
		// Provide user-friendly error message instead of exposing internal errors
		errors.General = "Failed to create source. Please check your configuration and try again."
		w.WriteHeader(http.StatusInternalServerError)
		csrfToken := h.csrf.GetToken(r)
		formValues := make(map[string]string)
		for key, values := range r.Form {
			if len(values) > 0 {
				formValues[key] = values[0]
			}
		}
		component := components.AddSourceForm(csrfToken, errors, formValues)
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
	ctx, cancel := context.WithTimeout(r.Context(), DefaultRequestTimeout)
	defer cancel()

	id := chi.URLParam(r, "id")
	if id == "" {
		respondWithError(w, "Source ID is missing", "Missing source ID in request", nil, http.StatusBadRequest)
		return
	}

	// Delete source via collector
	if err := h.collector.DeleteSource(ctx, id); err != nil {
		respondWithError(w, "Failed to delete source. Please try again.", "Failed to delete source", err, http.StatusInternalServerError)
		return
	}

	// Success: return 204 No Content (htmx will remove the element)
	w.WriteHeader(http.StatusNoContent)
}
