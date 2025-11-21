package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cheolwanpark/meows/collector/internal/db"
	"github.com/cheolwanpark/meows/collector/internal/personalization"
	"github.com/cheolwanpark/meows/collector/internal/scheduler"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Handler holds dependencies for HTTP handlers
type Handler struct {
	db             *db.DB
	scheduler      *scheduler.Scheduler
	profileService *personalization.UpdateService
}

// NewHandler creates a new Handler
func NewHandler(database *db.DB, sched *scheduler.Scheduler, profService *personalization.UpdateService) *Handler {
	return &Handler{
		db:             database,
		scheduler:      sched,
		profileService: profService,
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

	// Validate profile_id
	if req.ProfileID == "" {
		respondError(w, http.StatusBadRequest, "profile_id is required")
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
		ProfileID:  req.ProfileID,
		Status:     "idle",
		CreatedAt:  time.Now(),
	}

	// Insert into database
	_, err = h.db.Exec(`
		INSERT INTO sources (id, type, config, external_id, profile_id, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, source.ID, source.Type, source.Config, source.ExternalID, source.ProfileID, source.Status, source.CreatedAt)

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
	if err := json.NewEncoder(w).Encode(toSourceResponse(source)); err != nil {
		slog.Error("Failed to encode response", "error", err)
	}
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
	profileID := r.URL.Query().Get("profile_id")

	// Require profile_id for authorization
	if profileID == "" {
		respondError(w, http.StatusBadRequest, "profile_id is required")
		return
	}

	query := "SELECT id, type, config, external_id, last_run_at, last_success_at, last_error, status, created_at FROM sources WHERE profile_id = ?"
	args := []interface{}{profileID}

	if sourceType != "" {
		query += " AND type = ?"
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

	if err := json.NewEncoder(w).Encode(sources); err != nil {
		slog.Error("Failed to encode sources response", "error", err)
	}
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
	profileID := r.URL.Query().Get("profile_id")

	// Require profile_id for authorization
	if profileID == "" {
		respondError(w, http.StatusBadRequest, "profile_id is required")
		return
	}

	var src db.Source
	var lastRunAt, lastSuccessAt sql.NullTime
	var lastError, externalID sql.NullString

	err := h.db.QueryRow(`
		SELECT id, type, config, external_id, last_run_at, last_success_at, last_error, status, created_at
		FROM sources WHERE id = ? AND profile_id = ?
	`, id, profileID).Scan(
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

	if err := json.NewEncoder(w).Encode(toSourceResponse(&src)); err != nil {
		slog.Error("Failed to encode source response", "error", err)
	}
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
	profileID := r.URL.Query().Get("profile_id")

	// Require profile_id for authorization
	if profileID == "" {
		respondError(w, http.StatusBadRequest, "profile_id is required")
		return
	}

	var req UpdateSourceRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Fetch existing source with profile ownership check
	var src db.Source
	var lastRunAt, lastSuccessAt sql.NullTime
	var lastError, externalID sql.NullString

	err := h.db.QueryRow("SELECT id, type, config, external_id, last_run_at, last_success_at, last_error, status, created_at FROM sources WHERE id = ? AND profile_id = ?", id, profileID).
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

	// Update database with profile ownership check
	result, err := h.db.Exec("UPDATE sources SET config = ?, external_id = ? WHERE id = ? AND profile_id = ?", src.Config, src.ExternalID, id, profileID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to update source: %v", err))
		return
	}

	// Check if any rows were affected
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to verify update: %v", err))
		return
	}

	if rowsAffected == 0 {
		respondError(w, http.StatusNotFound, "source not found")
		return
	}

	// No need to register job - scheduler runs all sources on global schedule

	if err := json.NewEncoder(w).Encode(toSourceResponse(&src)); err != nil {
		slog.Error("Failed to encode source response", "error", err)
	}
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
	profileID := r.URL.Query().Get("profile_id")

	// Require profile_id for authorization
	if profileID == "" {
		respondError(w, http.StatusBadRequest, "profile_id is required")
		return
	}

	// No need to unregister job - scheduler loads all sources dynamically

	// Delete from database (cascade deletes articles and comments) with profile ownership check
	result, err := h.db.Exec("DELETE FROM sources WHERE id = ? AND profile_id = ?", id, profileID)
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
	profileID := r.URL.Query().Get("profile_id")

	// Require profile_id for authorization
	if profileID == "" {
		respondError(w, http.StatusBadRequest, "profile_id is required")
		return
	}

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

	// No need to unregister job - scheduler loads all sources dynamically

	// Delete directly from database with profile ownership check (cascade deletes articles and comments)
	result, err := h.db.Exec("DELETE FROM sources WHERE type = ? AND external_id = ? AND profile_id = ?", sourceType, externalID, profileID)
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

// TriggerSource godoc
// @Summary Trigger immediate crawl for a single source
// @Description Manually triggers an immediate fetch for the specified source (fire-and-forget)
// @Tags sources
// @Accept json
// @Produce json
// @Param id path string true "Source ID (UUID)"
// @Success 202 {object} map[string]string "Crawl triggered successfully"
// @Failure 404 {object} ErrorResponse "Source not found"
// @Failure 409 {object} ErrorResponse "Source is already running"
// @Failure 500 {object} ErrorResponse "Failed to trigger crawl"
// @Router /sources/{id}/trigger [post]
func (h *Handler) TriggerSource(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	profileID := r.URL.Query().Get("profile_id")

	// Require profile_id for authorization
	if profileID == "" {
		respondError(w, http.StatusBadRequest, "profile_id is required")
		return
	}

	// Atomically claim the source by updating status only if not already running
	// Include profile ownership check
	result, err := h.db.Exec(`
		UPDATE sources
		SET status = 'running'
		WHERE id = ? AND status != 'running' AND profile_id = ?
	`, id, profileID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to update status: %v", err))
		return
	}

	// Check if update actually happened (0 rows = either not found, already running, or not owned)
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to check rows affected: %v", err))
		return
	}

	if rowsAffected == 0 {
		// Either source doesn't exist, is already running, or not owned - check which
		var exists bool
		err := h.db.QueryRow("SELECT 1 FROM sources WHERE id = ? AND profile_id = ?", id, profileID).Scan(&exists)
		if err == sql.ErrNoRows {
			respondError(w, http.StatusNotFound, "source not found")
			return
		}
		if err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to check source existence: %v", err))
			return
		}
		// Source exists but status was already 'running'
		respondError(w, http.StatusConflict, "source is already running")
		return
	}

	// Fetch source details for the goroutine with profile ownership check
	var src db.Source
	var lastRunAt, lastSuccessAt sql.NullTime
	var lastError, externalID sql.NullString

	err = h.db.QueryRow(`
		SELECT id, type, config, external_id, profile_id, last_run_at, last_success_at,
		       last_error, status, created_at
		FROM sources WHERE id = ? AND profile_id = ?
	`, id, profileID).Scan(
		&src.ID, &src.Type, &src.Config, &externalID, &src.ProfileID,
		&lastRunAt, &lastSuccessAt, &lastError, &src.Status, &src.CreatedAt,
	)

	if err != nil {
		// Revert status update since we can't proceed - best effort
		if _, err := h.db.Exec("UPDATE sources SET status = 'idle' WHERE id = ? AND profile_id = ?", id, profileID); err != nil {
			slog.Error("Failed to revert source status after error", "id", id, "error", err)
		}
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to fetch source: %v", err))
		return
	}

	// Populate nullable fields
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

	// Trigger async crawl (fire-and-forget)
	go h.scheduler.RunSingleSource(&src)

	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(map[string]string{
		"message": "crawl triggered",
	}); err != nil {
		slog.Error("Failed to encode response", "error", err)
	}
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

	if err := json.NewEncoder(w).Encode(schedule); err != nil {
		slog.Error("Failed to encode schedule response", "error", err)
	}
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
	profileID := r.URL.Query().Get("profile_id")
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

	// Build query - include like information if profile_id provided
	var query string
	args := []interface{}{}

	if profileID != "" {
		// Include like status and scope to profile
		query = `
			SELECT a.id, a.source_id, a.external_id, a.profile_id, a.title, a.author,
			       a.content, a.url, a.written_at, a.metadata, a.created_at,
			       CASE WHEN l.id IS NOT NULL THEN 1 ELSE 0 END as liked,
			       COALESCE(l.id, '') as like_id
			FROM articles a
			LEFT JOIN likes l ON a.id = l.article_id AND l.profile_id = ?
			WHERE a.profile_id = ?
		`
		args = append(args, profileID, profileID)
	} else {
		// No like status, no profile filter
		query = `
			SELECT id, source_id, external_id, profile_id, title, author, content, url, written_at, metadata, created_at
			FROM articles
			WHERE 1=1
		`
	}

	if sourceID != "" {
		if profileID != "" {
			query += " AND a.source_id = ?"
		} else {
			query += " AND source_id = ?"
		}
		args = append(args, sourceID)
	}

	if since != nil {
		if profileID != "" {
			query += " AND a.written_at >= ?"
		} else {
			query += " AND written_at >= ?"
		}
		args = append(args, *since)
	}

	if profileID != "" {
		query += " ORDER BY a.written_at DESC LIMIT ? OFFSET ?"
	} else {
		query += " ORDER BY written_at DESC LIMIT ? OFFSET ?"
	}
	args = append(args, limit, offset)

	rows, err := h.db.Query(query, args...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to query articles: %v", err))
		return
	}
	defer rows.Close()

	if profileID != "" {
		// Return articles with like status
		articlesWithLikes := []ArticleWithLikeStatus{}
		for rows.Next() {
			var article ArticleWithLikeStatus
			var url, metadata sql.NullString
			var liked int

			err := rows.Scan(
				&article.ID,
				&article.SourceID,
				&article.ExternalID,
				&article.ProfileID,
				&article.Title,
				&article.Author,
				&article.Content,
				&url,
				&article.WrittenAt,
				&metadata,
				&article.CreatedAt,
				&liked,
				&article.LikeID,
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
			article.Liked = (liked == 1)

			articlesWithLikes = append(articlesWithLikes, article)
		}

		if err := rows.Err(); err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("error iterating articles: %v", err))
			return
		}

		if err := json.NewEncoder(w).Encode(articlesWithLikes); err != nil {
			slog.Error("Failed to encode articles response", "error", err)
		}
	} else {
		// Return regular articles without like status
		articles := []db.Article{}
		for rows.Next() {
			var article db.Article
			var url, metadata sql.NullString

			err := rows.Scan(
				&article.ID,
				&article.SourceID,
				&article.ExternalID,
				&article.ProfileID,
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

		if err := json.NewEncoder(w).Encode(articles); err != nil {
			slog.Error("Failed to encode articles response", "error", err)
		}
	}
}

// ArticleDetailResponse represents an article with its comments
type ArticleDetailResponse struct {
	Article    ArticleWithLikeStatus `json:"article"`
	Comments   []db.Comment          `json:"comments"`
	SourceType string                `json:"source_type"` // "reddit" or "semantic_scholar"
}

// GetArticle godoc
// @Summary Get article detail with comments
// @Description Get a specific article by ID with all its comments in nested tree structure
// @Tags articles
// @Accept json
// @Produce json
// @Param id path string true "Article ID (UUID)"
// @Param profile_id query string false "Profile ID for like status"
// @Success 200 {object} ArticleDetailResponse
// @Failure 400 {object} ErrorResponse "Invalid article ID format"
// @Failure 404 {object} ErrorResponse "Article not found"
// @Failure 500 {object} ErrorResponse "Database error"
// @Router /articles/{id} [get]
func (h *Handler) GetArticle(w http.ResponseWriter, r *http.Request) {
	articleID := chi.URLParam(r, "id")
	profileID := r.URL.Query().Get("profile_id")

	// Validate UUID format
	if _, err := uuid.Parse(articleID); err != nil {
		respondError(w, http.StatusBadRequest, "invalid article ID format")
		return
	}

	// Query article with like status
	var article ArticleWithLikeStatus
	var url, metadata sql.NullString
	var liked int

	if profileID != "" {
		// Validate profile_id UUID format
		if _, err := uuid.Parse(profileID); err != nil {
			respondError(w, http.StatusBadRequest, "invalid profile_id format")
			return
		}

		// Query with like status for specific profile
		err := h.db.QueryRow(`
			SELECT a.id, a.source_id, a.external_id, a.profile_id, a.title, a.author, a.content, a.url, a.written_at, a.metadata, a.created_at,
			       CASE WHEN l.id IS NOT NULL THEN 1 ELSE 0 END as liked,
			       COALESCE(l.id, '') as like_id
			FROM articles a
			LEFT JOIN likes l ON a.id = l.article_id AND l.profile_id = ?
			WHERE a.id = ?
		`, profileID, articleID).Scan(
			&article.ID,
			&article.SourceID,
			&article.ExternalID,
			&article.ProfileID,
			&article.Title,
			&article.Author,
			&article.Content,
			&url,
			&article.WrittenAt,
			&metadata,
			&article.CreatedAt,
			&liked,
			&article.LikeID,
		)
		if err != nil {
			if err == sql.ErrNoRows {
				respondError(w, http.StatusNotFound, "article not found")
				return
			}
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to query article: %v", err))
			return
		}
		article.Liked = liked == 1
	} else {
		// Query without like status
		err := h.db.QueryRow(`
			SELECT id, source_id, external_id, profile_id, title, author, content, url, written_at, metadata, created_at
			FROM articles
			WHERE id = ?
		`, articleID).Scan(
			&article.ID,
			&article.SourceID,
			&article.ExternalID,
			&article.ProfileID,
			&article.Title,
			&article.Author,
			&article.Content,
			&url,
			&article.WrittenAt,
			&metadata,
			&article.CreatedAt,
		)
		if err != nil {
			if err == sql.ErrNoRows {
				respondError(w, http.StatusNotFound, "article not found")
				return
			}
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to query article: %v", err))
			return
		}
		article.Liked = false
		article.LikeID = ""
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

	// Return comments in flat order - frontend will use ParentID to build tree
	comments := flatComments

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

	if err := json.NewEncoder(w).Encode(response); err != nil {
		slog.Error("Failed to encode article detail response", "error", err)
	}
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
	if err := json.NewEncoder(w).Encode(health); err != nil {
		slog.Error("Failed to encode health response", "error", err)
	}
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

	if err := json.NewEncoder(w).Encode(metrics); err != nil {
		slog.Error("Failed to encode metrics response", "error", err)
	}
}

// Note: Global config endpoints (GET/PATCH /config) removed
// Configuration is now file-based (.config.yaml) and requires service restart to apply changes

// CreateProfile godoc
// @Summary Create a new profile
// @Description Create a new profile with AI-generated character (async)
// @Tags profiles
// @Accept json
// @Produce json
// @Param profile body CreateProfileRequest true "Profile data"
// @Success 201 {object} db.Profile
// @Failure 400 {object} ErrorResponse "Invalid request body"
// @Failure 500 {object} ErrorResponse "Database error"
// @Router /profiles [post]
func (h *Handler) CreateProfile(w http.ResponseWriter, r *http.Request) {
	var req CreateProfileRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Nickname == "" {
		respondError(w, http.StatusBadRequest, "nickname is required")
		return
	}

	// Create profile
	profile := &db.Profile{
		ID:              uuid.New().String(),
		Nickname:        req.Nickname,
		UserDescription: req.UserDescription,
		CharacterStatus: "pending",
		Milestone:       "init",
		CreatedAt:       time.Now(),
	}

	// Insert into database
	_, err := h.db.Exec(`
		INSERT INTO profiles (id, nickname, user_description, character_status, milestone, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, profile.ID, profile.Nickname, profile.UserDescription, profile.CharacterStatus, profile.Milestone, profile.CreatedAt)

	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create profile: %v", err))
		return
	}

	// Trigger character generation asynchronously
	h.profileService.UpdateCharacter(r.Context(), profile.ID)

	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(profile); err != nil {
		slog.Error("Failed to encode profile response", "error", err)
	}
}

// ListProfiles godoc
// @Summary List all profiles
// @Description Get all profiles
// @Tags profiles
// @Accept json
// @Produce json
// @Success 200 {array} db.Profile
// @Failure 500 {object} ErrorResponse "Database error"
// @Router /profiles [get]
func (h *Handler) ListProfiles(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(`
		SELECT id, nickname, user_description, character, character_status,
		       character_error, milestone, updated_at, created_at
		FROM profiles
		ORDER BY created_at DESC
	`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to query profiles: %v", err))
		return
	}
	defer rows.Close()

	var profiles []db.Profile
	for rows.Next() {
		var profile db.Profile
		err := rows.Scan(
			&profile.ID, &profile.Nickname, &profile.UserDescription, &profile.Character,
			&profile.CharacterStatus, &profile.CharacterError, &profile.Milestone,
			&profile.UpdatedAt, &profile.CreatedAt,
		)
		if err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to scan profile: %v", err))
			return
		}
		profiles = append(profiles, profile)
	}

	if profiles == nil {
		profiles = []db.Profile{}
	}

	if err := json.NewEncoder(w).Encode(profiles); err != nil {
		slog.Error("Failed to encode profiles response", "error", err)
	}
}

