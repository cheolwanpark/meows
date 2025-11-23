package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/cheolwanpark/meows/collector/internal/config"
	"github.com/cheolwanpark/meows/collector/internal/db"
	"github.com/google/uuid"
	"golang.org/x/time/rate"
)

// HackerNewsSource implements the Source interface for Hacker News
type HackerNewsSource struct {
	source          *db.Source
	config          *db.HackerNewsConfig
	client          *http.Client
	limiter         *rate.Limiter
	maxCommentDepth int
}

// HackerNews API response structures
type hnItem struct {
	ID          int     `json:"id"`
	Type        string  `json:"type"` // "story", "comment", "job", "poll", "pollopt"
	By          string  `json:"by"`
	Time        int64   `json:"time"`
	Text        string  `json:"text"`
	Dead        bool    `json:"dead"`
	Deleted     bool    `json:"deleted"`
	Parent      int     `json:"parent"`
	Kids        []int   `json:"kids"`
	URL         string  `json:"url"`
	Score       int     `json:"score"`
	Title       string  `json:"title"`
	Descendants int     `json:"descendants"`
}

// commentQueueItem represents an item in the BFS queue for comment fetching
type commentQueueItem struct {
	hnID       int
	parentUUID *string
	depth      int
}

// htmlComment represents a parsed comment from HTML before conversion to db.Comment
type htmlComment struct {
	externalID string
	author     string
	text       string
	depth      int
	timestamp  time.Time
}

// NewHackerNewsSource creates a new Hacker News source
func NewHackerNewsSource(
	source *db.Source,
	credentials *config.CredentialsConfig,
	sharedLimiter *rate.Limiter,
	maxCommentDepth int,
) (*HackerNewsSource, error) {
	var config db.HackerNewsConfig
	if err := json.Unmarshal(source.Config, &config); err != nil {
		return nil, fmt.Errorf("invalid hackernews config: %w", err)
	}

	hs := &HackerNewsSource{
		source:          source,
		config:          &config,
		client:          &http.Client{Timeout: 30 * time.Second},
		limiter:         sharedLimiter, // Use shared rate limiter per source type
		maxCommentDepth: maxCommentDepth,
	}

	// HN API is public, no credentials needed
	_ = credentials

	return hs, nil
}

// SourceType returns "hackernews"
func (h *HackerNewsSource) SourceType() string {
	return "hackernews"
}

// Validate checks if the configuration is valid
func (h *HackerNewsSource) Validate() error {
	// Validate ItemType
	if err := validateEnum(h.config.ItemType, []string{"top", "new", "best", "ask", "show", "job"}, "item_type"); err != nil {
		return err
	}

	// Set defaults
	if h.config.Limit <= 0 {
		h.config.Limit = 30
	}
	if h.config.Limit > 100 {
		return fmt.Errorf("limit must be <= 100, got %d", h.config.Limit)
	}

	// Set default for MaxCommentDepth if not configured
	if h.config.MaxCommentDepth <= 0 {
		h.config.MaxCommentDepth = 3
	}
	if h.config.MaxCommentDepth > 10 {
		return fmt.Errorf("max_comment_depth must be <= 10, got %d", h.config.MaxCommentDepth)
	}
	// Respect global MaxCommentDepth limit
	if h.config.MaxCommentDepth > h.maxCommentDepth {
		h.config.MaxCommentDepth = h.maxCommentDepth
	}

	if h.config.MaxCommentsPerArticle <= 0 {
		h.config.MaxCommentsPerArticle = 100
	}
	if h.config.MaxCommentsPerArticle > 500 {
		return fmt.Errorf("max_comments_per_article must be <= 500, got %d", h.config.MaxCommentsPerArticle)
	}

	return nil
}

