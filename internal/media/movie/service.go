package movie

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/datahearth/streamline/ent"
	entmovie "github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/posters"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

var (
	tracer = otel.Tracer("github.com/datahearth/streamline/internal/media/movie")
	meter  = otel.Meter("github.com/datahearth/streamline/internal/media/movie")
)

var (
	moviesAdded   metric.Int64Counter
	moviesUpdated metric.Int64Counter
	moviesDeleted metric.Int64Counter
)

func init() {
	moviesAdded = otelx.Must(meter.Int64Counter(
		"streamline.movies.added",
		metric.WithDescription("Movies added"),
	))
	moviesUpdated = otelx.Must(meter.Int64Counter(
		"streamline.movies.updated",
		metric.WithDescription("Movies updated"),
	))
	moviesDeleted = otelx.Must(meter.Int64Counter(
		"streamline.movies.deleted",
		metric.WithDescription("Movies deleted"),
	))

	ctx := context.Background()
	moviesAdded.Add(ctx, 0)
	moviesUpdated.Add(ctx, 0)
	moviesDeleted.Add(ctx, 0)
}

var ErrNoQualityProfile = errors.New("no quality profile configured")

type Manager interface {
	Add(
		ctx context.Context,
		tmdbID uint32,
		qualityProfile string,
	) (*ent.Movie, string, error)
	List(ctx context.Context, page, limit uint16) ([]*ent.Movie, uint32, error)
	FilterList(ctx context.Context, p FilterParams) ([]*ent.Movie, uint32, error)
	Get(ctx context.Context, id uint32) (*ent.Movie, error)
	GetByTMDBID(ctx context.Context, tmdbID uint32) (*ent.Movie, error)
	Update(ctx context.Context, id uint32, p UpdateParams) (*ent.Movie, error)
	Delete(ctx context.Context, id uint32, opts DeleteOptions) error
	DeleteFile(
		ctx context.Context,
		movieID, fileID uint32,
		opts DeleteFileOptions,
	) error
	RefreshOne(ctx context.Context, id uint32) (*ent.Movie, error)
	Counts(ctx context.Context) (Counts, error)
	AnnotateTMDBResults(
		ctx context.Context,
		results []metadata.MovieResult,
	) ([]AnnotatedTMDBResult, error)
}

// DeleteOptions controls Delete behaviour.
type DeleteOptions struct {
	// DeleteFiles removes attached media_files from disk before the row delete.
	DeleteFiles bool
}

// AnnotatedTMDBResult pairs a TMDB search hit with library state. AlreadyAdded
// is true when a movie row with this tmdb_id exists.
type AnnotatedTMDBResult struct {
	metadata.MovieResult
	AlreadyAdded bool
}

type UpdateParams struct {
	Status         *entmovie.Status
	QualityProfile *string
	Monitored      *bool
}

type Counts struct {
	Total       int
	Wanted      int
	Downloading int
	Available   int
	// Trend holds the cumulative library size at the end of each of the last
	// trendDays days, oldest first; the final element equals Total.
	Trend []int
}

// trendDays is the width of the dashboard sparkline window.
const trendDays = 30

// FilterParams.Status == "" means "all"; other fields default to sensible values.
type FilterParams struct {
	Status string
	Query  string
	Sort   string
	Order  string
	Page   uint16
	Limit  uint16
}

// metadataMinRefreshInterval bounds the TMDB call rate of the metadata-refresh
// scheduler job: only movies whose update_time is older than this are touched.
const metadataMinRefreshInterval = 24 * time.Hour

// MetadataRefresher is the consumer-facing surface for the metadata-refresh
// scheduler job (jobs.MetadataRefresh).
type MetadataRefresher interface {
	RefreshStale(ctx context.Context) error
}

type Service struct {
	db       db.Store
	metadata metadata.Provider
	posters  posters.Manager
	download download.Downloader
}

func NewService(
	store db.Store,
	meta metadata.Provider,
	posters posters.Manager,
	dl download.Downloader,
) *Service {
	return &Service{db: store, metadata: meta, posters: posters, download: dl}
}

