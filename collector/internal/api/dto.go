package api

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/cheolwanpark/meows/collector/internal/db"
)

// SourceResponse is a safe DTO for Source that omits sensitive credentials
// This prevents exposing OAuth tokens, API keys, and other secrets in API responses
// Schedule is now global (see GlobalConfig), credentials are in environment variables
// @Description Source response with sanitized configuration (credentials omitted)
type SourceResponse struct {
	ID            string     `json:"id" example:"550e8400-e29b-41d4-a716-446655440000"`
	Type          string     `json:"type" example:"reddit" enums:"reddit,semantic_scholar,hackernews"`
	ConfigSummary string     `json:"config_summary" example:"subreddit: golang, sort: hot, limit: 100"`
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
// Schedule is now global (configured separately)
// @Description Request body for creating a new crawling source
type CreateSourceRequest struct {
	Type      string          `json:"type" example:"reddit" enums:"reddit,semantic_scholar,hackernews"`
	Config    json.RawMessage `json:"config"`
	ProfileID string          `json:"profile_id" example:"550e8400-e29b-41d4-a716-446655440000"`
}

// UpdateSourceRequest represents the request body for updating a source
// @Description Request body for updating an existing source
type UpdateSourceRequest struct {
	Config *json.RawMessage `json:"config,omitempty"`
}

// toSourceResponse converts a db.Source to a safe SourceResponse
// It extracts a non-sensitive summary from the config instead of exposing credentials
func toSourceResponse(src *db.Source) SourceResponse {
	return SourceResponse{
		ID:            src.ID,
		Type:          src.Type,
		ConfigSummary: extractConfigSummary(src.Type, src.Config),
		ExternalID:    src.ExternalID,
		LastRunAt:     src.LastRunAt,
		LastSuccessAt: src.LastSuccessAt,
		LastError:     src.LastError,
		Status:        src.Status,
		CreatedAt:     src.CreatedAt,
	}
}

// extractConfigSummary creates a safe, human-readable summary of the config
// Credentials are now in environment variables, not per-source config
func extractConfigSummary(sourceType string, config json.RawMessage) string {
	switch sourceType {
	case "reddit":
		var redditConfig db.RedditConfig
		if err := json.Unmarshal(config, &redditConfig); err != nil {
			return "invalid config"
		}
		return fmt.Sprintf("subreddit: %s, sort: %s, limit: %d",
			redditConfig.Subreddit, redditConfig.Sort, redditConfig.Limit)

	case "semantic_scholar":
		var s2Config db.SemanticScholarConfig
		if err := json.Unmarshal(config, &s2Config); err != nil {
			return "invalid config"
		}
		if s2Config.Mode == "search" && s2Config.Query != nil {
			return fmt.Sprintf("query: %s, mode: %s, max_results: %d",
				*s2Config.Query, s2Config.Mode, s2Config.MaxResults)
		} else if s2Config.Mode == "recommendations" && s2Config.PaperID != nil {
			return fmt.Sprintf("paper_id: %s, mode: %s, max_results: %d",
				*s2Config.PaperID, s2Config.Mode, s2Config.MaxResults)
		}
		return fmt.Sprintf("mode: %s, max_results: %d",
			s2Config.Mode, s2Config.MaxResults)

	case "hackernews":
		var hnConfig db.HackerNewsConfig
		if err := json.Unmarshal(config, &hnConfig); err != nil {
			return "invalid config"
		}
		comments := "no"
		if hnConfig.IncludeComments {
			comments = "yes"
		}
		return fmt.Sprintf("%s stories (limit: %d, comments: %s)",
			hnConfig.ItemType, hnConfig.Limit, comments)

	default:
		return "unknown type"
	}
}

// CreateProfileRequest represents the request body for creating a new profile
// @Description Request body for creating a new profile
type CreateProfileRequest struct {
	Nickname        string `json:"nickname" example:"Tech Enthusiast"`
	UserDescription string `json:"user_description" example:"I love reading about Go and distributed systems"`
}

// UpdateProfileRequest represents the request body for updating a profile
// @Description Request body for updating a profile
type UpdateProfileRequest struct {
	Nickname        *string `json:"nickname,omitempty" example:"Tech Enthusiast"`
	UserDescription *string `json:"user_description,omitempty" example:"I love reading about Go and distributed systems"`
}

// CreateLikeRequest represents the request body for liking an article
// @Description Request body for liking an article
type CreateLikeRequest struct {
	ProfileID string `json:"profile_id" example:"550e8400-e29b-41d4-a716-446655440000"`
}

// ArticleWithLikeStatus extends db.Article with like status information
// @Description Article with like status for a specific profile
type ArticleWithLikeStatus struct {
	db.Article
	Liked      bool   `json:"liked" example:"true"`
	LikeID     string `json:"like_id,omitempty" example:"550e8400-e29b-41d4-a716-446655440000"`
	SourceType string `json:"source_type" example:"reddit"`
}

// ArticleListResponse represents paginated articles response
// @Description Paginated list of articles with has-more indicator
type ArticleListResponse struct {
	Articles []ArticleWithLikeStatus `json:"articles"`
	HasMore  bool                    `json:"has_more"`
	Limit    int                     `json:"limit"`
	Offset   int                     `json:"offset"`
}
