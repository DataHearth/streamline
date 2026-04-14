package db

import (
	"context"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/movie"
)

type CreateMovieParams struct {
	Title          string
	OriginalTitle  string
	Year           uint16
	TmdbID         uint32
	Status         movie.Status
	Overview       string
	Runtime        uint16
	QualityProfile string
}

func (db *DB) CreateMovie(
	ctx context.Context,
	p CreateMovieParams,
) (*ent.Movie, error) {
	b := db.client.Movie.Create().
		SetTitle(p.Title).
		SetOriginalTitle(p.OriginalTitle).
		SetYear(p.Year).
		SetTmdbID(p.TmdbID).
		SetStatus(p.Status)
	if p.Overview != "" {
		b.SetOverview(p.Overview)
	}
	if p.Runtime != 0 {
		b.SetRuntime(p.Runtime)
	}
	if p.QualityProfile != "" {
		b.SetQualityProfile(p.QualityProfile)
	}
	return b.Save(ctx)
}

func (db *DB) FindMovieByID(ctx context.Context, id uint32) (*ent.Movie, error) {
	return db.client.Movie.Query().
		Where(movie.IDEQ(id)).
		Only(ctx)
}

func (db *DB) CountMovies(ctx context.Context) (int, error) {
	return db.client.Movie.Query().Count(ctx)
}

func (db *DB) CountMoviesByStatus(
	ctx context.Context,
	status movie.Status,
) (int, error) {
	return db.client.Movie.Query().Where(movie.StatusEQ(status)).Count(ctx)
}

// MovieCreateTimesSince returns the create_time of every movie added on or
// after `since`, oldest first — used to bucket library growth into a trend.
func (db *DB) MovieCreateTimesSince(
	ctx context.Context,
	since time.Time,
) ([]time.Time, error) {
	rows, err := db.client.Movie.Query().
		Where(movie.CreateTimeGTE(since)).
		Order(ent.Asc(movie.FieldCreateTime)).
		Select(movie.FieldCreateTime).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]time.Time, len(rows))
	for i, r := range rows {
		out[i] = r.CreateTime
	}
	return out, nil
}

// ListMovies returns a page of movies newest first.
func (db *DB) ListMovies(
	ctx context.Context,
	offset, limit uint32,
) ([]*ent.Movie, error) {
	return db.client.Movie.Query().
		Offset(int(offset)).
		Limit(int(limit)).
		Order(ent.Desc(movie.FieldCreateTime)).
		All(ctx)
}

// ListWantedMovies returns every movie with status = wanted, regardless of
// cooldown or grab-failure state. Used by the rss-sync feed scanner to build
// a per-tick title+year lookup map.
func (db *DB) ListWantedMovies(ctx context.Context) ([]*ent.Movie, error) {
	return db.client.Movie.Query().
		Where(
			movie.StatusEQ(movie.StatusWanted),
			movie.MonitoredEQ(true),
		).
		All(ctx)
}

// UpcomingReleases returns wanted movies whose digital_release_date falls in
// [from, to), ordered by release date ascending. Used by the dashboard
// calendar modal.
func (db *DB) UpcomingReleases(
	ctx context.Context,
	from, to time.Time,
) ([]*ent.Movie, error) {
	return db.client.Movie.Query().
		Where(
			movie.StatusEQ(movie.StatusWanted),
			movie.DigitalReleaseDateGTE(from),
			movie.DigitalReleaseDateLT(to),
		).
		Order(ent.Asc(movie.FieldDigitalReleaseDate)).
		All(ctx)
}

// ListMoviesStaleSince returns movies whose updated_at is older than cutoff.
// Used by metadata-refresh to bound TMDB calls per tick.
func (db *DB) ListMoviesStaleSince(
	ctx context.Context,
	cutoff time.Time,
) ([]*ent.Movie, error) {
	return db.client.Movie.Query().
		Where(movie.UpdateTimeLT(cutoff)).
		All(ctx)
}

// ListEligibleMoviesForSync returns wanted movies not over the failure cap
// whose cooldown window has expired (or has never run). Movies with an
// in-flight download_record (downloading or importing) are excluded so a
// stale movie.status row doesn't trigger a redundant grab while the
// download pipeline is still working — defense against state drift.
func (db *DB) ListEligibleMoviesForSync(
	ctx context.Context,
	maxGrabFailures uint8,
	notSearchedSince time.Time,
) ([]*ent.Movie, error) {
	return db.client.Movie.Query().
		Where(
			movie.StatusEQ(movie.StatusWanted),
			movie.MonitoredEQ(true),
			movie.GrabFailuresLT(maxGrabFailures),
			movie.Or(
				movie.LastSearchAtIsNil(),
				movie.LastSearchAtLT(notSearchedSince),
			),
			movie.Not(movie.HasDownloadRecordsWith(
				downloadrecord.StatusIn(
					downloadrecord.StatusDownloading,
					downloadrecord.StatusImporting,
				),
			)),
		).
		All(ctx)
}

func (db *DB) DeleteMovie(ctx context.Context, id uint32) error {
	return db.client.Movie.DeleteOneID(id).Exec(ctx)
}

