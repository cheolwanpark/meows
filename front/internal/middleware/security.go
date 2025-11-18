package middleware

import "net/http"

// IsSecureRequest returns true if the request uses HTTPS through direct TLS termination.
//
// This function checks if the Go server is handling TLS directly (r.TLS != nil).
// For deployments behind a reverse proxy that terminates TLS, additional configuration
// would be needed to check X-Forwarded-Proto header.
//
// Use this to set the Secure flag on cookies consistently across the application.
func IsSecureRequest(r *http.Request) bool {
	return r.TLS != nil
}
