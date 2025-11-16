package source

import (
	"context"
	"fmt"
	"time"

	"github.com/cheolwanpark/meows/collector/internal/db"
	"golang.org/x/time/rate"
)

// Source is the interface that all content sources must implement
type Source interface {
	// Fetch retrieves articles since the given time
	Fetch(ctx context.Context, since time.Time) ([]db.Article, []db.Comment, error)

	// SourceType returns the type of this source ("reddit" or "semantic_scholar")
	SourceType() string

	// Validate checks if the source configuration is valid
	Validate() error
}

// Factory creates a Source from a database source record
// Loads fresh global config with decrypted credentials for each source run
func Factory(
	source *db.Source,
	database *db.DB,
	sharedLimiter *rate.Limiter,
	maxCommentDepth int,
) (Source, error) {
	// Load global config with decrypted credentials (fresh for each run)
	globalConfig, err := database.GetGlobalConfigWithCredentials()
	if err != nil {
		return nil, fmt.Errorf("failed to load global config with credentials: %w", err)
	}

	switch source.Type {
	case "reddit":
		return NewRedditSource(source, globalConfig, sharedLimiter, maxCommentDepth)
	case "semantic_scholar":
		return NewSemanticScholarSource(source, globalConfig, sharedLimiter)
	default:
		return nil, fmt.Errorf("unknown source type: %s", source.Type)
	}
}
