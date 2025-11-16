package db

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// GlobalConfig represents the global collector configuration
// Stored as a singleton row in the global_config table
type GlobalConfig struct {
	ID                              int       `json:"id"`
	CronExpr                        string    `json:"cron_expr"`
	RedditRateLimitDelayMs          int       `json:"reddit_rate_limit_delay_ms"`
	SemanticScholarRateLimitDelayMs int       `json:"semantic_scholar_rate_limit_delay_ms"`
	UpdatedAt                       time.Time `json:"updated_at"`
}

// Validate validates the global configuration
func (gc *GlobalConfig) Validate() error {
	// Validate cron expression
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if _, err := parser.Parse(gc.CronExpr); err != nil {
		return fmt.Errorf("invalid cron expression: %w", err)
	}

	// Validate rate limits
	if gc.RedditRateLimitDelayMs <= 0 {
		return fmt.Errorf("reddit rate limit delay must be positive, got %d", gc.RedditRateLimitDelayMs)
	}
	if gc.SemanticScholarRateLimitDelayMs <= 0 {
		return fmt.Errorf("semantic scholar rate limit delay must be positive, got %d", gc.SemanticScholarRateLimitDelayMs)
	}

	return nil
}

// Source represents a crawling source configuration
// Config field contains per-source settings only (no credentials, no schedule)
// Credentials are now stored in environment variables
// Schedule is now global (see GlobalConfig)
type Source struct {
	ID            string          `json:"id"`
	Type          string          `json:"type"`        // "reddit" or "semantic_scholar"
	Config        json.RawMessage `json:"config"`      // Per-source settings (subreddit, query, filters, etc.)
	ExternalID    string          `json:"external_id"` // For dedup (e.g., subreddit name)
	LastRunAt     *time.Time      `json:"last_run_at,omitempty"`
	LastSuccessAt *time.Time      `json:"last_success_at,omitempty"`
	LastError     string          `json:"last_error,omitempty"`
	Status        string          `json:"status"` // "idle" or "running"
	CreatedAt     time.Time       `json:"created_at"`
}

// Article represents a crawled article
// @Description Crawled article from Reddit or Semantic Scholar
type Article struct {
	ID         string          `json:"id" example:"550e8400-e29b-41d4-a716-446655440000"`
	SourceID   string          `json:"source_id" example:"660e8400-e29b-41d4-a716-446655440001"`
	ExternalID string          `json:"external_id" example:"abc123"` // Reddit post ID / S2 paper ID
	Title      string          `json:"title" example:"Understanding Go Concurrency"`
	Author     string          `json:"author" example:"user123"`
	Content    string          `json:"content" example:"This is the article content..."`
	URL        string          `json:"url,omitempty" example:"https://reddit.com/r/golang/comments/abc123"`
	WrittenAt  time.Time       `json:"written_at" example:"2024-11-15T08:00:00Z"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
	CreatedAt  time.Time       `json:"created_at" example:"2024-11-15T12:00:00Z"`
}

// Comment represents a comment on an article
type Comment struct {
	ID         string    `json:"id"`
	ArticleID  string    `json:"article_id"`
	ExternalID string    `json:"external_id"`
	Author     string    `json:"author"`
	Content    string    `json:"content"`
	WrittenAt  time.Time `json:"written_at"`
	ParentID   *string   `json:"parent_id,omitempty"` // NULL for top-level
	Depth      int       `json:"depth"`               // Reddit comment depth
}

// ScheduleEntry represents a scheduled job in the next 24 hours
// @Description Scheduled crawl job information
type ScheduleEntry struct {
	SourceID   string     `json:"source_id" example:"550e8400-e29b-41d4-a716-446655440000"`
	SourceType string     `json:"source_type" example:"reddit" enums:"reddit,semantic_scholar"`
	NextRun    time.Time  `json:"next_run" example:"2024-11-15T18:00:00Z"`
	LastRunAt  *time.Time `json:"last_run_at,omitempty" example:"2024-11-15T12:00:00Z"`
}

// RedditConfig holds Reddit-specific per-source configuration
// Credentials and rate limits are now global (see GlobalConfig and env vars)
type RedditConfig struct {
	Subreddit   string `json:"subreddit"`
	Sort        string `json:"sort"`                  // "hot", "new", "top", "rising"
	TimeFilter  string `json:"time_filter,omitempty"` // For "top": "hour", "day", "week", "month", "year", "all"
	Limit       int    `json:"limit"`
	MinScore    int    `json:"min_score"`
	MinComments int    `json:"min_comments"`
	UserAgent   string `json:"user_agent"`
}

// SemanticScholarConfig holds Semantic Scholar per-source configuration
// API key and rate limits are now global (see GlobalConfig and env vars)
type SemanticScholarConfig struct {
	Mode         string  `json:"mode"` // "search" or "recommendations"
	Query        *string `json:"query,omitempty"`
	PaperID      *string `json:"paper_id,omitempty"`
	Year         *string `json:"year,omitempty"`
	MaxResults   int     `json:"max_results"`
	MinCitations int     `json:"min_citations"`
}

// HealthStatus represents the health of the service
// @Description Service health status
type HealthStatus struct {
	Status    string    `json:"status" example:"healthy" enums:"healthy,unhealthy"`
	Database  string    `json:"database" example:"ok"`
	Scheduler string    `json:"scheduler" example:"ok"`
	Timestamp time.Time `json:"timestamp" example:"2024-11-15T12:00:00Z"`
}

// Metrics represents service metrics
// @Description Service metrics and statistics
type Metrics struct {
	TotalSources      int        `json:"total_sources" example:"10"`
	TotalArticles     int        `json:"total_articles" example:"1523"`
	ArticlesToday     int        `json:"articles_today" example:"45"`
	SourcesWithErrors int        `json:"sources_with_errors" example:"1"`
	LastCrawl         *time.Time `json:"last_crawl,omitempty" example:"2024-11-15T12:00:00Z"`
	Timestamp         time.Time  `json:"timestamp" example:"2024-11-15T12:05:00Z"`
}
