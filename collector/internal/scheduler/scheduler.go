package scheduler

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/cheolwanpark/meows/collector/internal/db"
	"github.com/cheolwanpark/meows/collector/internal/source"
	"github.com/robfig/cron/v3"
)

// Scheduler manages scheduled crawling jobs
type Scheduler struct {
	cron            *cron.Cron
	db              *db.DB
	maxCommentDepth int
	jobs            map[string]cron.EntryID // sourceID -> cron entry ID
	mu              sync.RWMutex
}

// New creates a new Scheduler
func New(database *db.DB, maxCommentDepth int) *Scheduler {
	c := cron.New(
		cron.WithChain(
			cron.SkipIfStillRunning(cron.DefaultLogger),
			cron.Recover(cron.DefaultLogger),
		),
	)

	return &Scheduler{
		cron:            c,
		db:              database,
		maxCommentDepth: maxCommentDepth,
		jobs:            make(map[string]cron.EntryID),
	}
}

// Start starts the scheduler
func (s *Scheduler) Start() {
	s.cron.Start()
	log.Println("Scheduler started")
}

// Stop stops the scheduler gracefully
func (s *Scheduler) Stop(ctx context.Context) error {
	stopCtx := s.cron.Stop()

	select {
	case <-stopCtx.Done():
		log.Println("Scheduler stopped gracefully")
		return nil
	case <-ctx.Done():
		return fmt.Errorf("scheduler shutdown timeout")
	}
}

// LoadSourcesFromDB loads all sources from the database and registers their jobs
func (s *Scheduler) LoadSourcesFromDB() error {
	rows, err := s.db.Query("SELECT id, type, config, cron_expr, external_id, last_run_at, last_success_at, last_error, status, created_at FROM sources")
	if err != nil {
		return fmt.Errorf("failed to query sources: %w", err)
	}
	defer rows.Close()

	var count int
	for rows.Next() {
		var src db.Source
		var lastRunAt, lastSuccessAt sql.NullTime
		var lastError sql.NullString
		var externalID sql.NullString

		err := rows.Scan(
			&src.ID,
			&src.Type,
			&src.Config,
			&src.CronExpr,
			&externalID,
			&lastRunAt,
			&lastSuccessAt,
			&lastError,
			&src.Status,
			&src.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("failed to scan source: %w", err)
		}

		if externalID.Valid {
			src.ExternalID = externalID.String
		}
		if lastRunAt.Valid {
			src.LastRunAt = &lastRunAt.Time
		}
		if lastSuccessAt.Valid {
			src.LastSuccessAt = &lastSuccessAt.Time
		}
		if lastError.Valid {
			src.LastError = lastError.String
		}

		if err := s.RegisterJob(&src); err != nil {
			log.Printf("Warning: failed to register job for source %s: %v", src.ID, err)
			continue
		}
		count++
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating sources: %w", err)
	}

	log.Printf("Loaded %d sources from database", count)
	return nil
}

// RegisterJob registers a cron job for a source
func (s *Scheduler) RegisterJob(src *db.Source) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove existing job if present
	if entryID, exists := s.jobs[src.ID]; exists {
		s.cron.Remove(entryID)
		delete(s.jobs, src.ID)
	}

	// Parse and validate cron expression
	_, err := cron.ParseStandard(src.CronExpr)
	if err != nil {
		return fmt.Errorf("invalid cron expression '%s': %w", src.CronExpr, err)
	}

	// Register the job
	// Capture sourceID to avoid closure issues
	sourceID := src.ID
	entryID, err := s.cron.AddFunc(src.CronExpr, func() {
		if err := s.runJob(sourceID); err != nil {
			log.Printf("Job failed for source %s: %v", sourceID, err)
		}
	})
	if err != nil {
		return fmt.Errorf("failed to add cron job: %w", err)
	}

	s.jobs[src.ID] = entryID
	log.Printf("Registered job for source %s (type: %s, schedule: %s)", src.ID, src.Type, src.CronExpr)
	return nil
}

// UnregisterJob removes a cron job for a source
func (s *Scheduler) UnregisterJob(sourceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entryID, exists := s.jobs[sourceID]; exists {
		s.cron.Remove(entryID)
		delete(s.jobs, sourceID)
		log.Printf("Unregistered job for source %s", sourceID)
	}
}

// runJob executes a crawling job for a source
func (s *Scheduler) runJob(sourceID string) error {
	ctx := context.Background()

	// Update status to 'running'
	if err := s.updateSourceStatus(sourceID, "running"); err != nil {
		return err
	}

	// Always set status back to 'idle' when done
	defer func() {
		if err := s.updateSourceStatus(sourceID, "idle"); err != nil {
			log.Printf("Failed to set source %s status to idle: %v", sourceID, err)
		}
	}()

	// Fetch source from database
	src, err := s.getSource(sourceID)
	if err != nil {
		return s.recordError(sourceID, err)
	}

	// Create source instance
	sourceImpl, err := source.Factory(src, s.maxCommentDepth)
	if err != nil {
		return s.recordError(sourceID, err)
	}

	// Determine 'since' time (last successful fetch or epoch)
	since := time.Unix(0, 0)
	if src.LastSuccessAt != nil {
		since = *src.LastSuccessAt
	}

	// Fetch articles and comments
	articles, comments, err := sourceImpl.Fetch(ctx, since)
	if err != nil {
		return s.recordError(sourceID, err)
	}

	// Store articles and comments in database
	if err := s.storeArticles(articles); err != nil {
		return s.recordError(sourceID, err)
	}

	if err := s.storeComments(comments); err != nil {
		return s.recordError(sourceID, err)
	}

	// Update last_run_at and last_success_at
	now := time.Now()
	_, err = s.db.Exec(
		"UPDATE sources SET last_run_at = ?, last_success_at = ?, last_error = NULL WHERE id = ?",
		now, now, sourceID,
	)
	if err != nil {
		return fmt.Errorf("failed to update source timestamps: %w", err)
	}

	log.Printf("Job completed for source %s: fetched %d articles, %d comments", sourceID, len(articles), len(comments))
	return nil
}

