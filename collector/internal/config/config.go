package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/robfig/cron/v3"
)

// Config represents the entire application configuration
type Config struct {
	Collector CollectorConfig
}

// CollectorConfig represents collector-specific configuration
type CollectorConfig struct {
	Server      ServerConfig
	Schedule    ScheduleConfig
	RateLimits  RateLimitsConfig
	Credentials CredentialsConfig
	Gemini      GeminiConfig
	Profile     ProfileConfig
}

// ServerConfig represents collector server settings
type ServerConfig struct {
	Port            int
	DBPath          string
	LogLevel        string
	EnableSwagger   bool
	MaxCommentDepth int
}

// ScheduleConfig represents scheduling configuration
type ScheduleConfig struct {
	CronExpr string
}

// RateLimitsConfig represents rate limiting configuration per source type
type RateLimitsConfig struct {
	RedditDelayMs          int
	SemanticScholarDelayMs int
	HackerNewsDelayMs      int
}

// CredentialsConfig represents global credentials shared by all sources
type CredentialsConfig struct {
	RedditClientID        string
	RedditClientSecret    string
	RedditUsername        string
	RedditPassword        string
	SemanticScholarAPIKey string
}

// GeminiConfig represents Gemini API configuration
type GeminiConfig struct {
	APIKey string
}

// ProfileConfig represents profile-related configuration
type ProfileConfig struct {
	DailyCronExpr        string
	MilestoneThreshold1  int  // First milestone threshold (default: 3 likes)
	MilestoneThreshold2  int  // Second milestone threshold (default: 10 likes)
	MilestoneThreshold3  int  // Third milestone threshold (default: 20 likes)
	CurationWorkers      int  // Number of concurrent curation workers (default: 5)
	CurationEnabled      bool // Enable article curation (default: true)
}

// LoadConfig loads and validates the configuration from environment variables
func LoadConfig() (*Config, error) {
	cfg := &Config{
		Collector: CollectorConfig{
			Server: ServerConfig{
				Port:            getEnvAsInt("COLLECTOR_PORT", 8080),
				DBPath:          getEnv("COLLECTOR_DB_PATH", "/data/meows.db"),
				LogLevel:        getEnv("COLLECTOR_LOG_LEVEL", "info"),
				EnableSwagger:   getEnvAsBool("COLLECTOR_ENABLE_SWAGGER", true),
				MaxCommentDepth: getEnvAsInt("COLLECTOR_MAX_COMMENT_DEPTH", 5),
			},
			Schedule: ScheduleConfig{
				CronExpr: getEnv("COLLECTOR_CRON_EXPR", "0 */6 * * *"),
			},
			RateLimits: RateLimitsConfig{
				RedditDelayMs:          getEnvAsInt("COLLECTOR_REDDIT_DELAY_MS", 2000),
				SemanticScholarDelayMs: getEnvAsInt("COLLECTOR_SEMANTIC_SCHOLAR_DELAY_MS", 1000),
				HackerNewsDelayMs:      getEnvAsInt("COLLECTOR_HACKERNEWS_DELAY_MS", 500),
			},
			Credentials: CredentialsConfig{
				RedditClientID:        getEnv("COLLECTOR_REDDIT_CLIENT_ID", ""),
				RedditClientSecret:    getEnv("COLLECTOR_REDDIT_CLIENT_SECRET", ""),
				RedditUsername:        getEnv("COLLECTOR_REDDIT_USERNAME", ""),
				RedditPassword:        getEnv("COLLECTOR_REDDIT_PASSWORD", ""),
				SemanticScholarAPIKey: getEnv("COLLECTOR_SEMANTIC_SCHOLAR_API_KEY", ""),
			},
			Gemini: GeminiConfig{
				APIKey: getEnv("GEMINI_API_KEY", ""),
			},
			Profile: ProfileConfig{
				DailyCronExpr:       getEnv("PROFILE_DAILY_CRON", "0 1 * * *"),
				MilestoneThreshold1: getEnvAsInt("PROFILE_MILESTONE_1", 3),
				MilestoneThreshold2: getEnvAsInt("PROFILE_MILESTONE_2", 10),
				MilestoneThreshold3: getEnvAsInt("PROFILE_MILESTONE_3", 20),
				CurationWorkers:     getEnvAsInt("PROFILE_CURATION_WORKERS", 5),
				CurationEnabled:     getEnvAsBool("PROFILE_CURATION_ENABLED", true),
			},
		},
	}

	// Validate
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

// Helper functions for environment variable parsing

// getEnv returns the environment variable value or the default value if not set
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvAsInt returns the environment variable as an integer or the default value
// Logs a warning and returns default if the value cannot be parsed
func getEnvAsInt(key string, defaultValue int) int {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}

	value, err := strconv.Atoi(valueStr)
	if err != nil {
		log.Printf("Warning: Invalid integer for %s=%s, using default %d", key, valueStr, defaultValue)
		return defaultValue
	}

	return value
}