// GetProfile godoc
// @Summary Get a profile by ID
// @Description Get a specific profile
// @Tags profiles
// @Accept json
// @Produce json
// @Param id path string true "Profile ID"
// @Success 200 {object} db.Profile
// @Failure 404 {object} ErrorResponse "Profile not found"
// @Failure 500 {object} ErrorResponse "Database error"
// @Router /profiles/{id} [get]
func (h *Handler) GetProfile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var profile db.Profile
	err := h.db.QueryRow(`
		SELECT id, nickname, user_description, character, character_status,
		       character_error, milestone, updated_at, created_at
		FROM profiles WHERE id = ?
	`, id).Scan(
		&profile.ID, &profile.Nickname, &profile.UserDescription, &profile.Character,
		&profile.CharacterStatus, &profile.CharacterError, &profile.Milestone,
		&profile.UpdatedAt, &profile.CreatedAt,
	)

	if err == sql.ErrNoRows {
		respondError(w, http.StatusNotFound, "profile not found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to query profile: %v", err))
		return
	}

	if err := json.NewEncoder(w).Encode(profile); err != nil {
		slog.Error("Failed to encode profile response", "error", err)
	}
}

// ProfileStatusResponse represents the status of character generation
type ProfileStatusResponse struct {
	CharacterStatus string  `json:"character_status"`
	CharacterError  *string `json:"character_error,omitempty"`
}

