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
	"golang.org/x/time/rate"
)

// Scheduler manages scheduled crawling jobs with global configuration
type Scheduler struct {
	cron            *cron.Cron
	db              *db.DB
	maxCommentDepth int
	mu              sync.RWMutex
	reloadMu        sync.Mutex // Serializes reload operations to prevent concurrent reloads
	globalConfig    *db.GlobalConfigDTO
	rateLimiters    map[string]*rate.Limiter // Long-lived rate limiters per source type
	isRunning       bool
}

// New creates a new Scheduler
func New(database *db.DB, maxCommentDepth int) (*Scheduler, error) {
	// Load global config from database (without credentials)
	globalConfig, err := database.GetGlobalConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load global config: %w", err)
	}

	s := &Scheduler{
		db:              database,
		maxCommentDepth: maxCommentDepth,
		globalConfig:    globalConfig,
	}

	// Create long-lived rate limiters
	s.rateLimiters = s.createRateLimiters(globalConfig)

	// Create cron instance (will be populated in Start)
	if err := s.createCron(); err != nil {
		return nil, err
	}

	return s, nil
}

// createCron creates a new cron instance with the current global config
func (s *Scheduler) createCron() error {
	s.cron = cron.New(
		cron.WithChain(
			cron.SkipIfStillRunning(cron.DefaultLogger),
			cron.Recover(cron.DefaultLogger),
		),
	)

	// Register the single global job
	_, err := s.cron.AddFunc(s.globalConfig.CronExpr, func() {
		if err := s.runAllSources(); err != nil {
			log.Printf("Global crawl job failed: %v", err)
		}
	})
	if err != nil {
		return fmt.Errorf("failed to register global cron job: %w", err)
	}

	log.Printf("Registered global cron job with schedule: %s", s.globalConfig.CronExpr)
	return nil
}

// Start starts the scheduler
func (s *Scheduler) Start() {
	s.cron.Start()
	log.Println("Scheduler started with global cron schedule")
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

// Reload reloads the global configuration and restarts the cron scheduler.
// NOTE: This function is not re-entrant and MUST NOT be called from a job
// managed by this scheduler, as it will cause a deadlock.
func (s *Scheduler) Reload() error {
	// Serialize reload operations - only one reload at a time
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()

	// Load and validate new config BEFORE stopping anything
	log.Printf("Loading new configuration...")
	newConfig, err := s.db.GetGlobalConfig()
	if err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}

	// Validate cron expression before stopping scheduler
	log.Printf("Validating new cron expression: %s", newConfig.CronExpr)
	_, err = cron.ParseStandard(newConfig.CronExpr)
	if err != nil {
		return fmt.Errorf("invalid cron expression '%s': %w", newConfig.CronExpr, err)
	}

	// Save old state for potential rollback
	s.mu.RLock()
	oldCron := s.cron
	oldConfig := s.globalConfig
	oldRateLimiters := s.rateLimiters
	s.mu.RUnlock()

	// Stop old scheduler WITHOUT holding write lock (prevents deadlock)
	log.Printf("Stopping current scheduler...")
	stopCtx := oldCron.Stop()

	// Wait for all jobs to finish with timeout
	select {
	case <-stopCtx.Done():
		log.Printf("Scheduler stopped successfully")
	case <-time.After(30 * time.Second):
		return fmt.Errorf("timeout waiting for running jobs to stop")
	}

	// Create new rate limiters
	newRateLimiters := s.createRateLimiters(newConfig)

	// Create new cron instance
	newCron := cron.New(
		cron.WithChain(
			cron.SkipIfStillRunning(cron.DefaultLogger),
			cron.Recover(cron.DefaultLogger),
		),
	)

	// Register global job with new cron
	_, err = newCron.AddFunc(newConfig.CronExpr, func() {
		if err := s.runAllSources(); err != nil {
			log.Printf("Global crawl job failed: %v", err)
		}
	})
	if err != nil {
		// Registration failed - create fresh cron with old config and rollback
		log.Printf("Failed to register new cron job, rolling back...")

		rollbackCron := cron.New(
			cron.WithChain(
				cron.SkipIfStillRunning(cron.DefaultLogger),
				cron.Recover(cron.DefaultLogger),
			),
		)
		_, rollbackErr := rollbackCron.AddFunc(oldConfig.CronExpr, func() {
			if err := s.runAllSources(); err != nil {
				log.Printf("Global crawl job failed: %v", err)
			}
		})
		if rollbackErr != nil {
			log.Printf("FATAL: Failed to rollback scheduler - system may be in broken state!")
			return fmt.Errorf("failed to register cron job AND rollback failed: original=%w, rollback=%v", err, rollbackErr)
		}

		// Rollback successful - restore old state and start rollback cron
		s.mu.Lock()
		s.cron = rollbackCron
		s.globalConfig = oldConfig
		s.rateLimiters = oldRateLimiters
		s.mu.Unlock()

		rollbackCron.Start()
		log.Printf("Rollback successful - scheduler restored to previous configuration")
		return fmt.Errorf("failed to register cron job, rolled back to old config: %w", err)
	}

	// Success - atomically swap in new state
	s.mu.Lock()
	s.cron = newCron
	s.globalConfig = newConfig
	s.rateLimiters = newRateLimiters
	s.mu.Unlock()

	// Start new scheduler
	newCron.Start()
	log.Printf("Scheduler reloaded successfully with schedule: %s", newConfig.CronExpr)

	return nil
}

