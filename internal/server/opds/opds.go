// Package opds serves an OPDS 1.2 catalog of the ebook library, behind HTTP
// Basic auth against the per-user OPDS token. Routes are relative so the
// router can be mounted at /opds; hrefs inside feeds are absolute.
package opds

import (
	"github.com/go-chi/chi/v5"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/appaccess"
	"github.com/datahearth/streamline/internal/posters"
)

type Handler struct {
	client  *ent.Client
	tracker *appaccess.Tracker
	posters posters.Manager
}

func New(
	client *ent.Client,
	tracker *appaccess.Tracker,
	posters posters.Manager,
) *Handler {
	return &Handler{client: client, tracker: tracker, posters: posters}
}

func (h *Handler) Router() chi.Router {
	r := chi.NewRouter()
	r.Use(h.requireAuth)
	r.Get("/", h.root)
	r.Get("/authors", h.authors)
	r.Get("/authors/{id}", h.authorBooks)
	r.Get("/recent", h.recent)
	r.Get("/search.xml", h.searchDescription)
	r.Get("/search", h.search)
	r.Get("/cover/{bookID}", h.cover)
	r.Get("/download/{bookID}/{format}", h.download)
	return r
}