// getEnvAsBool returns the environment variable as a boolean or the default value
// Accepts: true/false, 1/0, yes/no, on/off (case-insensitive)
// Logs a warning and returns default if the value cannot be parsed
func getEnvAsBool(key string, defaultValue bool) bool {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}

	// Normalize to lowercase for comparison
	valueStr = strings.ToLower(strings.TrimSpace(valueStr))

	switch valueStr {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		log.Printf("Warning: Invalid boolean for %s=%s, using default %v", key, valueStr, defaultValue)
		return defaultValue
	}
}

// Validate validates the entire configuration
func (c *Config) Validate() error {
	if err := c.Collector.Validate(); err != nil {
		return fmt.Errorf("collector config: %w", err)
	}
	return nil
}

// Validate validates collector configuration
func (c *CollectorConfig) Validate() error {
	// Server validation
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("COLLECTOR_PORT must be between 1 and 65535, got %d", c.Server.Port)
	}
	if c.Server.DBPath == "" {
		return fmt.Errorf("COLLECTOR_DB_PATH is required")
	}
	validLogLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLogLevels[c.Server.LogLevel] {
		return fmt.Errorf("COLLECTOR_LOG_LEVEL must be one of [debug, info, warn, error], got '%s'", c.Server.LogLevel)
	}
	if c.Server.MaxCommentDepth < 0 {
		return fmt.Errorf("COLLECTOR_MAX_COMMENT_DEPTH must be non-negative, got %d", c.Server.MaxCommentDepth)
	}

	// Schedule validation
	if c.Schedule.CronExpr == "" {
		return fmt.Errorf("COLLECTOR_CRON_EXPR is required")
	}
	// Validate cron expression syntax
	if _, err := cron.ParseStandard(c.Schedule.CronExpr); err != nil {
		return fmt.Errorf("COLLECTOR_CRON_EXPR is invalid: %w", err)
	}

	// Rate limits validation
	if c.RateLimits.RedditDelayMs < 0 {
		return fmt.Errorf("COLLECTOR_REDDIT_DELAY_MS must be non-negative, got %d", c.RateLimits.RedditDelayMs)
	}
	if c.RateLimits.SemanticScholarDelayMs < 0 {
		return fmt.Errorf("COLLECTOR_SEMANTIC_SCHOLAR_DELAY_MS must be non-negative, got %d", c.RateLimits.SemanticScholarDelayMs)
	}
	if c.RateLimits.HackerNewsDelayMs < 0 {
		return fmt.Errorf("COLLECTOR_HACKERNEWS_DELAY_MS must be non-negative, got %d", c.RateLimits.HackerNewsDelayMs)
	}

	// Credentials validation (check non-empty for required fields)
	if c.Credentials.RedditClientID == "" {
		return fmt.Errorf("COLLECTOR_REDDIT_CLIENT_ID is required")
	}
	if c.Credentials.RedditClientSecret == "" {
		return fmt.Errorf("COLLECTOR_REDDIT_CLIENT_SECRET is required")
	}
	if c.Credentials.RedditUsername == "" {
		return fmt.Errorf("COLLECTOR_REDDIT_USERNAME is required")
	}
	if c.Credentials.RedditPassword == "" {
		return fmt.Errorf("COLLECTOR_REDDIT_PASSWORD is required")
	}
	if c.Credentials.SemanticScholarAPIKey == "" {
		return fmt.Errorf("COLLECTOR_SEMANTIC_SCHOLAR_API_KEY is required")
	}

	return nil
}
