package tvshow

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/episode"
	enttvshow "github.com/datahearth/streamline/ent/tvshow"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/events"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/posters"
	"github.com/datahearth/streamline/internal/scheduler"
	"github.com/datahearth/streamline/internal/utils/numeric"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

var (
	tracer = otel.Tracer("github.com/datahearth/streamline/internal/media/tvshow")
	meter  = otel.Meter("github.com/datahearth/streamline/internal/media/tvshow")
)

var (
	showsAdded   metric.Int64Counter
	showsUpdated metric.Int64Counter
	showsDeleted metric.Int64Counter
)

func init() {
	showsAdded = otelx.Must(meter.Int64Counter(
		"streamline.tvshows.added",
		metric.WithDescription("TV shows added"),
	))
	showsUpdated = otelx.Must(meter.Int64Counter(
		"streamline.tvshows.updated",
		metric.WithDescription("TV shows updated"),
	))
	showsDeleted = otelx.Must(meter.Int64Counter(
		"streamline.tvshows.deleted",
		metric.WithDescription("TV shows deleted"),
	))

	ctx := context.Background()
	showsAdded.Add(ctx, 0)
	showsUpdated.Add(ctx, 0)
	showsDeleted.Add(ctx, 0)
}

var (
	ErrNoQualityProfile  = errors.New("no quality profile configured")
	ErrSeriesNotFound    = errors.New("series not found")
	ErrInvalidSeriesType = errors.New("invalid series type")
	ErrInvalidTVDBID     = errors.New("tvdb id must be non-zero")
	ErrSameTVDBID        = errors.New("series already points at that tvdb id")
	ErrSeriesExists      = errors.New("series already exists")
)

// metadataMinRefreshInterval bounds the TVDB call rate of the metadata-refresh
// scheduler job: only shows last refreshed longer ago than this are touched.
const metadataMinRefreshInterval = 24 * time.Hour

type Service struct {
	db       db.Store
	metadata metadata.TVProvider
	posters  posters.Manager
	download download.Downloader
}

func NewService(
	store db.Store,
	meta metadata.TVProvider,
	p posters.Manager,
	dl download.Downloader,
) *Service {
	return &Service{db: store, metadata: meta, posters: p, download: dl}
}

var _ Manager = (*Service)(nil)

// seedSeasons folds a provider TVDetails' flat episode list into the
// per-season seed shape the DB layer consumes.
func seedSeasons(d *metadata.TVDetails) []db.SeasonSeed {
	bySeason := map[uint16][]db.EpisodeSeed{}
	for _, e := range d.Episodes {
		bySeason[e.SeasonNumber] = append(bySeason[e.SeasonNumber], db.EpisodeSeed{
			Number:         e.Number,
			AbsoluteNumber: e.AbsoluteNumber,
			Title:          e.Title,
			Overview:       e.Overview,
			AirDate:        e.AirDate,
		})
	}
	monitorSpecials := config.Get().Library.MonitorSpecials
	seasons := make([]db.SeasonSeed, 0, len(d.Seasons))
	for _, si := range d.Seasons {
		seasons = append(seasons, db.SeasonSeed{
			Number:      si.Number,
			Name:        si.Name,
			Unmonitored: si.Number == 0 && !monitorSpecials,
			Episodes:    bySeason[si.Number],
		})
	}
	return seasons
}

func (s *Service) Add(
	ctx context.Context,
	tvdbID uint32,
	qualityProfile string,
) (*ent.TVShow, error) {
	ctx, span := tracer.Start(
		ctx,
		"tvshow.add",
		trace.WithAttributes(
			attribute.Int("tvdb.id", int(tvdbID)),
			attribute.String("quality_profile", qualityProfile),
		),
	)
	defer span.End()

	// An empty name resolves to quality_default_profile at read time; reject
	// only when the named profile (or default) resolves to nothing at all.
	if _, ok := config.ResolveQualityProfile(qualityProfile); !ok {
		return nil, otelx.RecordSpanError(span, ErrNoQualityProfile)
	}

	d, err := s.metadata.GetSeries(ctx, tvdbID)
	if err != nil {
		return nil, fmt.Errorf("tvdb get series: %w", err)
	}
	cast := s.seriesCast(ctx, tvdbID)

	show, err := s.db.CreateTVShow(ctx, db.CreateTVShowParams{
		Title:          d.Title,
		OriginalTitle:  d.OriginalTitle,
		Year:           d.Year,
		Overview:       d.Overview,
		TvdbID:         d.TVDBID,
		SeriesStatus:   d.Status,
		Type:           string(d.Type),
		Network:        d.Network,
		Creator:        d.Creator,
		Runtime:        d.Runtime,
		Rating:         float64(d.Rating),
		Genres:         d.Genres,
		Aliases:        d.Aliases,
		Cast:           cast,
		PosterPath:     d.PosterPath,
		QualityProfile: qualityProfile,
		Seasons:        seedSeasons(d),
		FirstAired:     metadata.ParseISODate(d.FirstAired),
	})
	if err != nil {
		return nil, fmt.Errorf("create tv show: %w", err)
	}

	s.fetchPoster(ctx, show.ID, d.PosterPath)
	s.enrichPeople(ctx, show.ID)

	showsAdded.Add(ctx, 1)
	slog.InfoContext(
		ctx,
		"tv show added",
		"title",
		show.Title,
		"tvdb_id",
		show.TvdbID,
	)
	return show, nil
}

// fetchPoster caches the show's poster in the background. Best-effort: a
// missing poster is cosmetic and must not fail the operation that asked for it.
func (s *Service) fetchPoster(ctx context.Context, id uint32, posterPath string) {
	if posterPath == "" || s.posters == nil {
		return
	}
	bg := context.WithoutCancel(ctx)
	src := metadata.TVDBArtworkURL(posterPath)
	go func() {
		if err := s.posters.Fetch(bg, "tvshows", id, src); err != nil {
			slog.WarnContext(bg, "tv poster fetch failed",
				"tvshow.id", id, "error", err)
		}
	}()
}

