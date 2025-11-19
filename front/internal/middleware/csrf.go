package middleware

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
)

const (
	csrfTokenLength = 32
	csrfCookieName  = "csrf_token"
	csrfFormField   = "csrf_token"
	csrfHeader      = "X-CSRF-Token"
)

// CSRF provides CSRF protection middleware
type CSRF struct {
}

// NewCSRF creates a new CSRF middleware
func NewCSRF() *CSRF {
	return &CSRF{}
}

// GetToken returns the CSRF token for the current request/session
func (c *CSRF) GetToken(r *http.Request) string {
	// Try to get existing token from cookie
	cookie, err := r.Cookie(csrfCookieName)
	if err == nil && cookie.Value != "" {
		return cookie.Value
	}

	// Generate new token
	return c.generateToken()
}

// SetToken sets the CSRF token as a cookie
func (c *CSRF) SetToken(w http.ResponseWriter, r *http.Request, token string) {
	// Use Secure flag only for HTTPS to prevent cookie rejection over HTTP
	// In production with TLS, this protects against token interception
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   24 * 3600, // 24 hours
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   IsSecureRequest(r), // Set Secure flag for HTTPS connections
	})
}

// Validate is middleware that validates CSRF tokens on state-changing requests
func (c *CSRF) Validate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only validate POST, PUT, PATCH, DELETE requests
		if r.Method != http.MethodPost && r.Method != http.MethodPut &&
			r.Method != http.MethodPatch && r.Method != http.MethodDelete {
			next.ServeHTTP(w, r)
			return
		}

		// Get token from cookie
		cookie, err := r.Cookie(csrfCookieName)
		if err != nil || cookie.Value == "" {
			// Log CSRF validation failure with context
			slog.Warn("CSRF token missing from cookie",
				"ip", r.RemoteAddr,
				"path", r.URL.Path,
				"method", r.Method,
				"user_agent", r.Header.Get("User-Agent"),
			)
			http.Error(w, "CSRF token missing", http.StatusForbidden)
			return
		}
		expectedToken := cookie.Value

		// Get token from request (try header first, then form)
		var requestToken string
		if requestToken = r.Header.Get(csrfHeader); requestToken == "" {
			// For htmx requests, token is in form or query params
			if err := r.ParseForm(); err == nil {
				requestToken = r.FormValue(csrfFormField)
			}
		}

		// Validate token
		if requestToken == "" || !c.validateToken(expectedToken, requestToken) {
			// Log CSRF validation failure with context
			slog.Warn("CSRF token validation failed",
				"ip", r.RemoteAddr,
				"path", r.URL.Path,
				"method", r.Method,
				"user_agent", r.Header.Get("User-Agent"),
				"token_present", requestToken != "",
				"token_source", func() string {
					if r.Header.Get(csrfHeader) != "" {
						return "header"
					}
					return "form"
				}(),
			)
			http.Error(w, "CSRF token invalid", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// generateToken generates a new random CSRF token
func (c *CSRF) generateToken() string {
	b := make([]byte, csrfTokenLength)
	if _, err := rand.Read(b); err != nil {
		// CRITICAL: If crypto/rand fails, we must fail hard
		// Never fall back to predictable tokens as that breaks CSRF protection
		panic(fmt.Sprintf("CSRF token generation failed - crypto/rand unavailable: %v", err))
	}
	return base64.URLEncoding.EncodeToString(b)
}

// validateToken compares two tokens in constant time
func (c *CSRF) validateToken(expected, actual string) bool {
	return subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}