func (s *Service) Add(
	ctx context.Context,
	tmdbID uint32,
	qualityProfile string,
) (*ent.Movie, string, error) {
	ctx, span := tracer.Start(ctx, "movie.add",
		trace.WithAttributes(
			attribute.Int64("tmdb.id", int64(tmdbID)),
			attribute.String("quality_profile", qualityProfile),
		),
	)
	defer span.End()

	// An empty name resolves to quality_default_profile at read time; reject
	// only when the named profile (or default) resolves to nothing at all.
	if _, ok := config.ResolveQualityProfile(qualityProfile); !ok {
		return nil, "", otelx.RecordSpanError(span, ErrNoQualityProfile)
	}

	details, err := s.metadata.GetMovie(ctx, tmdbID)
	if err != nil {
		return nil, "", otelx.RecordSpanError(
			span,
			fmt.Errorf("fetch tmdb metadata: %w", err),
		)
	}
	span.SetAttributes(attribute.String("movie.title", details.Title))
	if details.OriginalTitle != details.Title {
		span.SetAttributes(
			attribute.String("movie.original_title", details.OriginalTitle),
		)
	}

	m, err := s.db.CreateMovie(ctx, db.CreateMovieParams{
		Title:          details.Title,
		OriginalTitle:  details.OriginalTitle,
		Year:           details.Year,
		TmdbID:         tmdbID,
		Status:         entmovie.StatusWanted,
		Overview:       details.Overview,
		Runtime:        details.Runtime,
		QualityProfile: qualityProfile,
	})
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, "", otelx.RecordSpanError(
				span,
				fmt.Errorf("movie with tmdb_id %d already exists", tmdbID),
			)
		}
		return nil, "", otelx.RecordSpanError(
			span,
			fmt.Errorf("create movie: %w", err),
		)
	}
	span.SetAttributes(attribute.Int64("movie.id", int64(m.ID)))

	if err := s.fetchDigitalRelease(ctx, m); err != nil {
		slog.WarnContext(ctx, "digital release date not set on add",
			"movie.id", m.ID, "movie.tmdb_id", m.TmdbID, "error", err)
	}

	posterPath := details.PosterPath
	if posterPath != "" && s.posters != nil {
		bg := context.WithoutCancel(ctx)
		movieID := m.ID
		src := metadata.PosterURL(posterPath, "original")
		go func() {
			if err := s.posters.Fetch(bg, "movies", movieID, src); err != nil {
				slog.WarnContext(
					bg,
					"poster fetch failed",
					"movie_id",
					movieID,
					"error",
					err,
				)
			}
		}()
	}

	moviesAdded.Add(ctx, 1)
	slog.InfoContext(ctx, "movie added", "title", m.Title, "tmdb_id", m.TmdbID)
	return m, posterPath, nil
}

func (s *Service) List(
	ctx context.Context,
	page, limit uint16,
) ([]*ent.Movie, uint32, error) {
	ctx, span := tracer.Start(ctx, "movie.list",
		trace.WithAttributes(
			attribute.Int("page", int(page)),
			attribute.Int("limit", int(limit)),
		),
	)
	defer span.End()

	if page == 0 {
		return nil, 0, otelx.RecordSpanError(span, fmt.Errorf("page must be > 0"))
	}
	if limit == 0 {
		return nil, 0, otelx.RecordSpanError(span, fmt.Errorf("limit must be > 0"))
	}

	total, err := s.db.CountMovies(ctx)
	if err != nil {
		return nil, 0, otelx.RecordSpanError(
			span,
			fmt.Errorf("count movies: %w", err),
		)
	}

	movies, err := s.db.ListMovies(ctx, uint32(page-1)*uint32(limit), uint32(limit))
	if err != nil {
		return nil, 0, otelx.RecordSpanError(
			span,
			fmt.Errorf("list movies: %w", err),
		)
	}
	span.SetAttributes(attribute.Int64("results.total", int64(total)))

	return movies, uint32(
		total,
	), nil //nolint:gosec // total from COUNT is non-negative
}

func (s *Service) FilterList(
	ctx context.Context,
	p FilterParams,
) ([]*ent.Movie, uint32, error) {
	ctx, span := tracer.Start(ctx, "movie.filter_list",
		trace.WithAttributes(
			attribute.String("filter.status", p.Status),
			attribute.String("filter.query", p.Query),
			attribute.String("filter.sort", p.Sort),
			attribute.String("filter.order", p.Order),
			attribute.Int("page", int(p.Page)),
			attribute.Int("limit", int(p.Limit)),
		),
	)
	defer span.End()

	page := p.Page
	if page == 0 {
		page = 1
	}
	limit := p.Limit
	if limit == 0 {
		limit = 20
	}
	items, total, err := s.db.FilterMovies(ctx, db.FilterMoviesParams{
		Status: entmovie.Status(p.Status),
		Query:  p.Query,
		Sort:   p.Sort,
		Order:  p.Order,
		Offset: uint32(page-1) * uint32(limit),
		Limit:  uint32(limit),
	})
	if err != nil {
		return nil, 0, otelx.RecordSpanError(
			span,
			fmt.Errorf("filter movies: %w", err),
		)
	}
	span.SetAttributes(attribute.Int64("results.total", int64(total)))
	return items, uint32(total), nil //nolint:gosec // total is non-negative
}

