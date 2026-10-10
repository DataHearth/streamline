package music

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/observability"
	"github.com/datahearth/streamline/internal/otelx"
)

const (
	// sweepBatch is how many albums each leg of the sweep queues per run.
	sweepBatch = 20

	// maxRateLimitPause bounds how long the worker stands down after
	// MusicBrainz says to back off.
	maxRateLimitPause = 5 * time.Minute

	hydrationOK     = "ok"
	hydrationLight  = "light"
	hydrationFailed = "failed"
)

var hydrations = otelx.Must(meter.Int64Counter(
	"streamline.music.hydrations",
	metric.WithDescription("Album hydrations by outcome"),
))

// hydrateItem is one album waiting for its tracks, with what orders it within
// its artist's queue.
type hydrateItem struct {
	albumID   uint32
	monitored bool
	date      *time.Time
}

// hydrator is the in-memory queue behind the hydration worker: one FIFO per
// artist, served round-robin so a prolific artist does not hold every other
// artist's tracks for an hour.
type hydrator struct {
	mu      sync.Mutex
	queues  map[uint32][]hydrateItem
	order   []uint32
	next    int
	queued  map[uint32]uint32 // album id -> artist id, queued or in flight
	running bool
}

// newer orders monitored albums first, then newest first.
func (a hydrateItem) newer(b hydrateItem) int {
	if a.monitored != b.monitored {
		if a.monitored {
			return -1
		}
		return 1
	}
	switch {
	case a.date == nil && b.date != nil:
		return 1
	case a.date != nil && b.date == nil:
		return -1
	case a.date != nil && b.date != nil:
		if c := b.date.Compare(*a.date); c != 0 {
			return c
		}
	}
	return cmp.Compare(a.albumID, b.albumID)
}

// add queues the items not already queued and reports whether the worker has
// to be started.
func (h *hydrator) add(artistID uint32, items []hydrateItem) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.queues == nil {
		h.queues = map[uint32][]hydrateItem{}
		h.queued = map[uint32]uint32{}
	}
	added := false
	for _, it := range items {
		if _, dup := h.queued[it.albumID]; dup {
			continue
		}
		h.queued[it.albumID] = artistID
		h.queues[artistID] = append(h.queues[artistID], it)
		added = true
	}
	if !added {
		return false
	}
	slices.SortStableFunc(h.queues[artistID], hydrateItem.newer)
	if !slices.Contains(h.order, artistID) {
		h.order = append(h.order, artistID)
	}
	if h.running {
		return false
	}
	h.running = true
	return true
}

// pop takes the next album, one artist at a time in turn. It leaves running
// set when the queue is empty: the worker still has to refill before it may
// stop, and clearing the flag here would let an add start a second worker.
func (h *hydrator) pop() (hydrateItem, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for len(h.order) > 0 {
		if h.next >= len(h.order) {
			h.next = 0
		}
		artistID := h.order[h.next]
		q := h.queues[artistID]
		if len(q) == 0 {
			h.order = slices.Delete(h.order, h.next, h.next+1)
			delete(h.queues, artistID)
			continue
		}
		it := q[0]
		h.queues[artistID] = q[1:]
		h.next++
		return it, true
	}
	return hydrateItem{}, false
}

// stop clears running when nothing is queued, under the lock add checks, so an
// add can never be stranded behind a worker that has just decided to exit.
func (h *hydrator) stop() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, q := range h.queues {
		if len(q) > 0 {
			return false
		}
	}
	h.running = false
	return true
}

func (h *hydrator) done(albumID uint32) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.queued, albumID)
}

// remove drops an album from the queue, for a caller about to hydrate it
// itself.
func (h *hydrator) remove(albumID uint32) {
	h.mu.Lock()
	defer h.mu.Unlock()
	artistID, ok := h.queued[albumID]
	if !ok {
		return
	}
	delete(h.queued, albumID)
	h.queues[artistID] = slices.DeleteFunc(
		h.queues[artistID],
		func(it hydrateItem) bool { return it.albumID == albumID },
	)
}

// reset forgets everything queued and marks the worker stopped, for a worker
// that died: leaving its albums marked as queued would keep them out of every
// later queue.
func (h *hydrator) reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.queues, h.queued, h.order, h.next, h.running = nil, nil, nil, 0, false
}

// enqueueHydration queues albums for the background worker and starts it when
// idle. The worker outlives every request, so it runs on a context of its own:
// one that carried the first requester's identity would stamp every log line
// of the worker, for as long as it lives, with that user.
func (s *Service) enqueueHydration(artistID uint32, items ...hydrateItem) {
	if len(items) == 0 || !s.hydrate.add(artistID, items) {
		return
	}
	bg := metadata.Background(context.Background())
	go func() {
		defer observability.RecoverPanic(bg, "music.hydrate", s.hydrate.reset)
		s.hydrationWorker(bg)
	}()
}

