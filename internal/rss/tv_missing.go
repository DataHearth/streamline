package rss

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/episode"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/events"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/quality"
	"github.com/datahearth/streamline/internal/quality/qualityctx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// EpisodeMissingSearcher scans wanted episodes, searches indexers, and grabs
// the best matching release per season. When a whole season is wanted it
// prefers a single season pack over per-episode grabs.
type EpisodeMissingSearcher struct {
	store     EligibleEpisodeLister
	indexers  TVIndexerSearcher
	downloads EpisodeGrabber
}

func NewEpisodeMissingSearcher(
	store EligibleEpisodeLister,
	indexers TVIndexerSearcher,
	downloads EpisodeGrabber,
) *EpisodeMissingSearcher {
	return &EpisodeMissingSearcher{
		store:     store,
		indexers:  indexers,
		downloads: downloads,
	}
}

// Run performs one pass over every show with searchable episodes. Per-season
// errors are logged and never abort the pass; only a failure to list eligible
// shows is returned. Satisfies the MissingSearchRunner contract used by jobs.
func (s *EpisodeMissingSearcher) Run(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "rss.tv_missing_search")
	defer span.End()

	window, err := currentSearchWindow()
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	shows, err := s.eligibleShows(ctx, window)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	span.SetAttributes(attribute.Int("shows.count", len(shows)))
	// See MissingSearcher.Run: the client check is process-wide, so a pass
	// without one is dead work — but the eligible-show count is still worth
	// recording, hence the check sits after the query, not at the top.
	if _, ok := config.PickDownloadClient(); !ok {
		slog.InfoContext(ctx, "tv missing-search: no enabled download client",
			"shows", len(shows),
		)
		return nil
	}
	// grabbed tracks episode IDs already served this tick, across every show
	// and season. No producer today emits a union spanning seasons, so this
	// is currently a no-op; it is the authority for "already served", so a
	// future multi-season producer cannot re-search what it already covered.
	grabbed := make(map[uint32]struct{})
	for _, show := range shows {
		s.searchShow(ctx, show, grabbed)
	}
	return nil
}

// SearchShow runs one search-and-grab pass scoped to a single series, so a
// show with nothing searchable is a no-op. Powers POST /series/{id}/search.
// A user asked for this pass, so the operator throttles are waived — matching
// the movie twin, where SearchMovieNow calls SearchOne directly instead of
// going through the eligibility query. The hard guards still apply.
func (s *EpisodeMissingSearcher) SearchShow(
	ctx context.Context,
	showID uint32,
) error {
	ctx, span := tracer.Start(ctx, "rss.tv_search_show",
		trace.WithAttributes(attribute.Int64("tvshow.id", int64(showID))),
	)
	defer span.End()

	shows, err := s.eligibleShows(ctx, unthrottledWindow())
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	grabbed := make(map[uint32]struct{})
	for _, show := range shows {
		if show.ID != showID {
			continue
		}
		s.searchShow(ctx, show, grabbed)
	}
	return nil
}

// searchTally is what one series-scoped search pass did, aggregated across the
// seasons it touched. It becomes the payload of the single `searched` event.
type searchTally struct {
	seasons  []uint16
	episodes int
	grabbed  int
	packs    int
}

func (t *searchTally) add(seasonNumber uint16, s searchTally) {
	if s.episodes == 0 {
		return
	}
	t.seasons = append(t.seasons, seasonNumber)
	t.episodes += s.episodes
	t.grabbed += s.grabbed
	t.packs += s.packs
}

