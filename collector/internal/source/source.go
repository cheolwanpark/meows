package source

import (
	"context"
	"fmt"
	"time"

	"github.com/cheolwanpark/meows/collector/internal/db"
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
func Factory(source *db.Source, maxCommentDepth int) (Source, error) {
	switch source.Type {
	case "reddit":
		return NewRedditSource(source, maxCommentDepth)
	case "semantic_scholar":
		return NewSemanticScholarSource(source)
	default:
		return nil, fmt.Errorf("unknown source type: %s", source.Type)
	}
}