func (s *Service) List(
	ctx context.Context,
	page, limit uint16,
) ([]*ent.TVShow, uint32, error) {
	ctx, span := tracer.Start(ctx, "tvshow.list")
	defer span.End()
	if page == 0 {
		page = 1
	}
	if limit == 0 {
		limit = 20
	}
	total, err := s.db.CountTVShows(ctx)
	if err != nil {
		return nil, 0, otelx.RecordSpanError(span, err)
	}
	rows, err := s.db.ListTVShows(ctx, uint32(page-1)*uint32(limit), uint32(limit))
	if err != nil {
		return nil, 0, otelx.RecordSpanError(span, err)
	}
	return rows, numeric.SaturateU32(total), nil
}

// FilterList returns one page of series with the episode rollup the list view
// renders, leaving the season/episode tree unloaded — see db.FilterTVShows.
func (s *Service) FilterList(
	ctx context.Context,
	p FilterParams,
) ([]*ent.TVShow, map[uint32]db.EpisodeCounts, uint32, error) {
	ctx, span := tracer.Start(ctx, "tvshow.filter_list",
		trace.WithAttributes(
			attribute.String("filter.status", p.Status),
			attribute.String("filter.type", p.Type),
			attribute.String("filter.query", p.Query),
			attribute.String("filter.sort", p.Sort),
		))
	defer span.End()

	page := p.Page
	if page == 0 {
		page = 1
	}
	limit := p.Limit
	if limit == 0 {
		limit = 20
	}

	rows, counts, total, err := s.db.FilterTVShows(ctx, db.FilterTVShowsParams{
		Status:    p.Status,
		Type:      p.Type,
		Query:     strings.TrimSpace(p.Query),
		Sort:      p.Sort,
		Order:     p.Order,
		Offset:    uint32(page-1) * uint32(limit),
		Limit:     limit,
		Monitored: p.Monitored,
		Now:       time.Now(),
	})
	if err != nil {
		return nil, nil, 0, otelx.RecordSpanError(span, err)
	}
	return rows, counts, total, nil
}

func (s *Service) Get(ctx context.Context, id uint32) (*ent.TVShow, error) {
	ctx, span := tracer.Start(
		ctx,
		"tvshow.get",
		trace.WithAttributes(attribute.Int("tvshow.id", int(id))),
	)
	defer span.End()
	show, err := s.db.FindTVShowByID(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("tv show %d not found", id)
		}
		return nil, otelx.RecordSpanError(span, err)
	}
	return show, nil
}

// Counts tallies each facet of the series list against the filters applied to
// the *other* facets, so every dropdown says how many shows a value would
// leave — the "all" row included. p carries the filter state the list is
// currently under; a zero value counts the whole library, which is what the
// nav badge asks for.
func (s *Service) Counts(ctx context.Context, p FilterParams) (Counts, error) {
	ctx, span := tracer.Start(ctx, "tvshow.counts",
		trace.WithAttributes(
			attribute.String("filter.status", p.Status),
			attribute.String("filter.type", p.Type),
		))
	defer span.End()

	f, err := s.db.TVShowFacetCounts(ctx, db.FilterTVShowsParams{
		Status:    p.Status,
		Type:      p.Type,
		Query:     strings.TrimSpace(p.Query),
		Monitored: p.Monitored,
		Now:       time.Now(),
	})
	if err != nil {
		return Counts{}, otelx.RecordSpanError(span, err)
	}

	// The two episode tallies stay library-wide: they feed the nav badge and
	// the dashboard, neither of which knows the list's filters.
	wanted, err := s.db.CountWantedEpisodes(ctx)
	if err != nil {
		return Counts{}, otelx.RecordSpanError(span, err)
	}
	downloading, err := s.db.CountDownloadingEpisodes(ctx)
	if err != nil {
		return Counts{}, otelx.RecordSpanError(span, err)
	}

	return Counts{
		Total:               f.Total,
		StatusTotal:         f.StatusTotal,
		Continuing:          f.Continuing,
		Ended:               f.Ended,
		Upcoming:            f.Upcoming,
		Missing:             f.Missing,
		Downloading:         f.Downloading,
		Importing:           f.Importing,
		TypeTotal:           f.TypeTotal,
		Standard:            f.Standard,
		Anime:               f.Anime,
		Daily:               f.Daily,
		MonitoredTotal:      f.MonitoredTotal,
		Monitored:           f.Monitored,
		Unmonitored:         f.Unmonitored,
		WantedEpisodes:      wanted,
		DownloadingEpisodes: downloading,
	}, nil
}

// SeasonView holds the derived availability counts the UI needs per season.
type SeasonView struct {
	Number    uint16
	Available int
	Missing   int
	Unaired   int
	Total     int
}

// DeriveSeasonViews computes per-season counts from an eager-loaded show.
// An episode is in scope when it is monitored or already on disk: a special the
// user opted out of never inflates the missing counts, but one that was
// downloaded before being unmonitored still counts as something the library
// has — so a fully-unmonitored season of downloaded files still reads as
// available. available = episode has a media_file; unaired = air_date missing
// or in the future; missing = aired without a file. Available + Missing +
// Unaired therefore equals Total.
//
// An undated episode counts as unaired, not missing: providers announce a
// future season as dateless placeholders (often literally titled "TBA"), and
// treating "no date" as "aired" made every one of them a permanent gap the
// missing search re-ran against forever.
func DeriveSeasonViews(show *ent.TVShow, now time.Time) []SeasonView {
	views := make([]SeasonView, 0, len(show.Edges.Seasons))
	for _, se := range show.Edges.Seasons {
		v := SeasonView{Number: se.Number}
		for _, e := range se.Edges.Episodes {
			hasFile := len(e.Edges.MediaFiles) > 0
			if (!e.Monitored || !show.Monitored) && !hasFile {
				continue
			}
			v.Total++
			switch {
			case hasFile:
				v.Available++
			case e.AirDate.IsZero() || e.AirDate.After(now):
				v.Unaired++
			default:
				v.Missing++
			}
		}
		views = append(views, v)
	}
	return views
}

