package db

import (
	"context"
	"fmt"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/episode"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/predicate"
	"github.com/datahearth/streamline/ent/schema"
	"github.com/datahearth/streamline/ent/season"
	"github.com/datahearth/streamline/ent/tvshow"
)

type EpisodeSeed struct {
	Number         uint16
	AbsoluteNumber uint16
	Title          string
	Overview       string
	AirDate        *time.Time
}

// tba reports that the provider has announced the episode without publishing
// either a title or an air date. Those rows are placeholders — often for
// episodes that never materialise — so they start unmonitored and only become
// monitored once a refresh fills in one of the two.
func (e EpisodeSeed) tba() bool { return e.Title == "" && e.AirDate == nil }

type SeasonSeed struct {
	Number uint16
	Name   string
	// Unmonitored, not Monitored: the zero value has to mean "monitored", the
	// long-standing default for a freshly seeded season.
	Unmonitored bool
	Episodes    []EpisodeSeed
}

type CreateTVShowParams struct {
	Title          string
	OriginalTitle  string
	Year           uint16
	Overview       string
	TvdbID         uint32
	SeriesStatus   string
	Type           string
	Network        string
	Creator        string
	Runtime        uint16
	Rating         float64
	Genres         []string
	Cast           []schema.CastMember
	PosterPath     string
	QualityProfile string
	Seasons        []SeasonSeed
}

type UpdateTVShowParams struct {
	Monitored      *bool
	QualityProfile *string
	Type           *tvshow.Type
}

// UpdateTVShowMetadataParams carries the provider-sourced fields refreshed from
// TVDB. User-owned fields (monitored, quality_profile) are left untouched.
type UpdateTVShowMetadataParams struct {
	Title         string
	OriginalTitle string
	Year          uint16
	Overview      string
	Network       string
	Creator       string
	SeriesStatus  string
	Runtime       uint16
	Rating        float64
	Genres        []string
	Cast          []schema.CastMember
}

// UpdateTVShowMetadata persists refreshed provider metadata onto an existing
// show (used by RefreshOne so a metadata refresh actually surfaces changes).
func (db *DB) UpdateTVShowMetadata(
	ctx context.Context,
	id uint32,
	p UpdateTVShowMetadataParams,
) error {
	u := db.client.TVShow.UpdateOneID(id).
		SetTitle(p.Title).
		SetOriginalTitle(p.OriginalTitle).
		SetYear(p.Year).
		SetOverview(p.Overview).
		SetNetwork(p.Network).
		SetCreator(p.Creator).
		SetRuntime(p.Runtime).
		SetRating(p.Rating).
		SetGenres(p.Genres)
	// Cast comes from a separate provider call than the rest, so an empty
	// slice means "that call failed" far more often than "this show has no
	// actors" — keep whatever is already stored.
	if len(p.Cast) > 0 {
		u = u.SetCast(p.Cast)
	}
	if p.SeriesStatus != "" {
		u = u.SetSeriesStatus(tvshow.SeriesStatus(p.SeriesStatus))
	}
	// Type is deliberately not refreshed. It is inferred from genres and origin,
	// it decides whether episodes match by absolute number, and it is the one
	// piece of show metadata an operator can correct by hand — re-deriving it on
	// every refresh would silently undo that correction.
	return u.Exec(ctx)
}

