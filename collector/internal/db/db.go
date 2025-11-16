package db

import (
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"

	"github.com/cheolwanpark/meows/collector/internal/crypto"
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
		reddit_client_id TEXT,
		reddit_client_secret TEXT,
		reddit_username TEXT,
		reddit_password TEXT,
		semantic_scholar_api_key TEXT,
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

// GetGlobalConfig retrieves the global configuration WITHOUT credentials
// For API responses - credentials are omitted for security
func (db *DB) GetGlobalConfig() (*GlobalConfigDTO, error) {
	var config GlobalConfigDTO
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

// GetGlobalConfigWithCredentials retrieves the full global configuration with DECRYPTED credentials
// For internal use only (sources, scheduler, etc.) - NEVER expose via API
func (db *DB) GetGlobalConfigWithCredentials() (*GlobalConfig, error) {
	var config GlobalConfig
	var encRedditClientID, encRedditClientSecret, encRedditUsername, encRedditPassword, encS2APIKey sql.NullString

	err := db.QueryRow(`
		SELECT id, cron_expr, reddit_rate_limit_delay_ms, semantic_scholar_rate_limit_delay_ms,
		       reddit_client_id, reddit_client_secret, reddit_username, reddit_password,
		       semantic_scholar_api_key, updated_at
		FROM global_config WHERE id = 1
	`).Scan(
		&config.ID,
		&config.CronExpr,
		&config.RedditRateLimitDelayMs,
		&config.SemanticScholarRateLimitDelayMs,
		&encRedditClientID,
		&encRedditClientSecret,
		&encRedditUsername,
		&encRedditPassword,
		&encS2APIKey,
		&config.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get global config: %w", err)
	}

	// Decrypt credentials
	var decryptErr error
	if encRedditClientID.Valid {
		config.RedditClientID, decryptErr = crypto.Decrypt(encRedditClientID.String)
		if decryptErr != nil {
			return nil, fmt.Errorf("failed to decrypt reddit_client_id: %w", decryptErr)
		}
	}
	if encRedditClientSecret.Valid {
		config.RedditClientSecret, decryptErr = crypto.Decrypt(encRedditClientSecret.String)
		if decryptErr != nil {
			return nil, fmt.Errorf("failed to decrypt reddit_client_secret: %w", decryptErr)
		}
	}
	if encRedditUsername.Valid {
		config.RedditUsername, decryptErr = crypto.Decrypt(encRedditUsername.String)
		if decryptErr != nil {
			return nil, fmt.Errorf("failed to decrypt reddit_username: %w", decryptErr)
		}
	}
	if encRedditPassword.Valid {
		config.RedditPassword, decryptErr = crypto.Decrypt(encRedditPassword.String)
		if decryptErr != nil {
			return nil, fmt.Errorf("failed to decrypt reddit_password: %w", decryptErr)
		}
	}
	if encS2APIKey.Valid {
		config.SemanticScholarAPIKey, decryptErr = crypto.Decrypt(encS2APIKey.String)
		if decryptErr != nil {
			return nil, fmt.Errorf("failed to decrypt semantic_scholar_api_key: %w", decryptErr)
		}
	}

	return &config, nil
}

// UpdateGlobalConfigPartial performs a PATCH-style update with only non-nil fields
// Credentials are encrypted before storage
func (db *DB) UpdateGlobalConfigPartial(req *UpdateGlobalConfigRequest) error {
	// Build dynamic UPDATE query with only provided fields
	query := "UPDATE global_config SET "
	args := []interface{}{}
	updates := []string{}

	if req.CronExpr != nil {
		updates = append(updates, "cron_expr = ?")
		args = append(args, *req.CronExpr)
	}
	if req.RedditRateLimitDelayMs != nil {
		updates = append(updates, "reddit_rate_limit_delay_ms = ?")
		args = append(args, *req.RedditRateLimitDelayMs)
	}
	if req.SemanticScholarRateLimitDelayMs != nil {
		updates = append(updates, "semantic_scholar_rate_limit_delay_ms = ?")
		args = append(args, *req.SemanticScholarRateLimitDelayMs)
	}

	// Encrypt credentials before storing (store NULL for empty strings to clear credentials)
	if req.RedditClientID != nil {
		if *req.RedditClientID == "" {
			updates = append(updates, "reddit_client_id = NULL")
		} else {
			encrypted, err := crypto.Encrypt(*req.RedditClientID)
			if err != nil {
				return fmt.Errorf("failed to encrypt reddit_client_id: %w", err)
			}
			updates = append(updates, "reddit_client_id = ?")
			args = append(args, encrypted)
		}
	}
	if req.RedditClientSecret != nil {
		if *req.RedditClientSecret == "" {
			updates = append(updates, "reddit_client_secret = NULL")
		} else {
			encrypted, err := crypto.Encrypt(*req.RedditClientSecret)
			if err != nil {
				return fmt.Errorf("failed to encrypt reddit_client_secret: %w", err)
			}
			updates = append(updates, "reddit_client_secret = ?")
			args = append(args, encrypted)
		}
	}
	if req.RedditUsername != nil {
		if *req.RedditUsername == "" {
			updates = append(updates, "reddit_username = NULL")
		} else {
			encrypted, err := crypto.Encrypt(*req.RedditUsername)
			if err != nil {
				return fmt.Errorf("failed to encrypt reddit_username: %w", err)
			}
			updates = append(updates, "reddit_username = ?")
			args = append(args, encrypted)
		}
	}
	if req.RedditPassword != nil {
		if *req.RedditPassword == "" {
			updates = append(updates, "reddit_password = NULL")
		} else {
			encrypted, err := crypto.Encrypt(*req.RedditPassword)
			if err != nil {
				return fmt.Errorf("failed to encrypt reddit_password: %w", err)
			}
			updates = append(updates, "reddit_password = ?")
			args = append(args, encrypted)
		}
	}
	if req.SemanticScholarAPIKey != nil {
		if *req.SemanticScholarAPIKey == "" {
			updates = append(updates, "semantic_scholar_api_key = NULL")
		} else {
			encrypted, err := crypto.Encrypt(*req.SemanticScholarAPIKey)
			if err != nil {
				return fmt.Errorf("failed to encrypt semantic_scholar_api_key: %w", err)
			}
			updates = append(updates, "semantic_scholar_api_key = ?")
			args = append(args, encrypted)
		}
	}

	if len(updates) == 0 {
		return fmt.Errorf("no fields to update")
	}

	// Always update timestamp
	updates = append(updates, "updated_at = CURRENT_TIMESTAMP")

	// Complete query
	query += strings.Join(updates, ", ") + " WHERE id = 1"

	// Validate after building the update (fetch current config, apply changes, validate)
	config, err := db.GetGlobalConfig()
	if err != nil {
		return fmt.Errorf("failed to get current config for validation: %w", err)
	}

	// Apply updates to validate
	testConfig := &GlobalConfig{
		ID:                              config.ID,
		CronExpr:                        config.CronExpr,
		RedditRateLimitDelayMs:          config.RedditRateLimitDelayMs,
		SemanticScholarRateLimitDelayMs: config.SemanticScholarRateLimitDelayMs,
		UpdatedAt:                       config.UpdatedAt,
	}
	if req.CronExpr != nil {
		testConfig.CronExpr = *req.CronExpr
	}
	if req.RedditRateLimitDelayMs != nil {
		testConfig.RedditRateLimitDelayMs = *req.RedditRateLimitDelayMs
	}
	if req.SemanticScholarRateLimitDelayMs != nil {
		testConfig.SemanticScholarRateLimitDelayMs = *req.SemanticScholarRateLimitDelayMs
	}

	// Validate merged config
	if err := testConfig.Validate(); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Execute update
	_, err = db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to update global config: %w", err)
	}
	return nil
}
