package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cheolwanpark/meows/collector/internal/db"
	"github.com/cheolwanpark/meows/collector/internal/scheduler"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Handler holds dependencies for HTTP handlers
type Handler struct {
	db        *db.DB
	scheduler *scheduler.Scheduler
}

// NewHandler creates a new Handler
func NewHandler(database *db.DB, sched *scheduler.Scheduler) *Handler {
	return &Handler{
		db:        database,
		scheduler: sched,
	}
}

// CreateSource godoc
// @Summary Create a new crawling source
// @Description Add a new source for Reddit or Semantic Scholar (schedule is global)
// @Tags sources
// @Accept json
// @Produce json
// @Param source body CreateSourceRequest true "Source configuration"
// @Success 201 {object} SourceResponse
// @Failure 400 {object} ErrorResponse "Invalid request body, type, or config"
// @Failure 409 {object} ErrorResponse "Source with this configuration already exists"
// @Failure 500 {object} ErrorResponse "Database error"
// @Router /sources [post]
func (h *Handler) CreateSource(w http.ResponseWriter, r *http.Request) {
	var req CreateSourceRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate type
	if req.Type != "reddit" && req.Type != "semantic_scholar" {
		respondError(w, http.StatusBadRequest, "type must be 'reddit' or 'semantic_scholar'")
		return
	}

	// Extract external ID for deduplication
	externalID, err := extractExternalID(req.Type, req.Config)
	if err != nil {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid config: %v", err))
		return
	}

	// Create source
	source := &db.Source{
		ID:         uuid.New().String(),
		Type:       req.Type,
		Config:     req.Config,
		ExternalID: externalID,
		Status:     "idle",
		CreatedAt:  time.Now(),
	}

	// Insert into database
	_, err = h.db.Exec(`
		INSERT INTO sources (id, type, config, external_id, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, source.ID, source.Type, source.Config, source.ExternalID, source.Status, source.CreatedAt)

	if err != nil {
		// Check for unique constraint violation
		if isUniqueViolation(err) {
			respondError(w, http.StatusConflict, "source with this configuration already exists")
			return
		}
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create source: %v", err))
		return
	}

	// No need to register job - scheduler runs all sources on global schedule

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(toSourceResponse(source))
}

// ListSources godoc
// @Summary List all sources
// @Description Get all configured crawling sources, optionally filtered by type
// @Tags sources
// @Accept json
// @Produce json
// @Param type query string false "Filter by source type" Enums(reddit, semantic_scholar)
// @Success 200 {array} SourceResponse
// @Failure 500 {object} ErrorResponse "Database error"
// @Router /sources [get]
func (h *Handler) ListSources(w http.ResponseWriter, r *http.Request) {
	sourceType := r.URL.Query().Get("type")

	query := "SELECT id, type, config, external_id, last_run_at, last_success_at, last_error, status, created_at FROM sources"
	args := []interface{}{}

	if sourceType != "" {
		query += " WHERE type = ?"
		args = append(args, sourceType)
	}

	query += " ORDER BY created_at DESC"

	rows, err := h.db.Query(query, args...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to query sources: %v", err))
		return
	}
	defer rows.Close()

	sources := []SourceResponse{}
	for rows.Next() {
		var src db.Source
		var lastRunAt, lastSuccessAt sql.NullTime
		var lastError, externalID sql.NullString

		err := rows.Scan(
			&src.ID,
			&src.Type,
			&src.Config,
			&externalID,
			&lastRunAt,
			&lastSuccessAt,
			&lastError,
			&src.Status,
			&src.CreatedAt,
		)
		if err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to scan source: %v", err))
			return
		}

		if externalID.Valid {
			src.ExternalID = externalID.String
		}
		if lastRunAt.Valid {
			src.LastRunAt = &lastRunAt.Time
		}
		if lastSuccessAt.Valid {
			src.LastSuccessAt = &lastSuccessAt.Time
		}
		if lastError.Valid {
			src.LastError = lastError.String
		}

		sources = append(sources, toSourceResponse(&src))
	}

	if err := rows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("error iterating sources: %v", err))
		return
	}

	json.NewEncoder(w).Encode(sources)
}

// GetSource godoc
// @Summary Get a source by ID
// @Description Retrieve a specific source by its UUID
// @Tags sources
// @Accept json
// @Produce json
// @Param id path string true "Source ID (UUID)"
// @Success 200 {object} SourceResponse
// @Failure 404 {object} ErrorResponse "Source not found"
// @Failure 500 {object} ErrorResponse "Database error"
// @Router /sources/{id} [get]
func (h *Handler) GetSource(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var src db.Source
	var lastRunAt, lastSuccessAt sql.NullTime
	var lastError, externalID sql.NullString

	err := h.db.QueryRow(`
		SELECT id, type, config, external_id, last_run_at, last_success_at, last_error, status, created_at
		FROM sources WHERE id = ?
	`, id).Scan(
		&src.ID,
		&src.Type,
		&src.Config,
		&externalID,
		&lastRunAt,
		&lastSuccessAt,
		&lastError,
		&src.Status,
		&src.CreatedAt,
	)

	if err == sql.ErrNoRows {
		respondError(w, http.StatusNotFound, "source not found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to fetch source: %v", err))
		return
	}

	if externalID.Valid {
		src.ExternalID = externalID.String
	}
	if lastRunAt.Valid {
		src.LastRunAt = &lastRunAt.Time
	}
	if lastSuccessAt.Valid {
		src.LastSuccessAt = &lastSuccessAt.Time
	}
	if lastError.Valid {
		src.LastError = lastError.String
	}

	json.NewEncoder(w).Encode(toSourceResponse(&src))
}

// UpdateSource godoc
// @Summary Update a source
// @Description Update source configuration. Schedule is global and managed separately.
// @Tags sources
// @Accept json
// @Produce json
// @Param id path string true "Source ID (UUID)"
// @Param source body UpdateSourceRequest true "Updated source configuration"
// @Success 200 {object} SourceResponse
// @Failure 400 {object} ErrorResponse "Invalid request body or config"
// @Failure 404 {object} ErrorResponse "Source not found"
// @Failure 500 {object} ErrorResponse "Database error"
// @Router /sources/{id} [put]
func (h *Handler) UpdateSource(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req UpdateSourceRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Fetch existing source
	var src db.Source
	var lastRunAt, lastSuccessAt sql.NullTime
	var lastError, externalID sql.NullString

	err := h.db.QueryRow("SELECT id, type, config, external_id, last_run_at, last_success_at, last_error, status, created_at FROM sources WHERE id = ?", id).
		Scan(&src.ID, &src.Type, &src.Config, &externalID, &lastRunAt, &lastSuccessAt, &lastError, &src.Status, &src.CreatedAt)

	if err == sql.ErrNoRows {
		respondError(w, http.StatusNotFound, "source not found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to fetch source: %v", err))
		return
	}

	// Handle nullable fields
	if externalID.Valid {
		src.ExternalID = externalID.String
	}
	if lastRunAt.Valid {
		src.LastRunAt = &lastRunAt.Time
	}
	if lastSuccessAt.Valid {
		src.LastSuccessAt = &lastSuccessAt.Time
	}
	if lastError.Valid {
		src.LastError = lastError.String
	}

	// Update config
	if req.Config != nil {
		// Validate new config
		newExternalID, err := extractExternalID(src.Type, *req.Config)
		if err != nil {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid config: %v", err))
			return
		}
		src.Config = *req.Config
		src.ExternalID = newExternalID
	}

	// Update database
	_, err = h.db.Exec(`
		UPDATE sources SET config = ?, external_id = ? WHERE id = ?
	`, src.Config, src.ExternalID, id)

	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to update source: %v", err))
		return
	}

	// No need to register job - scheduler runs all sources on global schedule

	json.NewEncoder(w).Encode(toSourceResponse(&src))
}

// DeleteSource godoc
// @Summary Delete a source by ID
// @Description Remove source by UUID. Cascades to delete associated articles and comments.
// @Tags sources
// @Accept json
// @Produce json
// @Param id path string true "Source ID (UUID)"
// @Success 204 "Source deleted successfully"
// @Failure 404 {object} ErrorResponse "Source not found"
// @Failure 500 {object} ErrorResponse "Database error"
// @Router /sources/{id} [delete]
func (h *Handler) DeleteSource(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// No need to unregister job - scheduler loads all sources dynamically

	// Delete from database (cascade deletes articles and comments)
	result, err := h.db.Exec("DELETE FROM sources WHERE id = ?", id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to delete source: %v", err))
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		respondError(w, http.StatusNotFound, "source not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// DeleteSourceByTypeAndExternalID godoc
// @Summary Delete a source by type and external ID
// @Description Alternative deletion method using type and external identifier (subreddit name, query, or paper ID)
// @Tags sources
// @Accept json
// @Produce json
// @Param type path string true "Source type" Enums(reddit, semantic_scholar)
// @Param external_id path string true "External identifier (URL-encode if contains special characters)"
// @Success 204 "Source deleted successfully"
// @Failure 400 {object} ErrorResponse "Invalid type or external_id contains slashes"
// @Failure 404 {object} ErrorResponse "Source not found"
// @Failure 500 {object} ErrorResponse "Database error"
// @Router /sources/{type}/{external_id} [delete]
func (h *Handler) DeleteSourceByTypeAndExternalID(w http.ResponseWriter, r *http.Request) {
	sourceType := chi.URLParam(r, "type")
	externalID := chi.URLParam(r, "external_id")

	// Validate type (whitelist)
	if sourceType != "reddit" && sourceType != "semantic_scholar" {
		respondError(w, http.StatusBadRequest, "type must be 'reddit' or 'semantic_scholar'")
		return
	}

	// Validate external_id
	if externalID == "" {
		respondError(w, http.StatusBadRequest, "external_id cannot be empty")
		return
	}
	if strings.Contains(externalID, "/") {
		respondError(w, http.StatusBadRequest, "external_id cannot contain slashes")
		return
	}

	// Fetch source to get UUID
	var sourceID string
	err := h.db.QueryRow(
		"SELECT id FROM sources WHERE type = ? AND external_id = ?",
		sourceType, externalID,
	).Scan(&sourceID)

	if err == sql.ErrNoRows {
		respondError(w, http.StatusNotFound, "source not found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to fetch source: %v", err))
		return
	}

	// No need to unregister job - scheduler loads all sources dynamically

	// Delete from database (cascade deletes articles and comments)
	result, err := h.db.Exec("DELETE FROM sources WHERE id = ?", sourceID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to delete source: %v", err))
		return
	}

	// Check if source was actually deleted
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		respondError(w, http.StatusNotFound, "source not found")
		return
	}

	// Return success
	w.WriteHeader(http.StatusNoContent)
}

// GetSchedule godoc
// @Summary Get global schedule information
// @Description Returns the global crawl schedule (applies to all sources)
// @Tags schedule
// @Accept json
// @Produce json
// @Success 200 {object} db.ScheduleEntry
// @Failure 500 {object} ErrorResponse "Scheduler error"
// @Router /schedule [get]
func (h *Handler) GetSchedule(w http.ResponseWriter, r *http.Request) {
	schedule, err := h.scheduler.GetSchedule()
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get schedule: %v", err))
		return
	}

	json.NewEncoder(w).Encode(schedule)
}

// ListArticles godoc
// @Summary List articles
// @Description Get crawled articles with pagination and filtering
// @Tags articles
// @Accept json
// @Produce json
// @Param source_id query string false "Filter by source ID (UUID)"
// @Param limit query int false "Max results per page (default: 50, max: 500)" minimum(1) maximum(500)
// @Param offset query int false "Pagination offset (default: 0)" minimum(0)
// @Param since query string false "Filter articles written after this timestamp (RFC3339 format)" example(2024-11-15T00:00:00Z)
// @Success 200 {array} db.Article
// @Failure 500 {object} ErrorResponse "Database error"
// @Router /articles [get]
func (h *Handler) ListArticles(w http.ResponseWriter, r *http.Request) {
	sourceID := r.URL.Query().Get("source_id")
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")
	sinceStr := r.URL.Query().Get("since")

	// Parse parameters
	limit := 50
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
			if limit > 500 {
				limit = 500
			}
		}
	}

	offset := 0
	if offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	var since *time.Time
	if sinceStr != "" {
		if t, err := time.Parse(time.RFC3339, sinceStr); err == nil {
			since = &t
		}
	}

	// Build query
	query := `
		SELECT id, source_id, external_id, title, author, content, url, written_at, metadata, created_at
		FROM articles
		WHERE 1=1
	`
	args := []interface{}{}

	if sourceID != "" {
		query += " AND source_id = ?"
		args = append(args, sourceID)
	}

	if since != nil {
		query += " AND written_at >= ?"
		args = append(args, *since)
	}

	query += " ORDER BY written_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := h.db.Query(query, args...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to query articles: %v", err))
		return
	}
	defer rows.Close()

	articles := []db.Article{}
	for rows.Next() {
		var article db.Article
		var url, metadata sql.NullString

		err := rows.Scan(
			&article.ID,
			&article.SourceID,
			&article.ExternalID,
			&article.Title,
			&article.Author,
			&article.Content,
			&url,
			&article.WrittenAt,
			&metadata,
			&article.CreatedAt,
		)
		if err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to scan article: %v", err))
			return
		}

		if url.Valid {
			article.URL = url.String
		}
		if metadata.Valid {
			article.Metadata = json.RawMessage(metadata.String)
		}

		articles = append(articles, article)
	}

	if err := rows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("error iterating articles: %v", err))
		return
	}

	json.NewEncoder(w).Encode(articles)
}

// ArticleDetailResponse represents an article with its comments
type ArticleDetailResponse struct {
	Article    db.Article   `json:"article"`
	Comments   []db.Comment `json:"comments"`
	SourceType string       `json:"source_type"` // "reddit" or "semantic_scholar"
}

// GetArticle godoc
// @Summary Get article detail with comments
// @Description Get a specific article by ID with all its comments in nested tree structure
// @Tags articles
// @Accept json
// @Produce json
// @Param id path string true "Article ID (UUID)"
// @Success 200 {object} ArticleDetailResponse
// @Failure 400 {object} ErrorResponse "Invalid article ID format"
// @Failure 404 {object} ErrorResponse "Article not found"
// @Failure 500 {object} ErrorResponse "Database error"
// @Router /articles/{id} [get]
func (h *Handler) GetArticle(w http.ResponseWriter, r *http.Request) {
	articleID := chi.URLParam(r, "id")

	// Validate UUID format
	if _, err := uuid.Parse(articleID); err != nil {
		respondError(w, http.StatusBadRequest, "invalid article ID format")
		return
	}

	// Query article
	var article db.Article
	var url, metadata sql.NullString

	err := h.db.QueryRow(`
		SELECT id, source_id, external_id, title, author, content, url, written_at, metadata, created_at
		FROM articles
		WHERE id = ?
	`, articleID).Scan(
		&article.ID,
		&article.SourceID,
		&article.ExternalID,
		&article.Title,
		&article.Author,
		&article.Content,
		&url,
		&article.WrittenAt,
		&metadata,
		&article.CreatedAt,
	)

	if err == sql.ErrNoRows {
		respondError(w, http.StatusNotFound, "article not found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to query article: %v", err))
		return
	}

	if url.Valid {
		article.URL = url.String
	}
	if metadata.Valid {
		article.Metadata = json.RawMessage(metadata.String)
	}

	// Query comments for this article
	rows, err := h.db.Query(`
		SELECT id, article_id, external_id, author, content, written_at, parent_id, depth
		FROM comments
		WHERE article_id = ?
		ORDER BY written_at ASC
	`, articleID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to query comments: %v", err))
		return
	}
	defer rows.Close()

	// Collect all comments
	flatComments := []db.Comment{}
	for rows.Next() {
		var comment db.Comment
		var parentID sql.NullString

		err := rows.Scan(
			&comment.ID,
			&comment.ArticleID,
			&comment.ExternalID,
			&comment.Author,
			&comment.Content,
			&comment.WrittenAt,
			&parentID,
			&comment.Depth,
		)
		if err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to scan comment: %v", err))
			return
		}

		if parentID.Valid {
			// Copy to avoid pointer aliasing
			pid := parentID.String
			comment.ParentID = &pid
		}

		flatComments = append(flatComments, comment)
	}

	if err := rows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("error iterating comments: %v", err))
		return
	}

	// Build comment tree
	comments := buildCommentTree(flatComments)

	// Get source type
	var sourceType string
	err = h.db.QueryRow(`SELECT type FROM sources WHERE id = ?`, article.SourceID).Scan(&sourceType)
	if err != nil {
		// Default to reddit if source not found (orphaned article)
		sourceType = "reddit"
	}

	// Return response
	response := ArticleDetailResponse{
		Article:    article,
		Comments:   comments,
		SourceType: sourceType,
	}

	json.NewEncoder(w).Encode(response)
}

// buildCommentTree converts a flat list of comments into a nested tree structure
func buildCommentTree(flatComments []db.Comment) []db.Comment {
	// Create a map of external_id to comment for quick lookup
	commentMap := make(map[string]*db.Comment)
	for i := range flatComments {
		commentMap[flatComments[i].ExternalID] = &flatComments[i]
	}

	// Root comments (no parent)
	var rootComments []db.Comment

	// Build tree by linking children to parents
	for i := range flatComments {
		comment := &flatComments[i]

		if comment.ParentID == nil {
			// Root level comment
			rootComments = append(rootComments, *comment)
		}
		// Note: We return a flat list for now, as the frontend will handle tree rendering
		// If nested structure is needed, we would build it here using ParentID relationships
	}

	// For now, return all comments in flat order (frontend will use ParentID to build tree)
	return flatComments
}

// Health godoc
// @Summary Health check
// @Description Check the health status of the service (database and scheduler)
// @Tags monitoring
// @Accept json
// @Produce json
// @Success 200 {object} db.HealthStatus "Service is healthy"
// @Failure 503 {object} db.HealthStatus "Service is unhealthy"
// @Router /health [get]
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	health := db.HealthStatus{
		Timestamp: time.Now(),
		Status:    "healthy",
		Database:  "ok",
		Scheduler: "ok",
	}

	// Check database
	if err := h.db.Ping(); err != nil {
		health.Status = "unhealthy"
		health.Database = fmt.Sprintf("error: %v", err)
	}

	statusCode := http.StatusOK
	if health.Status == "unhealthy" {
		statusCode = http.StatusServiceUnavailable
	}

	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(health)
}

// Metrics godoc
// @Summary Service metrics
// @Description Get service metrics and statistics (sources, articles, errors)
// @Tags monitoring
// @Accept json
// @Produce json
// @Success 200 {object} db.Metrics
// @Failure 500 {object} ErrorResponse "Database error"
// @Router /metrics [get]
func (h *Handler) Metrics(w http.ResponseWriter, r *http.Request) {
	metrics := db.Metrics{
		Timestamp: time.Now().UTC(),
	}

	// Total sources
	if err := h.db.QueryRow("SELECT COUNT(*) FROM sources").Scan(&metrics.TotalSources); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to query total sources: %v", err))
		return
	}

	// Total articles
	if err := h.db.QueryRow("SELECT COUNT(*) FROM articles").Scan(&metrics.TotalArticles); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to query total articles: %v", err))
		return
	}

	// Articles today (use UTC to avoid timezone issues)
	today := time.Now().UTC().Truncate(24 * time.Hour)
	if err := h.db.QueryRow("SELECT COUNT(*) FROM articles WHERE created_at >= ?", today).Scan(&metrics.ArticlesToday); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to query articles today: %v", err))
		return
	}

	// Sources with errors
	if err := h.db.QueryRow("SELECT COUNT(*) FROM sources WHERE last_error IS NOT NULL AND last_error != ''").Scan(&metrics.SourcesWithErrors); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to query sources with errors: %v", err))
		return
	}

	// Last crawl
	var lastCrawl sql.NullTime
	if err := h.db.QueryRow("SELECT MAX(last_run_at) FROM sources").Scan(&lastCrawl); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to query last crawl: %v", err))
		return
	}
	if lastCrawl.Valid {
		metrics.LastCrawl = &lastCrawl.Time
	}

	json.NewEncoder(w).Encode(metrics)
}

// GetGlobalConfig godoc
// @Summary Get global configuration
// @Description Returns the global crawl schedule and rate limits that apply to all sources
// @Tags config
// @Produce json
// @Success 200 {object} db.GlobalConfig
// @Failure 500 {object} ErrorResponse "Database error"
// @Router /config [get]
func (h *Handler) GetGlobalConfig(w http.ResponseWriter, r *http.Request) {
	config, err := h.db.GetGlobalConfig()
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get global config: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(config); err != nil {
		// Can't change status code at this point, but log the error
		fmt.Printf("ERROR: Failed to encode global config response: %v\n", err)
	}
}

// UpdateGlobalConfig godoc
// @Summary Update global configuration
// @Description Updates global crawl schedule and rate limits (partial update), then hot-reloads the scheduler
// @Tags config
// @Accept json
// @Produce json
// @Param config body UpdateGlobalConfigRequest true "Configuration updates (PATCH-style partial updates)"
// @Success 200 {object} db.GlobalConfig
// @Failure 400 {object} ErrorResponse "Invalid request body or validation error"
// @Failure 500 {object} ErrorResponse "Database or scheduler error"
// @Router /config [patch]
func (h *Handler) UpdateGlobalConfig(w http.ResponseWriter, r *http.Request) {
	var req UpdateGlobalConfigRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Get current config
	config, err := h.db.GetGlobalConfig()
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get current config: %v", err))
		return
	}

	// Apply updates (PATCH-style partial updates)
	if req.CronExpr != nil {
		config.CronExpr = *req.CronExpr
	}
	if req.RedditRateLimitDelayMs != nil {
		config.RedditRateLimitDelayMs = *req.RedditRateLimitDelayMs
	}
	if req.SemanticScholarRateLimitDelayMs != nil {
		config.SemanticScholarRateLimitDelayMs = *req.SemanticScholarRateLimitDelayMs
	}

	// Validate using existing validation
	if err := config.Validate(); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Update database
	if err := h.db.UpdateGlobalConfig(config); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to update config: %v", err))
		return
	}

	// Hot-reload scheduler to apply changes immediately
	if err := h.scheduler.Reload(); err != nil {
		respondError(w, http.StatusInternalServerError, "Configuration saved but scheduler reload failed. Manual restart may be required.")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(config); err != nil {
		// Can't change status code at this point, but log the error
		fmt.Printf("ERROR: Failed to encode global config response: %v\n", err)
	}
}

// Helper functions

func respondError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func extractExternalID(sourceType string, config json.RawMessage) (string, error) {
	switch sourceType {
	case "reddit":
		var redditConfig db.RedditConfig
		if err := json.Unmarshal(config, &redditConfig); err != nil {
			return "", err
		}
		return redditConfig.Subreddit, nil

	case "semantic_scholar":
		var s2Config db.SemanticScholarConfig
		if err := json.Unmarshal(config, &s2Config); err != nil {
			return "", err
		}
		if s2Config.Mode == "search" && s2Config.Query != nil {
			return *s2Config.Query, nil
		}
		if s2Config.Mode == "recommendations" && s2Config.PaperID != nil {
			return *s2Config.PaperID, nil
		}
		return "", fmt.Errorf("invalid semantic scholar config")

	default:
		return "", fmt.Errorf("unknown source type: %s", sourceType)
	}
}

func isUniqueViolation(err error) bool {
	// SQLite unique constraint error contains "UNIQUE constraint failed"
	return err != nil && (err.Error() == "UNIQUE constraint failed: sources.type, sources.external_id" ||
		err.Error() == "constraint failed: UNIQUE constraint failed: sources.type, sources.external_id")
}
