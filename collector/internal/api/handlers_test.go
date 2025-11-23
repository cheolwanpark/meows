package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cheolwanpark/meows/collector/internal/config"
	"github.com/cheolwanpark/meows/collector/internal/db"
	"github.com/cheolwanpark/meows/collector/internal/gemini"
	"github.com/cheolwanpark/meows/collector/internal/personalization"
	"github.com/cheolwanpark/meows/collector/internal/scheduler"
	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

// setupTestDB creates a temporary in-memory SQLite database for testing
func setupTestDB(t *testing.T) *db.DB {
	// Use temp file instead of :memory: for better compatibility
	tmpFile := t.TempDir() + "/test.db"
	database, err := db.Init(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	return database
}

// setupTestConfig creates a minimal test configuration
func setupTestConfig() *config.Config {
	return &config.Config{
		Collector: config.CollectorConfig{
			Server: config.ServerConfig{
				DBPath:          ":memory:",
				Port:            8080,
				MaxCommentDepth: 5,
				LogLevel:        "info",
				EnableSwagger:   true,
			},
			Schedule: config.ScheduleConfig{
				CronExpr: "0 0 * * *",
			},
			RateLimits: config.RateLimitsConfig{
				RedditDelayMs:          1000,
				SemanticScholarDelayMs: 1000,
				HackerNewsDelayMs:      500,
			},
			Profile: config.ProfileConfig{
				MilestoneThreshold1: 3,
				MilestoneThreshold2: 10,
				MilestoneThreshold3: 20,
			},
			Gemini: config.GeminiConfig{
				APIKey: "", // Empty for tests
			},
		},
	}
}

// setupTestProfileService creates a test profile service for routing tests.
// Nil Gemini client is safe because these tests only verify routing,
// not character generation (which would use the Gemini client).
func setupTestProfileService(t *testing.T, database *db.DB) *personalization.UpdateService {
	// Try to create a Gemini client, but fall back to nil if empty API key fails
	geminiClient, err := gemini.NewClient(context.Background(), "")
	if err != nil {
		// Nil is acceptable since routing tests don't call UpdateCharacter()
		return personalization.NewUpdateService(database, nil, 3, 10, 20)
	}
	return personalization.NewUpdateService(database, geminiClient, 3, 10, 20)
}

// setupTestScheduler creates a scheduler for routing tests.
// Passes nil curationService since routing tests don't exercise curation.
func setupTestScheduler(t *testing.T, database *db.DB, profService *personalization.UpdateService) *scheduler.Scheduler {
	t.Helper()
	cfg := setupTestConfig()
	sched, err := scheduler.New(&cfg.Collector, database, profService, nil)
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}
	return sched
}

func TestDeleteSourceByTypeAndExternalID_InvalidType(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	cfg := setupTestConfig()
	profService := setupTestProfileService(t, database)
	sched := setupTestScheduler(t, database, profService)
	router := SetupRouter(cfg, database, sched, profService)

	req := httptest.NewRequest("DELETE", "/sources/invalid_type/test", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestDeleteSourceByTypeAndExternalID_EmptyExternalID(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	cfg := setupTestConfig()
	profService := setupTestProfileService(t, database)
	sched := setupTestScheduler(t, database, profService)
	router := SetupRouter(cfg, database, sched, profService)

	req := httptest.NewRequest("DELETE", "/sources/reddit/", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Chi router rejects empty path segments at routing layer (404)
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestDeleteSourceByTypeAndExternalID_ExternalIDWithSlash(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	cfg := setupTestConfig()
	profService := setupTestProfileService(t, database)
	sched := setupTestScheduler(t, database, profService)
	router := SetupRouter(cfg, database, sched, profService)

	req := httptest.NewRequest("DELETE", "/sources/reddit/test/with/slash", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Chi router rejects multi-segment params at routing layer (404)
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestDeleteSourceByTypeAndExternalID_NotFound(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	cfg := setupTestConfig()
	profService := setupTestProfileService(t, database)
	sched := setupTestScheduler(t, database, profService)
	router := SetupRouter(cfg, database, sched, profService)

	req := httptest.NewRequest("DELETE", "/sources/reddit/nonexistent?profile_id=test-profile", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestDeleteSourceByTypeAndExternalID_Success(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	cfg := setupTestConfig()
	profService := setupTestProfileService(t, database)
	sched := setupTestScheduler(t, database, profService)

	// Insert a test profile first
	_, err := database.Exec(`
		INSERT INTO profiles (id, nickname, user_description, created_at)
		VALUES ('test-profile-id', 'testuser', 'Test user', datetime('now'))
	`)
	if err != nil {
		t.Fatalf("Failed to insert test profile: %v", err)
	}

	// Insert a test source
	_, err = database.Exec(`
		INSERT INTO sources (id, type, external_id, profile_id, config, status, created_at)
		VALUES ('test-id', 'reddit', 'golang', 'test-profile-id', '{"subreddit":"golang"}', 'idle', datetime('now'))
	`)
	if err != nil {
		t.Fatalf("Failed to insert test source: %v", err)
	}

	router := SetupRouter(cfg, database, sched, profService)

	req := httptest.NewRequest("DELETE", "/sources/reddit/golang?profile_id=test-profile-id", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("Expected status 204, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Verify source was deleted
	var count int
	err = database.QueryRow("SELECT COUNT(*) FROM sources WHERE id = 'test-id'").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query database: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected source to be deleted, but found %d rows", count)
	}
}
