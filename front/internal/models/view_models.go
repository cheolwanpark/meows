package models

import (
	"net/url"
	"time"

	"github.com/cheolwanpark/meows/front/internal/collector"
)

// Article represents a news article for display in templates
type Article struct {
	ID        string
	SourceID  string
	Title     string
	Author    string
	URL       string
	Domain    string
	WrittenAt time.Time
	TimeAgo   string
	Score     int
	Comments  int
	Source    string // "reddit" or "semantic_scholar"
}

// FromCollectorArticle converts a collector.Article to a view model Article
func FromCollectorArticle(a collector.Article, sourceType string) Article {
	article := Article{
		ID:        a.ID,
		SourceID:  a.SourceID,
		Title:     a.Title,
		Author:    a.Author,
		URL:       a.URL,
		Domain:    ExtractDomain(a.URL),
		WrittenAt: a.WrittenAt,
		TimeAgo:   RelativeTime(a.WrittenAt),
		Source:    sourceType,
	}

	// Parse metadata based on source type
	if sourceType == "reddit" {
		article.Score, article.Comments = ParseRedditMetadata(a.Metadata)
	} else if sourceType == "semantic_scholar" {
		citations, _ := ParseS2Metadata(a.Metadata)
		article.Score = citations // Use citations as score for papers
		article.Comments = 0      // Papers don't have comments
	}

	// Default author if empty
	if article.Author == "" {
		article.Author = "unknown"
	}

	return article
}

// Source represents a crawling source for display in templates
type Source struct {
	ID            string
	Type          string
	Name          string
	URL           string
	Category      string
	CategoryEmoji string
	CronExpr      string
	Status        string
	LastRunAt     *time.Time
	LastRunAgo    string
	LastError     string
	IsActive      bool
}

// FromCollectorSource converts a collector.Source to a view model Source
func FromCollectorSource(s collector.Source) Source {
	source := Source{
		ID:        s.ID,
		Type:      s.Type,
		CronExpr:  s.CronExpr,
		Status:    s.Status,
		LastRunAt: s.LastRunAt,
		LastError: s.LastError,
	}

	// Determine category
	source.Category = DetermineCategory(s.Type, s.Config)
	source.CategoryEmoji = CategoryEmoji(source.Category)

	// Extract name and URL from config
	if s.Type == "reddit" {
		subreddit := ExtractConfigField(s.Config, "subreddit")
		source.Name = "r/" + subreddit
		source.URL = "https://reddit.com/r/" + subreddit
	} else if s.Type == "semantic_scholar" {
		mode := ExtractConfigField(s.Config, "mode")
		if mode == "search" {
			query := ExtractConfigField(s.Config, "query")
			source.Name = "S2: " + query
			source.URL = "https://www.semanticscholar.org/search?q=" + url.QueryEscape(query)
		} else {
			paperID := ExtractConfigField(s.Config, "paper_id")
			source.Name = "S2: Paper " + paperID
			source.URL = "https://www.semanticscholar.org/paper/" + url.PathEscape(paperID)
		}
	}

	// Calculate last run time ago
	if source.LastRunAt != nil {
		source.LastRunAgo = RelativeTime(*source.LastRunAt)
	}

	// Determine if active (idle status and no errors)
	source.IsActive = s.Status == "idle" && s.LastError == ""

	return source
}

// FormErrors holds validation errors for forms
type FormErrors struct {
	Name     string
	URL      string
	Category string
	Cron     string
	General  string
}

// HasErrors returns true if there are any validation errors
func (f FormErrors) HasErrors() bool {
	return f.Name != "" || f.URL != "" || f.Category != "" || f.Cron != "" || f.General != ""
}
