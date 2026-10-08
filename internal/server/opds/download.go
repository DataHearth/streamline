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
			mediafile.QualityEQ(format),
		).
		WithBook(func(q *ent.BookQuery) { q.WithAuthor() }).
		First(r.Context())
	if ent.IsNotFound(err) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.fail(w, r, "loading OPDS download failed", err)
		return
	}

	f, err := os.Open(
		mf.Path,
	) //nolint:gosec // path is the media_file row's, never request input
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

	ext := mf.Quality
	if ext == "other" {
		ext = strings.TrimPrefix(filepath.Ext(mf.Path), ".")
	}
	w.Header().Set("Content-Type", ebookContentType(mf.Quality))
	w.Header().
		Set("Content-Disposition", `attachment; filename="`+downloadName(mf.Edges.Book, ext)+`"`)
	http.ServeContent(w, r, filepath.Base(mf.Path), mf.UpdateTime, f)
}

func downloadName(bk *ent.Book, ext string) string {
	name := library.SanitizePath(bk.Title + " - " + bk.Edges.Author.Name)
	if ext == "" {
		return name
	}
	return name + "." + ext
}
