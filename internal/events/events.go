package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/mediaevent"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

var (
	tracer = otel.Tracer("github.com/datahearth/streamline/internal/events")
	meter  = otel.Meter("github.com/datahearth/streamline/internal/events")

	recordFailures metric.Int64Counter
)

func init() {
	recordFailures = otelx.Must(
		meter.Int64Counter("streamline.events.record.failures"),
	)
}

// Scope names which entity an event hangs off. A MediaEvent has three
// optional owner edges and exactly one must be set; Scope is how a caller
// says which, so no call site can write a row with none or two.
type Scope uint8

const (
	ScopeMovie Scope = iota
	ScopeEpisode
	// ScopeSeries carries only what belongs to no single episode — a search
	// issued at series or season scope. Per-episode outcomes use ScopeEpisode.
	ScopeSeries
)

// logKey is also the span/log attribute name for the owner id.
func (s Scope) logKey() string {
	switch s {
	case ScopeEpisode:
		return "episode.id"
	case ScopeSeries:
		return "tvshow.id"
	default:
		return "movie.id"
	}
}

func (s Scope) String() string {
	switch s {
	case ScopeEpisode:
		return "episode"
	case ScopeSeries:
		return "series"
	default:
		return "movie"
	}
}

// Record writes a MediaEvent row against the entity named by scope/ownerID.
// When client is nil the package default (set by Register at db-client
// construction) is used. Pass the bound client from a mutation (m.Client())
// or transaction (tx.Client()) to participate in an existing tx — ent routes
// the write through the tx automatically.
func Record(
	ctx context.Context,
	client *ent.Client,
	t Type,
	scope Scope,
	ownerID uint32,
	payload map[string]any,
) error {
	ctx, span := tracer.Start(ctx, "events.record", trace.WithAttributes(
		attribute.String("event.type", string(t)),
		attribute.Int64(scope.logKey(), int64(ownerID)),
	))
	defer span.End()

	if !t.Valid() {
		return otelx.RecordSpanError(
			span, fmt.Errorf("events: invalid type: %q", t),
		)
	}
	if ownerID == 0 {
		return otelx.RecordSpanError(
			span, fmt.Errorf("events: %s event with no %s", t, scope),
		)
	}
	c := client
	if c == nil {
		c = defaultClient
	}
	if c == nil {
		return otelx.RecordSpanError(span, errors.New(
			"events: no client (Register not called and explicit client nil)",
		))
	}
	q := c.MediaEvent.Create().SetType(mediaevent.Type(t))
	switch scope {
	case ScopeEpisode:
		q = q.SetEpisodeID(ownerID)
	case ScopeSeries:
		q = q.SetTvShowID(ownerID)
	default:
		q = q.SetMovieID(ownerID)
	}
	if payload != nil {
		q = q.SetPayload(payload)
	}
	if _, err := q.Save(ctx); err != nil {
		slog.ErrorContext(
			ctx,
			"failed to record event",
			"event.type",
			string(t),
			scope.logKey(),
			ownerID,
			"error",
			err,
		)
		recordFailures.Add(ctx, 1, metric.WithAttributes(
			attribute.String("event.type", string(t)),
		))
		return otelx.RecordSpanError(span, fmt.Errorf(
			"events: record %s for %s %d: %w", t, scope, ownerID, err,
		))
	}
	slog.InfoContext(
		ctx,
		"event recorded",
		"event.type",
		string(t),
		scope.logKey(),
		ownerID,
	)
	return nil
}

var defaultClient *ent.Client

// PurgeOldEvents deletes MediaEvent rows whose create_time is older
// than (now - retention). Returns the number of rows deleted.
func PurgeOldEvents(ctx context.Context, retention time.Duration) (int, error) {
	ctx, span := tracer.Start(ctx, "events.purge_old")
	defer span.End()

	if defaultClient == nil {
		return 0, otelx.RecordSpanError(
			span, errors.New("events: default client not registered"),
		)
	}
	cutoff := time.Now().Add(-retention)
	n, err := defaultClient.MediaEvent.Delete().
		Where(mediaevent.CreateTimeLT(cutoff)).
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
		return 0, otelx.RecordSpanError(span, fmt.Errorf(
			"events: purge older than %s: %w",
			cutoff.Format(time.RFC3339), err,
		))
	}
	span.SetAttributes(attribute.Int("events.purged", n))
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