// searchShow runs the pass over every season with wanted episodes and records
// one `searched` event for the whole invocation. Recording per episode would
// write a row per wanted episode per tick; the operator asked for a search of
// this series, so that is what the history shows — with the seasons actually
// touched in the payload, so a single-season pass still reads as one.
func (s *EpisodeMissingSearcher) searchShow(
	ctx context.Context,
	show *ent.TVShow,
	grabbed map[uint32]struct{},
) {
	// Both names, mirroring the movie searches. TVDB answers in
	// metadata.language, so show.Title is the *local* title — the one a scene
	// release is least likely to be named after. dedupTitles collapses them when
	// a show has no language override.
	titles := []string{show.Title, show.OriginalTitle}
	// Unfiltered season lengths, for sizing a pack. The season edges below are
	// narrowed to searchable episodes, and a pack costs the whole season. A
	// failure here only leaves the size bounds unscaled.
	counts, err := s.store.SeasonEpisodeCounts(ctx, []uint32{show.ID})
	if err != nil {
		slog.WarnContext(ctx, "tv missing-search: season counts unavailable",
			"tvshow.id", show.ID, "error", err)
	}
	perSeason := counts[show.ID]
	var tally searchTally
	for _, se := range show.Edges.Seasons {
		tally.add(
			se.Number,
			s.processSeason(ctx, show, se, titles, perSeason, grabbed),
		)
	}
	if tally.episodes == 0 {
		return
	}
	payload := map[string]any{
		"seasons":           tally.seasons,
		"episodes_searched": tally.episodes,
		"grabbed":           tally.grabbed,
	}
	if tally.packs > 0 {
		payload["packs"] = tally.packs
	}
	if err := events.Record(
		ctx, nil, events.TypeSearched, events.ScopeSeries, show.ID, payload,
	); err != nil {
		slog.WarnContext(ctx, "tv missing-search: record searched event failed",
			"tvshow.id", show.ID, "error", err)
	}
}

func (s *EpisodeMissingSearcher) eligibleShows(
	ctx context.Context,
	window searchWindow,
) ([]*ent.TVShow, error) {
	return s.store.ListEligibleEpisodesForSync(
		ctx,
		window.MaxGrabFailures,
		window.NotSearchedSince,
		time.Now(),
	)
}

// processSeason searches and grabs for one season's wanted episodes. The
// season's episode edge is already filtered to searchable rows by
// ListEligibleEpisodesForSync, and is narrowed again here against grabbed —
// episodes an earlier season's pack already served this tick — before the
// pack-vs-single decision, so a fully covered season searches nothing at all.
// With two or more still-open episodes it tries a season pack first;
// otherwise (or on no acceptable pack) it falls back to per-episode.
func (s *EpisodeMissingSearcher) processSeason(
	ctx context.Context,
	show *ent.TVShow,
	se *ent.Season,
	titles []string,
	perSeason map[uint16]int,
	grabbed map[uint32]struct{},
) searchTally {
	wanted := make([]*ent.Episode, 0, len(se.Edges.Episodes))
	for _, e := range se.Edges.Episodes {
		if _, already := grabbed[e.ID]; already {
			continue
		}
		wanted = append(wanted, e)
	}
	if len(wanted) == 0 {
		return searchTally{}
	}
	profile := qualityFor(ctx, show.QualityProfile)

	// Prefer a season pack when the whole season (2+ episodes) is wanted.
	if len(wanted) >= 2 &&
		s.grabSeasonPack(
			ctx,
			show,
			se,
			titles,
			wanted,
			profile,
			perSeason,
			grabbed,
		) {
		return searchTally{
			episodes: len(wanted),
			grabbed:  len(wanted),
			packs:    1,
		}
	}

	t := searchTally{episodes: len(wanted)}
	for _, e := range wanted {
		if s.grabEpisode(ctx, show, se, e, titles, profile, grabbed) {
			t.grabbed++
		}
	}
	return t
}

