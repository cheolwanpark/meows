package handlers

import (
	"context"
	"encoding/json"
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
	trigger := map[string]interface{}{
		"showToast": map[string]string{
			"type": "error",
			"text": userMsg,
		},
	}
	triggerJSON, err := json.Marshal(trigger)
	if err != nil {
		// This should never happen with simple map structures, but log just in case
		slog.Error("Failed to marshal toast trigger", "error", err)
	} else {
		w.Header().Set("HX-Trigger", string(triggerJSON))
	}

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

	// Get profile ID from middleware context (if available)
	profileID, _ := middleware.GetProfileID(r)

	// Parse pagination and curated parameters
	page, curated := h.parseHomeParams(r, profileID)
	offset := (page - 1) * HomePageSize

	// Fetch articles and pagination data
	articles, pagination, err := h.fetchArticleListData(ctx, profileID, page, offset, curated)
	if err != nil {
		slog.Error("Failed to fetch articles", "error", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		component := layouts.Base("Error", csrfToken, components.ErrorPage(
			"Service Unavailable",
			"Unable to fetch articles. The collector service may be down. Please try again later.",
		))
		component.Render(r.Context(), w)
		return
	}

	// Render page
	component := pages.HomePage(articles, pagination, csrfToken, profileID)
	component.Render(r.Context(), w)
}

// ArticleListPartial renders only the article list for HTMX partial updates
func (h *Handler) ArticleListPartial(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), DefaultRequestTimeout)
	defer cancel()

	// Get profile ID from middleware context (if available)
	profileID, _ := middleware.GetProfileID(r)

	// Parse pagination and curated parameters
	page, curated := h.parseHomeParams(r, profileID)
	offset := (page - 1) * HomePageSize

	// Fetch articles and pagination data
	articles, pagination, err := h.fetchArticleListData(ctx, profileID, page, offset, curated)
	if err != nil {
		slog.Error("Failed to fetch articles for partial", "error", err)
		// Return error message in the partial
		component := components.EmptyState(
			"Unable to load articles",
			"Please try again later.",
		)
		component.Render(r.Context(), w)
		return
	}

	// Render only the article list partial
	component := pages.ArticleList(articles, pagination, profileID)
	component.Render(r.Context(), w)
}

// parseHomeParams extracts page and curated parameters from the request
func (h *Handler) parseHomeParams(r *http.Request, profileID string) (page int, curated bool) {
	// Parse page parameter (default: 1)
	page = 1
	if pageStr := r.URL.Query().Get("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p >= 1 {
			page = p
		}
	}

	// Parse curated parameter (default: true if logged in, false otherwise)
	curated = profileID != "" // Default to curated if logged in
	if curatedStr := r.URL.Query().Get("curated"); curatedStr != "" {
		curated = curatedStr == "true"
	}

	// Force curated=false if no profile (can't filter curated without profile)
	if profileID == "" {
		curated = false
	}

	return page, curated
}

// fetchArticleListData fetches articles and builds pagination data
func (h *Handler) fetchArticleListData(ctx context.Context, profileID string, page, offset int, curated bool) ([]models.Article, models.Pagination, error) {
	// Fetch articles from collector
	response, err := h.collector.GetArticles(ctx, HomePageSize, offset, profileID, curated)
	if err != nil {
		return nil, models.Pagination{}, err
	}

	// Fetch sources to build source type lookup map (avoids N+1 queries)
	collectorSources, err := h.collector.GetSources(ctx, profileID)
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
	articles := make([]models.Article, 0, len(response.Articles))
	for _, a := range response.Articles {
		sourceType := sourceTypeMap[a.SourceID]
		if sourceType == "" {
			sourceType = "reddit" // Fallback for legacy/orphaned articles
		}
		articles = append(articles, models.FromCollectorArticle(a, sourceType))
	}

	// Build pagination
	totalPages := (response.Total + HomePageSize - 1) / HomePageSize
	if totalPages < 1 {
		totalPages = 1
	}

	pagination := models.Pagination{
		CurrentPage: page,
		TotalPages:  totalPages,
		TotalItems:  response.Total,
		PageSize:    HomePageSize,
		HasPrev:     page > 1,
		HasNext:     page < totalPages,
		IsCurated:   curated,
	}

	return articles, pagination, nil
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

	// Get profile ID from middleware context (if available)
	profileID, _ := middleware.GetProfileID(r)

	// Fetch article detail from collector
	detail, err := h.collector.GetArticle(ctx, articleID, profileID)
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
	component := pages.ArticleDetailPage(article, detail.Comments, csrfToken, profileID)
	component.Render(r.Context(), w)
}

