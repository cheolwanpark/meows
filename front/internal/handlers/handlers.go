package handlers

import (
	"context"
	"encoding/json"
	"html"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/cheolwanpark/meows/front/internal/collector"
	"github.com/cheolwanpark/meows/front/internal/middleware"
	"github.com/cheolwanpark/meows/front/internal/models"
	"github.com/cheolwanpark/meows/front/templates/components"
	"github.com/cheolwanpark/meows/front/templates/layouts"
	"github.com/cheolwanpark/meows/front/templates/pages"
	"github.com/go-chi/chi/v5"
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

// ArticleDetail renders the article detail page with comments
func (h *Handler) ArticleDetail(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), DefaultRequestTimeout)
	defer cancel()

	// Get CSRF token
	csrfToken := h.csrf.GetToken(r)
	h.csrf.SetToken(w, r, csrfToken)

	// Extract article ID from URL
	articleID := chi.URLParam(r, "id")
	if articleID == "" {
		// Render 404 page
		w.WriteHeader(http.StatusNotFound)
		component := layouts.Base("Not Found", csrfToken, components.ErrorPage(
			"Article Not Found",
			"The article you're looking for doesn't exist or has been removed.",
		))
		component.Render(r.Context(), w)
		return
	}

	// Fetch article detail from collector
	detail, err := h.collector.GetArticle(ctx, articleID)
	if err != nil {
		slog.Error("Failed to fetch article detail", "article_id", articleID, "error", err)

		// Check if it's a 404 error
		if err.Error() == "article not found" {
			w.WriteHeader(http.StatusNotFound)
			component := layouts.Base("Not Found", csrfToken, components.ErrorPage(
				"Article Not Found",
				"The article you're looking for doesn't exist or has been removed.",
			))
			component.Render(r.Context(), w)
			return
		}

		// Other errors (500)
		w.WriteHeader(http.StatusServiceUnavailable)
		component := layouts.Base("Error", csrfToken, components.ErrorPage(
			"Service Unavailable",
			"Unable to fetch article details. The collector service may be down. Please try again later.",
		))
		component.Render(r.Context(), w)
		return
	}

	// Convert article to view model using source type from detail
	article := models.FromCollectorArticle(detail.Article, detail.SourceType)

	// Render page
	component := pages.ArticleDetailPage(article, detail.Comments, csrfToken)
	component.Render(r.Context(), w)
}

// SourcesPage renders the source management page
func (h *Handler) SourcesPage(w http.ResponseWriter, r *http.Request) {
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

	// Validate inputs
	errors := models.FormErrors{}

	// Validate source type
	if sourceType == "" {
		errors.General = "Source type is required"
	} else if sourceType != "reddit" && sourceType != "semantic_scholar" {
		errors.General = "Invalid source type. Must be 'reddit' or 'semantic_scholar'"
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
		})
		component.Render(r.Context(), w)
		return
	}

	// Create source via collector
	createReq := collector.CreateSourceRequest{
		Type:   sourceType,
		Config: configJSON,
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

	// Fetch updated source list
	sources, err := h.collector.GetSources(ctx)
	if err != nil {
		slog.Error("Failed to fetch sources after deletion", "error", err)
		// Source was deleted successfully, but we can't render the updated list
		// Trigger a full page refresh to show accurate state
		w.Header().Set("HX-Refresh", "true")
		w.WriteHeader(http.StatusOK)
		return
	}

	// Convert to view models
	viewSources := make([]models.Source, len(sources))
	for i, s := range sources {
		viewSources[i] = models.FromCollectorSource(s)
	}

	// Return updated source list HTML
	csrfToken := h.csrf.GetToken(r)
	component := components.SourceList(viewSources, csrfToken)
	component.Render(r.Context(), w)
}

// SettingsPage renders the global settings page
func (h *Handler) SettingsPage(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), DefaultRequestTimeout)
	defer cancel()

	// Get CSRF token
	csrfToken := h.csrf.GetToken(r)
	h.csrf.SetToken(w, r, csrfToken)

	// Fetch current global config from collector
	config, err := h.collector.GetConfig(ctx)
	if err != nil {
		slog.Error("Failed to fetch global config", "error", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		component := layouts.Base("Error", csrfToken, components.ErrorPage(
			"Service Unavailable",
			"Unable to fetch settings. The collector service may be down. Please try again later.",
		))
		component.Render(r.Context(), w)
		return
	}

	// Render settings page
	component := pages.SettingsPage(*config, csrfToken, models.FormErrors{})
	component.Render(r.Context(), w)
}

// UpdateSettings handles global settings update (htmx endpoint)
func (h *Handler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), DefaultRequestTimeout)
	defer cancel()

	// Parse form
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`<div class="p-4 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-red-800 dark:text-red-200">Invalid form data. Please try again.</div>`))
		return
	}

	// Build update request with only provided fields
	req := collector.UpdateGlobalConfigRequest{}

	// Cron expression
	if cronExpr := r.FormValue("cron_expr"); cronExpr != "" {
		req.CronExpr = &cronExpr
	}

	// Rate limits
	if redditRL := r.FormValue("reddit_rate_limit_delay_ms"); redditRL != "" {
		if val, err := strconv.Atoi(redditRL); err == nil {
			req.RedditRateLimitDelayMs = &val
		}
	}
	if s2RL := r.FormValue("semantic_scholar_rate_limit_delay_ms"); s2RL != "" {
		if val, err := strconv.Atoi(s2RL); err == nil {
			req.SemanticScholarRateLimitDelayMs = &val
		}
	}

	// Credentials (only if non-empty)
	if clientID := r.FormValue("reddit_client_id"); clientID != "" {
		req.RedditClientID = &clientID
	}
	if clientSecret := r.FormValue("reddit_client_secret"); clientSecret != "" {
		req.RedditClientSecret = &clientSecret
	}
	if username := r.FormValue("reddit_username"); username != "" {
		req.RedditUsername = &username
	}
	if password := r.FormValue("reddit_password"); password != "" {
		req.RedditPassword = &password
	}
	if apiKey := r.FormValue("semantic_scholar_api_key"); apiKey != "" {
		req.SemanticScholarAPIKey = &apiKey
	}

	// Update config via collector
	if _, err := h.collector.UpdateConfig(ctx, req); err != nil {
		// Extract error message and return user-friendly HTML
		errorMsg := err.Error()
		// Escape HTML to prevent XSS
		escapedMsg := html.EscapeString(errorMsg)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`<div class="p-4 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-red-800 dark:text-red-200">Failed to update settings: ` + escapedMsg + `</div>`))
		return
	}

	// Return success message
	w.Header().Set("HX-Trigger", "settingsUpdated")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`<div class="p-4 bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-lg text-green-800 dark:text-green-200">Settings updated successfully!</div>`))
}
