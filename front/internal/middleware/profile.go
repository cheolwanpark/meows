package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/cheolwanpark/meows/front/internal/collector"
)

const (
	profileCookieName = "current_profile_id"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const ProfileIDKey contextKey = "profile_id"

// ProfileMiddleware provides profile context middleware
type ProfileMiddleware struct {
	collector *collector.Client
}

// NewProfileMiddleware creates a new profile middleware
func NewProfileMiddleware(c *collector.Client) *ProfileMiddleware {
	return &ProfileMiddleware{
		collector: c,
	}
}

// Middleware validates and injects profile context
// Routes that should be exempted: /profiles/setup, /static/*, /health, /favicon.ico
func (pm *ProfileMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Exempted routes
		if isExemptedRoute(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		// Try to get profile ID from cookie
		cookie, err := r.Cookie(profileCookieName)
		if err != nil || cookie.Value == "" {
			// No profile cookie - check if profiles exist
			pm.handleNoProfileCookie(w, r, next)
			return
		}

		profileID := cookie.Value

		// Validate profile exists via collector API
		ctx := r.Context()
		_, err = pm.collector.GetProfile(ctx, profileID)

		if err != nil {
			slog.Warn("Invalid profile ID in cookie", "profile_id", profileID, "error", err)
			// Clear invalid cookie
			http.SetCookie(w, &http.Cookie{
				Name:     profileCookieName,
				Value:    "",
				Path:     "/",
				MaxAge:   -1,
				HttpOnly: true,
				Secure:   IsSecureRequest(r), // Match cookie creation attributes
				SameSite: http.SameSiteLaxMode,
			})

			// Check if profiles exist
			pm.handleNoProfileCookie(w, r, next)
			return
		}

		// Inject profile ID into context
		ctx = context.WithValue(r.Context(), ProfileIDKey, profileID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// handleNoProfileCookie checks if any profiles exist and redirects to setup if none
func (pm *ProfileMiddleware) handleNoProfileCookie(w http.ResponseWriter, r *http.Request, next http.Handler) {
	ctx := r.Context()

	// Fetch all profiles to check if any exist
	profiles, err := pm.collector.GetProfiles(ctx)
	if err != nil {
		slog.Error("Failed to fetch profiles", "error", err)
		// Continue without profile context if API fails
		next.ServeHTTP(w, r)
		return
	}

	// If no profiles exist, redirect to setup page
	// But ONLY if we're not already on the setup page (prevent redirect loop)
	if len(profiles) == 0 && r.URL.Path != "/profiles/setup" {
		http.Redirect(w, r, "/profiles/setup", http.StatusFound)
		return
	}

	// Profiles exist but no cookie set - continue without profile context
	// (User will see profile switcher and can select one)
	next.ServeHTTP(w, r)
}

// isExemptedRoute checks if a route should bypass profile middleware
func isExemptedRoute(path string) bool {
	exemptedPaths := []string{
		"/profiles/setup",
		"/profiles/switcher",
		"/static/",
		"/health",
		"/favicon.ico",
		"/api/profiles",        // Profile creation endpoint (POST)
		"/api/profiles/",       // Profile status endpoint (GET /api/profiles/{id}/status)
	}

	for _, exempted := range exemptedPaths {
		if strings.HasPrefix(path, exempted) || path == exempted {
			return true
		}
	}

	return false
}

// GetProfileID extracts profile ID from request context
// Returns the profile ID and a boolean indicating if it was found
func GetProfileID(r *http.Request) (string, bool) {
	profileID, ok := r.Context().Value(ProfileIDKey).(string)
	return profileID, ok
}
