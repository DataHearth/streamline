package db

import (
	"context"
	"fmt"
	"slices"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/episode"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/predicate"
	"github.com/datahearth/streamline/ent/season"
	"github.com/datahearth/streamline/ent/tvshow"
)

// tvShowListColumns mirrors movieListColumns: every TVShow column except the
// cast blob, which the list view never renders and which ent otherwise
// decodes per row. Subtraction rather than enumeration so a new field lands
// in the list by default.
var tvShowListColumns = slices.DeleteFunc(
	slices.Clone(tvshow.Columns),
	func(c string) bool { return c == tvshow.FieldCast },
)

type FilterTVShowsParams struct {
	// Status is a series_status value, "missing" (has an aired, monitored
	// episode with no file), or "" / "all".
	Status string
	Type   string
	Query  string
	Sort   string
	Order  string
	Offset uint32
	Limit  uint16
	// Now anchors the aired/unaired split. Passed in so a caller can render a
	// consistent view across the filter and the counts.
	Now time.Time
}

// EpisodeCounts is the season/episode rollup the series list renders. It is
// what the list endpoint needs from the episode tree, and all it needs — the
// tree itself is loaded only by the detail view.
type EpisodeCounts struct {
	Total   uint32
	Have    uint32
	Wanted  uint32
	Unaired uint32
}

// FilterTVShows applies every filter, the sort and the page in SQL, and
// returns the page's shows *without* their season/episode tree. The list view
// wants three numbers per show, not the tree: eager-loading it cost ~121 KB
// per show and made a 23-show library a 2.8 MB response.
func (db *DB) FilterTVShows(
	ctx context.Context,
	p FilterTVShowsParams,
) ([]*ent.TVShow, map[uint32]EpisodeCounts, uint32, error) {
	base := db.client.TVShow.Query()
	if p.Type != "" {
		base = base.Where(tvshow.TypeEQ(tvshow.Type(p.Type)))
	}
	if p.Query != "" {
		base = base.Where(tvshow.TitleContainsFold(p.Query))
	}
	switch p.Status {
	case "", "all":
	case "missing":
		base = base.Where(tvshow.HasSeasonsWith(
			season.HasEpisodesWith(missingEpisode(p.Now)),
		))
	default:
		base = base.Where(tvshow.SeriesStatusEQ(tvshow.SeriesStatus(p.Status)))
	}

	total, err := base.Clone().Count(ctx)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("count tv shows: %w", err)
	}

	q := base.Offset(int(p.Offset)).Limit(int(p.Limit))
	// Each key carries the direction its name implies — the sort menu offers
	// "Year (newest)", "Rating (highest)", "Most episodes", so ascending is the
	// wrong answer to every one of them but "Title A–Z". An explicit order
	// overrides it.
	switch p.Sort {
	case "title":
		q = q.Order(orderBy(tvshow.FieldTitle, descending(p.Order, false)))
	case "year":
		q = q.Order(orderBy(tvshow.FieldYear, descending(p.Order, true)))
	case "rating":
		q = q.Order(orderBy(tvshow.FieldRating, descending(p.Order, true)))
	case "episodes":
		q = q.Order(orderByEpisodeCount(descending(p.Order, true)))
	default:
		q = q.Order(orderBy(tvshow.FieldCreateTime, descending(p.Order, true)))
	}

	rows, err := q.Select(tvShowListColumns...).All(ctx)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("list tv shows: %w", err)
	}
	ids := make([]uint32, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	counts, err := db.episodeCounts(ctx, ids, p.Now)
	if err != nil {
		return nil, nil, 0, err
	}
	return rows, counts, uint32(total), nil
}

func orderBy(field string, desc bool) tvshow.OrderOption {
	if desc {
		return ent.Desc(field)
	}
	return ent.Asc(field)
}

// descending resolves an explicit order against the sort key's natural one.
func descending(order string, naturally bool) bool {
	switch order {
	case "asc":
		return false
	case "desc":
		return true
	default:
		return naturally
	}
}

// missingEpisode matches an episode the library wants but does not have: it is
// monitored, has no file, and has aired. Mirrors DeriveSeasonViews' Missing
// bucket — the two must agree, or a show appears under the "missing" filter
// with nothing missing on its page. An undated episode is unaired, not
// missing.
func missingEpisode(now time.Time) predicate.Episode {
	return episode.And(
		episode.MonitoredEQ(true),
		episode.Not(episode.HasMediaFiles()),
		episode.AirDateNotNil(),
		episode.AirDateLTE(now),
	)
}

// TVShowStatusCounts returns the number of shows in each series_status, in one
// GROUP BY pass. A status with no rows is absent, which reads back as 0.
func (db *DB) TVShowStatusCounts(
	ctx context.Context,
) (map[tvshow.SeriesStatus]int, error) {
	var rows []struct {
		SeriesStatus tvshow.SeriesStatus `json:"series_status"`
		Count        int                 `json:"count"`
	}
	err := db.client.TVShow.Query().
		GroupBy(tvshow.FieldSeriesStatus).
		Aggregate(ent.Count()).
		Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("tv show status counts: %w", err)
	}
	out := make(map[tvshow.SeriesStatus]int, len(rows))
	for _, r := range rows {
		out[r.SeriesStatus] = r.Count
	}
	return out, nil
}