// RunNow manually triggers a crawl for all sources
func (s *Scheduler) RunNow() error {
	s.mu.Lock()
	if s.isRunning {
		s.mu.Unlock()
		return fmt.Errorf("crawl job is already running")
	}
	s.isRunning = true // Set BEFORE unlocking to prevent race
	s.mu.Unlock()

	// Run in goroutine to return immediately
	go func() {
		defer func() {
			s.mu.Lock()
			s.isRunning = false
			s.mu.Unlock()
		}()
		if err := s.runAllSources(); err != nil {
			log.Printf("Manual crawl job failed: %v", err)
		}
	}()

	return nil
}

// runAllSources orchestrates crawling all sources
// - Groups sources by type
// - Runs different types in parallel (goroutines)
// - Runs same-type sources sequentially
// - Uses shared rate limiter per source type
func (s *Scheduler) runAllSources() error {
	s.mu.Lock()
	if s.isRunning {
		s.mu.Unlock()
		log.Println("Crawl job already running, skipping this execution")
		return nil
	}
	s.isRunning = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.isRunning = false
		s.mu.Unlock()
	}()

	log.Println("Starting global crawl job for all sources")

	// Fetch all sources from DB
	sources, err := s.getAllSources()
	if err != nil {
		return fmt.Errorf("failed to fetch sources: %w", err)
	}

	if len(sources) == 0 {
		log.Println("No sources configured, skipping crawl")
		return nil
	}

	// Group sources by type
	typeGroups := s.groupSourcesByType(sources)

	// Launch goroutine per source type
	var wg sync.WaitGroup
	errChan := make(chan error, len(typeGroups))

	for sourceType, typeSources := range typeGroups {
		wg.Add(1)
		go func(typ string, srcs []*db.Source) {
			defer wg.Done()
			log.Printf("Starting sequential crawl for %d %s sources", len(srcs), typ)
			if err := s.runSourcesSequentially(srcs, s.rateLimiters[typ]); err != nil {
				errChan <- fmt.Errorf("%s sources failed: %w", typ, err)
			}
		}(sourceType, typeSources)
	}

	// Wait for all source types to complete
	wg.Wait()
	close(errChan)

	// Aggregate errors
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		log.Printf("Global crawl job completed with %d errors", len(errors))
		return fmt.Errorf("%d source type(s) failed", len(errors))
	}

	log.Println("Global crawl job completed successfully")
	return nil
}

