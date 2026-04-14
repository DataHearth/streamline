package tvshow

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/episode"
	enttvshow "github.com/datahearth/streamline/ent/tvshow"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/posters"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

var (
	tracer = otel.Tracer("github.com/datahearth/streamline/internal/media/tvshow")
	meter  = otel.Meter("github.com/datahearth/streamline/internal/media/tvshow")
)

var showsAdded metric.Int64Counter

func init() {
	showsAdded = otelx.Must(meter.Int64Counter(
		"streamline.tvshows.added",
		metric.WithDescription("TV shows added"),
	))

	ctx := context.Background()
	showsAdded.Add(ctx, 0)
}

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
			AirDate:        e.AirDate,
		})
	}
	seasons := make([]db.SeasonSeed, 0, len(d.Seasons))
	for _, si := range d.Seasons {
		seasons = append(seasons, db.SeasonSeed{
			Number:   si.Number,
			Name:     si.Name,
			Episodes: bySeason[si.Number],
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
		trace.WithAttributes(attribute.Int("tvdb.id", int(tvdbID))),
	)
	defer span.End()

	d, err := s.metadata.GetSeries(ctx, tvdbID)
	if err != nil {
		return nil, fmt.Errorf("tvdb get series: %w", err)
	}

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
		PosterPath:     d.PosterPath,
		QualityProfile: qualityProfile,
		Seasons:        seedSeasons(d),
	})
	if err != nil {
		return nil, fmt.Errorf("create tv show: %w", err)
	}

	if d.PosterPath != "" && s.posters != nil {
		bg := context.WithoutCancel(ctx)
		id := show.ID
		src := metadata.TVDBArtworkURL(d.PosterPath)
		go func() {
			if err := s.posters.Fetch(bg, "tvshows", id, src); err != nil {
				slog.WarnContext(
					bg,
					"tv poster fetch failed",
					"tvshow.id",
					id,
					"error",
					err,
				)
			}
		}()
	}

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
	return rows, uint32(total), nil
}

