package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client is an HTTP client for the collector API
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new collector API client
func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Article represents a crawled article from the collector
type Article struct {
	ID         string          `json:"id"`
	SourceID   string          `json:"source_id"`
	ExternalID string          `json:"external_id"`
	Title      string          `json:"title"`
	Author     string          `json:"author"`
	Content    string          `json:"content"`
	URL        string          `json:"url,omitempty"`
	WrittenAt  time.Time       `json:"written_at"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
}

// Source represents a crawling source from the collector
type Source struct {
	ID            string     `json:"id"`
	Type          string     `json:"type"`
	ConfigSummary string     `json:"config_summary"`
	ExternalID    string     `json:"external_id"`
	LastRunAt     *time.Time `json:"last_run_at,omitempty"`
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
	Status        string     `json:"status"`
	CreatedAt     time.Time  `json:"created_at"`
}

// CreateSourceRequest represents a request to create a new source
type CreateSourceRequest struct {
	Type   string          `json:"type"`
	Config json.RawMessage `json:"config"`
}

// ErrorResponse represents an error response from the collector
type ErrorResponse struct {
	Error string `json:"error"`
}

// GetArticles fetches articles from the collector
func (c *Client) GetArticles(ctx context.Context, limit, offset int) ([]Article, error) {
	url := fmt.Sprintf("%s/articles?limit=%d&offset=%d", c.baseURL, limit, offset)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var articles []Article
	if err := json.NewDecoder(resp.Body).Decode(&articles); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return articles, nil
}

// GetSources fetches sources from the collector
func (c *Client) GetSources(ctx context.Context) ([]Source, error) {
	url := fmt.Sprintf("%s/sources", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var sources []Source
	if err := json.NewDecoder(resp.Body).Decode(&sources); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return sources, nil
}

// CreateSource creates a new source in the collector
func (c *Client) CreateSource(ctx context.Context, req CreateSourceRequest) (*Source, error) {
	url := fmt.Sprintf("%s/sources", c.baseURL)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, c.parseError(resp)
	}

	var source Source
	if err := json.NewDecoder(resp.Body).Decode(&source); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &source, nil
}

// DeleteSource deletes a source from the collector
func (c *Client) DeleteSource(ctx context.Context, id string) error {
	url := fmt.Sprintf("%s/sources/%s", c.baseURL, id)

	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return c.parseError(resp)
	}

	return nil
}

// Comment represents a comment on an article
type Comment struct {
	ID         string    `json:"id"`
	ArticleID  string    `json:"article_id"`
	ExternalID string    `json:"external_id"`
	Author     string    `json:"author"`
	Content    string    `json:"content"`
	WrittenAt  time.Time `json:"written_at"`
	ParentID   *string   `json:"parent_id,omitempty"`
	Depth      int       `json:"depth"`
}

// ArticleDetail represents an article with its comments
type ArticleDetail struct {
	Article    Article   `json:"article"`
	Comments   []Comment `json:"comments"`
	SourceType string    `json:"source_type"`
}

// GetArticle fetches a single article with its comments from the collector
func (c *Client) GetArticle(ctx context.Context, id string) (*ArticleDetail, error) {
	url := fmt.Sprintf("%s/articles/%s", c.baseURL, id)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("article not found")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var detail ArticleDetail
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &detail, nil
}

// parseError parses an error response from the collector
func (c *Client) parseError(resp *http.Response) error {
	var errResp ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		return fmt.Errorf("collector returned status %d", resp.StatusCode)
	}
	return fmt.Errorf("collector error (status %d): %s", resp.StatusCode, errResp.Error)
}
