package personalization

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/cheolwanpark/meows/collector/internal/db"
	"github.com/cheolwanpark/meows/collector/internal/gemini"
	"google.golang.org/genai"
)

// UpdateService handles profile character updates
type UpdateService struct {
	db                  *db.DB
	gemini              *gemini.Client
	milestoneThreshold1 int
	milestoneThreshold2 int
	milestoneThreshold3 int
}

// NewUpdateService creates a new UpdateService
func NewUpdateService(db *db.DB, gemini *gemini.Client, threshold1, threshold2, threshold3 int) *UpdateService {
	return &UpdateService{
		db:                  db,
		gemini:              gemini,
		milestoneThreshold1: threshold1,
		milestoneThreshold2: threshold2,
		milestoneThreshold3: threshold3,
	}
}

// UpdateCharacter orchestrates character generation with transactions
// Launched as a goroutine, so lost updates on crash are acceptable
// Transactions are kept short and do not span network I/O
func (s *UpdateService) UpdateCharacter(ctx context.Context, profileID string) {
	go func() {
		// Detach from HTTP request context to allow background processing
		// Preserve trace values while removing cancellation
		detachedCtx := context.WithoutCancel(ctx)

		// Add 3-minute timeout for API calls (Gemini has 30s timeout per request, with retries ~2min max)
		apiCtx, apiCancel := context.WithTimeout(detachedCtx, 3*time.Minute)
		defer apiCancel()

		// Step 1: Set status to 'updating' in a quick transaction
		// Use short-lived context for DB writes to ensure they always complete
		dbCtx, dbCancel := context.WithTimeout(detachedCtx, 5*time.Second)
		err := s.setUpdatingStatus(dbCtx, profileID)
		dbCancel()
		if err != nil {
			slog.Error("Failed to set updating status", "profile_id", profileID, "error", err)
			return
		}

		// Step 2: Fetch all required data (no transaction needed for reads)
		profile, err := s.fetchProfile(apiCtx, profileID)
		if err != nil {
			slog.Error("Failed to fetch profile", "profile_id", profileID, "error", err)
			dbCtx, dbCancel := context.WithTimeout(detachedCtx, 5*time.Second)
			s.setCharacterError(dbCtx, profileID, err.Error())
			dbCancel()
			return
		}

		articles, err := s.fetchLikedArticles(apiCtx, profileID)
		if err != nil {
			slog.Error("Failed to fetch likes", "profile_id", profileID, "error", err)
			dbCtx, dbCancel := context.WithTimeout(detachedCtx, 5*time.Second)
			s.setCharacterError(dbCtx, profileID, err.Error())
			dbCancel()
			return
		}

		// Step 3: Build prompt
		prompt, err := s.BuildCharacterPrompt(profile, articles)
		if err != nil {
			slog.Error("Failed to build prompt", "profile_id", profileID, "error", err)
			dbCtx, dbCancel := context.WithTimeout(detachedCtx, 5*time.Second)
			s.setCharacterError(dbCtx, profileID, err.Error())
			dbCancel()
			return
		}

		// Step 4: Call Gemini API (no transaction during network I/O)
		// Create config with temperature tuning and system instruction
		temperature := float32(0.3) // Reduced temperature to minimize creative hallucinations (not fully deterministic)
		topP := float32(0.8)        // Standard diversity
		config := &genai.GenerateContentConfig{
			Temperature:       &temperature,
			TopP:              &topP,
			SystemInstruction: genai.NewContentFromText(SYSTEM_INSTRUCTION, ""), // Empty role for system instruction
			ResponseMIMEType:  "application/json",
		}
		character, err := s.gemini.GenerateContent(apiCtx, gemini.FLASH, prompt, config)
		if err != nil {
			errorMsg := err.Error()
			if apiCtx.Err() == context.DeadlineExceeded {
				errorMsg = "Character generation timed out after 3 minutes. Please try again."
			}
			slog.Error("Failed to generate character", "profile_id", profileID, "error", err)
			dbCtx, dbCancel := context.WithTimeout(detachedCtx, 5*time.Second)
			s.setCharacterError(dbCtx, profileID, errorMsg)
			dbCancel()
			return
		}

		// Step 5: Update result in a final atomic transaction
		// Use separate DB context to ensure write completes even if API calls took long
		dbCtx, dbCancel = context.WithTimeout(detachedCtx, 5*time.Second)
		err = s.updateCharacterResult(dbCtx, profileID, character)
		dbCancel()
		if err != nil {
			slog.Error("Failed to update character", "profile_id", profileID, "error", err)
			dbCtx, dbCancel := context.WithTimeout(detachedCtx, 5*time.Second)
			s.setCharacterError(dbCtx, profileID, err.Error())
			dbCancel()
			return
		}

		slog.Info("Character updated successfully", "profile_id", profileID)
	}()
}

// CheckMilestone determines if an update should trigger based on like count
// Returns (shouldUpdate, newMilestone)
func (s *UpdateService) CheckMilestone(currentMilestone string, likeCount int) (bool, string) {
	switch currentMilestone {
	case "init":
		if likeCount >= s.milestoneThreshold1 {
			return true, fmt.Sprintf("%d", s.milestoneThreshold1)
		}
	case fmt.Sprintf("%d", s.milestoneThreshold1):
		if likeCount >= s.milestoneThreshold2 {
			return true, fmt.Sprintf("%d", s.milestoneThreshold2)
		}
	case fmt.Sprintf("%d", s.milestoneThreshold2):
		if likeCount >= s.milestoneThreshold3 {
			return true, fmt.Sprintf("%d", s.milestoneThreshold3)
		}
	case fmt.Sprintf("%d", s.milestoneThreshold3):
		// Transition to weekly WITH final update
		return true, "weekly"
	case "weekly":
		// Weekly updates handled by cron
		return false, "weekly"
	}
	return false, currentMilestone
}
