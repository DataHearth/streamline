package music

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/scheduler"
)

const (
	// linksBatch is the page size of one listing.
	linksBatch = 50
	// linksRetries is how often a rate-limited row is asked again.
	linksRetries = 3
	// linksMaxWait caps a wait; a longer one ends the pass.
	linksMaxWait = 5 * time.Minute
)

// errLinksStop ends a pass early; the next run resumes.
var errLinksStop = errors.New("links backfill stopped")

// backfillLinks reads MusicBrainz's links for every artist and hydrated
// album never read (links_fetched_at NULL), with url-rels-only calls at
// background priority, at the end of music-metadata-refresh. A failed row waits
// for the next run; a rate limit is waited out (fetchLinks).
func (s *Service) backfillLinks(ctx context.Context) {
	ctx, span := tracer.Start(ctx, "music.backfill_links")
	defer span.End()
	ctx = metadata.Background(ctx)

	done, failed := 0, 0
	var lastFailure error
	progress := func(err error) error {
		switch {
		case err == nil:
			done++
			scheduler.Progress(ctx, done, 0)
			return nil
		case ctx.Err() != nil:
			return errLinksStop
		case errors.Is(err, metadata.ErrRateLimited):
			slog.InfoContext(ctx, "links backfill paused by MusicBrainz rate limit",
				"done", done, "error", err)
			return errLinksStop
		default:
			failed++
			lastFailure = err
			return nil
		}
	}

	err := s.backfillArtistLinks(ctx, progress)
	if err == nil {
		err = s.backfillAlbumLinks(ctx, progress)
	}
	span.SetAttributes(
		attribute.Int("links.done_count", done),
		attribute.Int("links.failed_count", failed),
	)
	if err != nil && !errors.Is(err, errLinksStop) {
		_ = otelx.RecordSpanError(span, err)
		slog.WarnContext(ctx, "links backfill failed", "error", err)
		return
	}
	if lastFailure != nil {
		// Rows are logged where they fail; the span carries the count.
		_ = otelx.RecordSpanError(span,
			fmt.Errorf("%d rows failed, the last: %w", failed, lastFailure))
	}
	if done+failed > 0 {
		slog.InfoContext(ctx, "links backfill complete",
			"done", done, "failed", failed)
	}
}

func (s *Service) backfillArtistLinks(
	ctx context.Context,
	progress func(error) error,
) error {
	var cursor uint32
	for {
		rows, err := s.db.ListArtistsAwaitingLinks(ctx, cursor, linksBatch)
		if err != nil {
			return err
		}
		for _, a := range rows {
			cursor = a.ID
			ids, err := fetchLinks(ctx, func() (map[string]string, error) {
				return s.metadata.ArtistLinks(ctx, a.Mbid)
			})
			err = s.storeLinks(ctx, "artist", a.ID, ids, err, s.db.SetArtistLinks)
			if stop := progress(err); stop != nil {
				return stop
			}
		}
		if len(rows) < linksBatch {
			return nil
		}
	}
}

func (s *Service) backfillAlbumLinks(
	ctx context.Context,
	progress func(error) error,
) error {
	var cursor uint32
	for {
		rows, err := s.db.ListAlbumsAwaitingLinks(ctx, cursor, linksBatch)
		if err != nil {
			return err
		}
		for _, a := range rows {
			cursor = a.ID
			// release_mbid is the release the album follows, a hand pick included.
			ids, err := fetchLinks(ctx, func() (map[string]string, error) {
				return s.metadata.AlbumLinks(ctx, a.Mbid, a.ReleaseMbid)
			})
			err = s.storeLinks(ctx, "album", a.ID, ids, err, s.db.SetAlbumLinks)
			if stop := progress(err); stop != nil {
				return stop
			}
		}
		if len(rows) < linksBatch {
			return nil
		}
	}
}

// fetchLinks retries a MusicBrainz rate limit up to linksRetries times, so
// a long pass does not end on the first 503.
func fetchLinks(
	ctx context.Context,
	fetch func() (map[string]string, error),
) (map[string]string, error) {
	for attempt := 0; ; attempt++ {
		ids, err := fetch()
		var limited *metadata.RateLimitedError
		if !errors.As(err, &limited) || attempt == linksRetries ||
			limited.RetryAfter > linksMaxWait {
			return ids, err
		}
		t := time.NewTimer(max(limited.RetryAfter, time.Second))
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, ctx.Err()
		case <-t.C:
		}
	}
}

// storeLinks writes one row's links; one MusicBrainz no longer knows is
// stamped with its links untouched.
func (s *Service) storeLinks(
	ctx context.Context,
	kind string,
	id uint32,
	ids map[string]string,
	fetchErr error,
	store func(context.Context, uint32, db.ExternalIDs, time.Time) error,
) error {
	switch {
	case errors.Is(fetchErr, metadata.ErrNotFound):
		ids = nil
	case fetchErr != nil:
		if !errors.Is(fetchErr, metadata.ErrRateLimited) && ctx.Err() == nil {
			slog.WarnContext(ctx, "links backfill: fetch failed",
				"links.kind", kind, "id", id, "error", fetchErr)
		}
		return fetchErr
	}
	if err := store(ctx, id, db.ExternalIDs(ids), time.Now()); err != nil {
		slog.WarnContext(ctx, "links backfill: store failed",
			"links.kind", kind, "id", id, "error", err)
		return err
	}
	return nil
}