func (s *Service) Update(
	ctx context.Context,
	id uint32,
	p UpdateParams,
) (*ent.TVShow, error) {
	ctx, span := tracer.Start(
		ctx,
		"tvshow.update",
		trace.WithAttributes(attribute.Int("tvshow.id", int(id))),
	)
	defer span.End()

	if p.QualityProfile != nil {
		if _, ok := config.ResolveQualityProfile(*p.QualityProfile); !ok {
			return nil, otelx.RecordSpanError(span, ErrNoQualityProfile)
		}
	}
	if p.Preset != "" {
		if err := s.applyPreset(ctx, id, p.Preset); err != nil {
			return nil, otelx.RecordSpanError(span, err)
		}
	}
	var showType *enttvshow.Type
	if p.Type != nil {
		t := enttvshow.Type(*p.Type)
		if err := enttvshow.TypeValidator(t); err != nil {
			return nil, otelx.RecordSpanError(span, ErrInvalidSeriesType)
		}
		showType = &t
	}
	if p.Monitored == nil && p.QualityProfile == nil && showType == nil {
		show, err := s.db.FindTVShowByID(ctx, id)
		if err != nil {
			return nil, otelx.RecordSpanError(span, err)
		}
		showsUpdated.Add(ctx, 1)
		return show, nil
	}
	// A series monitor toggle is a master switch: cascade it to every season and
	// episode so an unmonitored show leaves nothing for the fetcher to grab.
	if p.Monitored != nil {
		episodes, err := s.db.CascadeShowMonitored(ctx, id, *p.Monitored)
		if err != nil {
			return nil, otelx.RecordSpanError(span, err)
		}
		// One row carrying the count, not one per episode: the toggle is a
		// single action, and a long-running series would otherwise push a day
		// of real activity off the feed on its own.
		if err := events.Record(
			ctx, nil, events.TypeMonitoringChanged, events.ScopeSeries, id,
			map[string]any{
				"monitored": *p.Monitored,
				"episodes":  episodes,
			},
		); err != nil {
			slog.WarnContext(ctx, "record monitoring event failed",
				"tvshow.id", id, "error", err)
		}
	}
	show, err := s.db.UpdateTVShow(
		ctx,
		id,
		db.UpdateTVShowParams{
			Monitored:      p.Monitored,
			QualityProfile: p.QualityProfile,
			Type:           showType,
		},
	)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	showsUpdated.Add(ctx, 1)
	return show, nil
}

// applyPreset bulk-sets season/episode monitored flags as a one-shot preset.
// No ongoing policy is stored — monitoring mode is a preset only.
func (s *Service) applyPreset(ctx context.Context, id uint32, preset string) error {
	show, err := s.db.FindTVShowByID(ctx, id)
	if err != nil {
		return err
	}
	now := time.Now()
	pilot := pilotEpisode(show)
	// Collected into four buckets and written as four statements. Per-row
	// updates meant one autocommit per episode — a 250-episode show held the
	// one SQLite connection for ~270 of them, mid-request.
	var (
		epOn, epOff         []uint32
		seasonOn, seasonOff []uint32
	)
	for _, se := range show.Edges.Seasons {
		seasonMon := false
		for _, e := range se.Edges.Episodes {
			if presetWants(preset, e, now, e == pilot) {
				epOn = append(epOn, e.ID)
				seasonMon = true
			} else {
				epOff = append(epOff, e.ID)
			}
		}
		if seasonMon {
			seasonOn = append(seasonOn, se.ID)
		} else {
			seasonOff = append(seasonOff, se.ID)
		}
	}
	if err := s.db.SetEpisodesMonitored(ctx, epOn, true); err != nil {
		return err
	}
	if err := s.db.SetEpisodesMonitored(ctx, epOff, false); err != nil {
		return err
	}
	if err := s.db.SetSeasonsMonitored(ctx, seasonOn, true); err != nil {
		return err
	}
	return s.db.SetSeasonsMonitored(ctx, seasonOff, false)
}

// pilotEpisode returns the show's series premiere: the first episode of the
// lowest-numbered regular season. Season 0 carries specials, which providers
// emit alongside regular seasons and which sort first — so the pilot is never
// simply "the first episode of the first season". Relies on FindTVShowByID
// loading seasons and episodes in ascending number order. nil when the show
// has no regular-season episode.
func pilotEpisode(show *ent.TVShow) *ent.Episode {
	for _, se := range show.Edges.Seasons {
		if se.Number == 0 || len(se.Edges.Episodes) == 0 {
			continue
		}
		return se.Edges.Episodes[0]
	}
	return nil
}

func presetWants(
	preset string,
	e *ent.Episode,
	now time.Time,
	isPilot bool,
) bool {
	aired := !e.AirDate.IsZero() && !e.AirDate.After(now)
	hasFile := len(e.Edges.MediaFiles) > 0
	switch preset {
	case "all":
		return true
	case "none":
		return false
	case "future":
		return !aired
	case "missing":
		return !hasFile
	case "existing":
		return hasFile
	case "pilot":
		return isPilot
	default:
		return e.Monitored
	}
}