// runSourcesSequentially executes sources of the same type one after another
// Fetches each source then stores results in per-source atomic transaction
func (s *Scheduler) runSourcesSequentially(sources []*db.Source, limiter *rate.Limiter) error {
	for _, src := range sources {
		// Per-source timeout (5 minutes)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)

		log.Printf("Processing source %s (type: %s)", src.ID, src.Type)

		// Update status to running
		if err := s.updateSourceStatus(src.ID, "running"); err != nil {
			log.Printf("Failed to update status for source %s: %v", src.ID, err)
		}

		// Execute fetch with timeout
		articles, comments, fetchErr := s.runSourceWithTimeout(ctx, src, limiter)

		// Cancel context immediately (don't defer in loop)
		cancel()

		// Handle fetch error
		if fetchErr != nil {
			log.Printf("Source %s fetch failed: %v", src.ID, fetchErr)
			s.recordError(src.ID, fetchErr)

			// Update status back to idle
			if err := s.updateSourceStatus(src.ID, "idle"); err != nil {
				log.Printf("Failed to set source %s to idle: %v", src.ID, err)
			}

			// Continue with next source (don't fail entire type group)
			continue
		}

		// Store results in per-source atomic transaction
		tx, err := s.db.Begin()
		if err != nil {
			log.Printf("Source %s: failed to begin transaction: %v", src.ID, err)
			s.recordError(src.ID, fmt.Errorf("failed to begin transaction: %w", err))

			if err := s.updateSourceStatus(src.ID, "idle"); err != nil {
				log.Printf("Failed to set source %s to idle: %v", src.ID, err)
			}
			continue
		}
		defer tx.Rollback() // Safe: no-op if commit succeeds

		// Store articles
		if err := s.storeArticlesInTx(tx, articles); err != nil {
			log.Printf("Source %s: failed to store articles: %v", src.ID, err)
			s.recordError(src.ID, fmt.Errorf("failed to store articles: %w", err))

			if err := s.updateSourceStatus(src.ID, "idle"); err != nil {
				log.Printf("Failed to set source %s to idle: %v", src.ID, err)
			}
			continue
		}

		// Store comments
		if err := s.storeCommentsInTx(tx, comments); err != nil {
			log.Printf("Source %s: failed to store comments: %v", src.ID, err)
			s.recordError(src.ID, fmt.Errorf("failed to store comments: %w", err))

			if err := s.updateSourceStatus(src.ID, "idle"); err != nil {
				log.Printf("Failed to set source %s to idle: %v", src.ID, err)
			}
			continue
		}

		// Commit transaction
		if err := tx.Commit(); err != nil {
			log.Printf("Source %s: failed to commit transaction: %v", src.ID, err)
			s.recordError(src.ID, fmt.Errorf("failed to commit transaction: %w", err))

			if err := s.updateSourceStatus(src.ID, "idle"); err != nil {
				log.Printf("Failed to set source %s to idle: %v", src.ID, err)
			}
			continue
		}

		// Transaction successful - update timestamps
		now := time.Now()
		_, err = s.db.Exec(
			"UPDATE sources SET last_run_at = ?, last_success_at = ?, last_error = NULL, status = ? WHERE id = ?",
			now, now, "idle", src.ID,
		)
		if err != nil {
			log.Printf("Source %s: failed to update timestamps: %v", src.ID, err)
			// Don't treat this as a critical error - data was stored successfully
		}

		log.Printf("Source %s completed successfully: %d articles, %d comments inserted", src.ID, len(articles), len(comments))
	}

	return nil
}

// runSourceWithTimeout executes a single source fetch with timeout
// Returns fetched articles and comments for centralized storage
func (s *Scheduler) runSourceWithTimeout(ctx context.Context, src *db.Source, limiter *rate.Limiter) ([]db.Article, []db.Comment, error) {
	// Create source instance (Factory loads fresh global config with credentials from DB)
	sourceImpl, err := source.Factory(src, s.db, limiter, s.maxCommentDepth)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create source: %w", err)
	}

	// Determine since time
	since := time.Unix(0, 0)
	if src.LastSuccessAt != nil {
		since = *src.LastSuccessAt
	}

	// Fetch articles and comments
	articles, comments, err := sourceImpl.Fetch(ctx, since)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch failed: %w", err)
	}

	log.Printf("Source %s fetched: %d articles, %d comments", src.ID, len(articles), len(comments))
	return articles, comments, nil
}

// createRateLimiters creates rate limiters for each source type based on provided config
func (s *Scheduler) createRateLimiters(config *db.GlobalConfigDTO) map[string]*rate.Limiter {
	limiters := make(map[string]*rate.Limiter)

	// Reddit rate limiter (burst=10 to allow natural bursting within rate limit)
	redditReqPerSec := 1000.0 / float64(config.RedditRateLimitDelayMs)
	limiters["reddit"] = rate.NewLimiter(rate.Limit(redditReqPerSec), 10)

	// Semantic Scholar rate limiter (burst=10)
	s2ReqPerSec := 1000.0 / float64(config.SemanticScholarRateLimitDelayMs)
	limiters["semantic_scholar"] = rate.NewLimiter(rate.Limit(s2ReqPerSec), 10)

	return limiters
}

// groupSourcesByType groups sources by their type
func (s *Scheduler) groupSourcesByType(sources []*db.Source) map[string][]*db.Source {
	groups := make(map[string][]*db.Source)
	for _, src := range sources {
		groups[src.Type] = append(groups[src.Type], src)
	}
	return groups
}

