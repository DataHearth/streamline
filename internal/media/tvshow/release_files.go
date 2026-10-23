package tvshow

import (
	"context"
	"errors"
	"fmt"
	"path"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/tvshow"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/otelx"
)

// ErrSeasonNotFound is a season number the show has no episodes under.
var ErrSeasonNotFound = errors.New("season not found")

// PackFilesView is a pack's torrent read against the show before any grab:
// each file with the episode its name points at, and every episode of the
// scope with whether it holds a file, whether a grab without replace is for
// it (Wanted: still wanted and aired, what grabPackAndMark fetches), and
// whether the pack seems to carry it.
type PackFilesView struct {
	Files    []PackFile
	Episodes []PackEpisode
}

type PackFile struct {
	Path string
	Size int64
	// EpisodeID is the episode the file's name points at; 0 for none, which
	// is every file too small or not video, as the selective keep-set reads
	// them.
	EpisodeID uint32
}

type PackEpisode struct {
	ID        uint32
	Season    uint16
	Number    uint16
	Title     string
	HasFile   bool
	Wanted    bool
	InRelease bool
}

// PackReleaseFiles lists the files of one season or whole-series release and
// maps them onto the show's episodes with the matcher the selective keep-set
// and the importer share, so what it reports is what a grab would keep. A nil
// season is the whole show. It costs one request to the indexer, for the
// .torrent; a magnet answers download.ErrFileListUnavailable.
func (s *Service) PackReleaseFiles(
	ctx context.Context,
	seriesID uint32,
	season *uint16,
	result indexer.SearchResult,
) (*PackFilesView, error) {
	ctx, span := tracer.Start(ctx, "tvshow.pack_release_files",
		trace.WithAttributes(
			attribute.Int64("tvshow.id", int64(seriesID)),
			attribute.String("release.title", result.Title),
		))
	defer span.End()

	show, err := s.db.FindTVShowByID(ctx, seriesID)
	if err != nil {
		if ent.IsNotFound(err) {
			err = fmt.Errorf("series %d: %w", seriesID, ErrSeriesNotFound)
		}
		return nil, otelx.RecordSpanError(span, err)
	}
	var eps []*ent.Episode
	seasonOf := map[uint32]uint16{}
	for _, se := range show.Edges.Seasons {
		if season != nil && se.Number != *season {
			continue
		}
		for _, e := range se.Edges.Episodes {
			eps = append(eps, e)
			seasonOf[e.ID] = se.Number
		}
	}
	if len(eps) == 0 && season != nil {
		return nil, otelx.RecordSpanError(span,
			fmt.Errorf("season %d: %w", *season, ErrSeasonNotFound))
	}

	files, err := s.download.ReleaseFiles(ctx, result)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}

	anime := show.Type == tvshow.TypeAnime
	in := map[uint32]bool{}
	view := &PackFilesView{Files: make([]PackFile, 0, len(files))}
	for _, f := range files {
		pf := PackFile{Path: f.Path, Size: f.Size}
		if library.IsVideoPath(f.Path) && f.Size >= library.MinEpisodeSize {
			parsed := library.Parse(path.Base(f.Path))
			if _, ep := library.MatchEpisodeInSeason(
				parsed, show.Edges.Seasons, anime,
			); ep != nil {
				pf.EpisodeID = ep.ID
				in[ep.ID] = true
			}
		}
		view.Files = append(view.Files, pf)
	}

	wanted := map[uint32]bool{}
	for _, e := range wantedAiredEpisodes(eps, time.Now()) {
		wanted[e.ID] = true
	}
	view.Episodes = make([]PackEpisode, 0, len(eps))
	for _, e := range eps {
		view.Episodes = append(view.Episodes, PackEpisode{
			ID:        e.ID,
			Season:    seasonOf[e.ID],
			Number:    e.Number,
			Title:     e.Title,
			HasFile:   len(e.Edges.MediaFiles) > 0,
			Wanted:    wanted[e.ID],
			InRelease: in[e.ID],
		})
	}
	span.SetAttributes(
		attribute.Int("files", len(view.Files)),
		attribute.Int("episodes_matched", len(in)),
	)
	return view, nil
}