func (s *Service) SetSeasonMonitored(ctx context.Context, id uint32, m bool) error {
	// Cascade to the season's episodes so a season toggle isn't undone by the
	// fetcher still seeing monitored episodes underneath it.
	c, err := s.db.CascadeSeasonMonitored(ctx, id, m)
	if err != nil {
		return err
	}
	if c.ShowID == 0 {
		return nil
	}
	// Series-scoped with the season number in `seasons`, which is the shape
	// eventSubject already renders as "Show · Season 3" — a season is not an
	// event scope of its own. One row with the count, as for the show toggle.
	if err := events.Record(
		ctx, nil, events.TypeMonitoringChanged, events.ScopeSeries, c.ShowID,
		map[string]any{
			"monitored": m,
			"episodes":  c.Episodes,
			"seasons":   []uint16{c.Number},
		},
	); err != nil {
		slog.WarnContext(ctx, "record monitoring event failed",
			"tvshow.id", c.ShowID, "season.number", c.Number, "error", err)
	}
	return nil
}

func (s *Service) SetEpisodeMonitored(ctx context.Context, id uint32, m bool) error {
	if err := s.db.SetEpisodeMonitored(ctx, id, m); err != nil {
		return err
	}
	// A single episode is one deliberate toggle, so it gets its own row —
	// unlike the season and show cascades above, which report a count.
	if err := events.Record(
		ctx, nil, events.TypeMonitoringChanged, events.ScopeEpisode, id,
		map[string]any{"monitored": m},
	); err != nil {
		slog.WarnContext(ctx, "record monitoring event failed",
			"episode.id", id, "error", err)
	}
	return nil
}

// ApplySpecialsToExisting pushes the current library.monitor_specials value
// onto season 0 of every series already in the library — the toggle itself
// only governs seasons seeded after it flipped. Turning it off is the way to
// unmonitor specials in bulk.
func (s *Service) ApplySpecialsToExisting(ctx context.Context) (int, error) {
	monitored := config.Get().Library.MonitorSpecials
	ctx, span := tracer.Start(ctx, "tvshow.apply_specials_to_existing",
		trace.WithAttributes(attribute.Bool("monitor_specials", monitored)),
	)
	defer span.End()

	n, err := s.db.CascadeSpecialsMonitored(ctx, monitored)
	if err != nil {
		return 0, otelx.RecordSpanError(span, err)
	}
	span.SetAttributes(attribute.Int("seasons.updated", n))
	slog.InfoContext(ctx, "specials monitoring applied across the library",
		"monitor_specials", monitored,
		"seasons.updated", n,
	)
	return n, nil
}

func (s *Service) Delete(ctx context.Context, id uint32, opts DeleteOptions) error {
	ctx, span := tracer.Start(ctx, "tvshow.delete",
		trace.WithAttributes(
			attribute.Int("tvshow.id", int(id)),
			attribute.Bool("delete_files", opts.DeleteFiles),
		))
	defer span.End()

	if opts.DeleteFiles {
		show, err := s.db.FindTVShowByID(ctx, id)
		if err != nil {
			if ent.IsNotFound(err) {
				return otelx.RecordSpanError(
					span,
					fmt.Errorf("tv show %d not found", id),
				)
			}
			return otelx.RecordSpanError(span, err)
		}
		root := config.Get().Library.SeriesPath
		for _, se := range show.Edges.Seasons {
			for _, e := range se.Edges.Episodes {
				for _, f := range e.Edges.MediaFiles {
					if err := library.RemoveMediaFile(
						ctx,
						f.Path,
						root,
					); err != nil {
						slog.WarnContext(
							ctx,
							"delete tv file failed",
							"tvshow.id",
							id,
							"path",
							f.Path,
							"error",
							err,
						)
					}
				}
			}
		}
	}
	if err := s.db.DeleteTVShow(ctx, id); err != nil {
		if ent.IsNotFound(err) {
			return otelx.RecordSpanError(
				span,
				fmt.Errorf("tv show %d not found", id),
			)
		}
		return otelx.RecordSpanError(span, err)
	}
	if s.posters != nil {
		if err := s.posters.Remove("tvshows", id); err != nil {
			slog.WarnContext(ctx, "poster cache eviction failed",
				"tvshow.id", id, "error", err)
		}
	}
	showsDeleted.Add(ctx, 1)
	slog.InfoContext(
		ctx,
		"tv show deleted",
		"id",
		id,
		"delete_files",
		opts.DeleteFiles,
	)
	return nil
}

// DeleteFileOptions controls DeleteEpisodeFile.
type DeleteFileOptions struct {
	// RemoveTorrent also removes the source torrent from its download client.
	RemoveTorrent bool
}

// DeleteEpisodeFile removes an episode's media file from disk + DB and reverts
// the episode to "wanted" so the next monitored search re-grabs it. When
// opts.RemoveTorrent is set, the source torrent is also removed from its
// download client (best-effort).
func (s *Service) DeleteEpisodeFile(
	ctx context.Context, episodeID uint32, opts DeleteFileOptions,
) error {
	ctx, span := tracer.Start(ctx, "tvshow.delete_episode_file",
		trace.WithAttributes(
			attribute.Int64("episode.id", int64(episodeID)),
			attribute.Bool("remove_torrent", opts.RemoveTorrent),
		))
	defer span.End()

	mf, err := s.db.FindMediaFileByEpisodeID(ctx, episodeID)
	if err != nil {
		if ent.IsNotFound(err) {
			return otelx.RecordSpanError(span,
				fmt.Errorf("episode %d has no media file", episodeID))
		}
		return otelx.RecordSpanError(span, fmt.Errorf("find media_file: %w", err))
	}
	if err := library.RemoveMediaFile(ctx,
		mf.Path, config.Get().Library.SeriesPath,
	); err != nil {
		slog.WarnContext(ctx, "delete episode file from disk failed",
			"path", mf.Path, "error", err)
	}
	if err := s.db.DeleteMediaFileAndRevertEpisode(
		ctx, mf.ID, episodeID,
	); err != nil {
		return otelx.RecordSpanError(span, fmt.Errorf("delete + revert: %w", err))
	}
	if opts.RemoveTorrent {
		s.removeEpisodeSourceTorrent(ctx, episodeID)
	}
	slog.InfoContext(ctx, "episode media file deleted",
		"episode.id", episodeID, "media_file.id", mf.ID)
	return nil
}

