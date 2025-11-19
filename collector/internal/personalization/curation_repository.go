package personalization

import (
	"context"
	"fmt"

	"github.com/cheolwanpark/meows/collector/internal/db"
	"github.com/google/uuid"
)

// CurationRepository handles database operations for article curation
type CurationRepository struct {
	db *db.DB
}

// NewCurationRepository creates a new curation repository
func NewCurationRepository(db *db.DB) *CurationRepository {
	return &CurationRepository{db: db}
}

// FetchProfileForCuration retrieves a profile with its character for curation
func (r *CurationRepository) FetchProfileForCuration(ctx context.Context, profileID string) (*db.Profile, error) {
	query := `
		SELECT id, nickname, user_description, character, character_status, milestone, updated_at, created_at
		FROM profiles
		WHERE id = ?
	`

	var profile db.Profile
	err := r.db.QueryRowContext(ctx, query, profileID).Scan(
		&profile.ID,
		&profile.Nickname,
		&profile.UserDescription,
		&profile.Character,
		&profile.CharacterStatus,
		&profile.Milestone,
		&profile.UpdatedAt,
		&profile.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch profile: %w", err)
	}

	return &profile, nil
}

// InsertCurated inserts a curated article into the curated table
func (r *CurationRepository) InsertCurated(ctx context.Context, profileID, articleID, reason string) error {
	query := `
		INSERT INTO curated (id, profile_id, article_id, reason, created_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(profile_id, article_id) DO UPDATE SET
			reason = excluded.reason
	`

	id := uuid.New().String()
	_, err := r.db.ExecContext(ctx, query, id, profileID, articleID, reason)
	if err != nil {
		return fmt.Errorf("failed to insert curated article: %w", err)
	}

	return nil
}
