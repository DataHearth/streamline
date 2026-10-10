package subsonic

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/track"
)

// stream always serves the original file: maxBitRate and format are ignored.
func (h *Handler) stream(w http.ResponseWriter, r *http.Request) error {
	id, err := idParam(r, kindTrack)
	if err != nil {
		return err
	}
	mf, err := h.ent.MediaFile.Query().
		Where(
			mediafile.HasTrackWith(track.IDEQ(id)),
			mediafile.MissingSinceIsNil(),
		).
		Order(mediafile.ByID()).
		First(r.Context())
	if err != nil {
		return lookupErr(err)
	}
	return serveFile(w, r, mf)
}

func serveFile(w http.ResponseWriter, r *http.Request, mf *ent.MediaFile) error {
	f, err := os.Open(mf.Path) //nolint:gosec // library row, not request input
	if err != nil {
		return &apiError{Code: errNotFound, Message: "file missing"}
	}
	defer f.Close()

	w.Header().Set("Content-Type", contentTypeFor(suffixOf(mf)))
	http.ServeContent(w, r, filepath.Base(mf.Path), mf.UpdateTime, f)
	return nil
}

func (h *Handler) getCoverArt(w http.ResponseWriter, r *http.Request) error {
	kind, id, err := anyIDParam(r)
	if err != nil {
		return err
	}
	switch kind {
	case kindAlbum:
		h.posters.Serve(w, r, "albums", id)
	case kindArtist:
		h.posters.Serve(w, r, "artists", id)
	default:
		return errNotFoundErr
	}
	return nil
}