// removeEpisodeSourceTorrent best-effort removes the torrent that produced the
// episode's most recent grab. Absence or any failure is logged, never surfaced.
func (s *Service) removeEpisodeSourceTorrent(ctx context.Context, episodeID uint32) {
	rec, err := s.db.LatestImportedRecordForEpisode(ctx, episodeID)
	switch {
	case ent.IsNotFound(err):
		return
	case err != nil:
		slog.WarnContext(ctx, "lookup source torrent failed",
			"episode.id", episodeID, "error", err)
		return
	}
	if rec.TorrentHash == "" || rec.DownloadClientName == "" {
		return
	}
	if err := s.download.RemoveTorrent(
		ctx, rec.DownloadClientName, rec.TorrentHash, false,
	); err != nil {
		slog.WarnContext(ctx, "remove source torrent failed",
			"hash", rec.TorrentHash, "error", err)
	}
}

// GrabSeasonRelease dispatches a chosen season-pack release against the season's
// first episode and flips every wanted, aired episode in the season to
// "downloading" so the whole season reflects the grab immediately. Season-pack
// reconciliation maps the pack's files back to episodes on import.
func (s *Service) GrabSeasonRelease(
	ctx context.Context,
	seriesID uint32,
	seasonNumber uint16,
	result indexer.SearchResult,
	replaceExisting bool,
) error {
	ctx, span := tracer.Start(ctx, "tvshow.grab_season_release",
		trace.WithAttributes(
			attribute.Int64("tvshow.id", int64(seriesID)),
			attribute.Int("season.number", int(seasonNumber)),
			attribute.String("release.title", result.Title),
		))
	defer span.End()

	show, err := s.db.FindTVShowByID(ctx, seriesID)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	var eps []*ent.Episode
	for _, se := range show.Edges.Seasons {
		if se.Number == seasonNumber {
			eps = se.Edges.Episodes
			break
		}
	}
	if len(eps) == 0 {
		return otelx.RecordSpanError(span,
			fmt.Errorf("season %d has no episodes", seasonNumber))
	}
	return s.grabPackAndMark(ctx, span, result, eps, replaceExisting)
}

// GrabSeriesRelease dispatches a chosen whole-series (integral / multi-season)
// release against the first episode of the series and flips every wanted, aired
// episode across all seasons to "downloading".
func (s *Service) GrabSeriesRelease(
	ctx context.Context,
	seriesID uint32,
	result indexer.SearchResult,
	replaceExisting bool,
) error {
	ctx, span := tracer.Start(ctx, "tvshow.grab_series_release",
		trace.WithAttributes(
			attribute.Int64("tvshow.id", int64(seriesID)),
			attribute.String("release.title", result.Title),
		))
	defer span.End()

	show, err := s.db.FindTVShowByID(ctx, seriesID)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	var eps []*ent.Episode
	for _, se := range show.Edges.Seasons {
		eps = append(eps, se.Edges.Episodes...)
	}
	if len(eps) == 0 {
		return otelx.RecordSpanError(span,
			fmt.Errorf("series %d has no episodes", seriesID))
	}
	return s.grabPackAndMark(ctx, span, result, eps, replaceExisting)
}

// wantedAiredEpisodes narrows eps to the ones grabPackAndMark's status-marking
// loop would actually flip: still wanted and already aired. Shared between the
// GrabEpisode wanted-set and the marking loop so the two never disagree on
// what this grab is "for".
func wantedAiredEpisodes(eps []*ent.Episode, now time.Time) []*ent.Episode {
	var wanted []*ent.Episode
	for _, e := range eps {
		if e.Status != episode.StatusWanted {
			continue
		}
		if !e.AirDate.IsZero() && e.AirDate.After(now) {
			continue
		}
		wanted = append(wanted, e)
	}
	return wanted
}

// grabPackAndMark grabs one multi-episode release linked to the first episode
// and flips every wanted, aired episode in the set to "downloading". Anchoring
// the download record to a single episode matches the automatic season-pack
// path; import reconciliation maps the pack's files to the rest. Future-unaired
// episodes stay wanted since the pack can't contain them.
func (s *Service) grabPackAndMark(
	ctx context.Context,
	span trace.Span,
	result indexer.SearchResult,
	eps []*ent.Episode,
	replaceExisting bool,
) error {
	now := time.Now()
	// airedWanted is what the marking loop below may ever flip to
	// downloading, in both modes: the pack can't contain an unaired episode
	// regardless of what the operator asked to replace. The GrabEpisode
	// wanted-set is wider under replaceExisting — an explicit overwrite of
	// the whole set (manual replace grab) — since that mode is about the
	// selective-download keep-set, not about which episodes get marked.
	airedWanted := wantedAiredEpisodes(eps, now)
	wanted := airedWanted
	if replaceExisting {
		wanted = eps
	}
	wantedIDs := make([]uint32, len(wanted))
	for i, e := range wanted {
		wantedIDs[i] = e.ID
	}
	rec, err := s.download.GrabEpisode(ctx, result, eps[0].ID, wantedIDs)
	if err != nil {
		return otelx.RecordSpanError(span, fmt.Errorf("grab pack: %w", err))
	}
	if replaceExisting {
		if err := s.db.SetDownloadRecordReplaceMode(
			ctx, rec.ID, downloadrecord.ReplaceModeAll,
		); err != nil {
			slog.WarnContext(ctx, "grab pack: set replace mode failed",
				"download_record.id", rec.ID, "error", err)
		}
	}
	marked := 0
	for _, e := range airedWanted {
		if e.Status != episode.StatusWanted {
			continue
		}
		if err := s.db.SetEpisodeStatus(
			ctx, e.ID, episode.StatusDownloading,
		); err != nil {
			slog.WarnContext(ctx, "grab pack: set episode status failed",
				"episode.id", e.ID, "error", err)
			continue
		}
		marked++
	}
	span.SetAttributes(attribute.Int("episodes.marked", marked))
	slog.InfoContext(ctx, "grabbed pack release",
		"release", result.Title, "episodes.marked", marked)
	return nil
}

