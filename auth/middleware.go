package auth

import (
	"net/http"
	"strings"
)

// Middleware provides authentication middleware
type Middleware struct {
	token string
	enabled bool
}

// NewMiddleware creates a new authentication middleware
func NewMiddleware(token string) *Middleware {
	return &Middleware{
		token:   token,
		enabled: token != "",
	}
}

// Handler wraps an http.Handler with authentication
func (m *Middleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for /status endpoint
		if r.URL.Path == "/status" {
			next.ServeHTTP(w, r)
			return
		}

		// If auth is disabled, pass through
		if !m.enabled {
			next.ServeHTTP(w, r)
			return
		}

		// Check Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}

		// Check Bearer token format
		if !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token != m.token {
			http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}

		// Token is valid, proceed
		next.ServeHTTP(w, r)
	})
}

// IsEnabled returns true if authentication is enabled
func (m *Middleware) IsEnabled() bool {
	return m.enabled
}

// ValidateRequest checks if request has valid auth (without enforcing it)
func (m *Middleware) ValidateRequest(r *http.Request) bool {
	if !m.enabled {
		return true // Auth disabled, so request is valid
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return false
	}

	if !strings.HasPrefix(authHeader, "Bearer ") {
		return false
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")
	return token == m.token
}
