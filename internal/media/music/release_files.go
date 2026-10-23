package music

import (
	"cmp"
	"context"
	"slices"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/library/audiotags"
	"github.com/datahearth/streamline/internal/otelx"
)

// ReleaseFilesView is a release's torrent read against the album before any
// grab: each file with the track its name points at, and every track with
// whether it already holds a file and whether the release seems to carry it.
// It reads names, where the import reads tags, so it is a preview: a file
// named for no track can still fill one once its tags are read.
type ReleaseFilesView struct {
	Files  []ReleaseFileView
	Tracks []ReleaseTrackView
}

type ReleaseFileView struct {
	Path string
	Size int64
	// TrackID is the track the file's name points at; 0 for none, which is
	// every file that is not audio.
	TrackID uint32
}

type ReleaseTrackView struct {
	ID        uint32
	Disc      uint8
	Position  uint16
	Title     string
	HasFile   bool
	InRelease bool
}

// AlbumReleaseFiles lists the files of one release of the album and maps them
// onto its tracks. It costs one request to the indexer, for the .torrent; a
// magnet answers download.ErrFileListUnavailable.
func (s *Service) AlbumReleaseFiles(
	ctx context.Context,
	albumID uint32,
	result indexer.SearchResult,
) (*ReleaseFilesView, error) {
	ctx, span := tracer.Start(ctx, "music.album_release_files",
		trace.WithAttributes(
			attribute.Int("album.id", int(albumID)),
			attribute.String("release.title", result.Title),
		))
	defer span.End()

	a, err := s.db.FindAlbumByID(ctx, albumID)
	if err != nil {
		return nil, otelx.RecordSpanError(span, albumNotFound(err))
	}
	files, err := s.download.ReleaseFiles(ctx, result)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}

	tracks := a.Edges.Tracks
	var audio []string
	for _, f := range files {
		if audiotags.IsAudioFile(f.Path) {
			audio = append(audio, f.Path)
		}
	}
	// The importer's own decision, made over the whole release.
	matcher := library.NewTrackMatcherByName(tracks, audio)
	in := make(map[uint32]bool, len(tracks))
	view := &ReleaseFilesView{Files: make([]ReleaseFileView, 0, len(files))}
	for _, f := range files {
		fv := ReleaseFileView{Path: f.Path, Size: f.Size}
		if audiotags.IsAudioFile(f.Path) {
			if t := matcher.MatchByName(f.Path); t != nil {
				fv.TrackID = t.ID
				in[t.ID] = true
			}
		}
		view.Files = append(view.Files, fv)
	}
	view.Tracks = make([]ReleaseTrackView, 0, len(tracks))
	for _, t := range tracks {
		view.Tracks = append(view.Tracks, ReleaseTrackView{
			ID:        t.ID,
			Disc:      t.Disc,
			Position:  t.Position,
			Title:     t.Title,
			HasFile:   len(t.Edges.MediaFiles) > 0,
			InRelease: in[t.ID],
		})
	}
	slices.SortFunc(view.Tracks, func(x, y ReleaseTrackView) int {
		return cmp.Or(
			cmp.Compare(x.Disc, y.Disc),
			cmp.Compare(x.Position, y.Position),
		)
	})
	span.SetAttributes(
		attribute.Int("files", len(view.Files)),
		attribute.Int("tracks_matched", len(in)),
	)
	return view, nil
}
