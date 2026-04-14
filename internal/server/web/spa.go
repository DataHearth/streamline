package web

import (
	"log/slog"
	"net/http"

	webassets "github.com/datahearth/streamline/web"
)

// SPAShell serves the embedded Svelte single-page app entry HTML. Every
// non-API, non-static path returns the same shell; Routify takes over
// routing client-side from there. The shell loads the bundled JS + CSS
// from /static/dist/.
func (h *Handler) SPAShell(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if _, err := w.Write(webassets.SPAShell); err != nil {
		slog.DebugContext(
			r.Context(),
			"spa shell write failed",
			"error", err,
		)
	}
}

// APIDocs serves the embedded Scalar shell at /api/docs. The bundled docs
// JS + CSS live under /static/.
func (h *Handler) APIDocs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := w.Write(webassets.APIDocsShell); err != nil {
		slog.DebugContext(
			r.Context(),
			"api docs shell write failed",
			"error", err,
		)
	}
}