// seriesCast fetches top-billed actors, degrading to nil when TVDB's extended
// record is unavailable. Cast is a display-only section, so a flaky call must
// not fail the add/refresh around it — and nil leaves any stored cast intact
// rather than wiping it.
func (s *Service) seriesCast(
	ctx context.Context,
	tvdbID uint32,
) []metadata.CastMember {
	cast, err := s.metadata.GetSeriesCast(ctx, tvdbID)
	if err != nil {
		slog.WarnContext(ctx, "tvdb cast fetch failed",
			"tvshow.tvdb_id", tvdbID, "error", err)
		return nil
	}
	return cast
}

func (s *Service) RefreshOne(ctx context.Context, id uint32) (*ent.TVShow, error) {
	ctx, span := tracer.Start(
		ctx,
		"tvshow.refresh_one",
		trace.WithAttributes(attribute.Int("tvshow.id", int(id))),
	)
	defer span.End()
	show, err := s.db.FindTVShowByID(ctx, id)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	d, err := s.metadata.GetSeries(ctx, show.TvdbID)
	if err != nil {
		return nil, otelx.RecordSpanError(span, fmt.Errorf("tvdb refresh: %w", err))
	}
	cast := s.seriesCast(ctx, show.TvdbID)
	// Persist refreshed provider fields so changes (status, rating, network,
	// etc.) surface. Season/episode reconciliation is tracked separately.
	if err := s.db.UpdateTVShowMetadata(ctx, id, db.UpdateTVShowMetadataParams{
		Title:         d.Title,
		OriginalTitle: d.OriginalTitle,
		Year:          d.Year,
		Overview:      d.Overview,
		Network:       d.Network,
		Creator:       d.Creator,
		SeriesStatus:  d.Status,
		Runtime:       d.Runtime,
		Rating:        float64(d.Rating),
		Genres:        d.Genres,
		Aliases:       d.Aliases,
		Cast:          cast,
		FirstAired:    metadata.ParseISODate(d.FirstAired),
	}); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	s.enrichPeople(ctx, id)
	// Re-sync the season/episode tree so refreshed titles (e.g. a language
	// change) surface, an ongoing series picks up newly-aired episodes, and
	// provider-removed episodes/seasons are pruned. Their files are removed
	// from disk here (the DB layer only returns the paths).
	removed, err := s.db.ReconcileEpisodes(ctx, id, seedSeasons(d))
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	seriesRoot := config.Get().Library.SeriesPath
	for _, path := range removed {
		if err := library.RemoveMediaFile(ctx, path, seriesRoot); err != nil {
			slog.WarnContext(ctx, "remove pruned episode file failed",
				"tvshow.id", id, "path", path, "error", err)
		}
	}
	if err := s.db.SetTVShowRefreshedAt(ctx, id, time.Now()); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	// Add and Reidentify fetched the poster, refresh did not — so a cleared
	// cache stayed cleared for every show already in the library. Fetch
	// no-ops when the file is present, so this is free on a warm cache.
	s.fetchPoster(ctx, id, d.PosterPath)
	// Only when the provider actually moved something. RefreshStale runs this
	// over every stale show on a tick, and an unconditional event would bury a
	// day of real activity under one row per series in the library.
	if len(removed) > 0 || show.Title != d.Title {
		payload := map[string]any{"title": d.Title}
		if show.Title != d.Title {
			payload["old_title"] = show.Title
		}
		if len(removed) > 0 {
			payload["episodes_removed"] = len(removed)
		}
		if err := events.Record(
			ctx, nil, events.TypeMetadataRefreshed, events.ScopeSeries, id,
			payload,
		); err != nil {
			slog.WarnContext(ctx, "record metadata refresh event failed",
				"tvshow.id", id, "error", err)
		}
	}
	return s.db.FindTVShowByID(ctx, id)
}