// SourcesPage renders the source management page
func (h *Handler) SourcesPage(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), DefaultRequestTimeout)
	defer cancel()

	// Get CSRF token
	csrfToken := h.csrf.GetToken(r)
	h.csrf.SetToken(w, r, csrfToken)

	// Get profile ID from middleware context
	profileID, _ := middleware.GetProfileID(r)

	// Fetch sources from collector
	collectorSources, err := h.collector.GetSources(ctx, profileID)
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
	} else if sourceType != "reddit" && sourceType != "semantic_scholar" && sourceType != "hackernews" {
		errors.General = "Invalid source type. Must be 'reddit', 'semantic_scholar', or 'hackernews'"
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
	} else if sourceType == "hackernews" {
		config, configErr = buildHackerNewsConfig(r)
		if configErr != nil {
			errors.General = configErr.Error()
		}
	}

	// If there are errors, return form with errors
	if errors.HasErrors() {
		w.WriteHeader(http.StatusUnprocessableEntity)
		// Preserve form values for user convenience
		formValues := make(map[string]string)
		for key, values := range r.Form {
			if len(values) > 0 {
				formValues[key] = values[0]
			}
		}
		component := components.AddSourceForm(errors, formValues)
		component.Render(r.Context(), w)
		return
	}

	// Marshal config to JSON
	configJSON, err := json.Marshal(config)
	if err != nil {
		slog.Error("Failed to marshal config", "error", err)
		errors.General = "Failed to create source configuration"
		w.WriteHeader(http.StatusInternalServerError)
		component := components.AddSourceForm(errors, map[string]string{
			"source_type": sourceType,
		})
		component.Render(r.Context(), w)
		return
	}

	// Get profile ID from middleware context
	profileID, ok := middleware.GetProfileID(r)
	if !ok || profileID == "" {
		errors.General = "Please select a profile first"
		w.WriteHeader(http.StatusUnprocessableEntity)
		formValues := make(map[string]string)
		for key, values := range r.Form {
			if len(values) > 0 {
				formValues[key] = values[0]
			}
		}
		component := components.AddSourceForm(errors, formValues)
		component.Render(r.Context(), w)
		return
	}

	// Create source via collector
	createReq := collector.CreateSourceRequest{
		Type:      sourceType,
		Config:    configJSON,
		ProfileID: profileID,
	}

	source, err := h.collector.CreateSource(ctx, createReq)
	if err != nil {
		slog.Error("Failed to create source", "error", err, "type", sourceType)
		// Provide user-friendly error message instead of exposing internal errors
		errors.General = "Failed to create source. Please check your configuration and try again."
		w.WriteHeader(http.StatusInternalServerError)
		formValues := make(map[string]string)
		for key, values := range r.Form {
			if len(values) > 0 {
				formValues[key] = values[0]
			}
		}
		component := components.AddSourceForm(errors, formValues)
		component.Render(r.Context(), w)
		return
	}

	// Success: return source card for htmx to insert
	viewSource := models.FromCollectorSource(*source)

	// Trigger success toast
	trigger := map[string]interface{}{
		"showToast": map[string]string{
			"type": "success",
			"text": "Source added successfully",
		},
	}
	if triggerJSON, err := json.Marshal(trigger); err == nil {
		w.Header().Set("HX-Trigger", string(triggerJSON))
	}

	component := components.SourceCard(viewSource)
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

	// Get profile ID from middleware context
	profileID, _ := middleware.GetProfileID(r)

	// Delete source via collector
	if err := h.collector.DeleteSource(ctx, id, profileID); err != nil {
		respondWithError(w, "Failed to delete source. Please try again.", "Failed to delete source", err, http.StatusInternalServerError)
		return
	}

	// Fetch updated source list
	sources, err := h.collector.GetSources(ctx, profileID)
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

	// Trigger success toast
	trigger := map[string]interface{}{
		"showToast": map[string]string{
			"type": "success",
			"text": "Source removed successfully",
		},
	}
	if triggerJSON, err := json.Marshal(trigger); err == nil {
		w.Header().Set("HX-Trigger", string(triggerJSON))
	}

	// Return updated source list HTML
	component := components.SourceList(viewSources)
	component.Render(r.Context(), w)
}

