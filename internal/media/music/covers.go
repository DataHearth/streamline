package music

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/library/audiotags"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/observability"
	"github.com/datahearth/streamline/internal/otelx"
)

const (
	coverKind = "albums"

	coverSourceEmbedded     = "embedded"
	coverSourceFolder       = "folder"
	coverSourceDeezerUPC    = "deezer_upc"
	coverSourceDeezerSearch = "deezer_search"
	coverSourceCAA          = "caa"
	coverSourceNone         = "none"
)

var (
	meter = otel.Meter("github.com/datahearth/streamline/internal/media/music")

	coverResolutions = otelx.Must(meter.Int64Counter(
		"streamline.music.cover_resolutions",
		metric.WithDescription("Album cover resolutions by the source that won"),
	))
)

var (
	folderCoverNames = []string{"cover", "folder", "front"}
	folderCoverExts  = []string{".jpg", ".jpeg", ".png"}
)

// CoverResolver is what the adoption and import paths call once an album's
// files are on disk, so they do not depend on the whole service.
type CoverResolver interface {
	// ResolveCoversInBackground resolves each album's cover off the caller's
	// goroutine and never reports failure: a missing cover must not fail the
	// add, adopt or import that triggered it.
	ResolveCoversInBackground(ctx context.Context, albumIDs ...uint32)
}

var _ CoverResolver = (*Service)(nil)

func (s *Service) ResolveCoversInBackground(
	ctx context.Context,
	albumIDs ...uint32,
) {
	if len(albumIDs) == 0 {
		return
	}
	bg := context.WithoutCancel(ctx)
	go func() {
		defer observability.RecoverPanic(bg, "music.resolve_covers", nil)
		for _, id := range albumIDs {
			a, err := s.db.FindAlbumByID(bg, id)
			if err != nil {
				slog.WarnContext(bg, "album cover lookup failed",
					"album.id", id, "error", err)
				continue
			}
			s.resolveCover(bg, a)
		}
	}()
}

// hasCover reports whether a poster is already cached for the album.
func (s *Service) hasCover(albumID uint32) bool {
	st, err := os.Stat(s.posters.Path(coverKind, albumID))
	return err == nil && st.Size() > 0
}

// resolveCover stores the album's cover from the first source that yields an
// image: a picture embedded in one of its files, a cover file beside them,
// Deezer by barcode, Deezer by search, then the Cover Art Archive. The local
// sources run even when a cover is cached, since the album's own artwork
// outranks a downloaded one; the remote ones only when none is. It takes the
// album with its artist, tracks and their media files loaded.
func (s *Service) resolveCover(ctx context.Context, a *ent.Album) {
	ctx, span := tracer.Start(ctx, "music.resolve_cover",
		trace.WithAttributes(attribute.Int64("album.id", int64(a.ID))))
	defer span.End()

	var errs []error
	source := s.localCover(ctx, a, &errs)
	if source == "" && s.hasCover(a.ID) {
		span.SetAttributes(attribute.String("cover.source", "cached"))
		if len(errs) > 0 {
			err := otelx.RecordSpanError(span, errors.Join(errs...))
			slog.WarnContext(ctx, "album cover kept, local sources failed",
				"album.id", a.ID, "error", err)
		}
		return
	}
	if source == "" {
		source = s.remoteCover(ctx, a, &errs)
	}
	span.SetAttributes(attribute.String("cover.source", source))
	coverResolutions.Add(ctx, 1, metric.WithAttributes(
		attribute.String("source", source)))
	if source == coverSourceNone && len(errs) > 0 {
		err := otelx.RecordSpanError(span, errors.Join(errs...))
		slog.WarnContext(ctx, "album cover not resolved",
			"album.id", a.ID, "error", err)
	}
}

func (s *Service) localCover(
	ctx context.Context,
	a *ent.Album,
	errs *[]error,
) string {
	var paths []string
	for _, t := range a.Edges.Tracks {
		for _, mf := range t.Edges.MediaFiles {
			paths = append(paths, mf.Path)
		}
	}
	if len(paths) == 0 {
		return ""
	}

	for _, p := range paths {
		data, mime, err := audiotags.Picture(p)
		if err != nil {
			*errs = append(*errs, err)
			continue
		}
		if data == nil || (mime != "" && !strings.HasPrefix(mime, "image/")) {
			continue
		}
		if err := s.posters.Put(
			ctx,
			coverKind,
			a.ID,
			bytes.NewReader(data),
		); err != nil {
			*errs = append(*errs, err)
			continue
		}
		return coverSourceEmbedded
	}

	cover, err := folderCover(filepath.Dir(paths[0]))
	if err != nil {
		*errs = append(*errs, err)
		return ""
	}
	if cover == "" {
		return ""
	}
	if err := s.putFile(ctx, a.ID, cover); err != nil {
		*errs = append(*errs, err)
		return ""
	}
	return coverSourceFolder
}

func (s *Service) putFile(ctx context.Context, albumID uint32, path string) error {
	//nolint:gosec // path is a cover file found beside the album's own media
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return s.posters.Put(ctx, coverKind, albumID, f)
}

// folderCover finds cover|folder|front.jpg|jpeg|png in dir, any case, in that
// order of preference. "" when there is none.
func folderCover(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, name := range folderCoverNames {
		for _, ext := range folderCoverExts {
			for _, e := range entries {
				if !e.IsDir() && strings.EqualFold(e.Name(), name+ext) {
					return filepath.Join(dir, e.Name()), nil
				}
			}
		}
	}
	return "", nil
}

func (s *Service) remoteCover(
	ctx context.Context,
	a *ent.Album,
	errs *[]error,
) string {
	fetch := func(src, source string) string {
		if err := s.posters.Fetch(ctx, coverKind, a.ID, src); err != nil {
			*errs = append(*errs, err)
			return ""
		}
		return source
	}

	if s.covers != nil && a.Barcode != "" {
		src, err := s.covers.CoverByUPC(ctx, a.Barcode)
		if err != nil {
			*errs = append(*errs, err)
		} else if src != "" {
			if got := fetch(src, coverSourceDeezerUPC); got != "" {
				return got
			}
		}
	}

	if artist := a.Edges.Artist; s.covers != nil && artist != nil {
		hit, err := s.covers.SearchCover(ctx, artist.Name, a.Title)
		if err != nil {
			*errs = append(*errs, err)
		} else if hit != nil && library.TitleMatchesStrict(hit.ArtistName, artist.Name) {
			if got := fetch(hit.CoverURL, coverSourceDeezerSearch); got != "" {
				return got
			}
		}
	}

	if src := metadata.CoverArtURL(a.Mbid); src != "" {
		if got := fetch(src, coverSourceCAA); got != "" {
			return got
		}
	}
	return coverSourceNone
}
