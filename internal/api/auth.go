package api

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// apiKeyAuth middleware rejects requests to protected routes when an API key
// is configured but the request does not present a matching key. The key may
// be supplied via the Authorization: Bearer <key> header, the
// X-API-Key: <key> header, or the ?api_key=<key> query parameter (the latter
// is needed for EventSource, which does not support custom headers). When
// apiKey is empty the middleware is a no-op.
func apiKeyAuth(apiKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if apiKey == "" {
			return next
		}
		expected := []byte(apiKey)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			provided := extractAPIKey(r)
			if provided == "" || subtle.ConstantTimeCompare([]byte(provided), expected) != 1 {
				renderError(w, r, http.StatusUnauthorized, "unauthorized")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func extractAPIKey(r *http.Request) string {
	if raw := strings.TrimSpace(r.Header.Get("X-API-Key")); raw != "" {
		return raw
	}
	if auth := strings.TrimSpace(r.Header.Get("Authorization")); auth != "" {
		if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
			return strings.TrimSpace(auth[7:])
		}
	}
	if q := strings.TrimSpace(r.URL.Query().Get("api_key")); q != "" {
		return q
	}
	return ""
}
