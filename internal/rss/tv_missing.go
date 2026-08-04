package rss

import (
	"context"
	"log/slog"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/episode"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/otelx"
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
	for _, show := range shows {
		titles := []string{show.Title}
		for _, se := range show.Edges.Seasons {
			s.processSeason(ctx, show, se, titles)
		}
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
	for _, show := range shows {
		if show.ID != showID {
			continue
		}
		titles := []string{show.Title}
		for _, se := range show.Edges.Seasons {
			s.processSeason(ctx, show, se, titles)
		}
	}
	return nil
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
// ListEligibleEpisodesForSync. With two or more wanted episodes it tries a
// season pack first; otherwise (or on no acceptable pack) it falls back to
// per-episode.
func (s *EpisodeMissingSearcher) processSeason(
	ctx context.Context,
	show *ent.TVShow,
	se *ent.Season,
	titles []string,
) {
	wanted := se.Edges.Episodes
	if len(wanted) == 0 {
		return
	}
	quality := qualityFor(ctx, show.QualityProfile)

	// Prefer a season pack when the whole season (2+ episodes) is wanted.
	if len(wanted) >= 2 &&
		s.grabSeasonPack(ctx, show, se, titles, wanted, quality) {
		return
	}

	for _, e := range wanted {
		s.grabEpisode(ctx, show, se, e, titles, quality)
	}
}

// grabSeasonPack searches for a season pack and, on the first acceptable
// result, grabs it against the first wanted episode and marks every wanted
// episode in the season as downloading. Reports whether a pack was grabbed.
func (s *EpisodeMissingSearcher) grabSeasonPack(
	ctx context.Context,
	show *ent.TVShow,
	se *ent.Season,
	titles []string,
	wanted []*ent.Episode,
	quality QualityConfig,
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
	for _, r := range packs {
		if !quality.Accepts(r.Title) {
			continue
		}
		if _, err := s.downloads.GrabEpisode(ctx, r, wanted[0].ID); err != nil {
			slog.WarnContext(ctx, "tv missing-search: season-pack grab failed",
				"show", show.Title, "season", se.Number,
				"release", r.Title, "error", err)
			continue
		}
		span.SetAttributes(attribute.String("release.title", r.Title))
		now := time.Now()
		for _, e := range wanted {
			s.markDownloading(ctx, e.ID, now)
		}
		slog.InfoContext(ctx, "tv missing-search: grabbed season pack",
			"show", show.Title, "season", se.Number,
			"release", r.Title, "episodes", len(wanted))
		return true
	}
	return false
}

// grabEpisode searches for a single episode and grabs the first acceptable
// result, bumping/resetting grab_failures accordingly. last_search_at is
// stamped whenever the indexer responds so the cooldown counter advances.
func (s *EpisodeMissingSearcher) grabEpisode(
	ctx context.Context,
	show *ent.TVShow,
	se *ent.Season,
	e *ent.Episode,
	titles []string,
	quality QualityConfig,
) {
	results, err := s.indexers.SearchEpisode(
		ctx, titles, show.TvdbID, se.Number, e.Number,
	)
	if err != nil {
		slog.WarnContext(ctx, "tv missing-search: episode search failed",
			"show", show.Title, "season", se.Number, "episode", e.Number,
			"error", err)
		return
	}

	for _, r := range results {
		if !quality.Accepts(r.Title) {
			continue
		}
		if _, err := s.downloads.GrabEpisode(ctx, r, e.ID); err != nil {
			slog.WarnContext(ctx, "tv missing-search: episode grab failed",
				"show", show.Title, "season", se.Number, "episode", e.Number,
				"release", r.Title, "error", err)
			if ierr := s.store.IncrementEpisodeGrabFailures(ctx, e.ID); ierr != nil {
				slog.WarnContext(ctx,
					"tv missing-search: bump episode grab_failures failed",
					"episode.id", e.ID, "error", ierr)
			}
			continue
		}
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
		s.stampLastSearch(ctx, e.ID, time.Now())
		slog.InfoContext(ctx, "tv missing-search: grabbed episode",
			"show", show.Title, "season", se.Number, "episode", e.Number,
			"release", r.Title)
		return
	}
	// No acceptable release: still advance the cooldown counter.
	s.stampLastSearch(ctx, e.ID, time.Now())
}

// markDownloading flips an episode to downloading and stamps last_search_at,
// logging (not returning) any store failure.
func (s *EpisodeMissingSearcher) markDownloading(
	ctx context.Context,
	id uint32,
	when time.Time,
) {
	if err := s.store.SetEpisodeStatus(
		ctx, id, episode.StatusDownloading,
	); err != nil {
		slog.WarnContext(ctx, "tv missing-search: set episode status failed",
			"episode.id", id, "error", err)
	}
	s.stampLastSearch(ctx, id, when)
}

// stampLastSearch records last_search_at, logging any store failure.
func (s *EpisodeMissingSearcher) stampLastSearch(
	ctx context.Context,
	id uint32,
	when time.Time,
) {
	if err := s.store.SetEpisodeLastSearchAt(ctx, id, when); err != nil {
		slog.WarnContext(ctx,
			"tv missing-search: set episode last_search_at failed",
			"episode.id", id, "error", err)
	}
}
