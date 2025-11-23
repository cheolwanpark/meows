package source

import (
	"context"
	"fmt"
	"time"

	"github.com/cheolwanpark/meows/collector/internal/config"
	"github.com/cheolwanpark/meows/collector/internal/db"
	"golang.org/x/time/rate"
)

// Source is the interface that all content sources must implement
type Source interface {
	// Fetch retrieves articles since the given time
	Fetch(ctx context.Context, since time.Time) ([]db.Article, []db.Comment, error)

	// SourceType returns the type of this source ("reddit", "semantic_scholar", or "hackernews")
	SourceType() string

	// Validate checks if the source configuration is valid
	Validate() error
}

// Factory creates a Source from a database source record with credentials from config file
func Factory(
	source *db.Source,
	credentials *config.CredentialsConfig,
	sharedLimiter *rate.Limiter,
	maxCommentDepth int,
) (Source, error) {
	switch source.Type {
	case "reddit":
		return NewRedditSource(source, credentials, sharedLimiter, maxCommentDepth)
	case "semantic_scholar":
		return NewSemanticScholarSource(source, credentials, sharedLimiter)
	case "hackernews":
		return NewHackerNewsSource(source, credentials, sharedLimiter, maxCommentDepth)
	default:
		return nil, fmt.Errorf("unknown source type: %s", source.Type)
	}
}
