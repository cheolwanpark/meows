package api

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/cheolwanpark/meows/collector/internal/db"
)

// SourceResponse is a safe DTO for Source that omits sensitive credentials
// This prevents exposing OAuth tokens, API keys, and other secrets in API responses
// @Description Source response with sanitized configuration (credentials omitted)
type SourceResponse struct {
	ID            string     `json:"id" example:"550e8400-e29b-41d4-a716-446655440000"`
	Type          string     `json:"type" example:"reddit" enums:"reddit,semantic_scholar"`
	ConfigSummary string     `json:"config_summary" example:"subreddit: golang, sort: hot, limit: 100, oauth: yes"`
	CronExpr      string     `json:"cron_expr" example:"0 */6 * * *"`
	ExternalID    string     `json:"external_id" example:"golang"`
	LastRunAt     *time.Time `json:"last_run_at,omitempty" example:"2024-11-15T12:00:00Z"`
	LastSuccessAt *time.Time `json:"last_success_at,omitempty" example:"2024-11-15T12:00:00Z"`
	LastError     string     `json:"last_error,omitempty" example:""`
	Status        string     `json:"status" example:"idle" enums:"idle,running"`
	CreatedAt     time.Time  `json:"created_at" example:"2024-11-15T10:00:00Z"`
}

// ErrorResponse represents a standard error response
// @Description Standard error response format
type ErrorResponse struct {
	Error string `json:"error" example:"invalid request body"`
}

// CreateSourceRequest represents the request body for creating a new source
// @Description Request body for creating a new crawling source
type CreateSourceRequest struct {
	Type     string          `json:"type" example:"reddit" enums:"reddit,semantic_scholar"`
	Config   json.RawMessage `json:"config"`
	CronExpr string          `json:"cron_expr" example:"0 */6 * * *"`
}

// UpdateSourceRequest represents the request body for updating a source
// @Description Request body for updating an existing source
type UpdateSourceRequest struct {
	Config   *json.RawMessage `json:"config,omitempty"`
	CronExpr *string          `json:"cron_expr,omitempty" example:"0 */12 * * *"`
}

// toSourceResponse converts a db.Source to a safe SourceResponse
// It extracts a non-sensitive summary from the config instead of exposing credentials
func toSourceResponse(src *db.Source) SourceResponse {
	return SourceResponse{
		ID:            src.ID,
		Type:          src.Type,
		ConfigSummary: extractConfigSummary(src.Type, src.Config),
		CronExpr:      src.CronExpr,
		ExternalID:    src.ExternalID,
		LastRunAt:     src.LastRunAt,
		LastSuccessAt: src.LastSuccessAt,
		LastError:     src.LastError,
		Status:        src.Status,
		CreatedAt:     src.CreatedAt,
	}
}

// extractConfigSummary creates a safe, human-readable summary of the config
// without exposing sensitive credentials
func extractConfigSummary(sourceType string, config json.RawMessage) string {
	switch sourceType {
	case "reddit":
		var redditConfig db.RedditConfig
		if err := json.Unmarshal(config, &redditConfig); err != nil {
			return "invalid config"
		}
		oauth := "no"
		if redditConfig.OAuth != nil {
			oauth = "yes"
		}
		return fmt.Sprintf("subreddit: %s, sort: %s, limit: %d, oauth: %s",
			redditConfig.Subreddit, redditConfig.Sort, redditConfig.Limit, oauth)

	case "semantic_scholar":
		var s2Config db.SemanticScholarConfig
		if err := json.Unmarshal(config, &s2Config); err != nil {
			return "invalid config"
		}
		hasAPIKey := "no"
		if s2Config.APIKey != "" {
			hasAPIKey = "yes"
		}
		if s2Config.Mode == "search" && s2Config.Query != nil {
			return fmt.Sprintf("query: %s, mode: %s, max_results: %d, api_key: %s",
				*s2Config.Query, s2Config.Mode, s2Config.MaxResults, hasAPIKey)
		} else if s2Config.Mode == "recommendations" && s2Config.PaperID != nil {
			return fmt.Sprintf("paper_id: %s, mode: %s, max_results: %d, api_key: %s",
				*s2Config.PaperID, s2Config.Mode, s2Config.MaxResults, hasAPIKey)
		}
		return fmt.Sprintf("mode: %s, max_results: %d, api_key: %s",
			s2Config.Mode, s2Config.MaxResults, hasAPIKey)

	default:
		return "unknown type"
	}
}
