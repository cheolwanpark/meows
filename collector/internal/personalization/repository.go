package personalization

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/cheolwanpark/meows/collector/internal/db"
)

// setUpdatingStatus sets the profile status to 'updating' in a transaction
func (s *UpdateService) setUpdatingStatus(ctx context.Context, profileID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			slog.Error("Failed to rollback transaction", "profile_id", profileID, "error", err)
		}
	}()

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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			slog.Error("Failed to rollback transaction", "profile_id", profileID, "error", err)
		}
	}()

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