func (s *Service) Get(ctx context.Context, id uint32) (*ent.Movie, error) {
	ctx, span := tracer.Start(ctx, "movie.get",
		trace.WithAttributes(attribute.Int64("movie.id", int64(id))),
	)
	defer span.End()

	m, err := s.db.FindMovieByID(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, otelx.RecordSpanError(
				span,
				fmt.Errorf("movie %d not found", id),
			)
		}
		return nil, otelx.RecordSpanError(span, fmt.Errorf("get movie: %w", err))
	}
	return m, nil
}

// GetByTMDBID returns the library row for the given TMDB id, or (nil, nil)
// when none exists. The TMDB preview path treats absence as "not in library"
// rather than as an error.
func (s *Service) GetByTMDBID(
	ctx context.Context,
	tmdbID uint32,
) (*ent.Movie, error) {
	ctx, span := tracer.Start(ctx, "movie.get_by_tmdb_id",
		trace.WithAttributes(attribute.Int64("tmdb.id", int64(tmdbID))),
	)
	defer span.End()

	m, err := s.db.FindMovieByTMDBID(ctx, tmdbID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, otelx.RecordSpanError(
			span,
			fmt.Errorf("get movie by tmdb_id: %w", err),
		)
	}
	return m, nil
}

func (s *Service) Counts(ctx context.Context) (Counts, error) {
	ctx, span := tracer.Start(ctx, "movie.counts")
	defer span.End()

	count := func(label string, fn func() (int, error)) (int, error) {
		n, err := fn()
		if err != nil {
			return 0, otelx.RecordSpanError(
				span,
				fmt.Errorf("count %s: %w", label, err),
			)
		}
		return n, nil
	}

	total, err := count(
		"movies",
		func() (int, error) { return s.db.CountMovies(ctx) },
	)
	if err != nil {
		return Counts{}, err
	}
	wanted, err := count("wanted", func() (int, error) {
		return s.db.CountMoviesByStatus(ctx, entmovie.StatusWanted)
	})
	if err != nil {
		return Counts{}, err
	}
	downloading, err := count("downloading", func() (int, error) {
		return s.db.CountMoviesByStatus(ctx, entmovie.StatusDownloading)
	})
	if err != nil {
		return Counts{}, err
	}
	available, err := count("available", func() (int, error) {
		return s.db.CountMoviesByStatus(ctx, entmovie.StatusAvailable)
	})
	if err != nil {
		return Counts{}, err
	}
	span.SetAttributes(
		attribute.Int("counts.total", total),
		attribute.Int("counts.wanted", wanted),
		attribute.Int("counts.downloading", downloading),
		attribute.Int("counts.available", available),
	)
	trend, err := s.movieTrend(ctx, total)
	if err != nil {
		return Counts{}, err
	}

	return Counts{
		Total:       total,
		Wanted:      wanted,
		Downloading: downloading,
		Available:   available,
		Trend:       trend,
	}, nil
}

// movieTrend returns the cumulative library size at the end of each of the
// last trendDays days (oldest first), ending at `total` today. Movies added
// before the window form a flat baseline; an empty library yields all zeros.
func (s *Service) movieTrend(ctx context.Context, total int) ([]int, error) {
	const day = 24 * time.Hour
	todayStart := time.Now().UTC().Truncate(day)
	windowStart := todayStart.Add(-time.Duration(trendDays-1) * day)

	recent, err := s.db.MovieCreateTimesSince(ctx, windowStart)
	if err != nil {
		return nil, err
	}

	added := make([]int, trendDays)
	for _, t := range recent {
		idx := min(max(int(t.UTC().Sub(windowStart)/day), 0), trendDays-1)
		added[idx]++
	}

	// Movies created before the window are the starting baseline.
	baseline := max(total-len(recent), 0)

	trend := make([]int, trendDays)
	cum := baseline
	for i := range trend {
		cum += added[i]
		trend[i] = cum
	}
	return trend, nil
}

func (s *Service) Update(
	ctx context.Context,
	id uint32,
	p UpdateParams,
) (*ent.Movie, error) {
	ctx, span := tracer.Start(ctx, "movie.update",
		trace.WithAttributes(attribute.Int64("movie.id", int64(id))),
	)
	defer span.End()

	m, err := s.db.UpdateMovie(ctx, id, db.UpdateMovieParams{
		Status:         p.Status,
		QualityProfile: p.QualityProfile,
		Monitored:      p.Monitored,
	})
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, otelx.RecordSpanError(
				span,
				fmt.Errorf("movie %d not found", id),
			)
		}
		return nil, otelx.RecordSpanError(span, fmt.Errorf("update movie: %w", err))
	}
	moviesUpdated.Add(ctx, 1)
	return m, nil
}