// TriggerSource handles manual source trigger (htmx endpoint)
func (h *Handler) TriggerSource(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), DefaultRequestTimeout)
	defer cancel()

	id := chi.URLParam(r, "id")
	if id == "" {
		respondWithError(w, "Source ID is missing", "Missing source ID in request", nil, http.StatusBadRequest)
		return
	}

	// Get profile ID from middleware context
	profileID, _ := middleware.GetProfileID(r)

	// Trigger source via collector
	if err := h.collector.TriggerSource(ctx, id, profileID); err != nil {
		// Check status code using type assertion
		if statusErr, ok := err.(*collector.StatusError); ok {
			switch statusErr.StatusCode {
			case http.StatusConflict:
				// 409: Source is already running
				trigger := map[string]interface{}{
					"showToast": map[string]string{
						"type": "info",
						"text": "Source is already running",
					},
				}
				if triggerJSON, err := json.Marshal(trigger); err == nil {
					w.Header().Set("HX-Trigger", string(triggerJSON))
				}
				w.WriteHeader(http.StatusOK)
				return
			case http.StatusNotFound:
				// 404: Source not found (shouldn't happen with valid UI, but handle it)
				respondWithError(w, "Source not found. Please refresh the page.", "Source not found", err, http.StatusNotFound)
				return
			}
		}

		respondWithError(w, "Failed to trigger crawl. Please try again.", "Failed to trigger source", err, http.StatusInternalServerError)
		return
	}

	// Success toast
	trigger := map[string]interface{}{
		"showToast": map[string]string{
			"type": "success",
			"text": "Crawl started! Check back in a few moments.",
		},
	}
	if triggerJSON, err := json.Marshal(trigger); err == nil {
		w.Header().Set("HX-Trigger", string(triggerJSON))
	}

	w.WriteHeader(http.StatusOK)
}

// ProfileSetup renders the profile setup page
func (h *Handler) ProfileSetup(w http.ResponseWriter, r *http.Request) {
	// Get CSRF token
	csrfToken := h.csrf.GetToken(r)
	h.csrf.SetToken(w, r, csrfToken)

	// Render profile setup page
	component := pages.ProfileSetup(csrfToken)
	component.Render(r.Context(), w)
}

// SwitchProfile handles profile switching by setting cookie and redirecting
func (h *Handler) SwitchProfile(w http.ResponseWriter, r *http.Request) {
	profileID := chi.URLParam(r, "id")
	ctx, cancel := context.WithTimeout(r.Context(), DefaultRequestTimeout)
	defer cancel()

	// Validate profile exists
	_, err := h.collector.GetProfile(ctx, profileID)
	if err != nil {
		slog.Error("Failed to validate profile", "profile_id", profileID, "error", err)
		http.Error(w, "Invalid profile", http.StatusBadRequest)
		return
	}

	// Set cookie with security flags
	http.SetCookie(w, &http.Cookie{
		Name:     "current_profile_id",
		Value:    profileID,
		Path:     "/",
		MaxAge:   30 * 24 * 60 * 60, // 30 days
		HttpOnly: true,
		Secure:   middleware.IsSecureRequest(r), // Set Secure flag for HTTPS connections
		SameSite: http.SameSiteLaxMode,
	})

	// Return success (client will handle page reload)
	w.WriteHeader(http.StatusOK)
}

// ProfileSwitcherPartial returns the profile switcher component (for HTMX)
func (h *Handler) ProfileSwitcherPartial(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), DefaultRequestTimeout)
	defer cancel()

	// Get current profile ID from cookie
	var currentProfileID string
	if cookie, err := r.Cookie("current_profile_id"); err == nil {
		currentProfileID = cookie.Value
	}

	// Fetch all profiles
	profiles, err := h.collector.GetProfiles(ctx)
	if err != nil {
		slog.Error("Failed to fetch profiles for switcher", "error", err)
		// Return empty/error state
		profiles = []collector.Profile{}
	}

	// Render profile switcher component
	component := components.ProfileSwitcher(profiles, currentProfileID)
	component.Render(r.Context(), w)
}

