package models

import (
	"net/url"
	"strings"
	"time"

	"github.com/cheolwanpark/meows/front/internal/collector"
)

// Article represents a news article for display in templates
type Article struct {
	ID        string
	SourceID  string
	Title     string
	Author    string
	Content   string // Article body/selftext
	URL       string
	Domain    string
	WrittenAt time.Time
	TimeAgo   string
	Score     int
	Comments  int
	Source    string // "reddit" or "semantic_scholar"
	Liked     bool   // Whether current profile has liked this article
	LikeID    string // ID of the like (for unlike button)
}

// FromCollectorArticle converts a collector.Article to a view model Article
func FromCollectorArticle(a collector.Article) Article {
	sourceType := a.SourceType
	if sourceType == "" {
		sourceType = "reddit" // Fallback for legacy data
	}

	article := Article{
		ID:        a.ID,
		SourceID:  a.SourceID,
		Title:     a.Title,
		Author:    a.Author,
		Content:   a.Content,
		URL:       a.URL,
		Domain:    ExtractDomain(a.URL),
		WrittenAt: a.WrittenAt,
		TimeAgo:   RelativeTime(a.WrittenAt),
		Source:    sourceType,
		Liked:     a.Liked,
		LikeID:    a.LikeID,
	}

	// Parse metadata based on source type
	switch sourceType {
	case "reddit":
		article.Score, article.Comments = ParseRedditMetadata(a.Metadata)
	case "semantic_scholar":
		citations, _ := ParseS2Metadata(a.Metadata)
		article.Score = citations
	case "hackernews":
		article.Score, article.Comments = ParseHackerNewsMetadata(a.Metadata)
	}

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
		Status:    s.Status,
		LastRunAt: s.LastRunAt,
		LastError: s.LastError,
	}

	// Determine category using ExternalID (which contains subreddit/query/paper_id)
	source.Category = DetermineCategory(s.Type, s.ExternalID)
	source.CategoryEmoji = CategoryEmoji(source.Category)

	// Extract name and URL using ExternalID
	if s.Type == "reddit" {
		if s.ExternalID != "" {
			source.Name = "r/" + s.ExternalID
			source.URL = "https://reddit.com/r/" + url.PathEscape(s.ExternalID)
		} else {
			source.Name = s.ConfigSummary
			source.URL = ""
		}
	} else if s.Type == "semantic_scholar" {
		if s.ExternalID != "" {
			// Determine if this is a paper ID or a search query
			// Paper IDs: DOI (starts with "10.") or S2 Corpus ID (40-char hex string)
			isPaperID := strings.HasPrefix(s.ExternalID, "10.") || (len(s.ExternalID) == 40 && isHexString(s.ExternalID))

			if isPaperID {
				source.Name = "S2: Paper " + s.ExternalID
				source.URL = "https://www.semanticscholar.org/paper/" + url.PathEscape(s.ExternalID)
			} else {
				// Search query
				source.Name = "S2: " + s.ExternalID
				source.URL = "https://www.semanticscholar.org/search?q=" + url.QueryEscape(s.ExternalID)
			}
		} else {
			source.Name = s.ConfigSummary
			source.URL = ""
		}
	} else if s.Type == "hackernews" {
		if s.ExternalID != "" {
			// ExternalID is the story type (top, new, best, ask, show, job)
			storyTypeNames := map[string]string{
				"top":  "Top Stories",
				"new":  "New Stories",
				"best": "Best Stories",
				"ask":  "Ask HN",
				"show": "Show HN",
				"job":  "Jobs",
			}
			// Map item_type to actual HN URLs (top→news, new→newest, job→jobs)
			storyTypeURLs := map[string]string{
				"top":  "news",
				"new":  "newest",
				"best": "best",
				"ask":  "ask",
				"show": "show",
				"job":  "jobs",
			}
			storyName := storyTypeNames[s.ExternalID]
			if storyName == "" {
				storyName = s.ExternalID
			}
			storyURL := storyTypeURLs[s.ExternalID]
			if storyURL == "" {
				storyURL = s.ExternalID
			}
			source.Name = "HN: " + storyName
			source.URL = "https://news.ycombinator.com/" + storyURL
		} else {
			source.Name = s.ConfigSummary
			source.URL = ""
		}
	} else {
		// Fallback for unknown source types
		source.Name = s.ConfigSummary
		if source.Name == "" {
			source.Name = "Unknown Source"
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

// Pagination holds pagination state for templates
type Pagination struct {
	Page      int
	HasPrev   bool
	HasNext   bool
	IsCurated bool
}

// PrevPage returns the previous page number
func (p Pagination) PrevPage() int {
	if p.HasPrev {
		return p.Page - 1
	}
	return 1
}

// NextPage returns the next page number
func (p Pagination) NextPage() int {
	if p.HasNext {
		return p.Page + 1
	}
	return p.Page
}

// FormErrors holds validation errors for forms
type FormErrors struct {
	Name     string
	URL      string
	Category string
	General  string
}

// HasErrors returns true if there are any validation errors
func (f FormErrors) HasErrors() bool {
	return f.Name != "" || f.URL != "" || f.Category != "" || f.General != ""
}
