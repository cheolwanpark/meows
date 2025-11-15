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
	"github.com/robfig/cron/v3"
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
// @Description Add a new source with cron schedule for Reddit or Semantic Scholar
// @Tags sources
// @Accept json
// @Produce json
// @Param source body CreateSourceRequest true "Source configuration"
// @Success 201 {object} SourceResponse
// @Failure 400 {object} ErrorResponse "Invalid request body, type, cron expression, or config"
// @Failure 409 {object} ErrorResponse "Source with this configuration already exists"
// @Failure 500 {object} ErrorResponse "Database or scheduler error"
// @Router /sources [post]
func (h *Handler) CreateSource(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type     string          `json:"type"`
		Config   json.RawMessage `json:"config"`
		CronExpr string          `json:"cron_expr"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate type
	if req.Type != "reddit" && req.Type != "semantic_scholar" {
		respondError(w, http.StatusBadRequest, "type must be 'reddit' or 'semantic_scholar'")
		return
	}

	// Validate cron expression
	if _, err := cron.ParseStandard(req.CronExpr); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid cron expression: %v", err))
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
		CronExpr:   req.CronExpr,
		ExternalID: externalID,
		Status:     "idle",
		CreatedAt:  time.Now(),
	}

	// Insert into database
	_, err = h.db.Exec(`
		INSERT INTO sources (id, type, config, cron_expr, external_id, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, source.ID, source.Type, source.Config, source.CronExpr, source.ExternalID, source.Status, source.CreatedAt)

	if err != nil {
		// Check for unique constraint violation
		if isUniqueViolation(err) {
			respondError(w, http.StatusConflict, "source with this configuration already exists")
			return
		}
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create source: %v", err))
		return
	}

	// Register job in scheduler
	if err := h.scheduler.RegisterJob(source); err != nil {
		// Rollback: delete the source
		h.db.Exec("DELETE FROM sources WHERE id = ?", source.ID)
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to register job: %v", err))
		return
	}

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

	query := "SELECT id, type, config, cron_expr, external_id, last_run_at, last_success_at, last_error, status, created_at FROM sources"
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
			&src.CronExpr,
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
		SELECT id, type, config, cron_expr, external_id, last_run_at, last_success_at, last_error, status, created_at
		FROM sources WHERE id = ?
	`, id).Scan(
		&src.ID,
		&src.Type,
		&src.Config,
		&src.CronExpr,
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
// @Description Update source configuration and/or cron schedule. Jobs are automatically rescheduled.
// @Tags sources
// @Accept json
// @Produce json
// @Param id path string true "Source ID (UUID)"
// @Param source body UpdateSourceRequest true "Updated source configuration"
// @Success 200 {object} SourceResponse
// @Failure 400 {object} ErrorResponse "Invalid request body, config, or cron expression"
// @Failure 404 {object} ErrorResponse "Source not found"
// @Failure 500 {object} ErrorResponse "Database or scheduler error"
// @Router /sources/{id} [put]
func (h *Handler) UpdateSource(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req struct {
		Config   *json.RawMessage `json:"config,omitempty"`
		CronExpr *string          `json:"cron_expr,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Fetch existing source
	var src db.Source
	var lastRunAt, lastSuccessAt sql.NullTime
	var lastError, externalID sql.NullString

	err := h.db.QueryRow("SELECT id, type, config, cron_expr, external_id, last_run_at, last_success_at, last_error, status, created_at FROM sources WHERE id = ?", id).
		Scan(&src.ID, &src.Type, &src.Config, &src.CronExpr, &externalID, &lastRunAt, &lastSuccessAt, &lastError, &src.Status, &src.CreatedAt)

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

	// Update fields
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
	if req.CronExpr != nil {
		// Validate cron expression
		if _, err := cron.ParseStandard(*req.CronExpr); err != nil {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid cron expression: %v", err))
			return
		}
		src.CronExpr = *req.CronExpr
	}

	// Update database
	_, err = h.db.Exec(`
		UPDATE sources SET config = ?, cron_expr = ?, external_id = ? WHERE id = ?
	`, src.Config, src.CronExpr, src.ExternalID, id)

	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to update source: %v", err))
		return
	}

	// Re-register job with new schedule
	if err := h.scheduler.RegisterJob(&src); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to update schedule: %v", err))
		return
	}

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

	// Unregister from scheduler first
	h.scheduler.UnregisterJob(id)

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

	// Unregister from scheduler first (consistent with DeleteSource)
	h.scheduler.UnregisterJob(sourceID)

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
// @Summary Get upcoming scheduled jobs
// @Description Returns all crawl jobs scheduled to run in the next 24 hours
// @Tags schedule
// @Accept json
// @Produce json
// @Success 200 {array} db.ScheduleEntry
// @Failure 500 {object} ErrorResponse "Scheduler error"
// @Router /schedule [get]
func (h *Handler) GetSchedule(w http.ResponseWriter, r *http.Request) {
	schedule, err := h.scheduler.GetSchedule(24 * time.Hour)
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
