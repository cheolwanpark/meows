package db

import (
	"encoding/json"
	"time"
)

// Source represents a crawling source configuration
// WARNING: Config field may contain sensitive credentials (OAuth tokens, API keys)
// which are exposed in API responses. Add authentication to API before production use.
type Source struct {
	ID            string          `json:"id"`
	Type          string          `json:"type"` // "reddit" or "semantic_scholar"
	Config        json.RawMessage `json:"config"`
	CronExpr      string          `json:"cron_expr"`
	ExternalID    string          `json:"external_id"` // For dedup (e.g., subreddit name)
	LastRunAt     *time.Time      `json:"last_run_at,omitempty"`
	LastSuccessAt *time.Time      `json:"last_success_at,omitempty"`
	LastError     string          `json:"last_error,omitempty"`
	Status        string          `json:"status"` // "idle" or "running"
	CreatedAt     time.Time       `json:"created_at"`
}

// Article represents a crawled article
type Article struct {
	ID         string          `json:"id"`
	SourceID   string          `json:"source_id"`
	ExternalID string          `json:"external_id"` // Reddit post ID / S2 paper ID
	Title      string          `json:"title"`
	Author     string          `json:"author"`
	Content    string          `json:"content"`
	URL        string          `json:"url,omitempty"`
	WrittenAt  time.Time       `json:"written_at"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
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
type ScheduleEntry struct {
	SourceID   string     `json:"source_id"`
	SourceType string     `json:"source_type"`
	NextRun    time.Time  `json:"next_run"`
	LastRunAt  *time.Time `json:"last_run_at,omitempty"`
}

// RedditConfig holds Reddit-specific configuration
type RedditConfig struct {
	Subreddit        string `json:"subreddit"`
	Sort             string `json:"sort"`                  // "hot", "new", "top", "rising"
	TimeFilter       string `json:"time_filter,omitempty"` // For "top": "hour", "day", "week", "month", "year", "all"
	Limit            int    `json:"limit"`
	MinScore         int    `json:"min_score"`
	MinComments      int    `json:"min_comments"`
	UserAgent        string `json:"user_agent"`
	RateLimitDelayMs int    `json:"rate_limit_delay_ms"`
	OAuth            *struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		Username     string `json:"username"`
		Password     string `json:"password"`
	} `json:"oauth,omitempty"`
}

// SemanticScholarConfig holds Semantic Scholar configuration
type SemanticScholarConfig struct {
	Mode             string  `json:"mode"` // "search" or "recommendations"
	Query            *string `json:"query,omitempty"`
	PaperID          *string `json:"paper_id,omitempty"`
	Year             *string `json:"year,omitempty"`
	MaxResults       int     `json:"max_results"`
	MinCitations     int     `json:"min_citations"`
	APIKey           string  `json:"api_key,omitempty"`
	RateLimitDelayMs int     `json:"rate_limit_delay_ms"`
}

// HealthStatus represents the health of the service
type HealthStatus struct {
	Status    string    `json:"status"` // "healthy" or "unhealthy"
	Database  string    `json:"database"`
	Scheduler string    `json:"scheduler"`
	Timestamp time.Time `json:"timestamp"`
}

// Metrics represents service metrics
type Metrics struct {
	TotalSources      int        `json:"total_sources"`
	TotalArticles     int        `json:"total_articles"`
	ArticlesToday     int        `json:"articles_today"`
	SourcesWithErrors int        `json:"sources_with_errors"`
	LastCrawl         *time.Time `json:"last_crawl,omitempty"`
	Timestamp         time.Time  `json:"timestamp"`
}
