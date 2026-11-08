package music

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/scheduler"
)

const (
	// linksBatch is how many rows one listing of the links backfill reads.
	linksBatch = 50
	// linksRetries is how many times one row is asked again after
	// MusicBrainz refuses it, each after the wait it named.
	linksRetries = 3
	// linksMaxWait caps that wait; a longer one ends the pass for the day.
	linksMaxWait = 5 * time.Minute
)

// errLinksStop ends a backfill pass early: MusicBrainz is refusing, or the
// run is shutting down. The rows left keep links_fetched_at NULL and the next
// run carries on from them.
var errLinksStop = errors.New("links backfill stopped")

// backfillLinks reads MusicBrainz's links for every artist and every hydrated
// album stored before streamline kept them (links_fetched_at NULL): one small
// url-rels call per artist and at most two per album, never the refresh's
// listing or the hydration's heavy release call. An add, a refresh and a
// hydration stamp the row themselves, so once the library has been through it
// a run finds nothing and costs a query.
//
// It runs to the end of the backlog inside music-metadata-refresh, at
// background priority, so a lookup or an add never queues behind it; the
// first run over a large library takes hours (about 2 s an artist and 4 s an
// album at the default interval), and the scheduler does not start the job
// again while it runs. A failed row is skipped for this pass (the cursor moves
// past it) and retried on the next. A rate limit is waited out (fetchLinks);
// one that outlasts the retries ends the pass, and the next run resumes.
func (s *Service) backfillLinks(ctx context.Context) {
	ctx, span := tracer.Start(ctx, "music.backfill_links")
	defer span.End()
	ctx = metadata.Background(ctx)

	done, failed := 0, 0
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
	if done+failed > 0 {
		slog.InfoContext(ctx, "links backfill complete",
			"done", done, "failed", failed)
	}
}

func (s *Service) backfillArtistLinks(ctx context.Context, progress func(error) error) error {
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

func (s *Service) backfillAlbumLinks(ctx context.Context, progress func(error) error) error {
	var cursor uint32
	for {
		rows, err := s.db.ListAlbumsAwaitingLinks(ctx, cursor, linksBatch)
		if err != nil {
			return err
		}
		for _, a := range rows {
			cursor = a.ID
			// release_mbid is the release the album follows, the person's
			// pick included: the one a hydration reads streaming links from.
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

// fetchLinks calls fetch, waiting out a MusicBrainz rate limit and asking
// again up to linksRetries times: the limiter holds the background class for
// the wait MusicBrainz names, and a pass of hours would otherwise end on the
// first 503 and leave the backlog to the next day.
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

// storeLinks writes one row's links. A row MusicBrainz no longer knows is
// stamped with its links untouched, so it is not asked for again; any other
// failure is returned for progress to count.
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
