package personalization

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/cheolwanpark/meows/collector/internal/db"
	"github.com/cheolwanpark/meows/collector/internal/gemini"
	"google.golang.org/genai"
)

const (
	// CURATION_SYSTEM_INSTRUCTION guides the AI to evaluate article relevance
	CURATION_SYSTEM_INSTRUCTION = `You are an intelligent article curator that evaluates whether articles match a user's interests based on their profile character.

Your task is to determine if an article is relevant and interesting to the user. Return a JSON response with:
- pass: true if the article matches the user's interests, false otherwise
- reason: A brief explanation (1-2 sentences) of your decision

Be thoughtful but concise in your reasoning.`

	// Curation service configuration constants
	curationJobBufferSize   = 100                    // Buffer 100 jobs (~10 seconds of work at 10 req/sec)
	curationRateLimit       = 100 * time.Millisecond // 10 requests per second to Gemini API
	curationRetryDelay      = 1 * time.Second        // Wait 1 second before retry
	curationAPITimeout      = 10 * time.Second       // Timeout for individual Gemini API calls
	curationPromptMaxLength = 500                    // Truncate article content to 500 chars to save tokens
)

// CurationResult represents the result of article curation
type CurationResult struct {
	Pass   bool   `json:"pass"`
	Reason string `json:"reason"`
}

// CurationJob represents a batch of articles to curate for a profile
type CurationJob struct {
	ProfileID string
	Articles  []db.Article
}

// CurationService handles async article curation with worker pool
type CurationService struct {
	db      *db.DB
	repo    *CurationRepository
	gemini  *gemini.Client
	workers int
	enabled bool

	jobChan     chan CurationJob
	workerWg    sync.WaitGroup
	shutdownCh  chan struct{}
	rateLimiter *time.Ticker
}

// NewCurationService creates a new curation service with worker pool
func NewCurationService(db *db.DB, gemini *gemini.Client, workers int, enabled bool) *CurationService {
	cs := &CurationService{
		db:         db,
		repo:       NewCurationRepository(db),
		gemini:     gemini,
		workers:    workers,
		enabled:    enabled,
		jobChan:    make(chan CurationJob, curationJobBufferSize),
		shutdownCh: make(chan struct{}),
	}

	// Only create rate limiter if curation is enabled
	if enabled {
		cs.rateLimiter = time.NewTicker(curationRateLimit)
	}

	return cs
}

// Start launches the worker pool
func (s *CurationService) Start() {
	if !s.enabled {
		slog.Info("Curation service disabled, not starting workers")
		return
	}

	slog.Info("Starting curation service", "workers", s.workers)
	for i := 0; i < s.workers; i++ {
		s.workerWg.Add(1)
		go s.worker(i)
	}
}

// Stop gracefully shuts down the worker pool
func (s *CurationService) Stop() {
	if !s.enabled {
		return
	}

	slog.Info("Stopping curation service")
	close(s.shutdownCh) // Signal shutdown first
	s.workerWg.Wait()   // Wait for all workers to finish
	close(s.jobChan)    // Then close channel (safe - no more sends)
	if s.rateLimiter != nil {
		s.rateLimiter.Stop()
	}
	slog.Info("Curation service stopped")
}

// EnqueueArticles adds articles to the curation queue (non-blocking)
func (s *CurationService) EnqueueArticles(profileID string, articles []db.Article) {
	if !s.enabled || len(articles) == 0 {
		return
	}

	// Non-blocking send - if queue is full, skip (curation is best-effort)
	select {
	case s.jobChan <- CurationJob{ProfileID: profileID, Articles: articles}:
		slog.Debug("Enqueued articles for curation", "profile_id", profileID, "count", len(articles))
	default:
		slog.Warn("Curation queue full, skipping articles", "profile_id", profileID, "count", len(articles))
	}
}

// worker processes curation jobs from the queue
func (s *CurationService) worker(id int) {
	defer s.workerWg.Done()
	slog.Debug("Curation worker started", "worker_id", id)

	for {
		select {
		case <-s.shutdownCh:
			slog.Debug("Curation worker shutting down", "worker_id", id)
			return

		case job, ok := <-s.jobChan:
			if !ok {
				slog.Debug("Curation worker exiting (channel closed)", "worker_id", id)
				return
			}

			s.processJob(id, job)
		}
	}
}

