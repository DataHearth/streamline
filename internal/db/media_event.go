package db

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/episode"
	"github.com/datahearth/streamline/ent/mediaevent"
	"github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/ent/season"
	"github.com/datahearth/streamline/ent/tvshow"
)

type ActivityFilter struct {
	Types    []mediaevent.Type
	MovieID  *uint32
	SeriesID *uint32
	Since    *time.Time
	Before   *time.Time
	Limit    int
	Cursor   string
}

type ActivityResult struct {
	Events     []*ent.MediaEvent
	NextCursor string
}

const defaultActivityLimit = 50

func (db *DB) RecentActivity(
	ctx context.Context,
	f ActivityFilter,
) (*ActivityResult, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = defaultActivityLimit
	}

	// An episode row renders as "Show — S01E03", so the season and its show
	// come along with it; without them the feed can only name a number.
	q := db.client.MediaEvent.Query().
		Order(ent.Desc(mediaevent.FieldCreateTime), ent.Desc(mediaevent.FieldID)).
		WithMovie().
		WithTvShow().
		WithEpisode(func(eq *ent.EpisodeQuery) {
			eq.WithSeason(func(sq *ent.SeasonQuery) { sq.WithTvShow() })
		})

	if len(f.Types) > 0 {
		q = q.Where(mediaevent.TypeIn(f.Types...))
	}
	if f.MovieID != nil {
		q = q.Where(mediaevent.HasMovieWith(movie.ID(*f.MovieID)))
	}
	// A series' history is split across the show itself (season-scoped
	// searches) and its episodes, so both sides are matched.
	if f.SeriesID != nil {
		q = q.Where(mediaevent.Or(
			mediaevent.HasTvShowWith(tvshow.ID(*f.SeriesID)),
			mediaevent.HasEpisodeWith(
				episode.HasSeasonWith(season.HasTvShowWith(tvshow.ID(*f.SeriesID))),
			),
		))
	}
	if f.Since != nil {
		q = q.Where(mediaevent.CreateTimeGTE(*f.Since))
	}
	if f.Before != nil {
		q = q.Where(mediaevent.CreateTimeLT(*f.Before))
	}
	if f.Cursor != "" {
		ts, id, err := decodeActivityCursor(f.Cursor)
		if err != nil {
			return nil, fmt.Errorf("recent activity: decode cursor: %w", err)
		}
		q = q.Where(
			mediaevent.Or(
				mediaevent.CreateTimeLT(ts),
				mediaevent.And(
					mediaevent.CreateTimeEQ(ts),
					mediaevent.IDLT(id),
				),
			),
		)
	}

	rows, err := q.Limit(limit + 1).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("recent activity: query: %w", err)
	}

	res := &ActivityResult{}
	if len(rows) > limit {
		res.Events = rows[:limit]
		last := res.Events[limit-1]
		res.NextCursor = encodeActivityCursor(last.CreateTime, last.ID)
	} else {
		res.Events = rows
	}
	return res, nil
}

func encodeActivityCursor(t time.Time, id uint32) string {
	raw := fmt.Sprintf("%d|%d", t.UnixNano(), id)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeActivityCursor(s string) (time.Time, uint32, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, 0, err
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, 0, errors.New("malformed cursor")
	}
	ns, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, 0, err
	}
	id, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return time.Time{}, 0, err
	}
	return time.Unix(0, ns).UTC(), uint32(id), nil
}