// ProfileEditPage renders the profile edit page for the current user
func (h *Handler) ProfileEditPage(w http.ResponseWriter, r *http.Request) {
	// Get current profile ID from middleware context
	profileID, ok := r.Context().Value(middleware.ProfileIDKey).(string)
	if !ok || profileID == "" {
		http.Redirect(w, r, "/profiles/setup", http.StatusSeeOther)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), DefaultRequestTimeout)
	defer cancel()

	// Fetch current profile from collector
	profile, err := h.collector.GetProfile(ctx, profileID)
	if err != nil {
		slog.Error("Failed to fetch profile for editing", "profile_id", profileID, "error", err)
		http.Error(w, "Profile not found", http.StatusNotFound)
		return
	}

	// Get CSRF token
	csrfToken := h.csrf.GetToken(r)

	// Render profile edit page
	component := pages.ProfileEdit(*profile, csrfToken)
	component.Render(r.Context(), w)
}

// UpdateProfileHandler handles profile update requests (PATCH /api/profile)
func (h *Handler) UpdateProfileHandler(w http.ResponseWriter, r *http.Request) {
	// Get current profile ID from middleware context
	profileID, ok := r.Context().Value(middleware.ProfileIDKey).(string)
	if !ok || profileID == "" {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Parse form data
	if err := r.ParseForm(); err != nil {
		respondWithError(w, "Invalid form data", "Form parse error", err, http.StatusBadRequest)
		return
	}

	nickname := r.FormValue("nickname")
	userDescription := r.FormValue("user_description")

	// Validate inputs
	if nickname == "" {
		respondWithError(w, "Nickname is required", "Nickname validation failed", nil, http.StatusBadRequest)
		return
	}

	if len(nickname) > 50 {
		respondWithError(w, "Nickname too long (max 50 characters)", "Nickname validation failed", nil, http.StatusBadRequest)
		return
	}

	if len(userDescription) > 500 {
		respondWithError(w, "Description too long (max 500 characters)", "Description validation failed", nil, http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), DefaultRequestTimeout)
	defer cancel()

	// Update profile via collector API
	updates := map[string]string{
		"nickname":         nickname,
		"user_description": userDescription,
	}

	err := h.collector.UpdateProfile(ctx, profileID, updates)
	if err != nil {
		slog.Error("Failed to update profile", "profile_id", profileID, "error", err)
		respondWithError(w, "Failed to update profile", "Collector update error", err, http.StatusInternalServerError)
		return
	}

	// Return "updating" to trigger frontend polling if description changed
	// The collector automatically triggers character regeneration when description changes
	w.Header().Set("HX-Trigger", `{"showToast": "Profile updated!"}`)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("updating"))
}

// GetProfileStatusAPI returns the character generation status for the current user's profile
func (h *Handler) GetProfileStatusAPI(w http.ResponseWriter, r *http.Request) {
	// Get current profile ID from middleware context
	profileID, ok := r.Context().Value(middleware.ProfileIDKey).(string)
	if !ok || profileID == "" {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), DefaultRequestTimeout)
	defer cancel()

	// Get profile status from collector
	status, err := h.collector.GetProfileStatus(ctx, profileID)
	if err != nil {
		slog.Error("Failed to get profile status", "profile_id", profileID, "error", err)
		http.Error(w, "Failed to get status", http.StatusInternalServerError)
		return
	}

	// Return JSON status
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// LikeArticle handles article like requests (HTMX partial)
func (h *Handler) LikeArticle(w http.ResponseWriter, r *http.Request) {
	articleID := chi.URLParam(r, "id")
	profileID := r.FormValue("profile_id")
	ctx, cancel := context.WithTimeout(r.Context(), DefaultRequestTimeout)
	defer cancel()

	// Validate inputs
	if profileID == "" {
		respondWithError(w, "Profile required", "Profile ID missing in like request", nil, http.StatusBadRequest)
		return
	}

	// Create like via collector API
	like, err := h.collector.LikeArticle(ctx, articleID, profileID)
	if err != nil {
		slog.Error("Failed to like article", "article_id", articleID, "profile_id", profileID, "error", err)

		// Set toast error trigger
		trigger := map[string]interface{}{
			"showToast": map[string]string{
				"type": "error",
				"text": "Failed to like article",
			},
		}
		if triggerJSON, err := json.Marshal(trigger); err == nil {
			w.Header().Set("HX-Trigger", string(triggerJSON))
		}

		// Return the unliked button (rollback)
		component := components.LikeButton(articleID, false, "", profileID)
		component.Render(r.Context(), w)
		return
	}

	// Return the liked button (new state)
	component := components.LikeButton(articleID, true, like.ID, profileID)
	component.Render(r.Context(), w)
}

