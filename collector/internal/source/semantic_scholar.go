package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/cheolwanpark/meows/collector/internal/db"
	"github.com/google/uuid"
	"golang.org/x/time/rate"
)

// SemanticScholarSource implements the Source interface for Semantic Scholar
type SemanticScholarSource struct {
	source  *db.Source
	config  *db.SemanticScholarConfig
	client  *http.Client
	limiter *rate.Limiter
	apiKey  string // From global config (environment variable)
}

// Semantic Scholar API response structures
type s2SearchResponse struct {
	Total  int       `json:"total"`
	Offset int       `json:"offset"`
	Next   int       `json:"next"`
	Data   []s2Paper `json:"data"`
}

type s2RecommendationsResponse struct {
	RecommendedPapers []s2Paper `json:"recommendedPapers"`
}

type s2Paper struct {
	PaperID       string     `json:"paperId"`
	Title         string     `json:"title"`
	Abstract      string     `json:"abstract"`
	Year          int        `json:"year"`
	CitationCount int        `json:"citationCount"`
	URL           string     `json:"url"`
	Authors       []s2Author `json:"authors"`
}

type s2Author struct {
	AuthorID string `json:"authorId"`
	Name     string `json:"name"`
}

// NewSemanticScholarSource creates a new Semantic Scholar source
// Uses global config for API key (loaded from encrypted DB)
func NewSemanticScholarSource(
	source *db.Source,
	globalConfig *db.GlobalConfig,
	sharedLimiter *rate.Limiter,
) (*SemanticScholarSource, error) {
	var config db.SemanticScholarConfig
	if err := json.Unmarshal(source.Config, &config); err != nil {
		return nil, fmt.Errorf("invalid semantic scholar config: %w", err)
	}

	ss := &SemanticScholarSource{
		source:  source,
		config:  &config,
		client:  &http.Client{Timeout: 30 * time.Second},
		limiter: sharedLimiter,                      // Use shared rate limiter per source type
		apiKey:  globalConfig.SemanticScholarAPIKey, // Use API key from encrypted DB
	}

	return ss, nil
}

// SourceType returns "semantic_scholar"
func (s *SemanticScholarSource) SourceType() string {
	return "semantic_scholar"
}

// Validate checks if the configuration is valid
func (s *SemanticScholarSource) Validate() error {
	if s.config.Mode != "search" && s.config.Mode != "recommendations" {
		return fmt.Errorf("mode must be 'search' or 'recommendations', got: %s", s.config.Mode)
	}

	if s.config.Mode == "search" {
		if s.config.Query == nil || *s.config.Query == "" {
			return fmt.Errorf("query is required for search mode")
		}
	}

	if s.config.Mode == "recommendations" {
		if s.config.PaperID == nil || *s.config.PaperID == "" {
			return fmt.Errorf("paper_id is required for recommendations mode")
		}
	}

	if s.config.MaxResults <= 0 {
		s.config.MaxResults = 100
	}

	return nil
}

// Fetch retrieves papers from Semantic Scholar
func (s *SemanticScholarSource) Fetch(ctx context.Context, since time.Time) ([]db.Article, []db.Comment, error) {
	if err := s.Validate(); err != nil {
		return nil, nil, err
	}

	var papers []s2Paper
	var err error

	if s.config.Mode == "search" {
		papers, err = s.fetchSearch(ctx)
	} else {
		papers, err = s.fetchRecommendations(ctx)
	}

	if err != nil {
		return nil, nil, err
	}

	// Convert papers to articles
	articles := make([]db.Article, 0, len(papers))
	for _, paper := range papers {
		// Apply filters
		if paper.CitationCount < s.config.MinCitations {
			continue
		}

		articles = append(articles, s.paperToArticle(paper))
	}

	// Semantic Scholar doesn't have comments
	return articles, []db.Comment{}, nil
}