// ReconcileEpisodes syncs the stored season/episode tree with freshly fetched
// provider metadata: existing rows get their season name and episode
// title/air date refreshed, seasons/episodes the provider now reports but we
// don't have yet are inserted, and seasons/episodes the provider no longer
// reports are deleted (their media_file/download_record rows cascade). It
// returns the on-disk paths of files whose episodes were removed so the caller
// can delete them from disk — the DB layer never touches the filesystem.
// User-owned state (monitored, status, grab counters) on surviving rows is
// preserved, except that a TBA episode gaining a title or air date is promoted
// back to monitored when its season is monitored; new episodes inherit their
// season's monitored flag unless they are still TBA, a brand-new season
// defaults to monitored.
func (db *DB) ReconcileEpisodes(
	ctx context.Context,
	showID uint32,
	seasons []SeasonSeed,
) ([]string, error) {
	show, err := db.FindTVShowByID(ctx, showID)
	if err != nil {
		return nil, err
	}
	existing := make(map[uint16]*ent.Season, len(show.Edges.Seasons))
	for _, sr := range show.Edges.Seasons {
		existing[sr.Number] = sr
	}
	// Provider's episode numbers per season, for the deletion pass.
	want := make(map[uint16]map[uint16]bool, len(seasons))
	for _, s := range seasons {
		m := make(map[uint16]bool, len(s.Episodes))
		for _, e := range s.Episodes {
			m[e.Number] = true
		}
		want[s.Number] = m
	}

	tx, err := db.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	for _, s := range seasons {
		sr := existing[s.Number]
		if sr == nil {
			if sr, err = tx.Season.Create().
				SetNumber(s.Number).
				SetName(s.Name).
				SetMonitored(!s.Unmonitored).
				SetTvShowID(showID).
				Save(ctx); err != nil {
				tx.Rollback()
				return nil, err
			}
		} else if s.Name != sr.Name {
			if err := tx.Season.UpdateOne(sr).SetName(s.Name).Exec(ctx); err != nil {
				tx.Rollback()
				return nil, err
			}
		}

		haveEp := make(map[uint16]*ent.Episode, len(sr.Edges.Episodes))
		for _, er := range sr.Edges.Episodes {
			haveEp[er.Number] = er
		}
		for _, e := range s.Episodes {
			er := haveEp[e.Number]
			if er == nil {
				b := tx.Episode.Create().
					SetNumber(e.Number).
					SetAbsoluteNumber(e.AbsoluteNumber).
					SetTitle(e.Title).
					SetOverview(e.Overview).
					SetMonitored(sr.Monitored && !e.tba()).
					SetSeasonID(sr.ID)
				if e.AirDate != nil {
					b = b.SetAirDate(*e.AirDate)
				}
				if _, err := b.Save(ctx); err != nil {
					tx.Rollback()
					return nil, err
				}
				continue
			}
			u, changed := tx.Episode.UpdateOne(er), false
			if e.Title != er.Title {
				u, changed = u.SetTitle(e.Title), true
			}
			if e.Overview != er.Overview {
				u, changed = u.SetOverview(e.Overview), true
			}
			if e.AirDate != nil && !e.AirDate.Equal(er.AirDate) {
				u, changed = u.SetAirDate(*e.AirDate), true
			}
			if er.Title == "" && er.AirDate.IsZero() &&
				!e.tba() && sr.Monitored && !er.Monitored {
				u, changed = u.SetMonitored(true), true
			}
			if changed {
				if err := u.Exec(ctx); err != nil {
					tx.Rollback()
					return nil, err
				}
			}
		}
	}

	// Deletion pass. Guarded: an empty provider response means a failed/partial
	// fetch, not "the show has no episodes" — deleting then would wipe the
	// library, so skip. Within a surviving season we only prune episodes when
	// the provider actually reported some for it (an empty set is treated as
	// "unknown", not "all removed").
	var removedFiles []string
	if len(seasons) > 0 {
		for _, sr := range show.Edges.Seasons {
			epSet, kept := want[sr.Number]
			if !kept {
				removedFiles = appendEpisodeFiles(removedFiles, sr.Edges.Episodes)
				if err := tx.Season.DeleteOne(sr).Exec(ctx); err != nil {
					tx.Rollback()
					return nil, err
				}
				continue
			}
			if len(epSet) == 0 {
				continue
			}
			for _, er := range sr.Edges.Episodes {
				if epSet[er.Number] {
					continue
				}
				removedFiles = appendEpisodeFiles(removedFiles, []*ent.Episode{er})
				if err := tx.Episode.DeleteOne(er).Exec(ctx); err != nil {
					tx.Rollback()
					return nil, err
				}
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return removedFiles, nil
}

func appendEpisodeFiles(paths []string, eps []*ent.Episode) []string {
	for _, er := range eps {
		for _, mf := range er.Edges.MediaFiles {
			paths = append(paths, mf.Path)
		}
	}
	return paths
}

// CreateTVShow inserts the show + its seasons + episodes in one transaction.
func (db *DB) CreateTVShow(
	ctx context.Context,
	p CreateTVShowParams,
) (*ent.TVShow, error) {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	show, err := tx.TVShow.Create().
		SetTitle(p.Title).
		SetOriginalTitle(p.OriginalTitle).
		SetYear(p.Year).
		SetOverview(p.Overview).
		SetTvdbID(p.TvdbID).
		SetSeriesStatus(tvshow.SeriesStatus(orDefault(p.SeriesStatus, "continuing"))).
		SetType(tvshow.Type(orDefault(p.Type, "standard"))).
		SetNetwork(p.Network).
		SetCreator(p.Creator).
		SetRuntime(p.Runtime).
		SetRating(p.Rating).
		SetGenres(p.Genres).
		SetCast(p.Cast).
		SetPosterPath(p.PosterPath).
		SetQualityProfile(p.QualityProfile).
		Save(ctx)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	for _, s := range p.Seasons {
		seasonRow, err := tx.Season.Create().
			SetNumber(s.Number).
			SetName(s.Name).
			SetMonitored(!s.Unmonitored).
			SetTvShowID(show.ID).
			Save(ctx)
		if err != nil {
			tx.Rollback()
			return nil, err
		}
		for _, e := range s.Episodes {
			b := tx.Episode.Create().
				SetNumber(e.Number).
				SetAbsoluteNumber(e.AbsoluteNumber).
				SetTitle(e.Title).
				SetOverview(e.Overview).
				SetMonitored(!s.Unmonitored && !e.tba()).
				SetSeasonID(seasonRow.ID)
			if e.AirDate != nil {
				b = b.SetAirDate(*e.AirDate)
			}
			if _, err := b.Save(ctx); err != nil {
				tx.Rollback()
				return nil, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return db.FindTVShowByID(ctx, show.ID)
}

func (db *DB) FindTVShowByID(ctx context.Context, id uint32) (*ent.TVShow, error) {
	return db.client.TVShow.Query().
		Where(tvshow.IDEQ(id)).
		WithSeasons(func(q *ent.SeasonQuery) {
			q.Order(ent.Asc(season.FieldNumber)).
				WithEpisodes(func(eq *ent.EpisodeQuery) {
					eq.Order(ent.Asc(episode.FieldNumber)).WithMediaFiles()
				})
		}).
		Only(ctx)
}

func (db *DB) FindTVShowByTVDBID(
	ctx context.Context,
	tvdbID uint32,
) (*ent.TVShow, error) {
	row, err := db.client.TVShow.Query().Where(tvshow.TvdbIDEQ(tvdbID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	return row, err
}

// ListTVShowsStaleSince returns shows never refreshed, or last refreshed
// before cutoff. Mirrors ListMoviesStaleSince: keyed on last_refreshed_at, not
// update_time, because that column moves on every write (episode import,
// status change) and would make the refresh tick a no-op.
func (db *DB) ListTVShowsStaleSince(
	ctx context.Context,
	cutoff time.Time,
) ([]*ent.TVShow, error) {
	return db.client.TVShow.Query().
		Where(tvshow.Or(
			tvshow.LastRefreshedAtIsNil(),
			tvshow.LastRefreshedAtLT(cutoff),
		)).
		All(ctx)
}

func (db *DB) ListTVShows(
	ctx context.Context,
	offset, limit uint32,
) ([]*ent.TVShow, error) {
	return db.client.TVShow.Query().
		Order(ent.Desc(tvshow.FieldCreateTime)).
		Offset(int(offset)).Limit(int(limit)).
		WithSeasons(func(q *ent.SeasonQuery) {
			q.WithEpisodes(func(eq *ent.EpisodeQuery) { eq.WithMediaFiles() })
		}).
		All(ctx)
}

func (db *DB) CountTVShows(ctx context.Context) (int, error) {
	return db.client.TVShow.Query().Count(ctx)
}

// CountTVShowsByStatus counts shows in the given series_status. Powers the
// continuing/ended tallies on the series counts endpoint.
func (db *DB) CountTVShowsByStatus(
	ctx context.Context,
	status tvshow.SeriesStatus,
) (int, error) {
	return db.client.TVShow.Query().
		Where(tvshow.SeriesStatusEQ(status)).
		Count(ctx)
}

func (db *DB) UpdateTVShow(
	ctx context.Context,
	id uint32,
	p UpdateTVShowParams,
) (*ent.TVShow, error) {
	u := db.client.TVShow.UpdateOneID(id)
	if p.Monitored != nil {
		u = u.SetMonitored(*p.Monitored)
	}
	if p.QualityProfile != nil {
		u = u.SetQualityProfile(*p.QualityProfile)
	}
	if p.Type != nil {
		u = u.SetType(*p.Type)
	}
	if _, err := u.Save(ctx); err != nil {
		return nil, err
	}
	return db.FindTVShowByID(ctx, id)
}

func (db *DB) SetTVShowRefreshedAt(
	ctx context.Context,
	id uint32,
	when time.Time,
) error {
	return db.client.TVShow.UpdateOneID(id).SetLastRefreshedAt(when).Exec(ctx)
}

// SetTVShowTVDBID repoints a row at a different TVDB show. The season/episode
// tree still describes the old show until the caller reconciles it, so this is
// never useful on its own — see tvshow.Service.Reidentify.
func (db *DB) SetTVShowTVDBID(ctx context.Context, id, tvdbID uint32) error {
	return db.client.TVShow.UpdateOneID(id).SetTvdbID(tvdbID).Exec(ctx)
}

// DetachEpisodeMediaFiles clears the episode edge on every media file under
// show, returning the detached rows. Used by re-identify to lift the files out
// of the way before the episode tree is replaced: ReconcileEpisodes deletes
// episodes the new provider entry does not report and hands back their paths
// for deletion from disk, which for a *different* show is every file there is.
func (db *DB) DetachEpisodeMediaFiles(
	ctx context.Context,
	showID uint32,
) ([]*ent.MediaFile, error) {
	rows, err := db.client.MediaFile.Query().
		Where(mediafile.HasEpisodeWith(
			episode.HasSeasonWith(season.HasTvShowWith(tvshow.ID(showID))),
		)).
		WithEpisode(func(eq *ent.EpisodeQuery) { eq.WithSeason() }).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("query episode media files: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	ids := make([]uint32, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	if err := db.client.MediaFile.Update().
		Where(mediafile.IDIn(ids...)).
		ClearEpisode().
		Exec(ctx); err != nil {
		return nil, fmt.Errorf("detach episode media files: %w", err)
	}
	return rows, nil
}

// AttachMediaFileToEpisode re-points a detached media file at an episode.
func (db *DB) AttachMediaFileToEpisode(
	ctx context.Context,
	mediaFileID, episodeID uint32,
) error {
	return db.client.MediaFile.UpdateOneID(mediaFileID).
		SetEpisodeID(episodeID).
		Exec(ctx)
}

func (db *DB) DeleteTVShow(ctx context.Context, id uint32) error {
	return db.client.TVShow.DeleteOneID(id).Exec(ctx)
}

func (db *DB) SetSeasonMonitored(
	ctx context.Context,
	id uint32,
	monitored bool,
) error {
	return db.client.Season.UpdateOneID(id).SetMonitored(monitored).Exec(ctx)
}

func (db *DB) SetEpisodeMonitored(
	ctx context.Context,
	id uint32,
	monitored bool,
) error {
	return db.client.Episode.UpdateOneID(id).SetMonitored(monitored).Exec(ctx)
}

// CascadeShowMonitored sets every season and episode of a show to monitored,
// so toggling a series' monitor flag flows down to its whole tree (an
// unmonitored show must not leave monitored episodes for the fetcher to grab).
// Both bulk updates run in one transaction.
func (db *DB) CascadeShowMonitored(
	ctx context.Context,
	showID uint32,
	monitored bool,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if _, err := tx.Season.Update().
		Where(season.HasTvShowWith(tvshow.ID(showID))).
		SetMonitored(monitored).Save(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("cascade seasons monitored: %w", err)
	}
	if _, err := tx.Episode.Update().
		Where(episode.HasSeasonWith(season.HasTvShowWith(tvshow.ID(showID)))).
		SetMonitored(monitored).Save(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("cascade episodes monitored: %w", err)
	}
	return tx.Commit()
}

// CascadeSpecialsMonitored sets season 0 — and its episodes — across the whole
// library, retro-applying library.monitor_specials to series added before the
// toggle was flipped. Returns the number of seasons touched.
//
// Switching specials ON skips unmonitored shows: a monitored episode under an
// unmonitored show would hand the fetcher work its owner explicitly turned
// off. Switching OFF has no such hazard and applies everywhere, so a season 0
// left monitored under an unmonitored show still gets cleaned up.
func (db *DB) CascadeSpecialsMonitored(
	ctx context.Context,
	monitored bool,
) (int, error) {
	target := []predicate.Season{season.NumberEQ(0)}
	if monitored {
		target = append(
			target,
			season.HasTvShowWith(tvshow.MonitoredEQ(true)),
		)
	}
	inLibrary := season.And(target...)
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	n, err := tx.Season.Update().
		Where(inLibrary).
		SetMonitored(monitored).Save(ctx)
	if err != nil {
		tx.Rollback()
		return 0, fmt.Errorf("set specials monitored: %w", err)
	}
	if _, err := tx.Episode.Update().
		Where(episode.HasSeasonWith(inLibrary)).
		SetMonitored(monitored).Save(ctx); err != nil {
		tx.Rollback()
		return 0, fmt.Errorf("cascade specials episodes monitored: %w", err)
	}
	return n, tx.Commit()
}

// CascadeSeasonMonitored sets a season and all its episodes to monitored in one
// transaction, so a season toggle flows down to its episodes.
func (db *DB) CascadeSeasonMonitored(
	ctx context.Context,
	seasonID uint32,
	monitored bool,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if err := tx.Season.UpdateOneID(seasonID).
		SetMonitored(monitored).Exec(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("set season monitored: %w", err)
	}
	if _, err := tx.Episode.Update().
		Where(episode.HasSeasonWith(season.ID(seasonID))).
		SetMonitored(monitored).Save(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("cascade season episodes monitored: %w", err)
	}
	return tx.Commit()
}

func (db *DB) SetEpisodeStatus(
	ctx context.Context,
	id uint32,
	status episode.Status,
) error {
	return db.client.Episode.UpdateOneID(id).SetStatus(status).Exec(ctx)
}

func (db *DB) SetEpisodeLastSearchAt(
	ctx context.Context,
	id uint32,
	when time.Time,
) error {
	return db.client.Episode.UpdateOneID(id).SetLastSearchAt(when).Exec(ctx)
}

func (db *DB) IncrementEpisodeGrabFailures(ctx context.Context, id uint32) error {
	ep, err := db.client.Episode.Get(ctx, id)
	if err != nil {
		return err
	}
	return db.client.Episode.UpdateOneID(id).
		SetGrabFailures(ep.GrabFailures + 1).
		Exec(ctx)
}

func (db *DB) ResetEpisodeGrabFailures(ctx context.Context, id uint32) error {
	return db.client.Episode.UpdateOneID(id).SetGrabFailures(0).Exec(ctx)
}

// ListEligibleEpisodesForSync is the TV twin of ListEligibleMoviesForSync:
// shows whose episode edges are narrowed to the rows a missing-search pass
// may act on — wanted, monitored, under the failure cap, past their cooldown
// window (or never searched), already aired, and with no in-flight
// download_record. Episodes without an air date are excluded: a provider
// announces a future season as dateless placeholders, so "no date" reliably
// means "not out yet" and searching for one burns a slot every tick forever.
func (db *DB) ListEligibleEpisodesForSync(
	ctx context.Context,
	maxGrabFailures uint8,
	notSearchedSince time.Time,
	airedBefore time.Time,
) ([]*ent.TVShow, error) {
	eligible := []predicate.Episode{
		episode.MonitoredEQ(true),
		episode.StatusEQ(episode.StatusWanted),
		episode.GrabFailuresLT(maxGrabFailures),
		episode.Or(
			episode.LastSearchAtIsNil(),
			episode.LastSearchAtLT(notSearchedSince),
		),
		episode.AirDateNotNil(),
		episode.AirDateLTE(airedBefore),
		episode.Not(episode.HasDownloadRecordsWith(
			downloadrecord.StatusIn(
				downloadrecord.StatusDownloading,
				downloadrecord.StatusImporting,
			),
		)),
	}
	return db.client.TVShow.Query().
		Where(tvshow.HasSeasonsWith(season.HasEpisodesWith(eligible...))).
		WithSeasons(func(q *ent.SeasonQuery) {
			q.Order(ent.Asc(season.FieldNumber)).
				WithEpisodes(func(eq *ent.EpisodeQuery) {
					eq.Where(eligible...).Order(ent.Asc(episode.FieldNumber))
				})
		}).
		All(ctx)
}

// ListUpgradeCandidateShows is the TV twin of ListUpgradeCandidateMovies:
// shows whose episode edges are narrowed to the rows an upgrade may replace —
// monitored, already holding a file, and with nothing in flight. The media
// files come loaded, since the feed scanner scores what is on disk against
// the incoming release. An episode mid-grab is excluded by its download
// record rather than by episode.status, matching the sibling
// ListEligibleEpisodesForSync: the status write after a grab is best-effort
// (markEpisodeDownloading logs and continues), so a status-only filter would
// re-grab an episode every tick once that one write had failed.
func (db *DB) ListUpgradeCandidateShows(ctx context.Context) ([]*ent.TVShow, error) {
	candidates := []predicate.Episode{
		episode.MonitoredEQ(true),
		episode.HasMediaFiles(),
		episode.Not(episode.HasDownloadRecordsWith(
			downloadrecord.StatusIn(
				downloadrecord.StatusDownloading,
				downloadrecord.StatusImporting,
			),
		)),
	}
	return db.client.TVShow.Query().
		Where(tvshow.HasSeasonsWith(season.HasEpisodesWith(candidates...))).
		WithSeasons(func(q *ent.SeasonQuery) {
			q.Order(ent.Asc(season.FieldNumber)).
				WithEpisodes(func(eq *ent.EpisodeQuery) {
					eq.Where(candidates...).
						Order(ent.Asc(episode.FieldNumber)).
						WithMediaFiles()
				})
		}).
		All(ctx)
}

// SeasonEpisodeCounts totals every season's episodes for the given shows,
// unfiltered. It is the denominator a pack's size is measured against: a
// season pack makes us download all of it whether or not we wanted every
// episode, so the bound has to be the season's real length. The scanners'
// own episode indexes are filtered to what a pass cares about — monitored,
// missing, upgrade-eligible — and are the wrong number for this.
//
// A season absent from the map is one the library tracks no episodes for;
// callers must read that as unknown, not as zero-length.
func (db *DB) SeasonEpisodeCounts(
	ctx context.Context,
	showIDs []uint32,
) (map[uint32]map[uint16]int, error) {
	out := make(map[uint32]map[uint16]int, len(showIDs))
	if len(showIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		ShowID uint32 `sql:"show_id"`
		Number uint16 `sql:"number"`
		Total  int    `sql:"total"`
	}
	err := db.client.Episode.Query().
		Where(episode.HasSeasonWith(season.HasTvShowWith(tvshow.IDIn(showIDs...)))).
		Modify(func(s *entsql.Selector) {
			b := entsql.Dialect(s.Dialect())
			se := b.Table(season.Table).As("cnt_season")
			s.Join(se).On(s.C(episode.SeasonColumn), se.C(season.FieldID))
			s.Select(
				entsql.As(se.C(season.TvShowColumn), "show_id"),
				entsql.As(se.C(season.FieldNumber), "number"),
				entsql.As("COUNT(*)", "total"),
			).GroupBy(se.C(season.TvShowColumn), se.C(season.FieldNumber))
		}).
		Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("season episode counts: %w", err)
	}
	for _, r := range rows {
		if out[r.ShowID] == nil {
			out[r.ShowID] = make(map[uint16]int)
		}
		out[r.ShowID][r.Number] = r.Total
	}
	return out, nil
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// ListUpcomingEpisodes returns monitored episodes whose air_date falls within
// [from, to], oldest first, with season + show eager-loaded for the calendar.
func (db *DB) ListUpcomingEpisodes(
	ctx context.Context,
	from, to time.Time,
) ([]*ent.Episode, error) {
	return db.client.Episode.Query().
		Where(
			episode.MonitoredEQ(true),
			episode.AirDateGTE(from),
			episode.AirDateLTE(to),
		).
		WithSeason(func(q *ent.SeasonQuery) { q.WithTvShow() }).
		WithMediaFiles().
		Order(ent.Asc(episode.FieldAirDate)).
		All(ctx)
}
