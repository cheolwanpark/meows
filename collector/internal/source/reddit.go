package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/cheolwanpark/meows/collector/internal/config"
	"github.com/cheolwanpark/meows/collector/internal/db"
	"github.com/google/uuid"
	"golang.org/x/time/rate"
)

// RedditSource implements the Source interface for Reddit
type RedditSource struct {
	source          *db.Source
	config          *db.RedditConfig
	client          *http.Client
	limiter         *rate.Limiter
	maxCommentDepth int
}

// RedditResponse structures
type redditListingResponse struct {
	Data struct {
		Children []struct {
			Data redditPost `json:"data"`
		} `json:"children"`
		After string `json:"after"`
	} `json:"data"`
}

type redditPost struct {
	ID                    string  `json:"id"`
	Title                 string  `json:"title"`
	Selftext              string  `json:"selftext"`
	Author                string  `json:"author"`
	CreatedUTC            float64 `json:"created_utc"`
	Score                 int     `json:"score"`
	NumComments           int     `json:"num_comments"`
	Permalink             string  `json:"permalink"`
	URL                   string  `json:"url"`
	Subreddit             string  `json:"subreddit"`
	SubredditNamePrefixed string  `json:"subreddit_name_prefixed"`
}

type redditCommentsResponse []interface{}

// NewRedditSource creates a new Reddit source
// Uses credentials from config file
func NewRedditSource(
	source *db.Source,
	credentials *config.CredentialsConfig,
	sharedLimiter *rate.Limiter,
	maxCommentDepth int,
) (*RedditSource, error) {
	var config db.RedditConfig
	if err := json.Unmarshal(source.Config, &config); err != nil {
		return nil, fmt.Errorf("invalid reddit config: %w", err)
	}

	rs := &RedditSource{
		source:          source,
		config:          &config,
		client:          &http.Client{Timeout: 30 * time.Second},
		limiter:         sharedLimiter, // Use shared rate limiter per source type
		maxCommentDepth: maxCommentDepth,
	}

	// OAuth authentication using credentials from config file
	// If credentials are set, use OAuth; otherwise fall back to anonymous access
	// Implementation note: Reddit OAuth setup would go here
	// For now, credentials are available in: credentials.RedditClientID, RedditClientSecret, RedditUsername, RedditPassword
	_ = credentials // Credentials available but OAuth not yet implemented

	return rs, nil
}

// SourceType returns "reddit"
func (r *RedditSource) SourceType() string {
	return "reddit"
}

// Validate checks if the configuration is valid
func (r *RedditSource) Validate() error {
	if r.config.Subreddit == "" {
		return fmt.Errorf("subreddit is required")
	}
	if r.config.Sort == "" {
		r.config.Sort = "hot"
	}
	if err := validateEnum(r.config.Sort, []string{"hot", "new", "top", "rising"}, "sort"); err != nil {
		return err
	}
	if r.config.Limit <= 0 {
		r.config.Limit = 100
	}
	if r.config.UserAgent == "" {
		return fmt.Errorf("user_agent is required")
	}
	return nil
}

// Fetch retrieves Reddit posts and comments
func (r *RedditSource) Fetch(ctx context.Context, since time.Time) ([]db.Article, []db.Comment, error) {
	if err := r.Validate(); err != nil {
		return nil, nil, err
	}

	var allArticles []db.Article
	var allComments []db.Comment

	after := ""
	remaining := r.config.Limit

	for remaining > 0 {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		default:
		}

		// Rate limiting
		if err := r.limiter.Wait(ctx); err != nil {
			return nil, nil, err
		}

		// Fetch posts
		posts, nextAfter, err := r.fetchPosts(ctx, after, min(remaining, 100))
		if err != nil {
			return nil, nil, err
		}

		if len(posts) == 0 {
			break
		}

		// Convert posts to articles and fetch comments
		for _, post := range posts {
			// Skip if older than since
			postTime := time.Unix(int64(post.CreatedUTC), 0)
			if postTime.Before(since) {
				continue
			}

			// Apply filters
			if post.Score < r.config.MinScore || post.NumComments < r.config.MinComments {
				continue
			}

			article := r.postToArticle(post)
			allArticles = append(allArticles, article)

			// Fetch comments for this post
			if r.maxCommentDepth > 0 && post.NumComments > 0 {
				comments, err := r.fetchComments(ctx, post.ID, article.ID)
				if err != nil {
					// Log error but continue
					fmt.Printf("Warning: failed to fetch comments for post %s: %v\n", post.ID, err)
				} else {
					allComments = append(allComments, comments...)
				}
			}
		}

		remaining -= len(posts)
		after = nextAfter

		if after == "" {
			break
		}
	}

	return allArticles, allComments, nil
}

