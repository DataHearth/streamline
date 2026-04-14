package db

import (
	"context"
	"fmt"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/episode"
	"github.com/datahearth/streamline/ent/season"
	"github.com/datahearth/streamline/ent/tvshow"
)

type EpisodeSeed struct {
	Number         uint16
	AbsoluteNumber uint16
	Title          string
	AirDate        *time.Time
}

type SeasonSeed struct {
	Number   uint16
	Name     string
	Episodes []EpisodeSeed
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
	PosterPath     string
	QualityProfile string
	Seasons        []SeasonSeed
}

type UpdateTVShowParams struct {
	Monitored      *bool
	QualityProfile *string
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
	Type          string
	Runtime       uint16
	Rating        float64
	Genres        []string
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
	if p.SeriesStatus != "" {
		u = u.SetSeriesStatus(tvshow.SeriesStatus(p.SeriesStatus))
	}
	if p.Type != "" {
		u = u.SetType(tvshow.Type(p.Type))
	}
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
// preserved; new episodes inherit their season's monitored flag, a brand-new
// season defaults to monitored.
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
					SetMonitored(sr.Monitored).
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
			if e.AirDate != nil && !e.AirDate.Equal(er.AirDate) {
				u, changed = u.SetAirDate(*e.AirDate), true
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

// ListWantedEpisodes returns shows (with seasons+episodes eager-loaded) that
// have at least one monitored, wanted episode. The episode edges are filtered
// to only those wanted+monitored rows; the caller (rss/missing searcher)
// applies the aired-date cutoff.
func (db *DB) ListWantedEpisodes(ctx context.Context) ([]*ent.TVShow, error) {
	return db.client.TVShow.Query().
		Where(tvshow.HasSeasonsWith(
			season.HasEpisodesWith(
				episode.MonitoredEQ(true),
				episode.StatusEQ(episode.StatusWanted),
			),
		)).
		WithSeasons(func(q *ent.SeasonQuery) {
			q.WithEpisodes(func(eq *ent.EpisodeQuery) {
				eq.Where(episode.MonitoredEQ(true), episode.StatusEQ(episode.StatusWanted)).
					WithMediaFiles()
			})
		}).
		All(ctx)
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
		Order(ent.Asc(episode.FieldAirDate)).
		All(ctx)
}