// FilterMoviesParams: Status/Query empty disables the respective clause;
// Limit must be > 0.
type FilterMoviesParams struct {
	Status movie.Status
	Query  string
	Sort   string // "title" | "year" | "create_time"
	Order  string // "asc" | "desc"
	Offset uint32
	Limit  uint32
}

func (db *DB) FilterMovies(
	ctx context.Context,
	p FilterMoviesParams,
) ([]*ent.Movie, int, error) {
	base := db.client.Movie.Query()
	if p.Status != "" {
		base = base.Where(movie.StatusEQ(p.Status))
	}
	if p.Query != "" {
		base = base.Where(movie.Or(
			movie.TitleContainsFold(p.Query),
			movie.OriginalTitleContainsFold(p.Query),
		))
	}

	total, err := base.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	q := base.Offset(int(p.Offset)).Limit(int(p.Limit))
	desc := p.Order == "desc"
	switch p.Sort {
	case "title":
		if desc {
			q = q.Order(ent.Desc(movie.FieldTitle))
		} else {
			q = q.Order(ent.Asc(movie.FieldTitle))
		}
	case "year":
		if desc {
			q = q.Order(ent.Desc(movie.FieldYear))
		} else {
			q = q.Order(ent.Asc(movie.FieldYear))
		}
	default:
		if p.Order == "asc" {
			q = q.Order(ent.Asc(movie.FieldCreateTime))
		} else {
			q = q.Order(ent.Desc(movie.FieldCreateTime))
		}
	}

	items, err := q.All(ctx)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (db *DB) FindMovieByTMDBID(
	ctx context.Context,
	tmdbID uint32,
) (*ent.Movie, error) {
	return db.client.Movie.Query().Where(movie.TmdbIDEQ(tmdbID)).Only(ctx)
}

// FindMoviesByTMDBIDs returns movies whose tmdb_id is in tmdbIDs. Used by
// movie.AnnotateTMDBResults to flag already-added rows in TMDB search
// results without an N+1 lookup loop.
func (db *DB) FindMoviesByTMDBIDs(
	ctx context.Context,
	tmdbIDs []uint32,
) ([]*ent.Movie, error) {
	if len(tmdbIDs) == 0 {
		return nil, nil
	}
	return db.client.Movie.Query().
		Where(movie.TmdbIDIn(tmdbIDs...)).
		All(ctx)
}

func (db *DB) UpdateMovieStatus(
	ctx context.Context,
	id uint32,
	status movie.Status,
) error {
	return db.client.Movie.UpdateOneID(id).SetStatus(status).Exec(ctx)
}

type UpdateMovieParams struct {
	Status         *movie.Status
	QualityProfile *string
	Monitored      *bool
}

type UpdateMovieMetadataParams struct {
	Title         string
	OriginalTitle string
	Overview      string
	Year          uint16
	Runtime       uint16
}

// UpdateMovieMetadata updates only the TMDB-sourced metadata fields
// (title, original_title, year, overview, runtime). Status and
// QualityProfileID are intentionally not touched — those are owned by the
// lifecycle update path.
func (db *DB) UpdateMovieMetadata(
	ctx context.Context,
	id uint32,
	p UpdateMovieMetadataParams,
) error {
	return db.client.Movie.UpdateOneID(id).
		SetTitle(p.Title).
		SetOriginalTitle(p.OriginalTitle).
		SetYear(p.Year).
		SetOverview(p.Overview).
		SetRuntime(p.Runtime).
		Exec(ctx)
}

// SetMovieDigitalReleaseDate sets or clears digital_release_date based on
// whether `date` is non-nil. Used by metadata-refresh after the TMDB
// release_dates lookup.
func (db *DB) SetMovieDigitalReleaseDate(
	ctx context.Context,
	id uint32,
	date *time.Time,
) error {
	upd := db.client.Movie.UpdateOneID(id)
	if date != nil {
		upd.SetDigitalReleaseDate(*date)
	} else {
		upd.ClearDigitalReleaseDate()
	}
	return upd.Exec(ctx)
}

func (db *DB) UpdateMovie(
	ctx context.Context,
	id uint32,
	p UpdateMovieParams,
) (*ent.Movie, error) {
	b := db.client.Movie.UpdateOneID(id)
	if p.Status != nil {
		b.SetStatus(*p.Status)
	}
	if p.QualityProfile != nil {
		b.SetQualityProfile(*p.QualityProfile)
	}
	if p.Monitored != nil {
		b.SetMonitored(*p.Monitored)
	}
	return b.Save(ctx)
}

func (db *DB) SetMovieLastSearchAt(
	ctx context.Context,
	id uint32,
	when time.Time,
) error {
	return db.client.Movie.UpdateOneID(id).SetLastSearchAt(when).Exec(ctx)
}

func (db *DB) IncrementMovieGrabFailures(ctx context.Context, id uint32) error {
	return db.client.Movie.UpdateOneID(id).AddGrabFailures(1).Exec(ctx)
}

func (db *DB) ResetMovieGrabFailures(ctx context.Context, id uint32) error {
	return db.client.Movie.UpdateOneID(id).SetGrabFailures(0).Exec(ctx)
}