// fetchPosts fetches a page of Reddit posts
func (r *RedditSource) fetchPosts(ctx context.Context, after string, limit int) ([]redditPost, string, error) {
	u := fmt.Sprintf("https://www.reddit.com/r/%s/%s.json", r.config.Subreddit, r.config.Sort)

	params := url.Values{}
	params.Set("limit", fmt.Sprintf("%d", limit))
	if after != "" {
		params.Set("after", after)
	}
	if r.config.Sort == "top" && r.config.TimeFilter != "" {
		params.Set("t", r.config.TimeFilter)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", u+"?"+params.Encode(), nil)
	if err != nil {
		return nil, "", err
	}

	req.Header.Set("User-Agent", r.config.UserAgent)

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("reddit API returned %d: %s", resp.StatusCode, string(body))
	}

	var listing redditListingResponse
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		return nil, "", fmt.Errorf("failed to decode response: %w", err)
	}

	posts := make([]redditPost, len(listing.Data.Children))
	for i, child := range listing.Data.Children {
		posts[i] = child.Data
	}

	return posts, listing.Data.After, nil
}

// fetchComments fetches comments for a Reddit post
func (r *RedditSource) fetchComments(ctx context.Context, postID string, articleID string) ([]db.Comment, error) {
	// Rate limiting
	if err := r.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	u := fmt.Sprintf("https://www.reddit.com/comments/%s.json", postID)

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", r.config.UserAgent)

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("reddit API returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var response redditCommentsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to decode comments: %w", err)
	}

	// Extract comments from response
	// Response is an array: [post_listing, comments_listing]
	if len(response) < 2 {
		return []db.Comment{}, nil
	}

	commentsListing, ok := response[1].(map[string]interface{})
	if !ok {
		return []db.Comment{}, nil
	}

	var comments []db.Comment
	r.extractComments(commentsListing, articleID, "", 0, &comments)

	return comments, nil
}

// extractComments recursively extracts comments from Reddit API response
func (r *RedditSource) extractComments(listing interface{}, articleID string, parentID string, depth int, comments *[]db.Comment) {
	if depth > r.maxCommentDepth {
		return
	}

	listingMap, ok := listing.(map[string]interface{})
	if !ok {
		return
	}

	data, ok := listingMap["data"].(map[string]interface{})
	if !ok {
		return
	}

	children, ok := data["children"].([]interface{})
	if !ok {
		return
	}

	for _, child := range children {
		childMap, ok := child.(map[string]interface{})
		if !ok {
			continue
		}

		kind, _ := childMap["kind"].(string)
		if kind != "t1" { // t1 is comment, t3 is "more" link
			continue
		}

		commentData, ok := childMap["data"].(map[string]interface{})
		if !ok {
			continue
		}

		id, _ := commentData["id"].(string)
		body, _ := commentData["body"].(string)
		author, _ := commentData["author"].(string)
		createdUTC, _ := commentData["created_utc"].(float64)

		if id == "" || body == "" {
			continue
		}

		comment := db.Comment{
			ID:         uuid.New().String(),
			ArticleID:  articleID,
			ExternalID: id,
			Author:     author,
			Content:    body,
			WrittenAt:  time.Unix(int64(createdUTC), 0),
			Depth:      depth,
		}

		if parentID != "" {
			comment.ParentID = &parentID
		}

		*comments = append(*comments, comment)

		// Process replies
		if replies, ok := commentData["replies"].(map[string]interface{}); ok {
			r.extractComments(replies, articleID, id, depth+1, comments)
		}
	}
}

// postToArticle converts a Reddit post to an Article
func (r *RedditSource) postToArticle(post redditPost) db.Article {
	metadata, _ := json.Marshal(map[string]interface{}{
		"score":        post.Score,
		"num_comments": post.NumComments,
		"subreddit":    post.Subreddit,
	})

	return db.Article{
		ID:         uuid.New().String(),
		SourceID:   r.source.ID,
		ExternalID: post.ID,
		Title:      post.Title,
		Author:     post.Author,
		Content:    post.Selftext,
		URL:        "https://www.reddit.com" + post.Permalink,
		WrittenAt:  time.Unix(int64(post.CreatedUTC), 0),
		Metadata:   metadata,
		CreatedAt:  time.Now(),
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
