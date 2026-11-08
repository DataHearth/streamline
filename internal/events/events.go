package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
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

// Scope names which entity an event hangs off. A MediaEvent has seven
// optional owner edges and exactly one must be set; Scope is how a caller
// says which, so no call site can write a row with none or two.
type Scope uint8

const (
	ScopeMovie Scope = iota
	ScopeEpisode
	// ScopeSeries carries only what belongs to no single episode — a search
	// issued at series or season scope. Per-episode outcomes use ScopeEpisode.
	ScopeSeries
	// ScopeAlbum carries an album's lifecycle: grabs, imports, removals and
	// searches. An album is the unit a grab and an import cover, so no event
	// is ever filed against a single track.
	ScopeAlbum
	// ScopeArtist carries what belongs to no single album — a discography
	// pack, the artist entering the library.
	ScopeArtist
	// ScopeBook carries a book's lifecycle, either slot; the payload's
	// book_kind says which.
	ScopeBook
	// ScopeBookSeries carries what belongs to a series as a whole: the series
	// being added, a series-wide search.
	ScopeBookSeries
)

// logKey is also the span/log attribute name for the owner id.
func (s Scope) logKey() string {
	switch s {
	case ScopeEpisode:
		return "episode.id"
	case ScopeSeries:
		return "tvshow.id"
	case ScopeAlbum:
		return "album.id"
	case ScopeArtist:
		return "artist.id"
	case ScopeBook:
		return "book.id"
	case ScopeBookSeries:
		return "book_series.id"
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
	case ScopeAlbum:
		return "album"
	case ScopeArtist:
		return "artist"
	case ScopeBook:
		return "book"
	case ScopeBookSeries:
		return "book series"
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
	case ScopeAlbum:
		q = q.SetAlbumID(ownerID)
	case ScopeArtist:
		q = q.SetArtistID(ownerID)
	case ScopeBook:
		q = q.SetBookID(ownerID)
	case ScopeBookSeries:
		q = q.SetBookSeriesID(ownerID)
	default:
		q = q.SetMovieID(ownerID)
	}
	payload = withDownloadID(ctx, payload)
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

// PayloadDownloadID is the payload key naming the download record an event
// came from: a grab, the download completing or failing, a hold, and every
// event its import records (the imported row, a replaced file's removal). The
// activity feed hands it on as ActivityEvent.download_id, which is what links
// a history row to the record's detail on Activity.
const PayloadDownloadID = "download_id"

type downloadRecordKey struct{}

// WithDownloadRecord marks ctx as working on behalf of one download record, so
// every event recorded under it names that record. The importer sets it per
// record: the imported events of a movie or an episode come from the media
// file create hook, which cannot see the record behind the file.
func WithDownloadRecord(ctx context.Context, id uint32) context.Context {
	if id == 0 {
		return ctx
	}
	return context.WithValue(ctx, downloadRecordKey{}, id)
}

func downloadRecordFrom(ctx context.Context) uint32 {
	v, _ := ctx.Value(downloadRecordKey{}).(uint32)
	return v
}

// withDownloadID returns payload with the ctx's download record in it, unless
// the caller named one already. A copy: a caller's map is not ours to change.
func withDownloadID(ctx context.Context, payload map[string]any) map[string]any {
	id := downloadRecordFrom(ctx)
	if id == 0 {
		return payload
	}
	if _, ok := payload[PayloadDownloadID]; ok {
		return payload
	}
	out := maps.Clone(payload)
	if out == nil {
		out = map[string]any{}
	}
	out[PayloadDownloadID] = id
	return out
}

// DownloadID reads an event payload's download record back, or 0. A payload
// round-trips through JSON, so the number comes back a float64.
func DownloadID(payload map[string]any) uint32 {
	switch v := payload[PayloadDownloadID].(type) {
	case float64:
		if v > 0 && v <= float64(^uint32(0)) && v == float64(uint32(v)) {
			return uint32(v)
		}
	case uint32:
		return v
	case int:
		if v > 0 && int64(v) <= int64(^uint32(0)) {
			return uint32(v)
		}
	}
	return 0
}

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
