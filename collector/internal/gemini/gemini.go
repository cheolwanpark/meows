package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
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

// sanitizeJSONResponse removes markdown code block wrappers and extra whitespace
// from Gemini responses. LLMs often wrap JSON in ```json...``` blocks or add preambles.
func sanitizeJSONResponse(text string) string {
	// Trim whitespace
	text = strings.TrimSpace(text)

	// Check for markdown code blocks anywhere in the response
	// Handles cases like "Here is the JSON:\n```json\n{...}\n```"
	if strings.Contains(text, "```json") {
		// Extract content between ```json and ```
		start := strings.Index(text, "```json")
		if start != -1 {
			text = text[start+7:] // Skip past ```json
			if end := strings.Index(text, "```"); end != -1 {
				text = text[:end]
			}
			text = strings.TrimSpace(text)
		}
	} else if strings.Contains(text, "```") {
		// Handle generic code blocks without "json" marker
		start := strings.Index(text, "```")
		if start != -1 {
			text = text[start+3:]
			if end := strings.Index(text, "```"); end != -1 {
				text = text[:end]
			}
			text = strings.TrimSpace(text)
		}
	}

	// Fallback: If no code blocks found and text doesn't start with {,
	// try to extract first JSON object
	if !strings.HasPrefix(text, "{") && !strings.HasPrefix(text, "[") {
		if start := strings.Index(text, "{"); start != -1 {
			text = text[start:]
		}
	}

	return text
}

// GenerateContentTyped makes a Gemini API call and unmarshals the response into type T.
// This is a generic function that handles any JSON response structure, with retry logic
// and proper error handling. Use this for responses with custom schemas.
//
// The function:
//   - Retries up to 3 times with exponential backoff
//   - Sanitizes responses (strips markdown code blocks)
//   - Logs raw responses on unmarshal failures for debugging
//   - Returns detailed errors with attempt numbers
//
// Example:
//
//	type MyResponse struct {
//	    Field1 string `json:"field1"`
//	    Field2 bool   `json:"field2"`
//	}
//	resp, err := gemini.GenerateContentTyped[MyResponse](client, ctx, FLASH, prompt, config)
func GenerateContentTyped[T any](c *Client, ctx context.Context, model string, prompt string, config *genai.GenerateContentConfig) (*T, error) {
	const (
		maxRetries     = 3
		baseDelay      = 1 * time.Second
		requestTimeout = 30 * time.Second
	)

	// Use provided config or create default, always ensure ResponseMIMEType is set
	if config == nil {
		config = &genai.GenerateContentConfig{}
	}
	if config.ResponseMIMEType == "" {
		config.ResponseMIMEType = "application/json"
	}

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			delay := baseDelay * time.Duration(1<<uint(attempt-1))
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		// Create context with timeout
		reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)

		// Call Gemini API
		result, err := c.client.Models.GenerateContent(
			reqCtx,
			model,
			genai.Text(prompt),
			config,
		)

		if err != nil {
			cancel()
			lastErr = fmt.Errorf("attempt %d: API call failed: %w", attempt+1, err)
			continue
		}

		// Extract text from response
		text := result.Text()
		if text == "" {
			cancel()
			lastErr = fmt.Errorf("attempt %d: empty response from API", attempt+1)
			continue
		}

		// Sanitize response (remove markdown wrappers)
		cleanText := sanitizeJSONResponse(text)

		// Parse JSON response into type T
		var response T
		if err := json.Unmarshal([]byte(cleanText), &response); err != nil {
			cancel()
			// Log raw response for debugging
			slog.Error("Failed to unmarshal Gemini response",
				"attempt", attempt+1,
				"error", err,
				"raw_response", text,
				"sanitized_response", cleanText,
			)
			lastErr = fmt.Errorf("attempt %d: failed to parse JSON response: %w (raw: %q)", attempt+1, err, text)
			continue
		}

		cancel()
		return &response, nil
	}

	return nil, fmt.Errorf("all %d attempts failed: %w", maxRetries, lastErr)
}

// GenerateContent calls Gemini API with retry logic and structured output parsing
// If config is nil, uses default config with ResponseMIMEType set to "application/json"
func (c *Client) GenerateContent(ctx context.Context, model string, prompt string, config *genai.GenerateContentConfig) (string, error) {
	const (
		maxRetries     = 3
		baseDelay      = 1 * time.Second
		requestTimeout = 30 * time.Second
	)

	// Use provided config or create default, always ensure ResponseMIMEType is set
	if config == nil {
		config = &genai.GenerateContentConfig{}
	}
	// Always enforce JSON response format regardless of config source
	if config.ResponseMIMEType == "" {
		config.ResponseMIMEType = "application/json"
	}

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

		// Call Gemini API with provided or default config
		result, err := c.client.Models.GenerateContent(
			reqCtx,
			model,
			genai.Text(prompt),
			config,
		)

		if err != nil {
			cancel() // Explicitly cancel instead of deferring to avoid accumulation
			lastErr = fmt.Errorf("attempt %d failed: %w", attempt+1, err)
			continue
		}

		// Extract text from response
		text := result.Text()
		if text == "" {
			cancel()
			lastErr = fmt.Errorf("attempt %d: empty response from API", attempt+1)
			continue
		}

		// Sanitize response (remove markdown wrappers)
		cleanText := sanitizeJSONResponse(text)

		// Parse JSON response
		var charResp CharacterResponse
		if err := json.Unmarshal([]byte(cleanText), &charResp); err != nil {
			cancel()
			// Log raw response for debugging
			slog.Error("Failed to unmarshal character response",
				"attempt", attempt+1,
				"error", err,
				"raw_response", text,
				"sanitized_response", cleanText,
			)
			lastErr = fmt.Errorf("attempt %d: failed to parse JSON response: %w (raw: %q)", attempt+1, err, text)
			continue
		}

		if charResp.Character == "" {
			cancel()
			lastErr = fmt.Errorf("attempt %d: character field is empty", attempt+1)
			continue
		}

		cancel() // Success path - clean up context
		return charResp.Character, nil
	}

	return "", fmt.Errorf("all %d attempts failed: %w", maxRetries, lastErr)
}

// Close closes the Gemini client (currently a no-op as genai.Client doesn't require explicit cleanup)
func (c *Client) Close() error {
	return nil
}
