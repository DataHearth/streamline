// Package subsonic serves the Subsonic REST API (1.16.1) over the adopted
// music library. It sits outside the OpenAPI surface, like /health.
package subsonic

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/posters"
)

type Handler struct {
	ent     *ent.Client
	posters posters.Manager
}

type Deps struct {
	Ent     *ent.Client
	Posters posters.Manager
}

func New(d Deps) *Handler {
	return &Handler{ent: d.Ent, posters: d.Posters}
}

type endpoint func(w http.ResponseWriter, r *http.Request) error

// Routes is mounted at /rest by the composition root.
func (h *Handler) Routes() http.Handler {
	endpoints := map[string]endpoint{
		"ping":            h.ping,
		"getLicense":      h.getLicense,
		"getMusicFolders": h.getMusicFolders,
		"getArtists":      h.getArtists,
		"getArtist":       h.getArtist,
		"getAlbum":        h.getAlbum,
		"getSong":         h.getSong,
		"getAlbumList2":   h.getAlbumList2,
		"search3":         h.search3,
		"stream":          h.stream,
		"getCoverArt":     h.getCoverArt,
	}

	r := chi.NewRouter()
	r.HandleFunc("/{endpoint}", func(w http.ResponseWriter, r *http.Request) {
		// Clients such as DSub and Symfonium send the parameters as a POST
		// form body; ParseForm merges that with the query into r.Form.
		if err := r.ParseForm(); err != nil {
			writeError(w, r, errGeneric, "Malformed request")
			return
		}
		if _, err := h.authenticate(r); err != nil {
			fail(w, r, err)
			return
		}
		name := strings.TrimSuffix(chi.URLParam(r, "endpoint"), ".view")
		fn, ok := endpoints[name]
		if !ok {
			writeError(w, r, errGeneric, "Not implemented")
			return
		}
		if err := fn(w, r); err != nil {
			fail(w, r, err)
		}
	})
	return r
}

func fail(w http.ResponseWriter, r *http.Request, err error) {
	if apiErr, ok := errors.AsType[*apiError](err); ok {
		writeError(w, r, apiErr.Code, apiErr.Message)
		return
	}
	slog.ErrorContext(r.Context(), "subsonic request failed", "error", err)
	writeError(w, r, errGeneric, "Internal error")
}
