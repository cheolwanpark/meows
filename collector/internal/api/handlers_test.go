package api

import (
	"net/http"
	"net/http/httptest"
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

	sched := scheduler.New(database, 5)
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

	sched := scheduler.New(database, 5)
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

	sched := scheduler.New(database, 5)
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

	sched := scheduler.New(database, 5)
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

	sched := scheduler.New(database, 5)

	// Insert a test source
	_, err := database.Exec(`
		INSERT INTO sources (id, type, external_id, config, cron_expr, status, created_at)
		VALUES ('test-id', 'reddit', 'golang', '{"subreddit":"golang"}', '0 0 * * *', 'idle', datetime('now'))
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
