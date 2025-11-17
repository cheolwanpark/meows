package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cheolwanpark/meows/collector/internal/db"
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

func TestDeleteSourceByTypeAndExternalID_InvalidType(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	sched, err := scheduler.New(database, 5)
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}
	router := SetupRouter(database, sched)

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

	sched, err := scheduler.New(database, 5)
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}
	router := SetupRouter(database, sched)

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

	sched, err := scheduler.New(database, 5)
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}
	router := SetupRouter(database, sched)

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

	sched, err := scheduler.New(database, 5)
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}
	router := SetupRouter(database, sched)

	req := httptest.NewRequest("DELETE", "/sources/reddit/nonexistent", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestDeleteSourceByTypeAndExternalID_Success(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	sched, err := scheduler.New(database, 5)
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}

	// Insert a test source
	_, err = database.Exec(`
		INSERT INTO sources (id, type, external_id, config, status, created_at)
		VALUES ('test-id', 'reddit', 'golang', '{"subreddit":"golang"}', 'idle', datetime('now'))
	`)
	if err != nil {
		t.Fatalf("Failed to insert test source: %v", err)
	}

	router := SetupRouter(database, sched)

	req := httptest.NewRequest("DELETE", "/sources/reddit/golang", nil)
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

func TestGetGlobalConfig(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	sched, err := scheduler.New(database, 5)
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}
	router := SetupRouter(database, sched)

	req := httptest.NewRequest("GET", "/config", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Verify response contains expected fields
	body := w.Body.String()
	if !strings.Contains(body, "cron_expr") {
		t.Errorf("Expected response to contain 'cron_expr', got: %s", body)
	}
}

func TestUpdateGlobalConfig_Valid(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	sched, err := scheduler.New(database, 5)
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}
	router := SetupRouter(database, sched)

	// Test valid update
	reqBody := `{"cron_expr":"0 */12 * * *","reddit_rate_limit_delay_ms":3000}`
	req := httptest.NewRequest("PATCH", "/config", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Verify config was updated in database
	config, err := database.GetGlobalConfig()
	if err != nil {
		t.Fatalf("Failed to get global config: %v", err)
	}
	if config.CronExpr != "0 */12 * * *" {
		t.Errorf("Expected cron_expr to be '0 */12 * * *', got '%s'", config.CronExpr)
	}
	if config.RedditRateLimitDelayMs != 3000 {
		t.Errorf("Expected reddit rate limit to be 3000, got %d", config.RedditRateLimitDelayMs)
	}
}

func TestUpdateGlobalConfig_InvalidCron(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	sched, err := scheduler.New(database, 5)
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}
	router := SetupRouter(database, sched)

	// Test invalid cron expression
	reqBody := `{"cron_expr":"invalid cron"}`
	req := httptest.NewRequest("PATCH", "/config", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestUpdateGlobalConfig_InvalidRateLimit(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	sched, err := scheduler.New(database, 5)
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}
	router := SetupRouter(database, sched)

	// Test negative rate limit
	reqBody := `{"reddit_rate_limit_delay_ms":-100}`
	req := httptest.NewRequest("PATCH", "/config", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d. Body: %s", w.Code, w.Body.String())
	}
}