// grabSeasonPack searches for a season pack and, working down the scored
// packs best-first, grabs one against the first wanted episode and marks
// every wanted episode in the season as downloading. The grab's wanted set is
// the season's missing episodes plus, under an upgrade-permitting profile, the
// on-disk episodes the same release beats — the feed scanner's grabPack rule,
// so a pack means the same thing whichever pass found it. Only the missing
// episodes are marked; a beaten one keeps its file's status until the importer
// decides it per file via replace_mode upgrades. Reports whether a pack was
// grabbed.
func (s *EpisodeMissingSearcher) grabSeasonPack(
	ctx context.Context,
	show *ent.TVShow,
	se *ent.Season,
	titles []string,
	wanted []*ent.Episode,
	profile quality.Profile,
	perSeason map[uint16]int,
	grabbed map[uint32]struct{},
) bool {
	ctx, span := tracer.Start(ctx, "rss.tv_season_pack",
		trace.WithAttributes(
			attribute.Int64("tvshow.id", int64(show.ID)),
			attribute.Int("season.number", int(se.Number)),
		),
	)
	defer span.End()

	packs, err := s.indexers.SearchSeason(ctx, titles, show.TvdbID, se.Number)
	if err != nil {
		slog.WarnContext(ctx, "tv missing-search: season-pack search failed",
			"show", show.Title, "season", se.Number, "error", err)
		return false
	}
	// Every result here is scoped to this one season: filterToSeason drops the
	// multi-season and complete packs, so one count covers the whole list.
	episodes := perSeason[se.Number]
	if episodes < 1 {
		episodes = singleRelease
	}
	ranked := rankAccepted(profile, packs, episodes)
	// Loaded at most once per pack attempt, and only with a pack worth
	// grabbing: a season whose results are all rejected is the steady-state
	// case and must not cost a query.
	var us *wantedShow
	if len(ranked) > 0 && profile.UpgradeAllowed {
		row, err := s.store.UpgradeCandidateShow(ctx, show.ID)
		if err != nil {
			slog.WarnContext(ctx, "tv missing-search: upgrade context load failed",
				"show", show.Title, "error", err)
		} else if row != nil {
			us = newWantedShow(row)
		}
	}
	for _, r := range ranked {
		// Which files a release beats is the release's own question — a weaker
		// pack replaces fewer of them — so the union is rebuilt per candidate
		// rather than once for the whole ranked list. The two halves can
		// overlap: ListEligibleEpisodesForSync matches on status=wanted without
		// excluding HasMediaFiles, and upgradeCandidateEpisodes is the mirror
		// image, so a wanted episode already holding a file can land in both.
		// Left undeduped: computeKeepSet and unionEpisodes both key by episode
		// ID and collapse it, and the importer never reads this list.
		release := qualityctx.ContextFromRelease(
			r.Title, r.Size, r.Seeders, episodes,
		)
		beat := beatEpisodes(us, se.Number, profile, release, grabbed)
		wantedIDs := make([]uint32, 0, len(wanted)+len(beat))
		for _, e := range wanted {
			wantedIDs = append(wantedIDs, e.ID)
		}
		for _, e := range beat {
			wantedIDs = append(wantedIDs, e.ID)
		}
		rec, err := s.downloads.GrabEpisode(
			ctx,
			r,
			wanted[0].ID,
			wantedIDs,
		)
		if err != nil {
			slog.WarnContext(ctx, "tv missing-search: season-pack grab failed",
				"show", show.Title, "season", se.Number,
				"release", r.Title, "error", err)
			// ErrNoWantedFiles means the release holds nothing for these
			// episodes, so the anchor is no closer to being filled than before
			// — the same standing the single-episode path counts. Left
			// uncounted, a season whose only packs are all mismatches would
			// re-search every tick forever without ever reaching the
			// max_grab_failures ceiling.
			if errors.Is(err, download.ErrNoWantedFiles) {
				if ierr := s.store.IncrementEpisodeGrabFailures(
					ctx, wanted[0].ID,
				); ierr != nil {
					slog.WarnContext(ctx,
						"tv missing-search: bump episode grab_failures failed",
						"episode.id", wanted[0].ID, "error", ierr)
				}
			}
			continue
		}
		span.SetAttributes(attribute.String("release.title", r.Title))
		for _, id := range wantedIDs {
			grabbed[id] = struct{}{}
		}
		// The pack was grabbed to fill the gap, but it may also beat files
		// already on disk. The importer decides that per episode, and upgrades
		// is what lets it.
		if err := s.store.SetDownloadRecordReplaceMode(
			ctx, rec.ID, downloadrecord.ReplaceModeUpgrades,
		); err != nil {
			slog.ErrorContext(ctx, "tv missing-search: set replace mode failed",
				"show", show.Title, "record.id", rec.ID, "error", err)
		}
		now := time.Now()
		for _, e := range wanted {
			markEpisodeDownloading(ctx, s.store, e.ID, now)
		}
		slog.InfoContext(ctx, "tv missing-search: grabbed season pack",
			"show", show.Title, "season", se.Number,
			"release", r.Title, "filled", len(wanted), "replaced", len(beat))
		return true
	}
	return false
}

