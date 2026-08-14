package web

import (
	"log/slog"
	"net/http"

	"github.com/datahearth/streamline/internal/auth"
	"github.com/go-chi/chi/v5"
)

// registerWebMediaServerRoutes wires the admin-only Plex PIN OAuth endpoints
// the settings SPA drives: a POST to begin the flow and a GET the SPA polls
// until Plex fills in the auth token. The POST rides the session cookie and
// changes state at plex.tv, so it goes through csrfGuard (see csrf.go) exactly
// like the /auth POSTs.
//
// Both routes sit on the bare root router, so the /api/v1 roleGuard never sees
// them and each has to carry its own admin check. The GET takes the opaque
// flow id begin handed out, never the plex.tv PIN id: the PIN id is a small
// sequential number, and the poll answers with a Plex account token.
func (h *Handler) registerWebMediaServerRoutes(r chi.Router) {
	r.With(csrfGuard).Post("/settings/media-servers/plex/pin", h.plexPinBegin)
	r.Get("/settings/media-servers/plex/pin/{flowID}", h.plexPinPoll)
}

// requireAdmin mirrors restapi.requireAdmin for the root-router web routes.
func requireAdmin(w http.ResponseWriter, r *http.Request) *auth.Claims {
	c := auth.ClaimsFromContext(r.Context())
	if c == nil || !auth.RoleAtLeast(c.Role, "admin") {
		writeError(w, r, http.StatusForbidden, "Admin role required.", "forbidden")
		return nil
	}
	return c
}

type plexPinBeginResponse struct {
	FlowID   string `json:"flow_id"`
	AuthURL  string `json:"auth_url"`
	ClientID string `json:"client_id"`
}

func (h *Handler) plexPinBegin(w http.ResponseWriter, r *http.Request) {
	claims := requireAdmin(w, r)
	if claims == nil {
		return
	}
	w.Header().Set("Cache-Control", "no-store, private")
	pin, err := h.mediaServers.BeginPlexPin(r.Context())
	if err != nil {
		slog.ErrorContext(r.Context(), "plex pin begin failed", "error", err)
		writeError(
			w, r, http.StatusBadGateway,
			"Couldn't reach Plex to start sign-in.", "",
		)
		return
	}
	flowID, err := h.plexFlows.begin(pin.ID, claims)
	if err != nil {
		slog.ErrorContext(r.Context(), "plex pin flow id failed", "error", err)
		writeError(
			w, r, http.StatusInternalServerError,
			"Couldn't start sign-in.", "",
		)
		return
	}
	writeJSON(w, r, http.StatusOK, plexPinBeginResponse{
		FlowID:   flowID,
		AuthURL:  pin.AuthURL,
		ClientID: pin.ClientID,
	})
}

type plexPinPollResponse struct {
	AuthToken string `json:"auth_token,omitempty"`
	Expired   bool   `json:"expired,omitempty"`
}

func (h *Handler) plexPinPoll(w http.ResponseWriter, r *http.Request) {
	claims := requireAdmin(w, r)
	if claims == nil {
		return
	}
	w.Header().Set("Cache-Control", "no-store, private")
	flowID := chi.URLParam(r, "flowID")
	pinID, ok := h.plexFlows.take(flowID, claims)
	if !ok {
		writeError(
			w, r, http.StatusNotFound,
			"Unknown or expired sign-in flow.", "",
		)
		return
	}
	res, err := h.mediaServers.PollPlexPin(r.Context(), pinID)
	if err != nil {
		slog.ErrorContext(r.Context(), "plex pin poll failed", "error", err)
		writeError(
			w, r, http.StatusBadGateway,
			"Couldn't reach Plex while waiting for sign-in.", "",
		)
		return
	}
	if res.AuthToken != "" || res.Expired {
		h.plexFlows.consume(flowID)
	}
	writeJSON(w, r, http.StatusOK, plexPinPollResponse{
		AuthToken: res.AuthToken,
		Expired:   res.Expired,
	})
}
