// Package web hosts the non-SPA HTTP surface: the SPA shell, the API docs
// shell, JSON auth endpoints (login / register / logout / config), and the
// OIDC redirect dance. Page rendering is owned entirely by the Svelte SPA;
// this package is intentionally small.
package web

import (
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/mediaserver"
	"github.com/datahearth/streamline/web"
	"github.com/go-chi/chi/v5"
)

// Handler exposes the non-SPA webui routes. Constructed in the composition
// root with the auth service-layer deps.
type Handler struct {
	auth         auth.Manager
	oidc         auth.OIDCManager
	limiter      auth.Limiter
	mediaServers mediaserver.Manager
}

// Deps is the dependency set required by the web Handler.
type Deps struct {
	Auth         auth.Manager
	OIDC         auth.OIDCManager
	Limiter      auth.Limiter
	MediaServers mediaserver.Manager
}

// New constructs a Handler from the given Deps.
func New(d Deps) *Handler {
	return &Handler{
		auth:         d.Auth,
		oidc:         d.OIDC,
		limiter:      d.Limiter,
		mediaServers: d.MediaServers,
	}
}

// Mount wires webui routes onto r. Caller is responsible for separately
// mounting the SPA shell + API docs on the desired paths.
func Mount(r chi.Router, h *Handler) {
	staticFS, err := fs.Sub(web.Assets, "static")
	if err != nil {
		slog.ErrorContext(context.Background(), "static sub-fs failed", "error", err)
		return
	}
	r.Handle(
		"/static/*",
		http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))),
	)
	h.registerWebAuthRoutes(r)
	h.registerWebMediaServerRoutes(r)
}

func writeJSON(w http.ResponseWriter, r *http.Request, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.ErrorContext(r.Context(), "json encode failed", "error", err)
	}
}

type errorBody struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

func writeError(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	message, code string,
) {
	writeJSON(w, r, status, errorBody{Message: message, Code: code})
}
