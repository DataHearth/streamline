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
	"github.com/datahearth/streamline/internal/utils/numeric"
)

// tvShowListColumns mirrors movieListColumns: every TVShow column except the
// cast blob, which the list view never renders and which ent otherwise
// decodes per row. Subtraction rather than enumeration so a new field lands
// in the list by default.
var tvShowListColumns = slices.DeleteFunc(
	slices.Clone(tvshow.Columns),
	func(c string) bool { return c == tvshow.FieldCast },
)

// Status values the list accepts beyond series_status and "missing": a show
// is downloading or importing when any of its episodes is, which is the same
// rule the library card badges by.
const (
	statusDownloading = "downloading"
	statusImporting   = "importing"
)

type FilterTVShowsParams struct {
	// Status is a series_status value, "missing" (has an aired, monitored
	// episode with no file), "downloading", "importing", or "" / "all".
	Status string
	Type   string
	Query  string
	Sort   string
	Order  string
	Offset uint32
	Limit  uint16
	// Monitored filters on the show's own flag; nil means "either".
	Monitored *bool
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
	// Downloading and Importing count what the show has in flight. Both cut
	// across the buckets above rather than replacing one — neither has a file
	// yet, so both are still Wanted.
	Downloading uint32
	Importing   uint32
	// Scope is what the in-flight grab covers, which is what the library card
	// names instead of the gaps behind it: "episode", "season" (one season,
	// several episodes) or "series" (spanning seasons). Empty when nothing is
	// in flight. Season is set for the first two, Episode for "episode" only.
	//
	// Derived from the in-flight episode rows rather than from the record's
	// release title: an operator can grab a pack in two goes, and what the
	// card should say is what is actually landing.
	Scope   string
	Season  uint16
	Episode uint16
	// InFlightEpisodes are the ids behind those counts, so a caller holding
	// the live queue can find this show's entries without a second query.
	InFlightEpisodes []uint32
}

// In-flight scopes, mirroring the API's SeriesDownloadScope.
const (
	scopeEpisode = "episode"
	scopeSeason  = "season"
	scopeSeries  = "series"
)

// widen folds one in-flight episode into the show's scope: the first row sets
// "episode", a second in the same season promotes to "season", and one in
// another season to "series". Season and Episode are cleared as they stop
// being true of the whole set, so a caller never reads a number the scope
// does not license.
func (c *EpisodeCounts) widen(seasonNo, number uint16, id uint32) {
	c.InFlightEpisodes = append(c.InFlightEpisodes, id)
	switch {
	case c.Scope == "":
		c.Scope, c.Season, c.Episode = scopeEpisode, seasonNo, number
	case c.Season != seasonNo:
		c.Scope, c.Season, c.Episode = scopeSeries, 0, 0
	case c.Scope == scopeEpisode:
		c.Scope, c.Episode = scopeSeason, 0
	}
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
	if p.Monitored != nil {
		base = base.Where(tvshow.MonitoredEQ(*p.Monitored))
	}
	switch p.Status {
	case "", "all":
	case "missing":
		base = base.Where(tvshow.HasSeasonsWith(
			season.HasEpisodesWith(missingEpisode(p.Now)),
		))
	case statusDownloading, statusImporting:
		base = base.Where(tvshow.HasSeasonsWith(
			season.HasEpisodesWith(episode.StatusEQ(episode.Status(p.Status))),
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
	return rows, counts, numeric.SaturateU32(total), nil
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

// CountTVShowsInFlight counts shows holding at least one episode in the given
// state — the per-show population behind the list's downloading and importing
// tabs, matching what FilterTVShows selects for the same status.
func (db *DB) CountTVShowsInFlight(
	ctx context.Context,
	status episode.Status,
) (int, error) {
	n, err := db.client.TVShow.Query().
		Where(tvshow.HasSeasonsWith(
			season.HasEpisodesWith(episode.StatusEQ(status)),
		)).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("count %s tv shows: %w", status, err)
	}
	return n, nil
}

// CountTVShowsMonitored counts shows whose own monitored flag is set. The
// unmonitored tally is the library total minus this, so the facet costs one
// query rather than two.
func (db *DB) CountTVShowsMonitored(ctx context.Context) (int, error) {
	n, err := db.client.TVShow.Query().
		Where(tvshow.MonitoredEQ(true)).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("count monitored tv shows: %w", err)
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
		EpisodeID uint32    `sql:"episode_id"`
		Season    uint16    `sql:"season_number"`
		Number    uint16    `sql:"number"`
		Monitored bool      `sql:"monitored"`
		AirDate   time.Time `sql:"air_date"`
		Status    string    `sql:"status"`
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
				entsql.As(s.C(episode.FieldID), "episode_id"),
				entsql.As(se.C(season.FieldNumber), "season_number"),
				s.C(episode.FieldNumber),
				s.C(episode.FieldMonitored),
				s.C(episode.FieldAirDate),
				s.C(episode.FieldStatus),
				entsql.As(hasFile, "has_file"),
			)
		}).
		Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("episode counts: %w", err)
	}

	for _, r := range rows {
		c := out[r.ShowID]
		// In flight is tallied before the scope guard below: a grab that is
		// landing is worth naming whether or not its episode is monitored,
		// and the card would otherwise stay silent through the whole import.
		switch r.Status {
		case string(episode.StatusDownloading):
			c.Downloading++
			c.widen(r.Season, r.Number, r.EpisodeID)
		case string(episode.StatusImporting):
			c.Importing++
			c.widen(r.Season, r.Number, r.EpisodeID)
		}
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
