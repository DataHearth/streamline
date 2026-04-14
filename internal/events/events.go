package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/movieevent"
)

var ErrInvalidType = errors.New("events: invalid type")

// Record writes a MovieEvent row. When client is nil the package
// default (set by Register at db-client construction) is used. Pass
// the bound client from a mutation (m.Client()) or transaction
// (tx.Client()) to participate in an existing tx — ent routes the
// write through the tx automatically.
func Record(
	ctx context.Context,
	client *ent.Client,
	t Type,
	movieID uint32,
	payload map[string]any,
) error {
	if !t.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidType, t)
	}
	c := client
	if c == nil {
		c = defaultClient
	}
	if c == nil {
		return errors.New(
			"events: no client (Register not called and explicit client nil)",
		)
	}
	q := c.MovieEvent.Create().
		SetType(movieevent.Type(t)).
		SetMovieID(movieID)
	if payload != nil {
		q = q.SetPayload(payload)
	}
	if _, err := q.Save(ctx); err != nil {
		slog.ErrorContext(
			ctx,
			"failed to record event",
			"event.type",
			string(t),
			"movie.id",
			movieID,
			"error",
			err,
		)
		return fmt.Errorf("events: record %s for movie %d: %w", t, movieID, err)
	}
	slog.InfoContext(
		ctx,
		"event recorded",
		"event.type",
		string(t),
		"movie.id",
		movieID,
	)
	return nil
}

var defaultClient *ent.Client

// PurgeOldEvents deletes MovieEvent rows whose create_time is older
// than (now - retention). Returns the number of rows deleted.
func PurgeOldEvents(ctx context.Context, retention time.Duration) (int, error) {
	if defaultClient == nil {
		return 0, errors.New("events: default client not registered")
	}
	cutoff := time.Now().Add(-retention)
	n, err := defaultClient.MovieEvent.Delete().
		Where(movieevent.CreateTimeLT(cutoff)).
		Exec(ctx)
	if err != nil {
		slog.ErrorContext(
			ctx,
			"failed to purge old events",
			"cutoff",
			cutoff.Format(time.RFC3339),
			"error",
			err,
		)
		return 0, fmt.Errorf(
			"events: purge older than %s: %w",
			cutoff.Format(time.RFC3339), err,
		)
	}
	slog.InfoContext(
		ctx,
		"purged old events",
		"count",
		n,
		"cutoff",
		cutoff.Format(time.RFC3339),
	)
	return n, nil
}
