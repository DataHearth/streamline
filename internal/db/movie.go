package db

import (
	"context"
	"fmt"
	"slices"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/ent/predicate"
	"github.com/datahearth/streamline/internal/metadata"
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
	Rating         float64
	Genres         []string
	Cast           []metadata.CastMember
	ReleaseDate    *time.Time
}

func (db *DB) CreateMovie(
	ctx context.Context,
	p CreateMovieParams,
) (*ent.Movie, error) {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	b := tx.Movie.Create().
		SetTitle(p.Title).
		SetOriginalTitle(p.OriginalTitle).
		SetYear(p.Year).
		SetTmdbID(p.TmdbID).
		SetStatus(p.Status).
		SetNillableReleaseDate(p.ReleaseDate)
	if p.Overview != "" {
		b.SetOverview(p.Overview)
	}
	if p.Runtime != 0 {
		b.SetRuntime(p.Runtime)
	}
	if p.QualityProfile != "" {
		b.SetQualityProfile(p.QualityProfile)
	}
	if p.Rating != 0 {
		b.SetRating(p.Rating)
	}
	if len(p.Genres) != 0 {
		b.SetGenres(p.Genres)
	}
	m, err := b.Save(ctx)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := replaceCast(
		ctx,
		tx.Client(),
		CastOwnerMovie,
		m.ID,
		p.Cast,
	); err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	// Re-read rather than hand back the row the transaction built: an entity
	// carries the client it was created with, and a committed tx's client
	// errors on every later query through it. CreateTVShow does the same.
	return db.FindMovieByID(ctx, m.ID)
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

// MovieTMDBIndex maps every tracked tmdb id to its movie row id. The bulk
// importer needs exactly this to tell an already-tracked title from a new
// one; it used to page the entire movie table *with media files attached* to
// build the same two columns.
func (db *DB) MovieTMDBIndex(ctx context.Context) (map[uint32]uint32, error) {
	var rows []struct {
		ID     uint32 `json:"id"`
		TmdbID uint32 `json:"tmdb_id"`
	}
	err := db.client.Movie.Query().
		Select(movie.FieldID, movie.FieldTmdbID).
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	out := make(map[uint32]uint32, len(rows))
	for _, r := range rows {
		out[r.TmdbID] = r.ID
	}
	return out, nil
}

// MovieCreateTimesSince returns the create_time of every movie added on or
// after `since`, oldest first — used to bucket library growth into a trend.
// Scanned into a one-field struct rather than .All(): a one-column Select
// still builds a fully zeroed *ent.Movie per row, which is the whole struct
// allocated to read one field. It cannot scan into []time.Time — ent reflects
// time.Time as a row struct and looks for a create_time field on it.
func (db *DB) MovieCreateTimesSince(
	ctx context.Context,
	since time.Time,
) ([]time.Time, error) {
	var rows []struct {
		CreateTime time.Time `json:"create_time"`
	}
	err := db.client.Movie.Query().
		Where(movie.CreateTimeGTE(since)).
		Order(ent.Asc(movie.FieldCreateTime)).
		Select(movie.FieldCreateTime).
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	out := make([]time.Time, len(rows))
	for i, r := range rows {
		out[i] = r.CreateTime
	}
	return out, nil
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

// ListUpgradeCandidateMovies returns every monitored movie that already has a
// file on disk, with those files loaded. The rss feed scanner scores each
// file against incoming releases to decide whether to grab an upgrade.
func (db *DB) ListUpgradeCandidateMovies(
	ctx context.Context,
) ([]*ent.Movie, error) {
	return db.client.Movie.Query().
		Where(
			movie.MonitoredEQ(true),
			movie.HasMediaFiles(),
			// A movie whose replacement is already in flight would be
			// re-grabbed every tick, and each failed re-grab bumps
			// grab_failures on a healthy movie. "importing" is in flight
			// too — the bytes are down but the file is not on disk yet.
			movie.StatusNotIn(
				movie.StatusDownloading,
				movie.StatusImporting,
			),
		).
		WithMediaFiles().
		All(ctx)
}

// UpcomingReleases returns wanted movies whose release falls in [from, to),
// ordered ascending. Used by the dashboard calendar modal.
//
// digital_release_date wins when set, and release_date (theatrical) stands in
// when it is not: TMDB publishes a type-4 entry only once a title is actually
// available to buy, so a film still in cinemas — exactly what an "upcoming"
// calendar is for — has a theatrical date and nothing else. Keying on the
// digital date alone left the movie half of the calendar permanently empty.
func (db *DB) UpcomingReleases(
	ctx context.Context,
	from, to time.Time,
) ([]*ent.Movie, error) {
	movies, err := db.client.Movie.Query().
		Where(
			movie.StatusEQ(movie.StatusWanted),
			movie.Or(
				movie.And(
					movie.DigitalReleaseDateGTE(from),
					movie.DigitalReleaseDateLT(to),
				),
				movie.And(
					movie.DigitalReleaseDateIsNil(),
					movie.ReleaseDateGTE(from),
					movie.ReleaseDateLT(to),
				),
			),
		).
		All(ctx)
	if err != nil {
		return nil, err
	}
	// ponytail: sorted in Go — one calendar window is a handful of rows, and
	// a COALESCE ORDER BY needs raw SQL past the ent builder.
	slices.SortFunc(movies, func(a, b *ent.Movie) int {
		return UpcomingReleaseDate(a).Compare(UpcomingReleaseDate(b))
	})
	return movies, nil
}

// UpcomingReleaseDate is the date UpcomingReleases matched m on: the digital
// release when TMDB has published one, the theatrical release otherwise.
func UpcomingReleaseDate(m *ent.Movie) time.Time {
	if m.DigitalReleaseDate != nil {
		return *m.DigitalReleaseDate
	}
	if m.ReleaseDate != nil {
		return *m.ReleaseDate
	}
	return time.Time{}
}

// ListMoviesStaleSince returns movies never refreshed, or last refreshed
// before cutoff. Used by metadata-refresh to bound TMDB calls per tick.
// Deliberately NOT keyed on update_time: that column moves on every write
// (orphan import, status change, grab), so a library that scans on boot would
// never look stale and the refresh would never run.
func (db *DB) ListMoviesStaleSince(
	ctx context.Context,
	cutoff time.Time,
) ([]*ent.Movie, error) {
	return db.client.Movie.Query().
		Where(movie.Or(
			movie.LastRefreshedAtIsNil(),
			movie.LastRefreshedAtLT(cutoff),
		)).
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
	// Monitored filters on the movie's own flag; nil means "either".
	Monitored *bool
	Query     string
	Sort      string // "title" | "year" | "create_time"
	Order     string // "asc" | "desc"
	Offset    uint32
	Limit     uint32
}

// movieFilters builds the list's predicates, leaving out the facet a caller
// is about to count. See db.facet: a facet counted against its own selection
// zeroes every other row, so its dropdown can never be changed back.
func movieFilters(p FilterMoviesParams, skip facet) []predicate.Movie {
	var out []predicate.Movie
	if p.Status != "" && skip != facetStatus {
		out = append(out, movie.StatusEQ(p.Status))
	}
	if p.Monitored != nil && skip != facetMonitored {
		out = append(out, movie.MonitoredEQ(*p.Monitored))
	}
	// The search box is not a facet — it has no "all" row to keep selectable —
	// so it narrows every tally.
	if p.Query != "" {
		out = append(out, func(s *entsql.Selector) {
			s.Where(entsql.Or(
				foldContains(s, movie.FieldTitle, p.Query),
				foldContains(s, movie.FieldOriginalTitle, p.Query),
			))
		})
	}
	return out
}

// MovieFacets is the movie counts endpoint's answer: a tally per status, a
// tally per monitoring value, and each facet's own "all" row. Total is the
// library, filtered by nothing.
type MovieFacets struct {
	Total int

	StatusTotal int
	ByStatus    map[movie.Status]int

	MonitoredTotal int
	Monitored      int
	Unmonitored    int
}

// MovieFacetCounts tallies the status and monitoring facets, each against the
// filters applied to the other.
func (db *DB) MovieFacetCounts(
	ctx context.Context,
	p FilterMoviesParams,
) (MovieFacets, error) {
	var out MovieFacets

	total, err := db.client.Movie.Query().Count(ctx)
	if err != nil {
		return out, fmt.Errorf("count movies: %w", err)
	}
	out.Total = total

	var rows []struct {
		Status movie.Status `json:"status"`
		Count  int          `json:"count"`
	}
	err = db.client.Movie.Query().
		Where(movieFilters(p, facetStatus)...).
		GroupBy(movie.FieldStatus).
		Aggregate(ent.Count()).
		Scan(ctx, &rows)
	if err != nil {
		return out, fmt.Errorf("group movies by status: %w", err)
	}
	out.ByStatus = make(map[movie.Status]int, len(rows))
	for _, r := range rows {
		out.ByStatus[r.Status] = r.Count
		out.StatusTotal += r.Count
	}

	monFilters := movieFilters(p, facetMonitored)
	monTotal, err := db.client.Movie.Query().Where(monFilters...).Count(ctx)
	if err != nil {
		return out, fmt.Errorf("count movies for monitoring: %w", err)
	}
	monitored, err := db.client.Movie.Query().
		Where(append(monFilters, movie.MonitoredEQ(true))...).
		Count(ctx)
	if err != nil {
		return out, fmt.Errorf("count monitored movies: %w", err)
	}
	out.MonitoredTotal = monTotal
	out.Monitored = monitored
	out.Unmonitored = monTotal - monitored

	return out, nil
}

func (db *DB) FilterMovies(
	ctx context.Context,
	p FilterMoviesParams,
) ([]*ent.Movie, int, error) {
	base := db.client.Movie.Query().Where(movieFilters(p, facetAll)...)

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
	Rating        float64
	Genres        []string
	Cast          []metadata.CastMember
	ReleaseDate   *time.Time
}

// UpdateMovieMetadata updates only the TMDB-sourced metadata fields. Status and
// QualityProfileID are intentionally not touched — those are owned by the
// lifecycle update path.
func (db *DB) UpdateMovieMetadata(
	ctx context.Context,
	id uint32,
	p UpdateMovieMetadataParams,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return err
	}
	if err := tx.Movie.UpdateOneID(id).
		SetTitle(p.Title).
		SetOriginalTitle(p.OriginalTitle).
		SetYear(p.Year).
		SetOverview(p.Overview).
		SetRuntime(p.Runtime).
		SetRating(p.Rating).
		SetGenres(p.Genres).
		SetNillableReleaseDate(p.ReleaseDate).
		SetLastRefreshedAt(time.Now()).
		Exec(ctx); err != nil {
		tx.Rollback()
		return err
	}
	if err := replaceCast(ctx, tx.Client(), CastOwnerMovie, id, p.Cast); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// SetMovieTMDBID repoints a row at a different TMDB title. Everything else on
// the row — files, history, requests, quality profile — is left alone; the
// caller refreshes metadata afterwards so title/year stop contradicting the id.
func (db *DB) SetMovieTMDBID(ctx context.Context, id, tmdbID uint32) error {
	return db.client.Movie.UpdateOneID(id).SetTmdbID(tmdbID).Exec(ctx)
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
