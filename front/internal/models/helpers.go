package models

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// RelativeTime converts a timestamp to a human-readable relative time string
func RelativeTime(t time.Time) string {
	duration := time.Since(t)

	if duration < time.Minute {
		return "just now"
	} else if duration < time.Hour {
		minutes := int(duration.Minutes())
		if minutes == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", minutes)
	} else if duration < 24*time.Hour {
		hours := int(duration.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	} else {
		days := int(duration.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}

// ExtractDomain extracts the domain from a URL
func ExtractDomain(rawURL string) string {
	if rawURL == "" {
		return ""
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}

	// Remove www. prefix
	domain := strings.TrimPrefix(parsed.Hostname(), "www.")
	return domain
}

// CategoryEmoji returns an emoji for a category
func CategoryEmoji(category string) string {
	emojiMap := map[string]string{
		"tech":     "💻",
		"news":     "📰",
		"science":  "🔬",
		"business": "💼",
		"other":    "📌",
	}

	if emoji, ok := emojiMap[category]; ok {
		return emoji
	}
	return "📌"
}

// ParseRedditMetadata extracts score and comments from Reddit metadata
func ParseRedditMetadata(metadata json.RawMessage) (score int, comments int) {
	if len(metadata) == 0 {
		return 0, 0
	}

	var data struct {
		Score       int `json:"score"`
		NumComments int `json:"num_comments"`
	}

	if err := json.Unmarshal(metadata, &data); err != nil {
		return 0, 0
	}

	return data.Score, data.NumComments
}

// ParseS2Metadata extracts citations and year from Semantic Scholar metadata
func ParseS2Metadata(metadata json.RawMessage) (citations int, year string) {
	if len(metadata) == 0 {
		return 0, ""
	}

	var data struct {
		Citations int    `json:"citations"`
		Year      string `json:"year"`
	}

	if err := json.Unmarshal(metadata, &data); err != nil {
		return 0, ""
	}

	return data.Citations, data.Year
}

// ExtractConfigField extracts a specific field from a JSON config
func ExtractConfigField(config json.RawMessage, field string) string {
	if len(config) == 0 {
		return ""
	}

	var data map[string]interface{}
	if err := json.Unmarshal(config, &data); err != nil {
		return ""
	}

	if value, ok := data[field]; ok {
		if str, ok := value.(string); ok {
			return str
		}
	}

	return ""
}

// DetermineCategory determines the category from a source config
func DetermineCategory(sourceType string, externalID string) string {
	// For Reddit, we can infer category from subreddit name (use externalID which contains the subreddit)
	if sourceType == "reddit" {
		subreddit := strings.ToLower(externalID)

		techSubreddits := []string{"programming", "golang", "javascript", "python", "technology", "coding", "webdev"}
		newsSubreddits := []string{"news", "worldnews", "politics"}
		scienceSubreddits := []string{"science", "askscience", "physics", "biology", "chemistry"}
		businessSubreddits := []string{"business", "economics", "finance", "entrepreneur"}

		for _, tech := range techSubreddits {
			if strings.Contains(subreddit, tech) {
				return "tech"
			}
		}
		for _, news := range newsSubreddits {
			if strings.Contains(subreddit, news) {
				return "news"
			}
		}
		for _, sci := range scienceSubreddits {
			if strings.Contains(subreddit, sci) {
				return "science"
			}
		}
		for _, biz := range businessSubreddits {
			if strings.Contains(subreddit, biz) {
				return "business"
			}
		}
	}

	// For Semantic Scholar, always science
	if sourceType == "semantic_scholar" {
		return "science"
	}

	return "other"
}

// isHexString checks if a string contains only hexadecimal characters
func isHexString(s string) bool {
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return len(s) > 0
}