// getSource retrieves a source from the database
func (s *Scheduler) getSource(sourceID string) (*db.Source, error) {
	var src db.Source
	var lastRunAt, lastSuccessAt sql.NullTime
	var lastError, externalID sql.NullString

	err := s.db.QueryRow(
		"SELECT id, type, config, cron_expr, external_id, last_run_at, last_success_at, last_error, status, created_at FROM sources WHERE id = ?",
		sourceID,
	).Scan(
		&src.ID,
		&src.Type,
		&src.Config,
		&src.CronExpr,
		&externalID,
		&lastRunAt,
		&lastSuccessAt,
		&lastError,
		&src.Status,
		&src.CreatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to fetch source: %w", err)
	}

	if externalID.Valid {
		src.ExternalID = externalID.String
	}
	if lastRunAt.Valid {
		src.LastRunAt = &lastRunAt.Time
	}
	if lastSuccessAt.Valid {
		src.LastSuccessAt = &lastSuccessAt.Time
	}
	if lastError.Valid {
		src.LastError = lastError.String
	}

	return &src, nil
}

// updateSourceStatus updates the status of a source
func (s *Scheduler) updateSourceStatus(sourceID, status string) error {
	_, err := s.db.Exec("UPDATE sources SET status = ? WHERE id = ?", status, sourceID)
	if err != nil {
		return fmt.Errorf("failed to update source status: %w", err)
	}
	return nil
}

// recordError records an error for a source
func (s *Scheduler) recordError(sourceID string, err error) error {
	now := time.Now()
	_, dbErr := s.db.Exec(
		"UPDATE sources SET last_run_at = ?, last_error = ? WHERE id = ?",
		now, err.Error(), sourceID,
	)
	if dbErr != nil {
		log.Printf("Failed to record error for source %s: %v", sourceID, dbErr)
	}
	return err
}

// storeArticles stores articles in the database using UPSERT
func (s *Scheduler) storeArticles(articles []db.Article) error {
	if len(articles) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO articles (id, source_id, external_id, title, author, content, url, written_at, metadata, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source_id, external_id) DO UPDATE SET
			metadata = excluded.metadata,
			content = excluded.content
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, article := range articles {
		_, err := stmt.Exec(
			article.ID,
			article.SourceID,
			article.ExternalID,
			article.Title,
			article.Author,
			article.Content,
			article.URL,
			article.WrittenAt,
			article.Metadata,
			article.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("failed to insert article %s: %w", article.ID, err)
		}
	}

	return tx.Commit()
}

// storeComments stores comments in the database using UPSERT
func (s *Scheduler) storeComments(comments []db.Comment) error {
	if len(comments) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO comments (id, article_id, external_id, author, content, written_at, parent_id, depth)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(article_id, external_id) DO NOTHING
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, comment := range comments {
		_, err := stmt.Exec(
			comment.ID,
			comment.ArticleID,
			comment.ExternalID,
			comment.Author,
			comment.Content,
			comment.WrittenAt,
			comment.ParentID,
			comment.Depth,
		)
		if err != nil {
			return fmt.Errorf("failed to insert comment %s: %w", comment.ID, err)
		}
	}

	return tx.Commit()
}

// GetSchedule returns scheduled jobs for the next duration
func (s *Scheduler) GetSchedule(duration time.Duration) ([]db.ScheduleEntry, error) {
	now := time.Now()
	cutoff := now.Add(duration)

	rows, err := s.db.Query(`
		SELECT id, type, cron_expr, last_run_at
		FROM sources
		WHERE status != 'running'
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query sources: %w", err)
	}
	defer rows.Close()

	var schedule []db.ScheduleEntry

	for rows.Next() {
		var id, sourceType, cronExpr string
		var lastRunAt sql.NullTime

		if err := rows.Scan(&id, &sourceType, &cronExpr, &lastRunAt); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Parse cron expression
		sched, err := cron.ParseStandard(cronExpr)
		if err != nil {
			log.Printf("Warning: invalid cron expression for source %s: %v", id, err)
			continue
		}

		// Calculate next run time
		var nextRun time.Time
		if lastRunAt.Valid {
			nextRun = sched.Next(lastRunAt.Time)
		} else {
			nextRun = sched.Next(now)
		}

		// Include if within duration
		if nextRun.Before(cutoff) {
			entry := db.ScheduleEntry{
				SourceID:   id,
				SourceType: sourceType,
				NextRun:    nextRun,
			}
			if lastRunAt.Valid {
				entry.LastRunAt = &lastRunAt.Time
			}
			schedule = append(schedule, entry)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return schedule, nil
}
