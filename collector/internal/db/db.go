package db

import (
	"database/sql"
	"fmt"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

// DB wraps the database connection
type DB struct {
	*sql.DB
}

// Init initializes the database connection and creates tables
func Init(dbPath string) (*DB, error) {
	// Open database connection
	sqlDB, err := sql.Open("sqlite3", "file:"+dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool for low memory usage
	sqlDB.SetMaxOpenConns(10)   // Limited concurrent connections
	sqlDB.SetMaxIdleConns(2)    // Keep minimal idle connections
	sqlDB.SetConnMaxLifetime(0) // No connection timeout

	// Test connection
	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	db := &DB{sqlDB}

	// Create schema
	if err := db.createSchema(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	return db, nil
}

// createSchema creates all necessary tables and indexes
func (db *DB) createSchema() error {
	schema := `
	-- Enable WAL mode for concurrent reads
	PRAGMA journal_mode=WAL;
	PRAGMA busy_timeout=5000;

	-- Global configuration table (singleton)
	CREATE TABLE IF NOT EXISTS global_config (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		cron_expr TEXT NOT NULL DEFAULT '0 */6 * * *',
		reddit_rate_limit_delay_ms INTEGER NOT NULL DEFAULT 2000,
		semantic_scholar_rate_limit_delay_ms INTEGER NOT NULL DEFAULT 1000,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Insert default configuration if not exists
	INSERT OR IGNORE INTO global_config (id) VALUES (1);

	-- Sources table (no cron_expr, no credentials in config)
	CREATE TABLE IF NOT EXISTS sources (
		id TEXT PRIMARY KEY,
		type TEXT NOT NULL,
		config TEXT NOT NULL,
		external_id TEXT,
		last_run_at DATETIME,
		last_success_at DATETIME,
		last_error TEXT,
		status TEXT DEFAULT 'idle',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(type, external_id)
	);

	-- Articles table
	CREATE TABLE IF NOT EXISTS articles (
		id TEXT PRIMARY KEY,
		source_id TEXT NOT NULL,
		external_id TEXT NOT NULL,
		title TEXT,
		author TEXT,
		content TEXT,
		url TEXT,
		written_at DATETIME,
		metadata TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (source_id) REFERENCES sources(id) ON DELETE CASCADE,
		UNIQUE(source_id, external_id)
	);

	-- Comments table
	CREATE TABLE IF NOT EXISTS comments (
		id TEXT PRIMARY KEY,
		article_id TEXT NOT NULL,
		external_id TEXT NOT NULL,
		author TEXT,
		content TEXT,
		written_at DATETIME,
		parent_id TEXT,
		depth INTEGER DEFAULT 0,
		FOREIGN KEY (article_id) REFERENCES articles(id) ON DELETE CASCADE,
		UNIQUE(article_id, external_id)
	);

	-- Indexes for performance
	CREATE INDEX IF NOT EXISTS idx_articles_source_time ON articles(source_id, written_at DESC);
	CREATE INDEX IF NOT EXISTS idx_articles_created ON articles(created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_comments_article ON comments(article_id);
	CREATE INDEX IF NOT EXISTS idx_sources_status ON sources(status);
	CREATE INDEX IF NOT EXISTS idx_sources_type ON sources(type);
	`

	_, err := db.Exec(schema)
	return err
}

// Close closes the database connection
func (db *DB) Close() error {
	return db.DB.Close()
}

// GetGlobalConfig retrieves the global configuration
func (db *DB) GetGlobalConfig() (*GlobalConfig, error) {
	var config GlobalConfig
	err := db.QueryRow(`
		SELECT id, cron_expr, reddit_rate_limit_delay_ms,
		       semantic_scholar_rate_limit_delay_ms, updated_at
		FROM global_config WHERE id = 1
	`).Scan(
		&config.ID,
		&config.CronExpr,
		&config.RedditRateLimitDelayMs,
		&config.SemanticScholarRateLimitDelayMs,
		&config.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get global config: %w", err)
	}
	return &config, nil
}

// UpdateGlobalConfig updates the global configuration
func (db *DB) UpdateGlobalConfig(config *GlobalConfig) error {
	// Validate before updating
	if err := config.Validate(); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	_, err := db.Exec(`
		UPDATE global_config
		SET cron_expr = ?,
		    reddit_rate_limit_delay_ms = ?,
		    semantic_scholar_rate_limit_delay_ms = ?,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = 1
	`,
		config.CronExpr,
		config.RedditRateLimitDelayMs,
		config.SemanticScholarRateLimitDelayMs,
	)
	if err != nil {
		return fmt.Errorf("failed to update global config: %w", err)
	}
	return nil
}
