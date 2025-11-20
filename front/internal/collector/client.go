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
	ProfileID  string          `json:"profile_id"`
	Title      string          `json:"title"`
	Author     string          `json:"author"`
	Content    string          `json:"content"`
	URL        string          `json:"url,omitempty"`
	WrittenAt  time.Time       `json:"written_at"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
	Liked      bool            `json:"liked,omitempty"`   // Only populated when profile_id provided
	LikeID     string          `json:"like_id,omitempty"` // Only populated when liked=true
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
	Type      string          `json:"type"`
	Config    json.RawMessage `json:"config"`
	ProfileID string          `json:"profile_id"`
}

// Profile represents a user profile
type Profile struct {
	ID              string     `json:"id"`
	Nickname        string     `json:"nickname"`
	UserDescription string     `json:"user_description"`
	Character       string     `json:"character,omitempty"`
	CharacterStatus string     `json:"character_status"` // pending, updating, ready, error
	CharacterError  string     `json:"character_error,omitempty"`
	Milestone       string     `json:"milestone"`
	UpdatedAt       *time.Time `json:"updated_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

// CreateProfileRequest represents a request to create a new profile
type CreateProfileRequest struct {
	Nickname        string `json:"nickname"`
	UserDescription string `json:"user_description"`
}

// UpdateProfileRequest represents a request to update a profile
type UpdateProfileRequest struct {
	Nickname        *string `json:"nickname,omitempty"`
	UserDescription *string `json:"user_description,omitempty"`
}

// Like represents a like on an article
type Like struct {
	ID        string    `json:"id"`
	ProfileID string    `json:"profile_id"`
	ArticleID string    `json:"article_id"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateLikeRequest represents a request to like an article
type CreateLikeRequest struct {
	ProfileID string `json:"profile_id"`
}

// ProfileStatus represents the status of a profile (for efficient polling)
type ProfileStatus struct {
	CharacterStatus string `json:"character_status"`
	CharacterError  string `json:"character_error,omitempty"`
}

// ErrorResponse represents an error response from the collector
type ErrorResponse struct {
	Error string `json:"error"`
}

// StatusError wraps an HTTP error with its status code
type StatusError struct {
	StatusCode int
	Message    string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("collector error (status %d): %s", e.StatusCode, e.Message)
}

// GetArticles fetches articles from the collector
// profileID is optional - if provided, articles will include like status for that profile
func (c *Client) GetArticles(ctx context.Context, limit, offset int, profileID string) ([]Article, error) {
	url := fmt.Sprintf("%s/articles?limit=%d&offset=%d", c.baseURL, limit, offset)
	if profileID != "" {
		url += fmt.Sprintf("&profile_id=%s", profileID)
	}

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

// TriggerSource triggers an immediate crawl for a specific source
func (c *Client) TriggerSource(ctx context.Context, id string) error {
	url := fmt.Sprintf("%s/sources/%s/trigger", c.baseURL, id)

	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
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

// GetProfiles fetches all profiles from the collector
func (c *Client) GetProfiles(ctx context.Context) ([]Profile, error) {
	url := fmt.Sprintf("%s/profiles", c.baseURL)

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

	var profiles []Profile
	if err := json.NewDecoder(resp.Body).Decode(&profiles); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return profiles, nil
}

// GetProfile fetches a single profile from the collector
func (c *Client) GetProfile(ctx context.Context, id string) (*Profile, error) {
	url := fmt.Sprintf("%s/profiles/%s", c.baseURL, id)

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
		return nil, fmt.Errorf("profile not found")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var profile Profile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &profile, nil
}

// GetProfileStatus fetches only the character status of a profile (for efficient polling)
func (c *Client) GetProfileStatus(ctx context.Context, id string) (*ProfileStatus, error) {
	url := fmt.Sprintf("%s/profiles/%s/status", c.baseURL, id)

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
		return nil, fmt.Errorf("profile not found")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var status ProfileStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &status, nil
}

// CreateProfile creates a new profile in the collector
func (c *Client) CreateProfile(ctx context.Context, nickname, userDescription string) (*Profile, error) {
	url := fmt.Sprintf("%s/profiles", c.baseURL)

	req := CreateProfileRequest{
		Nickname:        nickname,
		UserDescription: userDescription,
	}

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

	var profile Profile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &profile, nil
}

// UpdateProfile updates an existing profile in the collector
func (c *Client) UpdateProfile(ctx context.Context, id string, updates map[string]string) error {
	url := fmt.Sprintf("%s/profiles/%s", c.baseURL, id)

	req := UpdateProfileRequest{}
	if nickname, ok := updates["nickname"]; ok {
		req.Nickname = &nickname
	}
	if desc, ok := updates["user_description"]; ok {
		req.UserDescription = &desc
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "PATCH", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.parseError(resp)
	}

	return nil
}

// DeleteProfile deletes a profile from the collector
func (c *Client) DeleteProfile(ctx context.Context, id string) error {
	url := fmt.Sprintf("%s/profiles/%s", c.baseURL, id)

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

// LikeArticle creates a like for an article
func (c *Client) LikeArticle(ctx context.Context, articleID, profileID string) (*Like, error) {
	url := fmt.Sprintf("%s/articles/%s/like", c.baseURL, articleID)

	req := CreateLikeRequest{
		ProfileID: profileID,
	}

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

	var like Like
	if err := json.NewDecoder(resp.Body).Decode(&like); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &like, nil
}

// UnlikeArticle deletes a like
func (c *Client) UnlikeArticle(ctx context.Context, likeID string) error {
	url := fmt.Sprintf("%s/likes/%s", c.baseURL, likeID)

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

// parseError parses an error response from the collector
func (c *Client) parseError(resp *http.Response) error {
	var errResp ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		return &StatusError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("status %d", resp.StatusCode),
		}
	}
	return &StatusError{
		StatusCode: resp.StatusCode,
		Message:    errResp.Error,
	}
}
