package middleware

import (
	"net/http"
	"slices"

	"github.com/datahearth/streamline/internal/config"
)

const (
	corsMethods = "GET, HEAD, POST, PUT, PATCH, DELETE"
	corsHeaders = "Authorization, X-API-Key, Content-Type, Range, If-None-Match, If-Modified-Since"
	corsExposed = "Content-Length, Content-Range, Retry-After"
)

// CORS lets a page served from an origin listed in server.cors_origins read
// what the server answers, and answers that origin's preflights itself: a
// preflight carries no credentials, so past this point auth would refuse it
// and the browser would never send the real request.
//
// Access-Control-Allow-Credentials is never sent. A listed origin
// authenticates with a key in a header; the session cookie stays same-origin.
func CORS() func(http.Handler) http.Handler {
	origins := config.Get().Server.CORSOrigins
	return func(next http.Handler) http.Handler {
		if len(origins) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Add("Vary", "Origin")
			origin := r.Header.Get("Origin")
			if !slices.Contains(origins, origin) {
				next.ServeHTTP(w, r)
				return
			}
			h.Set("Access-Control-Allow-Origin", origin)
			if r.Method == http.MethodOptions &&
				r.Header.Get("Access-Control-Request-Method") != "" {
				h.Set("Access-Control-Allow-Methods", corsMethods)
				h.Set("Access-Control-Allow-Headers", corsHeaders)
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			h.Set("Access-Control-Expose-Headers", corsExposed)
			next.ServeHTTP(w, r)
		})
	}
}