// GetProfileStatus godoc
// @Summary Get profile character generation status
// @Description Get only the character generation status for efficient polling
// @Tags profiles
// @Produce json
// @Param id path string true "Profile ID"
// @Success 200 {object} ProfileStatusResponse
// @Failure 404 {object} ErrorResponse "Profile not found"
// @Failure 500 {object} ErrorResponse "Database error"
// @Router /profiles/{id}/status [get]
func (h *Handler) GetProfileStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var status ProfileStatusResponse
	err := h.db.QueryRow(`
		SELECT character_status, character_error
		FROM profiles WHERE id = ?
	`, id).Scan(&status.CharacterStatus, &status.CharacterError)

	if err == sql.ErrNoRows {
		respondError(w, http.StatusNotFound, "profile not found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to query profile status: %v", err))
		return
	}

	if err := json.NewEncoder(w).Encode(status); err != nil {
		slog.Error("Failed to encode status response", "error", err)
	}
}

// UpdateProfile godoc
// @Summary Update a profile
// @Description Update profile nickname and/or user description
// @Tags profiles
// @Accept json
// @Produce json
// @Param id path string true "Profile ID"
// @Param profile body UpdateProfileRequest true "Profile updates"
// @Success 200 {object} db.Profile
// @Failure 400 {object} ErrorResponse "Invalid request body"
// @Failure 404 {object} ErrorResponse "Profile not found"
// @Failure 500 {object} ErrorResponse "Database error"
// @Router /profiles/{id} [patch]
func (h *Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req UpdateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Check if profile exists and get current description
	var currentDesc string
	err := h.db.QueryRow("SELECT user_description FROM profiles WHERE id = ?", id).Scan(&currentDesc)
	if err == sql.ErrNoRows {
		respondError(w, http.StatusNotFound, "profile not found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to query profile: %v", err))
		return
	}

	// Build update query
	updates := []string{}
	args := []interface{}{}
	descriptionChanged := false

	if req.Nickname != nil {
		updates = append(updates, "nickname = ?")
		args = append(args, *req.Nickname)
	}

	if req.UserDescription != nil {
		if *req.UserDescription != currentDesc {
			descriptionChanged = true
		}
		updates = append(updates, "user_description = ?")
		args = append(args, *req.UserDescription)
	}

	if len(updates) == 0 {
		respondError(w, http.StatusBadRequest, "no updates provided")
		return
	}

	// Execute update
	args = append(args, id)
	query := fmt.Sprintf("UPDATE profiles SET %s WHERE id = ?", strings.Join(updates, ", "))
	_, err = h.db.Exec(query, args...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to update profile: %v", err))
		return
	}

	// Trigger character regeneration if description changed
	if descriptionChanged {
		h.profileService.UpdateCharacter(r.Context(), id)
	}

	// Fetch updated profile
	var profile db.Profile
	err = h.db.QueryRow(`
		SELECT id, nickname, user_description, character, character_status,
		       character_error, milestone, updated_at, created_at
		FROM profiles WHERE id = ?
	`, id).Scan(
		&profile.ID, &profile.Nickname, &profile.UserDescription, &profile.Character,
		&profile.CharacterStatus, &profile.CharacterError, &profile.Milestone,
		&profile.UpdatedAt, &profile.CreatedAt,
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to query updated profile: %v", err))
		return
	}

	if err := json.NewEncoder(w).Encode(profile); err != nil {
		slog.Error("Failed to encode profile response", "error", err)
	}
}

