package server

import (
	"net/http"
	"time"
)

// Connection deadlines for the public listener. The single-binary default
// exposes it directly (ServerConfig has no TLS option and the reverse proxy
// is optional), so a connection that never finishes its headers, never
// delivers its body, or never drains its response must not pin a goroutine
// and a socket indefinitely.
//
// writeTimeout has to clear the slowest synchronous handler: those chain
// outbound calls that otelx.HTTPClient already caps at 30s each, so two
// minutes leaves room for a short chain. The one streaming endpoint,
// GET /api/v1/schedules/events, clears the deadline on its own connection
// through http.ResponseController and is not bound by it.
const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = time.Minute
	writeTimeout      = 2 * time.Minute
	idleTimeout       = 2 * time.Minute
)

// NewHTTPServer builds the production HTTP server with those deadlines
// applied. MaxHeaderBytes stays at the 1MB default, which already bounds the
// header-flood variant.
func NewHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}