// hydrationWorker drains the queue one album at a time, then refills it from
// the albums still waiting in the database (restart recovery) until nothing
// new turns up. An album that failed in this run is not retried in it.
func (s *Service) hydrationWorker(ctx context.Context) {
	failed := map[uint32]struct{}{}
	for {
		it, ok := s.hydrate.pop()
		if !ok {
			if s.refillHydration(ctx, failed) || !s.hydrate.stop() {
				continue
			}
			return
		}
		err := s.hydrateAlbum(ctx, it.albumID, true)
		s.hydrate.done(it.albumID)
		var limited *metadata.RateLimitedError
		switch {
		case errors.As(err, &limited):
			pause := min(limited.RetryAfter, maxRateLimitPause)
			slog.WarnContext(ctx, "music hydration backing off",
				"retry_after", pause.Round(time.Second))
			if serr := sleep(ctx, pause); serr != nil {
				return
			}
			s.requeue(ctx, it)
		case err != nil:
			failed[it.albumID] = struct{}{}
		}
	}
}

func (s *Service) requeue(ctx context.Context, it hydrateItem) {
	a, err := s.db.FindAlbumByID(ctx, it.albumID)
	if err != nil || a.Edges.Artist == nil {
		return
	}
	s.hydrate.add(a.Edges.Artist.ID, []hydrateItem{it})
}