// RefreshStale re-fetches TMDB metadata for movies whose update_time is older
// than metadataMinRefreshInterval. Per-row failures are logged and skipped;
// the tick returns nil unless the initial DB query fails.
func (s *Service) RefreshStale(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "movie.refresh_stale")
	defer span.End()

	cutoff := time.Now().Add(-metadataMinRefreshInterval)
	movies, err := s.db.ListMoviesStaleSince(ctx, cutoff)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	span.SetAttributes(attribute.Int("refresh.candidate_count", len(movies)))

	refreshed, skipped := 0, 0
	for _, m := range movies {
		if err := s.refreshOne(ctx, m); err != nil {
			slog.WarnContext(ctx, "metadata-refresh: skipping movie",
				"movie.id", m.ID, "movie.tmdb_id", m.TmdbID, "error", err)
			skipped++
			continue
		}
		refreshed++
	}

	span.SetAttributes(
		attribute.Int("refresh.refreshed_count", refreshed),
		attribute.Int("refresh.skipped_count", skipped),
	)
	slog.InfoContext(ctx, "metadata refresh complete",
		"refreshed", refreshed, "skipped", skipped)
	return nil
}

func (s *Service) refreshOne(ctx context.Context, m *ent.Movie) error {
	ctx, span := tracer.Start(ctx, "movie.refresh_stale.movie",
		trace.WithAttributes(
			attribute.Int64("movie.id", int64(m.ID)),
			attribute.Int64("movie.tmdb_id", int64(m.TmdbID)),
		),
	)
	defer span.End()

	details, err := s.metadata.GetMovie(ctx, m.TmdbID)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	if err := s.db.UpdateMovieMetadata(ctx, m.ID, db.UpdateMovieMetadataParams{
		Title:         details.Title,
		OriginalTitle: details.OriginalTitle,
		Overview:      details.Overview,
		Year:          details.Year,
		Runtime:       details.Runtime,
	}); err != nil {
		return otelx.RecordSpanError(span, err)
	}

	if err := s.fetchDigitalRelease(ctx, m); err != nil {
		return otelx.RecordSpanError(span, err)
	}
	return nil
}

// fetchDigitalRelease persists m's configured-region digital (TMDB type-4)
// release date. Best-effort: an unset region or a failed TMDB lookup is
// swallowed (the metadata-refresh tick retries later); only the DB write
// surfaces an error.
func (s *Service) fetchDigitalRelease(ctx context.Context, m *ent.Movie) error {
	region := config.Get().Metadata.TMDBRegion
	if region == "" {
		return nil
	}
	date, err := s.metadata.FetchDigitalRelease(ctx, m.TmdbID, region)
	if err != nil {
		slog.WarnContext(ctx, "tmdb digital release fetch failed",
			"movie.id", m.ID, "movie.tmdb_id", m.TmdbID, "error", err)
		return nil
	}
	return s.db.SetMovieDigitalReleaseDate(ctx, m.ID, date)
}

// AnnotateTMDBResults batches the AlreadyAdded lookup so callers avoid a
// per-row FindMovieByTMDBID query (N+1) when rendering TMDB search rows.
func (s *Service) AnnotateTMDBResults(
	ctx context.Context,
	results []metadata.MovieResult,
) ([]AnnotatedTMDBResult, error) {
	ctx, span := tracer.Start(ctx, "movie.annotate_tmdb_results",
		trace.WithAttributes(attribute.Int("results.count", len(results))),
	)
	defer span.End()

	if len(results) == 0 {
		return nil, nil
	}
	ids := make([]uint32, 0, len(results))
	for _, r := range results {
		ids = append(ids, r.TMDBID)
	}
	existing, err := s.db.FindMoviesByTMDBIDs(ctx, ids)
	if err != nil {
		return nil, otelx.RecordSpanError(
			span,
			fmt.Errorf("find movies by tmdb ids: %w", err),
		)
	}
	added := make(map[uint32]struct{}, len(existing))
	for _, m := range existing {
		added[m.TmdbID] = struct{}{}
	}
	out := make([]AnnotatedTMDBResult, 0, len(results))
	for _, r := range results {
		_, hit := added[r.TMDBID]
		out = append(out, AnnotatedTMDBResult{MovieResult: r, AlreadyAdded: hit})
	}
	return out, nil
}