// processJob curates all articles for a profile
func (s *CurationService) processJob(workerID int, job CurationJob) {
	ctx := context.Background()

	// Fetch profile with character
	profile, err := s.repo.FetchProfileForCuration(ctx, job.ProfileID)
	if err != nil {
		slog.Error("Failed to fetch profile for curation", "profile_id", job.ProfileID, "error", err)
		return
	}

	// Skip if no character generated yet
	if profile.Character == nil || *profile.Character == "" {
		slog.Debug("Profile has no character, skipping curation", "profile_id", job.ProfileID)
		return
	}

	slog.Info("Processing curation job", "worker_id", workerID, "profile_id", job.ProfileID, "articles", len(job.Articles))

	// Curate each article
	curatedCount := 0
	errorCount := 0
	for _, article := range job.Articles {
		result, err := s.curateArticleWithRetry(ctx, profile, article)
		if err != nil {
			errorCount++
			slog.Error("Failed to curate article", "profile_id", job.ProfileID, "article_id", article.ID, "error", err)
			continue
		}

		if result.Pass {
			if err := s.repo.InsertCurated(ctx, job.ProfileID, article.ID, result.Reason); err != nil {
				slog.Error("Failed to insert curated article", "profile_id", job.ProfileID, "article_id", article.ID, "error", err)
			} else {
				curatedCount++
				slog.Debug("Article curated", "profile_id", job.ProfileID, "article_id", article.ID, "reason", result.Reason)
			}
		}
	}

	slog.Info("Curation job completed", "worker_id", workerID, "profile_id", job.ProfileID,
		"total", len(job.Articles), "curated", curatedCount, "errors", errorCount)
}

// curateArticleWithRetry curates a single article with retry logic
func (s *CurationService) curateArticleWithRetry(ctx context.Context, profile *db.Profile, article db.Article) (*CurationResult, error) {
	// Apply rate limiting
	<-s.rateLimiter.C

	// First attempt
	result, err := s.curateArticle(ctx, profile, article)
	if err == nil {
		return result, nil
	}

	slog.Warn("Curation failed, retrying", "article_id", article.ID, "error", err)

	// Wait before retry
	time.Sleep(curationRetryDelay)

	// Apply rate limiting again
	<-s.rateLimiter.C

	// Second attempt
	result, err = s.curateArticle(ctx, profile, article)
	if err == nil {
		return result, nil
	}

	// Both attempts failed - return error (fail closed, don't assume passed)
	return nil, fmt.Errorf("curation failed after retry: %w", err)
}

// curateArticle curates a single article using Gemini API
func (s *CurationService) curateArticle(ctx context.Context, profile *db.Profile, article db.Article) (*CurationResult, error) {
	// Build prompt
	prompt := s.buildCurationPrompt(profile, article)

	// Configure Gemini with structured output
	temperature := float32(0.3) // Lower temperature for more consistent decisions
	config := &genai.GenerateContentConfig{
		Temperature:       &temperature,
		SystemInstruction: genai.NewContentFromText(CURATION_SYSTEM_INSTRUCTION, ""),
		ResponseMIMEType:  "application/json",
		ResponseJsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pass": map[string]any{
					"type":        "boolean",
					"description": "Whether the article matches the user's interests",
				},
				"reason": map[string]any{
					"type":        "string",
					"description": "Brief explanation of the decision (1-2 sentences)",
				},
			},
			"required": []string{"pass", "reason"},
		},
	}

	// Create context with timeout
	apiCtx, cancel := context.WithTimeout(ctx, curationAPITimeout)
	defer cancel()

	// Call Gemini API with typed response
	result, err := gemini.GenerateContentTyped[CurationResult](s.gemini, apiCtx, gemini.FLASH, prompt, config)
	if err != nil {
		return nil, fmt.Errorf("gemini API call failed: %w", err)
	}

	return result, nil
}

// buildCurationPrompt creates the prompt for article curation
func (s *CurationService) buildCurationPrompt(profile *db.Profile, article db.Article) string {
	// Truncate content to save tokens
	content := article.Content
	if len(content) > curationPromptMaxLength {
		content = content[:curationPromptMaxLength] + "..."
	}

	// Handle empty fields
	author := article.Author
	if author == "" {
		author = "Unknown"
	}

	// Build prompt
	var sb strings.Builder
	sb.WriteString("You are evaluating if this article matches the user's interests.\n\n")
	sb.WriteString("User Profile Character:\n")
	if profile.Character != nil {
		sb.WriteString(*profile.Character)
	}
	sb.WriteString("\n\n")
	sb.WriteString("Article:\n")
	sb.WriteString(fmt.Sprintf("Title: %s\n", article.Title))
	sb.WriteString(fmt.Sprintf("Author: %s\n", author))
	sb.WriteString(fmt.Sprintf("Content: %s\n", content))
	sb.WriteString("\n")
	sb.WriteString("Does this article match the user's interests? Return JSON with {\"pass\": true/false, \"reason\": \"brief explanation\"}.")

	return sb.String()
}
