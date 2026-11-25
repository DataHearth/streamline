package tvshow

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/observability"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/scheduler"
)

// personDetailConcurrency bounds the provider person-detail calls one
// enrichment pass has in flight. A series credits around fifteen people and
// fetching them strictly serially is a visible pause on the add that asked
// for them; four keeps that short without turning one import into a burst
// against the provider's rate limit.
const personDetailConcurrency = 4

// enrichPeople fills in the biographical record of every person this series
// credits who has never been enriched, and runs *after* the title's
// transaction has committed.
//
// That ordering is the whole point. ReplaceCast writes the credits inside the
// caller's transaction, and this is single-writer SQLite: fifteen provider
// round-trips issued from inside that transaction would hold the write lock
// across the network and stall every other writer in the process for as long
// as the slowest one took.
//
// A person is enriched once ever — db.PeopleNeedingDetails only returns rows
// with a nil details_fetched_at — so an actor credited on thirty titles costs
// one call, not thirty.
//
// Nothing here can fail the import. The series and its cast are already
// committed and matter far more than a biography; a lookup that errors is
// logged and skipped with the stamp left nil, so the next metadata refresh
// retries it.
func (s *Service) enrichPeople(ctx context.Context, showID uint32) {
	ctx, span := tracer.Start(ctx, "tvshow.enrich_people",
		trace.WithAttributes(attribute.Int64("tvshow.id", int64(showID))),
	)
	defer span.End()

	people, err := s.db.PeopleNeedingDetails(ctx, db.CastOwnerSeries, showID)
	if err != nil {
		otelx.RecordSpanError(span, err)
		slog.WarnContext(ctx, "cast enrichment work list failed",
			"tvshow.id", showID, "error", err)
		return
	}
	if len(people) == 0 {
		return
	}
	span.SetAttributes(attribute.Int("people.pending", len(people)))

	sem := make(chan struct{}, personDetailConcurrency)
	var wg sync.WaitGroup
	for _, p := range people {
		// The series service holds the TVDB client and nothing else, so a
		// person carrying only a tmdb id is left to whichever movie credits
		// them; one with neither has nothing to look up at all.
		if p.TVDBID == 0 {
			continue
		}
		sem <- struct{}{}
		wg.Go(func() {
			defer observability.RecoverPanic(ctx, "tvshow.enrich_people", nil)
			defer func() { <-sem }()
			s.enrichPerson(ctx, p)
		})
	}
	wg.Wait()
}

func (s *Service) enrichPerson(ctx context.Context, p db.Person) {
	d, err := s.metadata.GetPerson(ctx, p.TVDBID)
	if err != nil {
		slog.WarnContext(ctx, "person details fetch failed",
			"person.id", p.ID, "person.tvdb_id", p.TVDBID, "error", err)
		return
	}
	if d == nil {
		return
	}
	if err := s.db.SavePersonDetails(ctx, p.ID, *d); err != nil {
		slog.WarnContext(ctx, "person details save failed",
			"person.id", p.ID, "error", err)
	}
}

// fetchEpisodeCredits reads TVDB's guest stars and crew for every aired
// episode that needs them (db.EpisodesNeedingCredits), one request each, so
// it runs in the metadata refresh job rather than on the add: a long show is
// hundreds of requests the first time and only its new episodes after that.
// A failed episode stays unstamped for the next tick; a rate limit ends the
// pass.
func (s *Service) fetchEpisodeCredits(ctx context.Context) {
	ctx, span := tracer.Start(ctx, "tvshow.fetch_episode_credits")
	defer span.End()

	eps, err := s.db.EpisodesNeedingCredits(ctx, time.Now())
	if err != nil {
		otelx.RecordSpanError(span, err)
		slog.WarnContext(ctx, "episode credits work list failed", "error", err)
		return
	}
	span.SetAttributes(attribute.Int("episodes.pending", len(eps)))

	var (
		limited atomic.Bool
		done    atomic.Int64
		failed  atomic.Int64
	)
	sem := make(chan struct{}, personDetailConcurrency)
	var wg sync.WaitGroup
	for _, e := range eps {
		if limited.Load() {
			break
		}
		sem <- struct{}{}
		wg.Go(func() {
			defer observability.RecoverPanic(
				ctx,
				"tvshow.fetch_episode_credits",
				nil,
			)
			defer func() { <-sem }()
			defer func() {
				scheduler.Progress(ctx, int(done.Add(1)), len(eps))
			}()
			credits, err := s.metadata.GetEpisodeCredits(ctx, e.TVDBID)
			if err == nil {
				err = s.db.ReplaceEpisodeCredits(ctx, e.ID, credits)
			}
			if err == nil {
				return
			}
			failed.Add(1)
			if errors.Is(err, metadata.ErrRateLimited) {
				limited.Store(true)
			}
			slog.WarnContext(ctx, "episode credits fetch failed",
				"episode.id", e.ID, "episode.tvdb_id", e.TVDBID, "error", err)
		})
	}
	wg.Wait()
	span.SetAttributes(
		attribute.Int64("episodes.failed", failed.Load()),
		attribute.Bool("rate_limited", limited.Load()),
	)
}

// fetchPeoplePhotos keeps a local copy of the photo of everyone a series or
// episode credits, which is what extensions read; Fetch skips one already
// cached. A failed download is retried by the next pass.
func (s *Service) fetchPeoplePhotos(ctx context.Context) {
	if s.posters == nil {
		return
	}
	ctx, span := tracer.Start(ctx, "tvshow.fetch_people_photos")
	defer span.End()
	people, err := s.db.PeopleWithPhotos(
		ctx,
		db.CastOwnerSeries,
		db.CastOwnerEpisode,
	)
	if err != nil {
		otelx.RecordSpanError(span, err)
		slog.WarnContext(ctx, "people photo work list failed", "error", err)
		return
	}
	span.SetAttributes(attribute.Int("people.count", len(people)))
	for _, p := range people {
		if err := s.posters.Posters().
			Fetch(ctx, "people", p.ID, p.ProfileURL); err != nil {
			slog.WarnContext(ctx, "person photo fetch failed",
				"person.id", p.ID, "error", err)
		}
	}
}