func (s *Service) Delete(
	ctx context.Context, id uint32, opts DeleteOptions,
) error {
	ctx, span := tracer.Start(ctx, "movie.delete",
		trace.WithAttributes(
			attribute.Int64("movie.id", int64(id)),
			attribute.Bool("delete_files", opts.DeleteFiles),
		),
	)
	defer span.End()

	if opts.DeleteFiles {
		files, err := s.db.ListMediaFilesByMovieID(ctx, id)
		if err != nil {
			return otelx.RecordSpanError(span,
				fmt.Errorf("list media_files: %w", err))
		}
		for _, f := range files {
			if err := os.Remove(f.Path); err != nil && !os.IsNotExist(err) {
				slog.WarnContext(ctx, "delete movie file failed",
					"movie.id", id, "path", f.Path, "error", err)
			}
		}
	}
	if err := s.db.DeleteMovie(ctx, id); err != nil {
		if ent.IsNotFound(err) {
			return otelx.RecordSpanError(span,
				fmt.Errorf("movie %d not found", id))
		}
		return otelx.RecordSpanError(span, fmt.Errorf("delete movie: %w", err))
	}
	moviesDeleted.Add(ctx, 1)
	slog.InfoContext(ctx, "movie deleted",
		"id", id, "delete_files", opts.DeleteFiles)
	return nil
}

// DeleteFileOptions controls DeleteFile.
type DeleteFileOptions struct {
	// RemoveTorrent also removes the source torrent from its download client.
	RemoveTorrent bool
}

// DeleteFile removes one of a movie's media files from disk + DB and reverts
// the movie to "wanted" so the next monitored search re-grabs it. When
// opts.RemoveTorrent is set, the source torrent is also removed from its
// download client (best-effort — a lingering torrent never fails the request).
func (s *Service) DeleteFile(
	ctx context.Context, movieID, fileID uint32, opts DeleteFileOptions,
) error {
	ctx, span := tracer.Start(ctx, "movie.delete_file",
		trace.WithAttributes(
			attribute.Int64("movie.id", int64(movieID)),
			attribute.Int64("media_file.id", int64(fileID)),
			attribute.Bool("remove_torrent", opts.RemoveTorrent),
		))
	defer span.End()

	mf, err := s.db.FindMediaFileByID(ctx, fileID)
	if err != nil {
		if ent.IsNotFound(err) {
			return otelx.RecordSpanError(span,
				fmt.Errorf("media file %d not found", fileID))
		}
		return otelx.RecordSpanError(span, fmt.Errorf("find media_file: %w", err))
	}
	// Remove the file only; leave the movie dir for the re-grab.
	if err := os.Remove(mf.Path); err != nil && !os.IsNotExist(err) {
		slog.WarnContext(ctx, "delete media file from disk failed",
			"path", mf.Path, "error", err)
	}
	if err := s.db.DeleteMediaFileAndRevertMovie(ctx, fileID, movieID); err != nil {
		return otelx.RecordSpanError(span, fmt.Errorf("delete + revert: %w", err))
	}
	if opts.RemoveTorrent {
		s.removeSourceTorrent(ctx, movieID)
	}
	slog.InfoContext(ctx, "media file deleted",
		"movie.id", movieID, "media_file.id", fileID)
	return nil
}

// removeSourceTorrent best-effort removes the torrent that produced the movie's
// most recent grab. Absence or any failure is logged, never surfaced.
func (s *Service) removeSourceTorrent(ctx context.Context, movieID uint32) {
	rec, err := s.db.LatestImportedRecordForMovie(ctx, movieID)
	switch {
	case ent.IsNotFound(err):
		return
	case err != nil:
		slog.WarnContext(ctx, "lookup source torrent failed",
			"movie.id", movieID, "error", err)
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

// RefreshOne re-fetches TMDB metadata for one movie and returns the updated
// row. Used by the manual "refresh metadata" UI action.
func (s *Service) RefreshOne(
	ctx context.Context, id uint32,
) (*ent.Movie, error) {
	ctx, span := tracer.Start(ctx, "movie.refresh_one",
		trace.WithAttributes(attribute.Int64("movie.id", int64(id))),
	)
	defer span.End()

	m, err := s.db.FindMovieByID(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, otelx.RecordSpanError(span,
				fmt.Errorf("movie %d not found", id))
		}
		return nil, otelx.RecordSpanError(span, fmt.Errorf("get movie: %w", err))
	}
	if err := s.refreshOne(ctx, m); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	refreshed, err := s.db.FindMovieByID(ctx, id)
	if err != nil {
		return nil, otelx.RecordSpanError(span,
			fmt.Errorf("reload movie: %w", err))
	}
	return refreshed, nil
}