// Reidentify points the series at a different TVDB show: metadata and the
// whole season/episode tree are replaced, and each existing file is re-attached
// to the episode carrying the same season and episode number. Files whose
// numbering has no counterpart in the new show are left on disk and returned
// so the caller can report them.
//
// The files are detached *before* the tree is replaced, and that ordering is
// the whole point. ReconcileEpisodes deletes seasons and episodes the provider
// no longer reports and hands their paths back for deletion from disk — with a
// swapped tvdb id that is every episode of the old show, so refreshing in place
// would wipe the library it was asked to repair.
func (s *Service) Reidentify(
	ctx context.Context,
	id, tvdbID uint32,
) (*ent.TVShow, []string, error) {
	ctx, span := tracer.Start(ctx, "tvshow.reidentify",
		trace.WithAttributes(
			attribute.Int64("tvshow.id", int64(id)),
			attribute.Int64("tvshow.tvdb_id", int64(tvdbID)),
		),
	)
	defer span.End()

	if tvdbID == 0 {
		return nil, nil, otelx.RecordSpanError(span, ErrInvalidTVDBID)
	}
	show, err := s.db.FindTVShowByID(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil, otelx.RecordSpanError(span,
				fmt.Errorf("series %d: %w", id, ErrSeriesNotFound))
		}
		return nil, nil, otelx.RecordSpanError(span, err)
	}
	if show.TvdbID == tvdbID {
		return nil, nil, otelx.RecordSpanError(span, ErrSameTVDBID)
	}
	// Unlike its movie twin, FindTVShowByTVDBID reports "not in the library" as
	// a nil row with a nil error — testing err alone would refuse every target.
	existing, err := s.db.FindTVShowByTVDBID(ctx, tvdbID)
	if err != nil {
		return nil, nil, otelx.RecordSpanError(span,
			fmt.Errorf("lookup target: %w", err))
	}
	if existing != nil {
		return nil, nil, otelx.RecordSpanError(span, ErrSeriesExists)
	}

	// Fetched before anything is mutated: a bad id or an unreachable TVDB
	// leaves the series exactly as it was.
	d, err := s.metadata.GetSeries(ctx, tvdbID)
	if err != nil {
		return nil, nil, otelx.RecordSpanError(span,
			fmt.Errorf("tvdb get series: %w", err))
	}

	detached, err := s.db.DetachEpisodeMediaFiles(ctx, id)
	if err != nil {
		return nil, nil, otelx.RecordSpanError(span, err)
	}
	span.SetAttributes(attribute.Int("files.detached", len(detached)))

	if err := s.db.SetTVShowTVDBID(ctx, id, tvdbID); err != nil {
		return nil, nil, otelx.RecordSpanError(span, err)
	}
	cast := s.seriesCast(ctx, tvdbID)
	if err := s.db.UpdateTVShowMetadata(ctx, id, db.UpdateTVShowMetadataParams{
		Title:         d.Title,
		OriginalTitle: d.OriginalTitle,
		Year:          d.Year,
		Overview:      d.Overview,
		Network:       d.Network,
		Creator:       d.Creator,
		SeriesStatus:  d.Status,
		Runtime:       d.Runtime,
		Rating:        float64(d.Rating),
		Genres:        d.Genres,
		Aliases:       d.Aliases,
		Cast:          cast,
		FirstAired:    metadata.ParseISODate(d.FirstAired),
	}); err != nil {
		return nil, nil, otelx.RecordSpanError(span, err)
	}
	s.enrichPeople(ctx, id)
	// A metadata refresh deliberately preserves `type` because an operator may
	// have corrected it. Re-identify is the exception: this is a different show,
	// so the correction was about the old one and re-inferring is right.
	newType := enttvshow.Type(d.Type)
	if _, err := s.db.UpdateTVShow(ctx, id, db.UpdateTVShowParams{
		Type: &newType,
	}); err != nil {
		return nil, nil, otelx.RecordSpanError(span, err)
	}
	if _, err := s.db.ReconcileEpisodes(ctx, id, seedSeasons(d)); err != nil {
		return nil, nil, otelx.RecordSpanError(span, err)
	}

	unmatched, err := s.reattachFiles(ctx, id, detached)
	if err != nil {
		return nil, nil, otelx.RecordSpanError(span, err)
	}
	span.SetAttributes(attribute.Int("files.unmatched", len(unmatched)))

	if err := s.db.SetTVShowRefreshedAt(ctx, id, time.Now()); err != nil {
		return nil, nil, otelx.RecordSpanError(span, err)
	}
	s.fetchPoster(ctx, id, d.PosterPath)

	refreshed, err := s.db.FindTVShowByID(ctx, id)
	if err != nil {
		return nil, nil, otelx.RecordSpanError(span, err)
	}
	slog.InfoContext(
		ctx,
		"series re-identified",
		"tvshow.id",
		id,
		"title",
		refreshed.Title,
		"tvdb_id",
		tvdbID,
		"files_kept",
		len(detached)-len(unmatched),
		"files_unmatched",
		len(unmatched),
	)
	if err := events.Record(
		ctx, nil, events.TypeReidentified, events.ScopeSeries, id,
		map[string]any{
			"old_tvdb_id":     show.TvdbID,
			"new_tvdb_id":     tvdbID,
			"title":           refreshed.Title,
			"files_kept":      len(detached) - len(unmatched),
			"files_unmatched": len(unmatched),
		},
	); err != nil {
		slog.WarnContext(ctx, "record re-identify event failed",
			"tvshow.id", id, "error", err)
	}
	return refreshed, unmatched, nil
}

// reattachFiles re-points each detached file at the new show's episode with the
// same season and episode number, flipping that episode to available. A file
// with no counterpart keeps its place on disk and loses only its row — a
// library scan can re-adopt it, which deleting the media could not undo.
func (s *Service) reattachFiles(
	ctx context.Context,
	showID uint32,
	detached []*ent.MediaFile,
) ([]string, error) {
	if len(detached) == 0 {
		return nil, nil
	}
	show, err := s.db.FindTVShowByID(ctx, showID)
	if err != nil {
		return nil, err
	}
	type slot struct{ season, episode uint16 }
	byNumber := make(map[slot]uint32)
	for _, se := range show.Edges.Seasons {
		for _, ep := range se.Edges.Episodes {
			byNumber[slot{se.Number, ep.Number}] = ep.ID
		}
	}

	var unmatched []string
	for _, mf := range detached {
		oldEp := mf.Edges.Episode
		if oldEp == nil || oldEp.Edges.Season == nil {
			unmatched = append(unmatched, mf.Path)
			continue
		}
		epID, ok := byNumber[slot{oldEp.Edges.Season.Number, oldEp.Number}]
		if !ok {
			unmatched = append(unmatched, mf.Path)
			if err := s.db.DeleteMediaFile(ctx, mf.ID); err != nil {
				return nil, fmt.Errorf(
					"drop unmatched media file %d: %w",
					mf.ID,
					err,
				)
			}
			continue
		}
		if err := s.db.AttachMediaFileToEpisode(ctx, mf.ID, epID); err != nil {
			return nil, fmt.Errorf("attach media file %d: %w", mf.ID, err)
		}
		if err := s.db.SetEpisodeStatus(
			ctx, epID, episode.StatusAvailable,
		); err != nil {
			return nil, fmt.Errorf("mark episode %d available: %w", epID, err)
		}
	}
	return unmatched, nil
}