// fetchSearch fetches papers using the search API
func (s *SemanticScholarSource) fetchSearch(ctx context.Context) ([]s2Paper, error) {
	var allPapers []s2Paper
	offset := 0
	limit := 100 // API limit per request

	for len(allPapers) < s.config.MaxResults {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Rate limiting
		if err := s.limiter.Wait(ctx); err != nil {
			return nil, err
		}

		params := url.Values{}
		params.Set("query", *s.config.Query)
		params.Set("offset", strconv.Itoa(offset))
		params.Set("limit", strconv.Itoa(limit))
		params.Set("fields", "paperId,title,abstract,year,citationCount,url,authors")

		if s.config.Year != nil && *s.config.Year != "" {
			params.Set("year", *s.config.Year)
		}

		u := "https://api.semanticscholar.org/graph/v1/paper/search?" + params.Encode()

		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err != nil {
			return nil, err
		}

		if s.apiKey != "" {
			req.Header.Set("x-api-key", s.apiKey)
		}

		resp, err := s.client.Do(req)
		if err != nil {
			return nil, err
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		if err != nil {
			return nil, err
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfter := resp.Header.Get("Retry-After")
			return nil, fmt.Errorf("rate limited, retry after: %s", retryAfter)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("semantic scholar API returned %d: %s", resp.StatusCode, string(body))
		}

		var response s2SearchResponse
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		if len(response.Data) == 0 {
			break
		}

		allPapers = append(allPapers, response.Data...)
		offset += len(response.Data)

		// Check if we have more results
		if response.Next == 0 || len(allPapers) >= s.config.MaxResults {
			break
		}

		// S2 API has a 10,000 offset limit
		if offset >= 10000 {
			break
		}
	}

	// Trim to max results
	if len(allPapers) > s.config.MaxResults {
		allPapers = allPapers[:s.config.MaxResults]
	}

	return allPapers, nil
}

// fetchRecommendations fetches paper recommendations
func (s *SemanticScholarSource) fetchRecommendations(ctx context.Context) ([]s2Paper, error) {
	// Rate limiting
	if err := s.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Set("fields", "paperId,title,abstract,year,citationCount,url,authors")
	params.Set("limit", strconv.Itoa(s.config.MaxResults))

	u := fmt.Sprintf("https://api.semanticscholar.org/recommendations/v1/papers/forpaper/%s?%s",
		*s.config.PaperID, params.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}

	if s.apiKey != "" {
		req.Header.Set("x-api-key", s.apiKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()

	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := resp.Header.Get("Retry-After")
		return nil, fmt.Errorf("rate limited, retry after: %s", retryAfter)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("semantic scholar API returned %d: %s", resp.StatusCode, string(body))
	}

	var response s2RecommendationsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return response.RecommendedPapers, nil
}

// paperToArticle converts a Semantic Scholar paper to an Article
func (s *SemanticScholarSource) paperToArticle(paper s2Paper) db.Article {
	authorNames := make([]string, len(paper.Authors))
	for i, author := range paper.Authors {
		authorNames[i] = author.Name
	}

	var primaryAuthor string
	if len(authorNames) > 0 {
		primaryAuthor = authorNames[0]
	}

	// Handle year zero-value (missing year from API)
	yearStr := ""
	if paper.Year > 0 {
		yearStr = strconv.Itoa(paper.Year)
	}

	metadata, err := json.Marshal(map[string]interface{}{
		"citations": paper.CitationCount,
		"year":      yearStr,
		"authors":   authorNames,
	})
	if err != nil {
		// Fallback to empty JSON object if marshaling fails
		metadata = []byte("{}")
	}

	// Use year as a proxy for written_at since we don't have exact date
	writtenAt := time.Date(paper.Year, 1, 1, 0, 0, 0, 0, time.UTC)

	return db.Article{
		ID:         uuid.New().String(),
		SourceID:   s.source.ID,
		ExternalID: paper.PaperID,
		Title:      paper.Title,
		Author:     primaryAuthor,
		Content:    paper.Abstract,
		URL:        paper.URL,
		WrittenAt:  writtenAt,
		Metadata:   metadata,
		CreatedAt:  time.Now(),
	}
}