// Fetch retrieves Hacker News stories and comments
func (h *HackerNewsSource) Fetch(ctx context.Context, since time.Time) ([]db.Article, []db.Comment, error) {
	if err := h.Validate(); err != nil {
		return nil, nil, err
	}

	var allArticles []db.Article
	var allComments []db.Comment

	// 1. Fetch story IDs
	storyIDs, err := h.fetchStoryIDs(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch story IDs: %w", err)
	}

	// 2. Limit to configured number
	if len(storyIDs) > h.config.Limit {
		storyIDs = storyIDs[:h.config.Limit]
	}

	// 3. Fetch and filter stories
	// NOTE: Deduplication optimization not implemented - the scheduler handles
	// duplicates via UPSERT (ON CONFLICT in storeArticlesInTx), so no data corruption occurs.
	for _, id := range storyIDs {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return allArticles, allComments, ctx.Err()
		default:
		}

		// Rate limiting
		if err := h.limiter.Wait(ctx); err != nil {
			return allArticles, allComments, err
		}

		// Fetch item details
		item, err := h.fetchItem(ctx, id)
		if err != nil {
			slog.Warn("Failed to fetch item",
				"item_id", id,
				"error", err)
			continue
		}

		// Skip null, deleted, or dead items
		if item == nil || item.Deleted || item.Dead {
			continue
		}

		// Skip if not a story/job/poll
		if item.Type != "story" && item.Type != "job" && item.Type != "poll" {
			continue
		}

		// Time filter - stop if item is older than since
		itemTime := time.Unix(item.Time, 0)
		if itemTime.Before(since) {
			// For "new" stories, they're chronologically sorted, so we can break
			// For "top", "best", "ask", "show", "job" they're ranked, so continue checking
			if h.config.ItemType == "new" {
				break // All subsequent items will be older
			}
			continue
		}

		// Score filter
		if item.Score < h.config.MinScore {
			continue
		}

		// Comment count filter
		if item.Descendants < h.config.MinComments {
			continue
		}

		// Convert to article
		article := h.itemToArticle(item)
		allArticles = append(allArticles, article)

		// Fetch comments if enabled
		if h.config.IncludeComments && item.Descendants > 0 {
			var comments []db.Comment
			var err error

			// Hybrid approach: try HTML scraping first, fall back to API
			if !h.config.ForceAPIMode {
				start := time.Now()
				comments, err = h.fetchCommentsViaHTML(ctx, item.ID, article.ID)
				duration := time.Since(start)

				if err != nil {
					// HTML scraping failed, fall back to API
					slog.Warn("HTML scraping failed, falling back to API",
						"story_id", item.ID,
						"error", err,
						"duration_ms", duration.Milliseconds())

					// Fallback to API if we have kids
					if len(item.Kids) > 0 {
						start = time.Now()
						comments, err = h.fetchCommentsViaAPI(ctx, item.Kids, article.ID)
						duration = time.Since(start)

						if err == nil {
							slog.Info("API fallback succeeded",
								"story_id", item.ID,
								"comment_count", len(comments),
								"duration_ms", duration.Milliseconds())
						}
					}
				} else {
					// HTML scraping succeeded
					slog.Debug("HTML scraping succeeded",
						"story_id", item.ID,
						"comment_count", len(comments),
						"duration_ms", duration.Milliseconds())
				}
			} else {
				// ForceAPIMode enabled, skip HTML scraping
				slog.Debug("ForceAPIMode enabled, using API",
					"story_id", item.ID)

				if len(item.Kids) > 0 {
					comments, err = h.fetchCommentsViaAPI(ctx, item.Kids, article.ID)
				}
			}

			if err != nil {
				slog.Warn("Failed to fetch comments",
					"item_id", id,
					"error", err)
			} else {
				allComments = append(allComments, comments...)
			}
		}
	}

	return allArticles, allComments, nil
}

// fetchStoryIDs fetches the list of story IDs for the configured item type
func (h *HackerNewsSource) fetchStoryIDs(ctx context.Context) ([]int, error) {
	// Rate limiting
	if err := h.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	u := fmt.Sprintf("https://hacker-news.firebaseio.com/v0/%sstories.json", h.config.ItemType)

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "meows-collector/1.0")

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HN API returned %d: %s", resp.StatusCode, string(body))
	}

	var ids []int
	if err := json.NewDecoder(resp.Body).Decode(&ids); err != nil {
		return nil, fmt.Errorf("failed to decode story IDs: %w", err)
	}

	return ids, nil
}

// fetchItem fetches a single item by ID
// Returns nil if the item doesn't exist (without error)
func (h *HackerNewsSource) fetchItem(ctx context.Context, id int) (*hnItem, error) {
	u := fmt.Sprintf("https://hacker-news.firebaseio.com/v0/item/%d.json", id)

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "meows-collector/1.0")

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HN API returned %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// HN API returns "null" for deleted/non-existent items
	if string(body) == "null" {
		return nil, nil
	}

	var item hnItem
	if err := json.Unmarshal(body, &item); err != nil {
		return nil, fmt.Errorf("failed to decode item: %w", err)
	}

	return &item, nil
}