// DeleteProfile godoc
// @Summary Delete a profile
// @Description Delete a profile and all associated data
// @Tags profiles
// @Accept json
// @Produce json
// @Param id path string true "Profile ID"
// @Success 204 "No content"
// @Failure 404 {object} ErrorResponse "Profile not found"
// @Failure 500 {object} ErrorResponse "Database error"
// @Router /profiles/{id} [delete]
func (h *Handler) DeleteProfile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	result, err := h.db.Exec("DELETE FROM profiles WHERE id = ?", id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to delete profile: %v", err))
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		respondError(w, http.StatusNotFound, "profile not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// LikeArticle godoc
// @Summary Like an article
// @Description Create a like for an article, triggers character update if milestone reached
// @Tags likes
// @Accept json
// @Produce json
// @Param id path string true "Article ID"
// @Param like body CreateLikeRequest true "Like data"
// @Success 201 {object} db.Like
// @Failure 400 {object} ErrorResponse "Invalid request body"
// @Failure 404 {object} ErrorResponse "Article not found"
// @Failure 409 {object} ErrorResponse "Already liked"
// @Failure 500 {object} ErrorResponse "Database error"
// @Router /articles/{id}/like [post]
func (h *Handler) LikeArticle(w http.ResponseWriter, r *http.Request) {
	articleID := chi.URLParam(r, "id")

	var req CreateLikeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.ProfileID == "" {
		respondError(w, http.StatusBadRequest, "profile_id is required")
		return
	}

	// Check if article exists (before transaction to avoid holding locks)
	var exists bool
	err := h.db.QueryRow("SELECT EXISTS(SELECT 1 FROM articles WHERE id = ?)", articleID).Scan(&exists)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to check article: %v", err))
		return
	}
	if !exists {
		respondError(w, http.StatusNotFound, "article not found")
		return
	}

	// Create like
	like := &db.Like{
		ID:        uuid.New().String(),
		ProfileID: req.ProfileID,
		ArticleID: articleID,
		CreatedAt: time.Now(),
	}

	// Begin transaction to prevent race conditions on milestone updates
	tx, err := h.db.Begin()
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to begin transaction: %v", err))
		return
	}

	// Use a flag to track if transaction was committed
	committed := false
	defer func() {
		if !committed {
			if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
				slog.Error("Failed to rollback transaction", "error", err)
			}
		}
	}()

	// Insert like within transaction
	_, err = tx.Exec(`
		INSERT INTO likes (id, profile_id, article_id, created_at)
		VALUES (?, ?, ?, ?)
	`, like.ID, like.ProfileID, like.ArticleID, like.CreatedAt)

	if err != nil {
		if isLikeUniqueViolation(err) {
			respondError(w, http.StatusConflict, "article already liked")
			return
		}
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create like: %v", err))
		return
	}

	// Check milestone within same transaction
	// Use SELECT FOR UPDATE to lock the profile row and prevent concurrent milestone updates
	var likeCount int
	var currentMilestone string

	// Lock the profile row to prevent race conditions on milestone updates
	// This ensures only one like request can check/update milestones at a time per profile
	err = tx.QueryRow(`
		SELECT milestone FROM profiles WHERE id = ?
		-- Note: SQLite doesn't support FOR UPDATE, but serializable isolation handles this
	`, req.ProfileID).Scan(&currentMilestone)

	var shouldUpdate bool
	var newMilestone string

	if err == nil {
		// Count likes AFTER inserting to get the new count including this like
		// This happens within the transaction, so it's consistent
		err = tx.QueryRow(`
			SELECT COUNT(*) FROM likes WHERE profile_id = ?
		`, req.ProfileID).Scan(&likeCount)

		if err == nil {
			shouldUpdate, newMilestone = h.profileService.CheckMilestone(currentMilestone, likeCount)
			if shouldUpdate {
				// Update milestone within transaction
				_, err = tx.Exec("UPDATE profiles SET milestone = ? WHERE id = ?", newMilestone, req.ProfileID)
				if err != nil {
					respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to update milestone: %v", err))
					return
				}
			}
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to commit transaction: %v", err))
		return
	}
	committed = true

	// Trigger character update AFTER transaction commits (no network I/O during transaction)
	if shouldUpdate {
		h.profileService.UpdateCharacter(r.Context(), req.ProfileID)
	}

	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(like); err != nil {
		slog.Error("Failed to encode like response", "error", err)
	}
}

// UnlikeArticle godoc
// @Summary Unlike an article
// @Description Delete a like
// @Tags likes
// @Accept json
// @Produce json
// @Param id path string true "Like ID"
// @Success 204 "No content"
// @Failure 404 {object} ErrorResponse "Like not found"
// @Failure 500 {object} ErrorResponse "Database error"
// @Router /likes/{id} [delete]
func (h *Handler) UnlikeArticle(w http.ResponseWriter, r *http.Request) {
	likeID := chi.URLParam(r, "id")

	result, err := h.db.Exec("DELETE FROM likes WHERE id = ?", likeID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to delete like: %v", err))
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		respondError(w, http.StatusNotFound, "like not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Helper functions

func respondError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": message}); err != nil {
		slog.Error("Failed to encode error response", "error", err)
	}
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
		err.Error() == "constraint failed: UNIQUE constraint failed: sources.type, sources.external_id, sources.profile_id" ||
		strings.Contains(err.Error(), "UNIQUE constraint failed: sources"))
}

func isLikeUniqueViolation(err error) bool {
	// SQLite unique constraint error for likes
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: likes.profile_id, likes.article_id")
}