// getAllSources fetches all sources from the database
func (s *Scheduler) getAllSources() ([]*db.Source, error) {
	rows, err := s.db.Query("SELECT id, type, config, external_id, last_run_at, last_success_at, last_error, status, created_at FROM sources")
	if err != nil {
		return nil, fmt.Errorf("failed to query sources: %w", err)
	}
	defer rows.Close()

	var sources []*db.Source
	for rows.Next() {
		var src db.Source
		var lastRunAt, lastSuccessAt sql.NullTime
		var lastError, externalID sql.NullString

		err := rows.Scan(
			&src.ID,
			&src.Type,
			&src.Config,
			&externalID,
			&lastRunAt,
			&lastSuccessAt,
			&lastError,
			&src.Status,
			&src.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan source: %w", err)
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

		sources = append(sources, &src)
	}

	return sources, rows.Err()
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
func (s *Scheduler) recordError(sourceID string, err error) {
	now := time.Now()
	_, dbErr := s.db.Exec(
		"UPDATE sources SET last_run_at = ?, last_error = ? WHERE id = ?",
		now, err.Error(), sourceID,
	)
	if dbErr != nil {
		log.Printf("Failed to record error for source %s: %v", sourceID, dbErr)
	}
}

// storeArticlesInTx stores articles in the database using UPSERT within a transaction
// Uses full PUT/overwrite semantics - updates all fields except id and created_at
func (s *Scheduler) storeArticlesInTx(tx *sql.Tx, articles []db.Article) error {
	if len(articles) == 0 {
		return nil
	}

	stmt, err := tx.Prepare(`
		INSERT INTO articles (id, source_id, external_id, title, author, content, url, written_at, metadata, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source_id, external_id) DO UPDATE SET
			title = excluded.title,
			author = excluded.author,
			content = excluded.content,
			url = excluded.url,
			written_at = excluded.written_at,
			metadata = excluded.metadata
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

	return nil
}

// storeArticles stores articles in the database using UPSERT (legacy wrapper)
// Deprecated: Use storeArticlesInTx for better transaction control
func (s *Scheduler) storeArticles(articles []db.Article) error {
	if len(articles) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if err := s.storeArticlesInTx(tx, articles); err != nil {
		return err
	}

	return tx.Commit()
}

// storeCommentsInTx stores comments in the database using UPSERT within a transaction
// Uses full PUT/overwrite semantics - updates all fields except id and created_at
func (s *Scheduler) storeCommentsInTx(tx *sql.Tx, comments []db.Comment) error {
	if len(comments) == 0 {
		return nil
	}

	stmt, err := tx.Prepare(`
		INSERT INTO comments (id, article_id, external_id, author, content, written_at, parent_id, depth)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(article_id, external_id) DO UPDATE SET
			author = excluded.author,
			content = excluded.content,
			written_at = excluded.written_at,
			parent_id = excluded.parent_id,
			depth = excluded.depth
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

	return nil
}

// storeComments stores comments in the database using UPSERT (legacy wrapper)
// Deprecated: Use storeCommentsInTx for better transaction control
func (s *Scheduler) storeComments(comments []db.Comment) error {
	if len(comments) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if err := s.storeCommentsInTx(tx, comments); err != nil {
		return err
	}

	return tx.Commit()
}

// GetSchedule returns the global schedule information
func (s *Scheduler) GetSchedule() (*db.ScheduleEntry, error) {
	s.mu.RLock()
	globalConfig := s.globalConfig
	s.mu.RUnlock()

	// Parse cron expression
	sched, err := cron.ParseStandard(globalConfig.CronExpr)
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression: %w", err)
	}

	// Get the latest last_run_at from all sources
	var lastRunAt sql.NullTime
	err = s.db.QueryRow("SELECT MAX(last_run_at) FROM sources").Scan(&lastRunAt)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get last run time: %w", err)
	}

	// Calculate next run
	var nextRun time.Time
	if lastRunAt.Valid {
		nextRun = sched.Next(lastRunAt.Time)
	} else {
		nextRun = sched.Next(time.Now())
	}

	entry := &db.ScheduleEntry{
		SourceID:   "global",
		SourceType: "all",
		NextRun:    nextRun,
	}
	if lastRunAt.Valid {
		entry.LastRunAt = &lastRunAt.Time
	}

	return entry, nil
}