// fetchCommentsViaAPI fetches comments using the Firebase API (BFS approach)
// This is the fallback method when HTML scraping fails
// Previously named: fetchCommentsBFS
func (h *HackerNewsSource) fetchCommentsViaAPI(ctx context.Context, rootKids []int, articleID string) ([]db.Comment, error) {
	var comments []db.Comment
	totalFetched := 0

	// Initialize BFS queue with root-level comment IDs
	queue := make([]commentQueueItem, 0, len(rootKids))
	for _, kidID := range rootKids {
		queue = append(queue, commentQueueItem{
			hnID:       kidID,
			parentUUID: nil,
			depth:      0,
		})
	}

	// Process queue
	for len(queue) > 0 && totalFetched < h.config.MaxCommentsPerArticle {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return comments, ctx.Err()
		default:
		}

		// Dequeue
		current := queue[0]
		queue = queue[1:]

		// Check depth limit
		if current.depth > h.config.MaxCommentDepth {
			continue
		}

		// Rate limiting
		if err := h.limiter.Wait(ctx); err != nil {
			return comments, err
		}

		// Fetch comment item
		item, err := h.fetchItem(ctx, current.hnID)
		if err != nil {
			slog.Warn("Failed to fetch comment",
				"comment_id", current.hnID,
				"error", err)
			continue
		}

		// Skip null, deleted, dead, or non-comment items
		if item == nil || item.Deleted || item.Dead || item.Type != "comment" {
			continue
		}

		// Convert to comment
		comment := db.Comment{
			ID:         uuid.New().String(),
			ArticleID:  articleID,
			ExternalID: fmt.Sprintf("%d", item.ID),
			Author:     item.By,
			Content:    item.Text,
			WrittenAt:  time.Unix(item.Time, 0),
			ParentID:   current.parentUUID,
			Depth:      current.depth,
		}

		comments = append(comments, comment)
		totalFetched++

		// Enqueue children for next level
		if len(item.Kids) > 0 && current.depth < h.config.MaxCommentDepth {
			// Store parent ID in a new variable to avoid pointer aliasing
			parentID := comment.ID
			for _, kidID := range item.Kids {
				queue = append(queue, commentQueueItem{
					hnID:       kidID,
					parentUUID: &parentID,
					depth:      current.depth + 1,
				})
			}
		}
	}

	return comments, nil
}

// itemToArticle converts a HN item to an Article
func (h *HackerNewsSource) itemToArticle(item *hnItem) db.Article {
	metadata, _ := json.Marshal(map[string]interface{}{
		"hn_id":       item.ID,
		"hn_type":     item.Type,
		"descendants": item.Descendants,
		"score":       item.Score,
		"by":          item.By,
	})

	// Use external URL if available, otherwise use HN item page
	articleURL := item.URL
	if articleURL == "" {
		articleURL = fmt.Sprintf("https://news.ycombinator.com/item?id=%d", item.ID)
	}

	return db.Article{
		ID:         uuid.New().String(),
		SourceID:   h.source.ID,
		ExternalID:   fmt.Sprintf("%d", item.ID),
		Title:      item.Title,
		Author:     item.By,
		Content:    item.Text, // For Ask HN posts
		URL:        articleURL,
		WrittenAt:  time.Unix(item.Time, 0),
		Metadata:   metadata,
		CreatedAt:  time.Now(),
	}
}

