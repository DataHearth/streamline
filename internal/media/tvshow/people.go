package tvshow

import (
	"context"
	"log/slog"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/observability"
	"github.com/datahearth/streamline/internal/otelx"
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
