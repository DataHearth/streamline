// Package httputil holds HTTP-related helpers shared across the webui.
package httputil

import (
	"net/http"

	"github.com/datahearth/streamline/internal/server/middleware"
)

// ClientIPString returns the client IP as a string, resolved via
// middleware.ClientIP (the chi ClientIPFrom* middleware), or RemoteAddr.
func ClientIPString(r *http.Request) string {
	ip := middleware.ClientIP(r)
	if ip == nil {
		return r.RemoteAddr
	}
	return ip.String()
}
