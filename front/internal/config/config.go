package config

import (
	"fmt"
	"os"
)

// Config represents the frontend configuration
type Config struct {
	Frontend FrontendConfig
}

// FrontendConfig contains frontend-specific settings
type FrontendConfig struct {
	Server       ServerConfig
	CollectorURL string
}

// ServerConfig contains HTTP server settings
type ServerConfig struct {
	Port string
}

// LoadConfig loads configuration from environment variables
// Validates all required fields and returns error if validation fails
func LoadConfig() (*Config, error) {
	cfg := &Config{
		Frontend: FrontendConfig{
			Server: ServerConfig{
				Port: getEnv("FRONTEND_PORT", "3000"),
			},
			CollectorURL: getEnv("FRONTEND_COLLECTOR_URL", "http://collector:8080"),
		},
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

// getEnv returns the environment variable value or the default value if not set
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.Frontend.Server.Port == "" {
		return fmt.Errorf("FRONTEND_PORT is required")
	}

	if c.Frontend.CollectorURL == "" {
		return fmt.Errorf("FRONTEND_COLLECTOR_URL is required")
	}

	return nil
}
