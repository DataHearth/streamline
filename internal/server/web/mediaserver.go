package web

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// registerWebMediaServerRoutes wires the cookie-authenticated Plex PIN
// OAuth endpoints the settings SPA drives: a POST to begin the flow and a
// GET the SPA polls until Plex fills in the auth token.
func (h *Handler) registerWebMediaServerRoutes(r chi.Router) {
	r.Post("/settings/media-servers/plex/pin", h.plexPinBegin)
	r.Get("/settings/media-servers/plex/pin/{pinID}", h.plexPinPoll)
}

type plexPinBeginResponse struct {
	PinID    uint64 `json:"pin_id"`
	AuthURL  string `json:"auth_url"`
	ClientID string `json:"client_id"`
}

func (h *Handler) plexPinBegin(w http.ResponseWriter, r *http.Request) {
	pin, err := h.mediaServers.BeginPlexPin(r.Context())
	if err != nil {
		slog.ErrorContext(r.Context(), "plex pin begin failed", "error", err)
		writeError(
			w, r, http.StatusBadGateway,
			"Couldn't reach Plex to start sign-in.", "",
		)
		return
	}
	writeJSON(w, r, http.StatusOK, plexPinBeginResponse{
		PinID:    pin.ID,
		AuthURL:  pin.AuthURL,
		ClientID: pin.ClientID,
	})
}

type plexPinPollResponse struct {
	AuthToken string `json:"auth_token,omitempty"`
	Expired   bool   `json:"expired,omitempty"`
}

func (h *Handler) plexPinPoll(w http.ResponseWriter, r *http.Request) {
	pinID, err := strconv.ParseUint(chi.URLParam(r, "pinID"), 10, 64)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "Invalid PIN id.", "")
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
	writeJSON(w, r, http.StatusOK, plexPinPollResponse{
		AuthToken: res.AuthToken,
		Expired:   res.Expired,
	})
}
