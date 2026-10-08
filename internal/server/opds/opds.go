// Package opds serves an OPDS 1.2 catalog of the ebook library, behind HTTP
// Basic auth against the per-user OPDS token. Routes are relative so the
// router can be mounted at /opds; hrefs inside feeds are absolute.
package opds

import (
	"github.com/go-chi/chi/v5"

	"github.com/datahearth/streamline/ent"
)

type Handler struct {
	client *ent.Client
}

func New(client *ent.Client) *Handler {
	return &Handler{client: client}
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
	return r
}