// grabEpisode searches for a single episode and grabs the best-scoring
// acceptable result, bumping/resetting grab_failures accordingly.
// last_search_at is stamped whenever the indexer responds so the cooldown
// counter advances. Reports whether a release was grabbed.
func (s *EpisodeMissingSearcher) grabEpisode(
	ctx context.Context,
	show *ent.TVShow,
	se *ent.Season,
	e *ent.Episode,
	titles []string,
	profile quality.Profile,
	grabbed map[uint32]struct{},
) bool {
	if _, already := grabbed[e.ID]; already {
		return false
	}
	results, _, err := s.indexers.SearchEpisode(
		ctx, titles, show.TvdbID, se.Number, e.Number,
	)
	if err != nil {
		slog.WarnContext(ctx, "tv missing-search: episode search failed",
			"show", show.Title, "season", se.Number, "episode", e.Number,
			"error", err)
		return false
	}

	for _, r := range rankAccepted(profile, results, singleRelease) {
		if _, err := s.downloads.GrabEpisode(
			ctx,
			r,
			e.ID,
			[]uint32{e.ID},
		); err != nil {
			slog.WarnContext(ctx, "tv missing-search: episode grab failed",
				"show", show.Title, "season", se.Number, "episode", e.Number,
				"release", r.Title, "error", err)
			if !transportFailure(err) {
				if ierr := s.store.IncrementEpisodeGrabFailures(
					ctx,
					e.ID,
				); ierr != nil {
					slog.WarnContext(ctx,
						"tv missing-search: bump episode grab_failures failed",
						"episode.id", e.ID, "error", ierr)
				}
			}
			continue
		}
		grabbed[e.ID] = struct{}{}
		if err := s.store.SetEpisodeStatus(
			ctx, e.ID, episode.StatusDownloading,
		); err != nil {
			slog.WarnContext(ctx,
				"tv missing-search: set episode status failed",
				"episode.id", e.ID, "error", err)
		}
		if err := s.store.ResetEpisodeGrabFailures(ctx, e.ID); err != nil {
			slog.WarnContext(ctx,
				"tv missing-search: reset episode grab_failures failed",
				"episode.id", e.ID, "error", err)
		}
		stampEpisodeSearch(ctx, s.store, e.ID, time.Now())
		slog.InfoContext(ctx, "tv missing-search: grabbed episode",
			"show", show.Title, "season", se.Number, "episode", e.Number,
			"release", r.Title)
		return true
	}
	// No acceptable release: still advance the cooldown counter.
	stampEpisodeSearch(ctx, s.store, e.ID, time.Now())
	return false
}

// markEpisodeDownloading flips an episode to downloading and stamps
// last_search_at, logging (not returning) any store failure.
func markEpisodeDownloading(
	ctx context.Context,
	store EligibleEpisodeLister,
	id uint32,
	when time.Time,
) {
	if err := store.SetEpisodeStatus(
		ctx, id, episode.StatusDownloading,
	); err != nil {
		slog.WarnContext(ctx, "tv sync: set episode status failed",
			"episode.id", id, "error", err)
	}
	stampEpisodeSearch(ctx, store, id, when)
}

// stampEpisodeSearch records last_search_at, logging any store failure.
func stampEpisodeSearch(
	ctx context.Context,
	store EligibleEpisodeLister,
	id uint32,
	when time.Time,
) {
	if err := store.SetEpisodeLastSearchAt(ctx, id, when); err != nil {
		slog.WarnContext(ctx,
			"tv sync: set episode last_search_at failed",
			"episode.id", id, "error", err)
	}
}
