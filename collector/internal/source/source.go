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

	// SourceType returns the type of this source ("reddit" or "semantic_scholar")
	SourceType() string

	// Validate checks if the source configuration is valid
	Validate() error
}

// Factory creates a Source from a database source record
// Accepts global config (rate limits), app config (secrets), and shared rate limiter
func Factory(
	source *db.Source,
	globalConfig *db.GlobalConfig,
	appConfig *config.Config,
	sharedLimiter *rate.Limiter,
	maxCommentDepth int,
) (Source, error) {
	switch source.Type {
	case "reddit":
		return NewRedditSource(source, globalConfig, appConfig, sharedLimiter, maxCommentDepth)
	case "semantic_scholar":
		return NewSemanticScholarSource(source, globalConfig, appConfig, sharedLimiter)
	default:
		return nil, fmt.Errorf("unknown source type: %s", source.Type)
	}
}
