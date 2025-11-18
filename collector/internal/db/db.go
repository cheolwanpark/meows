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

	-- Profiles table (Netflix-style multi-profile support)
	CREATE TABLE IF NOT EXISTS profiles (
		id TEXT PRIMARY KEY,
		nickname TEXT NOT NULL,
		user_description TEXT,
		character TEXT,
		character_status TEXT DEFAULT 'pending',
		character_error TEXT,
		milestone TEXT DEFAULT 'init',
		updated_at DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Sources table (per-source configuration)
	-- Global config (schedule, credentials, rate limits) now in .config.yaml file
	CREATE TABLE IF NOT EXISTS sources (
		id TEXT PRIMARY KEY,
		type TEXT NOT NULL,
		config TEXT NOT NULL,
		external_id TEXT,
		profile_id TEXT NOT NULL,
		last_run_at DATETIME,
		last_success_at DATETIME,
		last_error TEXT,
		status TEXT DEFAULT 'idle',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (profile_id) REFERENCES profiles(id) ON DELETE CASCADE,
		UNIQUE(type, external_id, profile_id)
	);

	-- Articles table
	CREATE TABLE IF NOT EXISTS articles (
		id TEXT PRIMARY KEY,
		source_id TEXT NOT NULL,
		external_id TEXT NOT NULL,
		profile_id TEXT NOT NULL,
		title TEXT,
		author TEXT,
		content TEXT,
		url TEXT,
		written_at DATETIME,
		metadata TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (source_id) REFERENCES sources(id) ON DELETE CASCADE,
		FOREIGN KEY (profile_id) REFERENCES profiles(id) ON DELETE CASCADE,
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

	-- Likes table (for article likes per profile)
	CREATE TABLE IF NOT EXISTS likes (
		id TEXT PRIMARY KEY,
		profile_id TEXT NOT NULL,
		article_id TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (profile_id) REFERENCES profiles(id) ON DELETE CASCADE,
		FOREIGN KEY (article_id) REFERENCES articles(id) ON DELETE CASCADE,
		UNIQUE(profile_id, article_id)
	);

	-- Indexes for performance
	CREATE INDEX IF NOT EXISTS idx_profiles_milestone ON profiles(milestone);
	CREATE INDEX IF NOT EXISTS idx_profiles_updated_at ON profiles(updated_at);
	CREATE INDEX IF NOT EXISTS idx_sources_profile ON sources(profile_id);
	CREATE INDEX IF NOT EXISTS idx_articles_source_time ON articles(source_id, written_at DESC);
	CREATE INDEX IF NOT EXISTS idx_articles_created ON articles(created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_articles_profile_created ON articles(profile_id, created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_comments_article ON comments(article_id);
	CREATE INDEX IF NOT EXISTS idx_sources_status ON sources(status);
	CREATE INDEX IF NOT EXISTS idx_sources_type ON sources(type);
	CREATE INDEX IF NOT EXISTS idx_likes_profile_created ON likes(profile_id, created_at DESC);
	`

	_, err := db.Exec(schema)
	return err
}

// Close closes the database connection
func (db *DB) Close() error {
	return db.DB.Close()
}
