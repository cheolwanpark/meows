package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"google.golang.org/genai"
	"google.golang.org/genai/tokenizer"
)

// Model constants
const (
	// TOKENIZER_MODEL is used for local token counting.
	// NOTE: The local tokenizer doesn't support preview models, so we use the stable release.
	// Token counts should be nearly identical between stable and preview variants.
	TOKENIZER_MODEL = "gemini-2.5-flash"

	// Generation models - these support preview versions
	FLASH      = "gemini-2.5-flash-preview-09-2025"
	FLASH_LITE = "gemini-2.5-flash-lite-preview-09-2025"
	MAX_TOKENS = 900000
)

// CharacterResponse represents the structured output from Gemini
type CharacterResponse struct {
	Character string `json:"character"`
}

// Client is a low-level Gemini API client
type Client struct {
	client    *genai.Client
	tokenizer *tokenizer.LocalTokenizer
}

// NewClient creates a new Gemini client
func NewClient(ctx context.Context, apiKey string) (*Client, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create genai client: %w", err)
	}

	tok, err := tokenizer.NewLocalTokenizer(TOKENIZER_MODEL)
	if err != nil {
		return nil, fmt.Errorf("failed to create tokenizer: %w", err)
	}

	return &Client{
		client:    client,
		tokenizer: tok,
	}, nil
}

// CountTokens counts tokens in a prompt without making an API call
func (c *Client) CountTokens(prompt string) (int, error) {
	contents := []*genai.Content{genai.NewContentFromText(prompt, "user")}
	result, err := c.tokenizer.CountTokens(contents, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to count tokens: %w", err)
	}
	return int(result.TotalTokens), nil
}

// GenerateContent calls Gemini API with retry logic and structured output parsing
func (c *Client) GenerateContent(ctx context.Context, model string, prompt string) (string, error) {
	const (
		maxRetries     = 3
		baseDelay      = 1 * time.Second
		requestTimeout = 30 * time.Second
	)

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			delay := baseDelay * time.Duration(1<<uint(attempt-1))
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(delay):
			}
		}

		// Create context with timeout
		reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
		defer cancel()

		// Call Gemini API
		result, err := c.client.Models.GenerateContent(
			reqCtx,
			model,
			genai.Text(prompt),
			&genai.GenerateContentConfig{
				ResponseMIMEType: "application/json",
			},
		)

		if err != nil {
			lastErr = fmt.Errorf("attempt %d failed: %w", attempt+1, err)
			continue
		}

		// Extract text from response
		text := result.Text()
		if text == "" {
			lastErr = fmt.Errorf("attempt %d: empty response from API", attempt+1)
			continue
		}

		// Parse JSON response
		var charResp CharacterResponse
		if err := json.Unmarshal([]byte(text), &charResp); err != nil {
			lastErr = fmt.Errorf("attempt %d: failed to parse JSON response: %w", attempt+1, err)
			continue
		}

		if charResp.Character == "" {
			lastErr = fmt.Errorf("attempt %d: character field is empty", attempt+1)
			continue
		}

		return charResp.Character, nil
	}

	return "", fmt.Errorf("all %d attempts failed: %w", maxRetries, lastErr)
}

// Close closes the Gemini client (currently a no-op as genai.Client doesn't require explicit cleanup)
func (c *Client) Close() error {
	return nil
}
