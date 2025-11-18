package profile

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/cheolwanpark/meows/collector/internal/db"
	"github.com/cheolwanpark/meows/collector/internal/gemini"
)

const (
	BASE_PROMPT_TEMPLATE = `You are a character narrator. Based on the user's preferences and reading habits, create a short, witty, and insightful character description (2-3 sentences).

User's Self-Description: %s

Previous Character: %s

Recent Articles Liked:
%s

Return JSON: {"character": "your description here"}`

	ARTICLE_TEMPLATE = "- [%s]: %s\n"
	CONTENT_PREVIEW  = 100 // Max characters for article content preview
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

// BuildCharacterPrompt creates a prompt with progressive article addition and token checking
func (s *UpdateService) BuildCharacterPrompt(profile *db.Profile, articles []db.Article) (string, error) {
	userDesc := profile.UserDescription
	if userDesc == "" {
		userDesc = "No description provided"
	}

	oldChar := "No previous character"
	if profile.Character != nil && *profile.Character != "" {
		oldChar = *profile.Character
	}

	// Start with empty articles list
	articlesText := ""
	if len(articles) == 0 {
		articlesText = "No articles liked yet"
	} else {
		// Progressively add articles up to token limit
		var builder strings.Builder
		for i, article := range articles {
			if i >= 20 {
				// Max 20 articles as specified
				break
			}

			// Prepare content preview
			content := article.Content
			if len(content) > CONTENT_PREVIEW {
				content = content[:CONTENT_PREVIEW] + "..."
			}

			// Add article to the list
			articleLine := fmt.Sprintf(ARTICLE_TEMPLATE, article.Title, content)
			builder.WriteString(articleLine)

			// Build test prompt with current articles
			testArticlesText := builder.String()
			testPrompt := fmt.Sprintf(BASE_PROMPT_TEMPLATE, userDesc, oldChar, testArticlesText)

			// Count tokens
			tokens, err := s.gemini.CountTokens(testPrompt)
			if err != nil {
				slog.Warn("Failed to count tokens, continuing anyway", "error", err)
			} else if tokens > gemini.MAX_TOKENS {
				// Remove last article and break
				slog.Info("Token limit reached", "articles_included", i, "tokens", tokens)
				// Rebuild without the last article
				builder.Reset()
				for j := 0; j < i; j++ {
					c := articles[j].Content
					if len(c) > CONTENT_PREVIEW {
						c = c[:CONTENT_PREVIEW] + "..."
					}
					builder.WriteString(fmt.Sprintf(ARTICLE_TEMPLATE, articles[j].Title, c))
				}
				break
			}
		}
		articlesText = builder.String()
		if articlesText == "" {
			articlesText = "No articles liked yet"
		}
	}

	return fmt.Sprintf(BASE_PROMPT_TEMPLATE, userDesc, oldChar, articlesText), nil
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
		character, err := s.gemini.GenerateContent(apiCtx, gemini.FLASH, prompt)
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

// setUpdatingStatus sets the profile status to 'updating' in a transaction
func (s *UpdateService) setUpdatingStatus(ctx context.Context, profileID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		UPDATE profiles
		SET character_status = 'updating'
		WHERE id = ?
	`, profileID)
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

// fetchProfile retrieves profile data without a transaction
func (s *UpdateService) fetchProfile(ctx context.Context, profileID string) (*db.Profile, error) {
	var profile db.Profile
	err := s.db.QueryRowContext(ctx, `
		SELECT id, nickname, user_description, character, character_status,
		       character_error, milestone, updated_at, created_at
		FROM profiles WHERE id = ?
	`, profileID).Scan(
		&profile.ID, &profile.Nickname, &profile.UserDescription, &profile.Character,
		&profile.CharacterStatus, &profile.CharacterError, &profile.Milestone,
		&profile.UpdatedAt, &profile.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("query profile: %w", err)
	}
	return &profile, nil
}

// fetchLikedArticles retrieves recent liked articles without a transaction
func (s *UpdateService) fetchLikedArticles(ctx context.Context, profileID string) ([]db.Article, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id, a.source_id, a.external_id, a.profile_id, a.title,
		       a.author, a.content, a.url, a.written_at, a.metadata, a.created_at
		FROM likes l
		JOIN articles a ON l.article_id = a.id
		WHERE l.profile_id = ?
		ORDER BY l.created_at DESC
		LIMIT 20
	`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query likes: %w", err)
	}
	defer rows.Close()

	var articles []db.Article
	for rows.Next() {
		var article db.Article
		var metadata sql.NullString
		err := rows.Scan(
			&article.ID, &article.SourceID, &article.ExternalID, &article.ProfileID,
			&article.Title, &article.Author, &article.Content, &article.URL,
			&article.WrittenAt, &metadata, &article.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan article: %w", err)
		}
		if metadata.Valid {
			article.Metadata = []byte(metadata.String)
		}
		articles = append(articles, article)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return articles, nil
}

// updateCharacterResult saves the generated character in an atomic transaction
func (s *UpdateService) updateCharacterResult(ctx context.Context, profileID string, character string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	now := time.Now()
	_, err = tx.ExecContext(ctx, `
		UPDATE profiles
		SET character = ?,
		    character_status = 'ready',
		    character_error = NULL,
		    updated_at = ?
		WHERE id = ?
	`, character, now, profileID)
	if err != nil {
		return fmt.Errorf("update character: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

// setCharacterError updates the profile with an error status
func (s *UpdateService) setCharacterError(ctx context.Context, profileID string, errorMsg string) {
	_, err := s.db.ExecContext(ctx, `
		UPDATE profiles
		SET character_status = 'error', character_error = ?
		WHERE id = ?
	`, errorMsg, profileID)
	if err != nil {
		slog.Error("Failed to set character error", "profile_id", profileID, "error", err)
	}
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
