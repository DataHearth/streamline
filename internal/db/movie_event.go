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
	"github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/ent/movieevent"
)

type ActivityFilter struct {
	Types   []movieevent.Type
	MovieID *uint32
	Since   *time.Time
	Before  *time.Time
	Limit   int
	Cursor  string
}

type ActivityResult struct {
	Events     []*ent.MovieEvent
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

	q := db.client.MovieEvent.Query().
		Order(ent.Desc(movieevent.FieldCreateTime), ent.Desc(movieevent.FieldID)).
		WithMovie()

	if len(f.Types) > 0 {
		q = q.Where(movieevent.TypeIn(f.Types...))
	}
	if f.MovieID != nil {
		q = q.Where(movieevent.HasMovieWith(movie.ID(*f.MovieID)))
	}
	if f.Since != nil {
		q = q.Where(movieevent.CreateTimeGTE(*f.Since))
	}
	if f.Before != nil {
		q = q.Where(movieevent.CreateTimeLT(*f.Before))
	}
	if f.Cursor != "" {
		ts, id, err := decodeActivityCursor(f.Cursor)
		if err != nil {
			return nil, fmt.Errorf("recent activity: decode cursor: %w", err)
		}
		q = q.Where(
			movieevent.Or(
				movieevent.CreateTimeLT(ts),
				movieevent.And(
					movieevent.CreateTimeEQ(ts),
					movieevent.IDLT(id),
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