// fetchCommentsViaHTML fetches all comments for a story by parsing the HTML page
// This is 45-83x faster than the API approach and eliminates timeout issues
// Returns (nil, error) on ANY failure to ensure clean API fallback (atomic operation)
func (h *HackerNewsSource) fetchCommentsViaHTML(ctx context.Context, storyID int, articleID string) ([]db.Comment, error) {
	// Rate limiting
	if err := h.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit error: %w", err)
	}

	// Fetch HTML page
	url := fmt.Sprintf("https://news.ycombinator.com/item?id=%d", storyID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "meows-collector/1.0")

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from HN", resp.StatusCode)
	}

	// Parse HTML
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	// Validate HTML structure (checks for layout changes, pagination, etc.)
	if !h.validateHTMLStructure(doc, storyID) {
		return nil, fmt.Errorf("HTML structure validation failed")
	}

	// Parse comments from HTML
	htmlComments, err := h.parseHTMLComments(doc)
	if err != nil {
		return nil, fmt.Errorf("failed to parse comments: %w", err)
	}

	// Convert to db.Comment with parent relationships
	comments, err := h.buildCommentsWithParents(htmlComments, articleID)
	if err != nil {
		return nil, fmt.Errorf("failed to build parent relationships: %w", err)
	}

	// Sanity checks for atomicity - ensure we didn't silently lose data

	// Check 1: If we parsed HTML comments but built none, depth filtering failed
	if len(comments) == 0 && len(htmlComments) > 0 {
		return nil, fmt.Errorf("parsed %d HTML comments but built 0 db.Comments (depth filtering issue?)", len(htmlComments))
	}

	// Check 2: If comment rows exist in HTML but we parsed 0, selector likely changed
	commentRows := doc.Find("tr.athing.comtr")
	if commentRows.Length() > 0 && len(htmlComments) == 0 {
		return nil, fmt.Errorf("found %d comment rows but parsed 0 comments (HTML structure may have changed)",
			commentRows.Length())
	}

	// Check 3: Warn if we lost >20% of comments during parsing (suspicious)
	if commentRows.Length() > 0 && len(htmlComments) < int(float64(commentRows.Length())*0.8) {
		slog.Warn("Significant comment loss during parsing",
			"story_id", storyID,
			"comment_rows_found", commentRows.Length(),
			"comments_parsed", len(htmlComments),
			"loss_pct", 100*(commentRows.Length()-len(htmlComments))/commentRows.Length())
	}

	slog.Debug("Successfully fetched comments via HTML",
		"story_id", storyID,
		"comment_count", len(comments),
		"html_rows", commentRows.Length())

	return comments, nil
}

// parseDepth extracts comment depth from HN HTML structure
// Returns (depth, true) on success, (0, false) if parsing fails
func parseDepth(row *goquery.Selection) (int, bool) {
	// HN uses img width to indicate depth: 0px=root, 40px=depth1, 80px=depth2, etc.
	indentImg := row.Find("td.ind img")
	if widthAttr, exists := indentImg.Attr("width"); exists {
		if width, err := strconv.Atoi(widthAttr); err == nil {
			return width / 40, true
		}
	}
	return 0, false
}

// parseHTMLComments parses comments from the HTML document
// Extracts all 8 Comment model fields and handles edge cases
func (h *HackerNewsSource) parseHTMLComments(doc *goquery.Document) ([]htmlComment, error) {
	var htmlComments []htmlComment

	// Parse all comment rows
	doc.Find("tr.athing.comtr").Each(func(i int, row *goquery.Selection) {
		hc := htmlComment{}

		// Get comment ID from row id attribute
		id, exists := row.Attr("id")
		if !exists || id == "" {
			return // Skip rows without IDs
		}
		hc.externalID = id

		// Get depth from img width
		depth, ok := parseDepth(row)
		if !ok {
			return // Skip malformed comments
		}
		hc.depth = depth

		// Get author
		author := row.Find("a.hnuser").First().Text()
		if author == "" {
			return // Skip comments without authors (deleted/dead)
		}
		hc.author = author

		// Get timestamp from title attribute
		ageSpan := row.Find("span.age")
		if titleAttr, exists := ageSpan.Attr("title"); exists {
			// Format: "2025-11-22T21:50:13 1763848213"
			parts := strings.Split(titleAttr, " ")
			if len(parts) >= 1 {
				t, err := time.Parse("2006-01-02T15:04:05", parts[0])
				if err != nil {
					slog.Warn("Failed to parse timestamp",
						"external_id", id,
						"timestamp", parts[0],
						"error", err)
					return
				}
				hc.timestamp = t
			}
		}

		// Get comment HTML content (preserve formatting, links, code blocks)
		commtextDiv := row.Find("div.commtext").First()
		htmlContent, err := commtextDiv.Html()
		if err != nil {
			slog.Warn("Failed to extract comment HTML",
				"external_id", id,
				"error", err)
			return
		}

		htmlContent = strings.TrimSpace(htmlContent)

		// Check for deleted/dead/flagged markers in text content
		textContent := commtextDiv.Text()
		textContent = strings.TrimSpace(textContent)
		if textContent == "[dead]" || textContent == "[flagged]" || textContent == "[deleted]" {
			return
		}

		if htmlContent == "" {
			return // Skip empty comments
		}

		hc.text = htmlContent

		// Only add if we got all essential fields
		if hc.externalID != "" && hc.author != "" && hc.text != "" && !hc.timestamp.IsZero() {
			htmlComments = append(htmlComments, hc)
		}
	})

	return htmlComments, nil
}

