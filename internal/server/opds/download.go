package opds

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/library"
)

func (h *Handler) download(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "bookID"), 10, 32)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	format := chi.URLParam(r, "format")

	mf, err := h.client.MediaFile.Query().
		Where(
			mediafile.HasBookWith(book.IDEQ(uint32(id))),
			mediafile.BookKindEQ(mediafile.BookKindEbook),
			mediafile.QualityEqualFold(format),
		).
		WithBook().
		First(r.Context())
	if ent.IsNotFound(err) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.fail(w, r, "loading OPDS download failed", err)
		return
	}

	//nolint:gosec // path is the media_file row's, never request input
	f, err := os.Open(mf.Path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.ErrorContext(
				r.Context(),
				"opening OPDS download failed",
				"error",
				err,
			)
		}
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	ext := strings.ToLower(mf.Quality)
	if ext == "" || ext == "other" {
		ext = strings.TrimPrefix(filepath.Ext(mf.Path), ".")
	}
	w.Header().Set("Content-Type", ebookContentType(mf.Quality))
	w.Header().
		Set("Content-Disposition", `attachment; filename="`+downloadName(mf.Edges.Book, ext)+`"`)
	http.ServeContent(w, r, filepath.Base(mf.Path), mf.UpdateTime, f)
}

func downloadName(bk *ent.Book, ext string) string {
	name := bk.Title + " - " + bk.AuthorName
	if ext != "" {
		name += "." + ext
	}
	return library.SanitizePath(name)
}

func (h *Handler) cover(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "bookID"), 10, 32)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h.posters.Serve(w, r, "books", uint32(id))
}
