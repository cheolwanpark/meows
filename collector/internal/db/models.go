package db

import (
	"encoding/json"
	"time"
)

// Source represents a crawling source configuration
// Config field contains per-source settings only (no credentials, no schedule)
// Global credentials and schedule are stored in .config.yaml file
type Source struct {
	ID            string          `json:"id"`
	Type          string          `json:"type"`        // "reddit", "semantic_scholar", or "hackernews"
	Config        json.RawMessage `json:"config"`      // Per-source settings (subreddit, query, filters, etc.)
	ExternalID    string          `json:"external_id"` // For dedup (e.g., subreddit name)
	ProfileID     string          `json:"profile_id"`  // Profile that owns this source
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
	ProfileID  string          `json:"profile_id" example:"770e8400-e29b-41d4-a716-446655440002"`
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

// Profile represents a user profile (Netflix-style)
// @Description User profile with AI-generated character description
type Profile struct {
	ID              string     `json:"id" example:"550e8400-e29b-41d4-a716-446655440000"`
	Nickname        string     `json:"nickname" example:"Tech Enthusiast"`
	UserDescription string     `json:"user_description" example:"I love reading about Go and distributed systems"`
	Character       *string    `json:"character,omitempty" example:"A curious developer who enjoys diving deep into systems programming"`
	CharacterStatus string     `json:"character_status" example:"ready" enums:"pending,ready,updating,error"`
	CharacterError  *string    `json:"character_error,omitempty"`
	Milestone       string     `json:"milestone" example:"init" enums:"init,3,10,20,weekly"`
	UpdatedAt       *time.Time `json:"updated_at,omitempty" example:"2024-11-15T12:00:00Z"`
	CreatedAt       time.Time  `json:"created_at" example:"2024-11-15T12:00:00Z"`
}

// Like represents a profile's like on an article
// @Description Article like by a profile
type Like struct {
	ID        string    `json:"id" example:"550e8400-e29b-41d4-a716-446655440000"`
	ProfileID string    `json:"profile_id" example:"660e8400-e29b-41d4-a716-446655440001"`
	ArticleID string    `json:"article_id" example:"770e8400-e29b-41d4-a716-446655440002"`
	CreatedAt time.Time `json:"created_at" example:"2024-11-15T12:00:00Z"`
}

// ScheduleEntry represents a scheduled job in the next 24 hours
// @Description Scheduled crawl job information
type ScheduleEntry struct {
	SourceID   string     `json:"source_id" example:"550e8400-e29b-41d4-a716-446655440000"`
	SourceType string     `json:"source_type" example:"reddit" enums:"reddit,semantic_scholar,hackernews"`
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

// HackerNewsConfig holds Hacker News per-source configuration
// No API key required (public API). Rate limits are global (see GlobalConfig and env vars)
type HackerNewsConfig struct {
	ItemType              string `json:"item_type"`                // "top", "new", "best", "ask", "show", "job"
	Limit                 int    `json:"limit"`                    // Max story IDs to fetch (1-100)
	MinScore              int    `json:"min_score"`                // Filter by minimum points
	MinComments           int    `json:"min_comments"`             // Filter by minimum descendants
	IncludeComments       bool   `json:"include_comments"`         // Whether to fetch comments
	MaxCommentDepth       int    `json:"max_comment_depth"`        // Max nesting level (0-10)
	MaxCommentsPerArticle int    `json:"max_comments_per_article"` // Max total comments per article (1-500)
	ForceAPIMode          bool   `json:"force_api_mode"`           // Force API-only mode (emergency rollback, default: false)
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