// buildCommentsWithParents converts htmlComments to db.Comments with parent relationships
// Uses depth-stack algorithm to reconstruct the parent-child tree
// Returns error if parent relationships cannot be determined (orphaned comments)
func (h *HackerNewsSource) buildCommentsWithParents(htmlComments []htmlComment, articleID string) ([]db.Comment, error) {
	var comments []db.Comment

	// Track last comment at each depth level to determine parents
	// depthStack[depth] = comment UUID
	depthStack := make(map[int]string)

	for _, hc := range htmlComments {
		// Apply depth limit
		if hc.depth > h.config.MaxCommentDepth {
			continue
		}

		// Create comment with UUID
		commentID := uuid.New().String()
		comment := db.Comment{
			ID:         commentID,
			ArticleID:  articleID,
			ExternalID: hc.externalID,
			Author:     hc.author,
			Content:    hc.text,
			WrittenAt:  hc.timestamp,
			Depth:      hc.depth,
		}

		// Determine parent using depth-stack algorithm
		if hc.depth == 0 {
			// Top-level comment, no parent
			comment.ParentID = nil
		} else {
			// Find parent from previous depth level
			parentDepth := hc.depth - 1
			parentID, exists := depthStack[parentDepth]
			if !exists {
				// Orphaned comment - parent doesn't exist
				// This indicates corrupted or out-of-order data
				return nil, fmt.Errorf("orphaned comment at depth %d (external_id=%s): parent at depth %d not found",
					hc.depth, hc.externalID, parentDepth)
			}
			comment.ParentID = &parentID
		}

		// Add to results
		comments = append(comments, comment)

		// Update depth stack
		depthStack[hc.depth] = commentID

		// Apply per-article limit
		if len(comments) >= h.config.MaxCommentsPerArticle {
			break
		}
	}

	return comments, nil
}

// validateHTMLStructure performs comprehensive validation on the HTML structure
// Returns false if HN appears to have changed their layout or if data is incomplete
func (h *HackerNewsSource) validateHTMLStructure(doc *goquery.Document, storyID int) bool {
	// Check 1: Story title exists with matching ID
	storySelector := fmt.Sprintf("tr.athing#%d", storyID)
	if doc.Find(storySelector).Length() == 0 {
		slog.Warn("HTML validation failed: story not found",
			"story_id", storyID,
			"reason", "missing_story")
		return false
	}

	// Check 2: Comment form exists (indicates we're on a valid item page)
	if doc.Find("form[method='post'][action='comment']").Length() == 0 {
		slog.Warn("HTML validation failed: comment form not found",
			"story_id", storyID,
			"reason", "no_form")
		return false
	}

	// Check 3: Detect pagination "More" link (indicates incomplete data)
	if doc.Find("a.morelink").Length() > 0 {
		slog.Info("HTML validation failed: pagination detected",
			"story_id", storyID,
			"reason", "pagination")
		return false
	}

	// Check 4: If comment rows exist, validate they have expected structure
	commentRows := doc.Find("tr.athing.comtr")
	if commentRows.Length() > 0 {
		firstRow := commentRows.First()
		// Should have indent column
		if firstRow.Find("td.ind").Length() == 0 {
			slog.Warn("HTML validation failed: missing indent column",
				"story_id", storyID,
				"reason", "missing_indent")
			return false
		}
		// Should have comment content area
		if firstRow.Find("div.commtext").Length() == 0 {
			slog.Warn("HTML validation failed: missing commtext div",
				"story_id", storyID,
				"reason", "missing_commtext")
			return false
		}
	}

	return true
}
