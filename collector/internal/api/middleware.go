package api

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/cheolwanpark/meows/collector/internal/db"
)

// Logger is a simple logging middleware
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a response writer wrapper to capture status code
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(rw, r)

		duration := time.Since(start)
		log.Printf("%s %s - %d (%v)", r.Method, r.URL.Path, rw.statusCode, duration)
	})
}

// responseWriter wraps http.ResponseWriter to capture the status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// ContentType sets the Content-Type header to application/json
func ContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const ProfileIDKey contextKey = "profile_id"

// ProfileContext middleware reads and validates profile_id cookie
// If the cookie is present and valid, it injects the profile ID into the request context
func ProfileContext(database *db.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("current_profile_id")

			if err != nil || cookie.Value == "" {
				// No profile cookie - continue without profile context
				next.ServeHTTP(w, r)
				return
			}

			// Validate profile exists
			var exists bool
			err = database.QueryRow("SELECT EXISTS(SELECT 1 FROM profiles WHERE id = ?)", cookie.Value).Scan(&exists)

			if err != nil || !exists {
				// Invalid profile - clear cookie
				http.SetCookie(w, &http.Cookie{
					Name:     "current_profile_id",
					Value:    "",
					Path:     "/",
					MaxAge:   -1,
					HttpOnly: true,
					SameSite: http.SameSiteLaxMode,
				})
				next.ServeHTTP(w, r)
				return
			}

			// Inject profile ID into context
			ctx := context.WithValue(r.Context(), ProfileIDKey, cookie.Value)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetProfileID extracts profile ID from request context
// Returns the profile ID and a boolean indicating if it was found
func GetProfileID(r *http.Request) (string, bool) {
	profileID, ok := r.Context().Value(ProfileIDKey).(string)
	return profileID, ok
}