// refillHydration queues up to sweepBatch albums still waiting for their
// tracks, minus the ones that failed this run, and reports whether it found
// any.
func (s *Service) refillHydration(
	ctx context.Context,
	failed map[uint32]struct{},
) bool {
	rows, err := s.db.ListAlbumsAwaitingHydration(ctx, sweepBatch+len(failed))
	if err != nil {
		slog.WarnContext(ctx, "music hydration refill failed", "error", err)
		return false
	}
	found := false
	for _, a := range rows {
		if _, bad := failed[a.ID]; bad || a.Edges.Artist == nil {
			continue
		}
		s.hydrate.add(a.Edges.Artist.ID, []hydrateItem{{
			albumID: a.ID, monitored: a.Monitored, date: a.ReleaseDate,
		}})
		found = true
	}
	return found
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// AlbumHydrator is what the album importer and the adoption path call before
// they match files to tracks: an album added as a stub has none.
type AlbumHydrator interface {
	// HydrateAlbum fetches the album's tracks when it has not been fetched,
	// synchronously and at interactive priority, and is a no-op otherwise.
	HydrateAlbum(ctx context.Context, albumID uint32) error
}

var _ AlbumHydrator = (*Service)(nil)

func (s *Service) HydrateAlbum(ctx context.Context, albumID uint32) error {
	s.hydrate.remove(albumID)
	return s.hydrateAlbum(ctx, albumID, false)
}

// hydrateAlbum fetches an album's release data and stores it. Without force it
// does nothing for an album whose tracks are already stored.
func (s *Service) hydrateAlbum(
	ctx context.Context,
	albumID uint32,
	force bool,
) error {
	a, err := s.db.FindAlbumByID(ctx, albumID)
	if ent.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !force && a.MetadataFetchedAt != nil {
		return nil
	}
	ctx, span := tracer.Start(ctx, "music.hydrate_album",
		trace.WithAttributes(attribute.Int64("album.id", int64(albumID))))
	defer span.End()

	d, err := s.metadata.GetReleaseGroup(ctx, a.Mbid)
	switch {
	case errors.Is(err, metadata.ErrNotFound):
		// MusicBrainz no longer has the release group: stamp it, or the sweep
		// would ask for it forever.
		d = &metadata.ReleaseGroupDetails{}
	case err != nil:
		hydrations.Add(ctx, 1, outcomeAttr(hydrationFailed))
		span.SetAttributes(attribute.String("outcome", hydrationFailed))
		return otelx.RecordSpanError(
			span,
			fmt.Errorf("hydrate album %d: %w", albumID, err),
		)
	}

	params, err := s.hydrationParams(ctx, a, d)
	if err != nil {
		hydrations.Add(ctx, 1, outcomeAttr(hydrationFailed))
		return otelx.RecordSpanError(span, err)
	}
	if err := s.db.SetAlbumHydration(ctx, albumID, params, time.Now()); err != nil {
		hydrations.Add(ctx, 1, outcomeAttr(hydrationFailed))
		span.SetAttributes(attribute.String("outcome", hydrationFailed))
		return otelx.RecordSpanError(
			span,
			fmt.Errorf("store album %d: %w", albumID, err),
		)
	}
	outcome := hydrationOK
	if !d.CreditsComplete {
		outcome = hydrationLight
	}
	hydrations.Add(ctx, 1, outcomeAttr(outcome))
	span.SetAttributes(
		attribute.String("outcome", outcome),
		attribute.Int("tracks", len(params.Tracks)),
	)
	if a.MetadataFetchedAt == nil && force {
		if full, err := s.db.FindAlbumByID(ctx, albumID); err == nil {
			s.resolveCover(ctx, full)
		}
	}
	return nil
}

func outcomeAttr(outcome string) metric.AddOption {
	return metric.WithAttributes(attribute.String("outcome", outcome))
}

// hydrationParams turns a release call into what the database stores. A
// performer is a guest when they are not the album's artist, not one of its
// members and appear on at most half of the recordings that credit any
// performer: MusicBrainz has no guest flag, so this is a heuristic.
func (s *Service) hydrationParams(
	ctx context.Context,
	a *ent.Album,
	d *metadata.ReleaseGroupDetails,
) (db.HydrationParams, error) {
	p := db.HydrationParams{
		ReleaseMBID:     d.ReleaseMBID,
		Barcode:         d.Barcode,
		Label:           d.Label,
		CatalogNumber:   d.CatalogNumber,
		Country:         d.Country,
		Media:           d.Media,
		Studio:          d.Studio,
		CreditsComplete: d.CreditsComplete,
	}
	for _, t := range d.Tracks {
		p.Tracks = append(p.Tracks, db.TrackSeed{
			MBID: t.MBID, Title: t.Title, Disc: t.Disc, Position: t.Position,
			Duration: t.Duration, Bonus: t.Bonus,
			Featuring: personSeeds(t.Featuring),
			Writers:   personSeeds(t.Writers),
		})
	}
	if !d.CreditsComplete {
		return p, nil
	}
	for _, c := range d.Credits {
		p.Credits = append(p.Credits, db.AlbumCreditSeed{
			Name: c.Name, MBID: c.MBID,
			Role: c.Role,
		})
	}
	var artistMBID string
	members := map[string]struct{}{}
	if a.Edges.Artist != nil {
		artistMBID = a.Edges.Artist.Mbid
		mbids, err := s.db.ArtistMemberMBIDs(ctx, a.Edges.Artist.ID)
		if err != nil {
			return p, fmt.Errorf("list members: %w", err)
		}
		for _, m := range mbids {
			members[m] = struct{}{}
		}
	}
	for _, pf := range d.Performers {
		_, member := members[pf.MBID]
		guest := pf.MBID != artistMBID && !member &&
			pf.Recordings*2 <= d.PerformerRecordings
		p.Performers = append(p.Performers, db.PerformerSeed{
			Name: pf.Name, MBID: pf.MBID,
			Instruments: pf.Instruments,
			Guest:       guest,
		})
	}
	return p, nil
}

func personSeeds(in []metadata.PersonInfo) []db.PersonSeed {
	out := make([]db.PersonSeed, len(in))
	for i, p := range in {
		out[i] = db.PersonSeed{Name: p.Name, MBID: p.MBID}
	}
	return out
}

// ResumeHydration queues the albums left unfinished by a previous run, so a
// restart does not wait for the next metadata refresh to carry on. It returns
// at once; the worker does the fetching.
func (s *Service) ResumeHydration(ctx context.Context) {
	s.sweepHydration(ctx)
}

// sweepHydration queues the albums the database says are unfinished: those
// whose tracks never arrived (restart recovery), those with tracks whose heavy
// release call never succeeded, and a few that came back trackless long ago.
func (s *Service) sweepHydration(ctx context.Context) {
	now := time.Now()
	legs := []struct {
		name string
		list func() ([]*ent.Album, error)
	}{
		{"awaiting", func() ([]*ent.Album, error) {
			return s.db.ListAlbumsAwaitingHydration(ctx, sweepBatch)
		}},
		{"credits", func() ([]*ent.Album, error) {
			return s.db.ListAlbumsAwaitingCredits(ctx, sweepBatch)
		}},
		{"trackless", func() ([]*ent.Album, error) {
			return s.db.ListAlbumsTrackless(
				ctx, now.Add(-tracklessStale), now.Add(tracklessHorizon),
				tracklessPerRun,
			)
		}},
	}
	for _, leg := range legs {
		rows, err := leg.list()
		if err != nil {
			slog.WarnContext(ctx, "music hydration sweep failed",
				"leg", leg.name, "error", err)
			continue
		}
		byArtist := map[uint32][]hydrateItem{}
		for _, a := range rows {
			if a.Edges.Artist == nil {
				continue
			}
			byArtist[a.Edges.Artist.ID] = append(
				byArtist[a.Edges.Artist.ID],
				hydrateItem{
					albumID:   a.ID,
					monitored: a.Monitored,
					date:      a.ReleaseDate,
				},
			)
		}
		for artistID, items := range byArtist {
			s.enqueueHydration(artistID, items...)
		}
	}
}