// UnlikeArticle handles article unlike requests (HTMX partial)
func (h *Handler) UnlikeArticle(w http.ResponseWriter, r *http.Request) {
	likeID := chi.URLParam(r, "id")
	ctx, cancel := context.WithTimeout(r.Context(), DefaultRequestTimeout)
	defer cancel()

	// Get profile ID from cookie
	var profileID string
	if cookie, err := r.Cookie("current_profile_id"); err == nil {
		profileID = cookie.Value
	}

	// Extract article ID from the like button context
	// Since we're swapping the button, we need to know the article ID to render the new button
	// We'll need to fetch the like details first, or store articleID in a hidden field
	// For now, let's accept it as a query parameter
	articleID := r.URL.Query().Get("article_id")
	if articleID == "" {
		// Try to get from form
		articleID = r.FormValue("article_id")
	}

	// Delete like via collector API
	err := h.collector.UnlikeArticle(ctx, likeID)
	if err != nil {
		slog.Error("Failed to unlike article", "like_id", likeID, "error", err)

		// Set toast error trigger
		trigger := map[string]interface{}{
			"showToast": map[string]string{
				"type": "error",
				"text": "Failed to unlike article",
			},
		}
		if triggerJSON, err := json.Marshal(trigger); err == nil {
			w.Header().Set("HX-Trigger", string(triggerJSON))
		}

		// Return the liked button (rollback)
		component := components.LikeButton(articleID, true, likeID, profileID)
		component.Render(r.Context(), w)
		return
	}

	// Return the unliked button (new state)
	component := components.LikeButton(articleID, false, "", profileID)
	component.Render(r.Context(), w)
}

// CreateProfile proxies profile creation to the collector API
func (h *Handler) CreateProfile(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), DefaultRequestTimeout)
	defer cancel()

	nickname := r.FormValue("nickname")
	userDescription := r.FormValue("user_description")

	if nickname == "" || userDescription == "" {
		http.Error(w, "nickname and user_description are required", http.StatusBadRequest)
		return
	}

	profile, err := h.collector.CreateProfile(ctx, nickname, userDescription)
	if err != nil {
		slog.Error("Failed to create profile", "error", err)
		http.Error(w, "Failed to create profile", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(profile)
}

// GetProfileStatus proxies profile status requests to the collector API
func (h *Handler) GetProfileStatus(w http.ResponseWriter, r *http.Request) {
	profileID := chi.URLParam(r, "id")
	ctx, cancel := context.WithTimeout(r.Context(), DefaultRequestTimeout)
	defer cancel()

	status, err := h.collector.GetProfileStatus(ctx, profileID)
	if err != nil {
		slog.Error("Failed to get profile status", "profile_id", profileID, "error", err)
		http.Error(w, "Failed to get profile status", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// GetCSRFToken returns a fresh CSRF token and updates the cookie
// This endpoint does NOT require CSRF validation (it's used to refresh the token)
func (h *Handler) GetCSRFToken(w http.ResponseWriter, r *http.Request) {
	// Get or generate a fresh CSRF token
	token := h.csrf.GetToken(r)

	// Set/refresh the cookie with the token
	h.csrf.SetToken(w, r, token)

	// Return token in both JSON body and header for flexibility
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-CSRF-Token", token)

	response := map[string]string{
		"token": token,
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		slog.Error("Failed to encode CSRF token response", "error", err)
		http.Error(w, "Failed to get CSRF token", http.StatusInternalServerError)
		return
	}
}