// CountTVShowsMissing counts shows with at least one aired, monitored episode
// that has no file — the population behind the list's "missing" filter, so it
// uses the same predicate rather than a second definition that could drift.
func (db *DB) CountTVShowsMissing(ctx context.Context, now time.Time) (int, error) {
	n, err := db.client.TVShow.Query().
		Where(tvshow.HasSeasonsWith(
			season.HasEpisodesWith(missingEpisode(now)),
		)).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("count missing tv shows: %w", err)
	}
	return n, nil
}

// orderByEpisodeCount sorts by the number of episodes without loading any. The
// count is a correlated subquery rather than a join so the page's LIMIT still
// applies to shows, not to episode rows.
//
// It counts what EpisodeCounts.Total counts — an episode is in scope when it is
// monitored or already on disk. Ranking raw rows instead put a daily soap with
// 2012 provider rows above a show the card said had more, because the card was
// showing the scoped 26.
func orderByEpisodeCount(desc bool) tvshow.OrderOption {
	return func(s *entsql.Selector) {
		dir := "ASC"
		if desc {
			dir = "DESC"
		}
		s.OrderExpr(entsql.Raw(fmt.Sprintf(
			"(SELECT COUNT(*) FROM %s AS oc_e "+
				"JOIN %s AS oc_s ON oc_e.%s = oc_s.%s "+
				"WHERE oc_s.%s = %s "+
				"AND (oc_e.%s = 1 OR EXISTS ("+
				"SELECT 1 FROM %s AS oc_f WHERE oc_f.%s = oc_e.%s))) %s",
			episode.Table, season.Table,
			episode.SeasonColumn, season.FieldID,
			season.TvShowColumn, s.C(tvshow.FieldID),
			episode.FieldMonitored,
			mediafile.Table, mediafile.EpisodeColumn, episode.FieldID,
			dir,
		)))
	}
}

// episodeCounts rolls up total/have/wanted/unaired per show for the given ids
// in one pass over a lean episode projection — id, show, monitored, air date
// and whether a file exists. No media_file rows are materialised.
func (db *DB) episodeCounts(
	ctx context.Context,
	showIDs []uint32,
	now time.Time,
) (map[uint32]EpisodeCounts, error) {
	out := make(map[uint32]EpisodeCounts, len(showIDs))
	if len(showIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		ShowID    uint32    `sql:"show_id"`
		Monitored bool      `sql:"monitored"`
		AirDate   time.Time `sql:"air_date"`
		HasFile   bool      `sql:"has_file"`
	}
	err := db.client.Episode.Query().
		Where(episode.HasSeasonWith(season.HasTvShowWith(tvshow.IDIn(showIDs...)))).
		Modify(func(s *entsql.Selector) {
			b := entsql.Dialect(s.Dialect())
			se := b.Table(season.Table).As("cnt_season")
			mf := b.Table(mediafile.Table).As("cnt_file")
			s.Join(se).On(s.C(episode.SeasonColumn), se.C(season.FieldID))
			hasFile := fmt.Sprintf(
				"EXISTS (SELECT 1 FROM %s AS %s WHERE %s = %s)",
				mediafile.Table, "cnt_file",
				mf.C(mediafile.EpisodeColumn), s.C(episode.FieldID),
			)
			s.Select(
				entsql.As(se.C(season.TvShowColumn), "show_id"),
				s.C(episode.FieldMonitored),
				s.C(episode.FieldAirDate),
				entsql.As(hasFile, "has_file"),
			)
		}).
		Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("episode counts: %w", err)
	}

	for _, r := range rows {
		c := out[r.ShowID]
		if !r.Monitored && !r.HasFile {
			out[r.ShowID] = c
			continue
		}
		c.Total++
		switch {
		case r.HasFile:
			c.Have++
		case r.AirDate.IsZero() || r.AirDate.After(now):
			c.Unaired++
		default:
			c.Wanted++
		}
		out[r.ShowID] = c
	}
	return out, nil
}

// CountWantedEpisodes counts monitored, wanted episodes across the library.
// A COUNT, not a walk: the shape this replaced loaded every matching show with
// its seasons, episodes and media files purely to take len() of the result.
func (db *DB) CountWantedEpisodes(ctx context.Context) (int, error) {
	n, err := db.client.Episode.Query().
		Where(
			episode.MonitoredEQ(true),
			episode.StatusEQ(episode.StatusWanted),
		).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("count wanted episodes: %w", err)
	}
	return n, nil
}

// CountDownloadingEpisodes counts episodes with a grab in flight. Only
// `downloading` — `importing` and `paused` are in flight too but neither is
// moving bytes, and this feeds a tile that reads as "what is coming down now".
func (db *DB) CountDownloadingEpisodes(ctx context.Context) (int, error) {
	n, err := db.client.Episode.Query().
		Where(episode.StatusEQ(episode.StatusDownloading)).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("count downloading episodes: %w", err)
	}
	return n, nil
}