// RefreshStale re-pulls TVDB metadata for shows not refreshed within
// metadataMinRefreshInterval. Per-row failures are logged and skipped; the
// tick returns nil unless the initial DB query fails.
func (s *Service) RefreshStale(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "tvshow.refresh_stale")
	defer span.End()
	cutoff := time.Now().Add(-metadataMinRefreshInterval)
	rows, err := s.db.ListTVShowsStaleSince(ctx, cutoff)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	span.SetAttributes(attribute.Int("refresh.candidate_count", len(rows)))

	refreshed, skipped := 0, 0
	for i, sh := range rows {
		scheduler.Progress(ctx, i, len(rows))
		if _, err := s.RefreshOne(ctx, sh.ID); err != nil {
			slog.WarnContext(ctx, "tv refresh failed",
				"tvshow.id", sh.ID, "error", err)
			skipped++
			continue
		}
		refreshed++
	}

	span.SetAttributes(
		attribute.Int("refresh.refreshed_count", refreshed),
		attribute.Int("refresh.skipped_count", skipped),
	)
	slog.InfoContext(ctx, "tv metadata refresh complete",
		"refreshed", refreshed, "skipped", skipped)
	return nil
}

// Manager is the tvshow service surface consumed by REST handlers and jobs.
type Manager interface {
	Add(
		ctx context.Context,
		tvdbID uint32,
		qualityProfile string,
	) (*ent.TVShow, error)
	List(ctx context.Context, page, limit uint16) ([]*ent.TVShow, uint32, error)
	FilterList(
		ctx context.Context,
		p FilterParams,
	) ([]*ent.TVShow, map[uint32]db.EpisodeCounts, uint32, error)
	Get(ctx context.Context, id uint32) (*ent.TVShow, error)
	Update(ctx context.Context, id uint32, p UpdateParams) (*ent.TVShow, error)
	Delete(ctx context.Context, id uint32, opts DeleteOptions) error
	DeleteEpisodeFile(
		ctx context.Context,
		episodeID uint32,
		opts DeleteFileOptions,
	) error
	GrabSeasonRelease(
		ctx context.Context,
		seriesID uint32,
		seasonNumber uint16,
		result indexer.SearchResult,
		replaceExisting bool,
	) error
	GrabSeriesRelease(
		ctx context.Context,
		seriesID uint32,
		result indexer.SearchResult,
		replaceExisting bool,
	) error
	Counts(ctx context.Context, p FilterParams) (Counts, error)
	RefreshOne(ctx context.Context, id uint32) (*ent.TVShow, error)
	Reidentify(
		ctx context.Context,
		id, tvdbID uint32,
	) (*ent.TVShow, []string, error)
	SetSeasonMonitored(ctx context.Context, id uint32, monitored bool) error
	SetEpisodeMonitored(ctx context.Context, id uint32, monitored bool) error
	ApplySpecialsToExisting(ctx context.Context) (int, error)
}

type DeleteOptions struct{ DeleteFiles bool }

type UpdateParams struct {
	Monitored      *bool
	QualityProfile *string
	// Type overrides the provider-inferred series type. It decides whether
	// episodes match by absolute number, so a wrong inference silently
	// mis-matches every file until an operator corrects it here.
	Type *string
	// Preset, when set, bulk-applies a monitoring preset to season/episode toggles.
	Preset string // "" | "all" | "future" | "missing" | "existing" | "pilot" | "none"
}

type FilterParams struct {
	Status string // series_status, "missing", "downloading" or "importing"
	Type   string // standard|anime|daily|""
	Query  string
	Sort   string // recent|title|year|rating|episodes
	Order  string
	Page   uint16
	Limit  uint16
	// Monitored filters on the show's own flag; nil means "either".
	Monitored *bool
}

// Counts is the series toolbar's whole model. Every facet is tallied with the
// *other* facets' filters applied and its own left out, so each dropdown says
// what picking a value would leave. Counting a facet against its own selection
// would zero every row but the chosen one, stranding the filter.
//
// Total alone is unconditional: it is the library, which the page header and
// the "no series yet" empty state ask for.
type Counts struct {
	Total int

	// StatusTotal is the status facet's "all" row: everything the type,
	// monitoring and search filters leave.
	StatusTotal int
	Continuing  int
	Ended       int
	Upcoming    int
	// Missing is how many shows have at least one aired, monitored episode
	// with no file — the population behind the list's "missing" tab, which is
	// a per-show fact and not derivable from WantedEpisodes.
	Missing int
	// Downloading and Importing are per-show counts like Missing — shows
	// holding at least one episode in that state, not episode counts.
	Downloading int
	Importing   int

	TypeTotal int
	Standard  int
	Anime     int
	Daily     int

	MonitoredTotal int
	Monitored      int
	Unmonitored    int

	// Both episode tallies are library-wide and ignore every filter: the nav
	// badge and the dashboard read them, and neither knows the list's state.
	WantedEpisodes      int
	DownloadingEpisodes int
}