// FilterList applies status/type/query/sort to the full library in memory.
// The dataset is small enough that a single fetch + in-Go filter is adequate;
// a later optimization can push the predicates into SQL.
func (s *Service) FilterList(
	ctx context.Context,
	p FilterParams,
) ([]*ent.TVShow, uint32, error) {
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

	rows, err := s.db.ListTVShows(ctx, 0, 10000)
	if err != nil {
		return nil, 0, otelx.RecordSpanError(span, err)
	}

	now := time.Now()
	query := strings.ToLower(strings.TrimSpace(p.Query))
	filtered := make([]*ent.TVShow, 0, len(rows))
	for _, sh := range rows {
		if p.Type != "" && string(sh.Type) != p.Type {
			continue
		}
		switch p.Status {
		case "", "all":
		case "missing":
			if !hasMissingEpisode(sh, now) {
				continue
			}
		default:
			if string(sh.SeriesStatus) != p.Status {
				continue
			}
		}
		if query != "" && !strings.Contains(strings.ToLower(sh.Title), query) {
			continue
		}
		filtered = append(filtered, sh)
	}

	sortShows(filtered, p.Sort, p.Order)

	total := uint32(len(filtered))
	start := int(page-1) * int(limit)
	if start >= len(filtered) {
		return []*ent.TVShow{}, total, nil
	}
	end := min(start+int(limit), len(filtered))
	return filtered[start:end], total, nil
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

func (s *Service) Counts(ctx context.Context) (Counts, error) {
	ctx, span := tracer.Start(ctx, "tvshow.counts")
	defer span.End()
	total, err := s.db.CountTVShows(ctx)
	if err != nil {
		return Counts{}, otelx.RecordSpanError(span, err)
	}
	continuing, err := s.db.CountTVShowsByStatus(
		ctx,
		enttvshow.SeriesStatusContinuing,
	)
	if err != nil {
		return Counts{}, otelx.RecordSpanError(span, err)
	}
	ended, err := s.db.CountTVShowsByStatus(ctx, enttvshow.SeriesStatusEnded)
	if err != nil {
		return Counts{}, otelx.RecordSpanError(span, err)
	}
	wantedShows, err := s.db.ListWantedEpisodes(ctx)
	if err != nil {
		return Counts{}, otelx.RecordSpanError(span, err)
	}
	wanted := 0
	for _, sh := range wantedShows {
		for _, se := range sh.Edges.Seasons {
			wanted += len(se.Edges.Episodes)
		}
	}
	return Counts{
		Total:          total,
		Continuing:     continuing,
		Ended:          ended,
		WantedEpisodes: wanted,
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
// available = episode has a media_file; unaired = air_date in the future;
// missing = everything else (aired/undated without a file).
func DeriveSeasonViews(show *ent.TVShow, now time.Time) []SeasonView {
	views := make([]SeasonView, 0, len(show.Edges.Seasons))
	for _, se := range show.Edges.Seasons {
		v := SeasonView{Number: se.Number, Total: len(se.Edges.Episodes)}
		for _, e := range se.Edges.Episodes {
			switch {
			case len(e.Edges.MediaFiles) > 0:
				v.Available++
			case !e.AirDate.IsZero() && e.AirDate.After(now):
				v.Unaired++
			default:
				v.Missing++
			}
		}
		views = append(views, v)
	}
	return views
}

// hasMissingEpisode reports whether the show has any aired/undated monitored
// episode without a media file (drives the "missing" status filter).
func hasMissingEpisode(show *ent.TVShow, now time.Time) bool {
	for _, v := range DeriveSeasonViews(show, now) {
		if v.Missing > 0 {
			return true
		}
	}
	return false
}

func sortShows(shows []*ent.TVShow, sortKey, order string) {
	less := func(i, j int) bool { return shows[i].CreateTime.After(shows[j].CreateTime) }
	switch sortKey {
	case "title":
		less = func(i, j int) bool {
			return strings.ToLower(shows[i].Title) < strings.ToLower(shows[j].Title)
		}
	case "year":
		less = func(i, j int) bool { return shows[i].Year < shows[j].Year }
	case "rating":
		less = func(i, j int) bool { return shows[i].Rating < shows[j].Rating }
	case "episodes":
		less = func(i, j int) bool { return episodeCount(shows[i]) < episodeCount(shows[j]) }
	}
	sort.SliceStable(shows, less)
	if order == "desc" {
		reverse(shows)
	}
}

func episodeCount(show *ent.TVShow) int {
	n := 0
	for _, se := range show.Edges.Seasons {
		n += len(se.Edges.Episodes)
	}
	return n
}

func reverse(shows []*ent.TVShow) {
	for i, j := 0, len(shows)-1; i < j; i, j = i+1, j-1 {
		shows[i], shows[j] = shows[j], shows[i]
	}
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

	if p.Preset != "" {
		if err := s.applyPreset(ctx, id, p.Preset); err != nil {
			return nil, otelx.RecordSpanError(span, err)
		}
	}
	if p.Monitored == nil && p.QualityProfile == nil {
		return s.db.FindTVShowByID(ctx, id)
	}
	// A series monitor toggle is a master switch: cascade it to every season and
	// episode so an unmonitored show leaves nothing for the fetcher to grab.
	if p.Monitored != nil {
		if err := s.db.CascadeShowMonitored(ctx, id, *p.Monitored); err != nil {
			return nil, otelx.RecordSpanError(span, err)
		}
	}
	return s.db.UpdateTVShow(
		ctx,
		id,
		db.UpdateTVShowParams{
			Monitored:      p.Monitored,
			QualityProfile: p.QualityProfile,
		},
	)
}

// applyPreset bulk-sets season/episode monitored flags as a one-shot preset.
// No ongoing policy is stored — monitoring mode is a preset only.
func (s *Service) applyPreset(ctx context.Context, id uint32, preset string) error {
	show, err := s.db.FindTVShowByID(ctx, id)
	if err != nil {
		return err
	}
	now := time.Now()
	first := true
	for _, se := range show.Edges.Seasons {
		seasonMon := false
		for _, e := range se.Edges.Episodes {
			want := presetWants(preset, e, now, first)
			first = false
			if err := s.db.SetEpisodeMonitored(ctx, e.ID, want); err != nil {
				return err
			}
			seasonMon = seasonMon || want
		}
		if err := s.db.SetSeasonMonitored(ctx, se.ID, seasonMon); err != nil {
			return err
		}
	}
	return nil
}

func presetWants(
	preset string,
	e *ent.Episode,
	now time.Time,
	isFirstEpisodeOfShow bool,
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
		return isFirstEpisodeOfShow
	default:
		return e.Monitored
	}
}

func (s *Service) SetSeasonMonitored(ctx context.Context, id uint32, m bool) error {
	// Cascade to the season's episodes so a season toggle isn't undone by the
	// fetcher still seeing monitored episodes underneath it.
	return s.db.CascadeSeasonMonitored(ctx, id, m)
}

func (s *Service) SetEpisodeMonitored(ctx context.Context, id uint32, m bool) error {
	return s.db.SetEpisodeMonitored(ctx, id, m)
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
		for _, se := range show.Edges.Seasons {
			for _, e := range se.Edges.Episodes {
				for _, f := range e.Edges.MediaFiles {
					if err := os.Remove(f.Path); err != nil && !os.IsNotExist(err) {
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
	if err := os.Remove(mf.Path); err != nil && !os.IsNotExist(err) {
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
		ctx, rec.DownloadClientName, rec.TorrentHash,
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
	rec, err := s.download.GrabEpisode(ctx, result, eps[0].ID)
	if err != nil {
		return otelx.RecordSpanError(span, fmt.Errorf("grab pack: %w", err))
	}
	if replaceExisting {
		if err := s.db.MarkDownloadRecordReplaceExisting(ctx, rec.ID); err != nil {
			slog.WarnContext(ctx, "grab pack: mark replace-existing failed",
				"download_record.id", rec.ID, "error", err)
		}
	}
	now := time.Now()
	marked := 0
	for _, e := range eps {
		if e.Status != episode.StatusWanted {
			continue
		}
		if !e.AirDate.IsZero() && e.AirDate.After(now) {
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
		Type:          string(d.Type),
		Runtime:       d.Runtime,
		Rating:        float64(d.Rating),
		Genres:        d.Genres,
	}); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	// Re-sync the season/episode tree so refreshed titles (e.g. a language
	// change) surface, an ongoing series picks up newly-aired episodes, and
	// provider-removed episodes/seasons are pruned. Their files are removed
	// from disk here (the DB layer only returns the paths).
	removed, err := s.db.ReconcileEpisodes(ctx, id, seedSeasons(d))
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	for _, path := range removed {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			slog.WarnContext(ctx, "remove pruned episode file failed",
				"tvshow.id", id, "path", path, "error", err)
		}
	}
	if err := s.db.SetTVShowRefreshedAt(ctx, id, time.Now()); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	return s.db.FindTVShowByID(ctx, id)
}

// RefreshStale re-pulls metadata for every show. A Phase 2 scheduler job wires
// this to the metadata_refresh tick.
func (s *Service) RefreshStale(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "tvshow.refresh_stale")
	defer span.End()
	rows, err := s.db.ListTVShows(ctx, 0, 10000)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	for _, sh := range rows {
		if _, err := s.RefreshOne(ctx, sh.ID); err != nil {
			slog.WarnContext(
				ctx,
				"tv refresh failed",
				"tvshow.id",
				sh.ID,
				"error",
				err,
			)
		}
	}
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
	FilterList(ctx context.Context, p FilterParams) ([]*ent.TVShow, uint32, error)
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
	Counts(ctx context.Context) (Counts, error)
	RefreshOne(ctx context.Context, id uint32) (*ent.TVShow, error)
	SetSeasonMonitored(ctx context.Context, id uint32, monitored bool) error
	SetEpisodeMonitored(ctx context.Context, id uint32, monitored bool) error
}

type DeleteOptions struct{ DeleteFiles bool }

type UpdateParams struct {
	Monitored      *bool
	QualityProfile *string
	// Preset, when set, bulk-applies a monitoring preset to season/episode toggles.
	Preset string // "" | "all" | "future" | "missing" | "existing" | "pilot" | "none"
}

type FilterParams struct {
	Status string // series_status filter, or "missing" for shows with wanted eps
	Type   string // standard|anime|daily|""
	Query  string
	Sort   string // recent|title|year|rating|episodes
	Order  string
	Page   uint16
	Limit  uint16
}

type Counts struct {
	Total          int
	Continuing     int
	Ended          int
	WantedEpisodes int
}
