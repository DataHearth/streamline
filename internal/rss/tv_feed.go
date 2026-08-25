package rss

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/tvshow"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/quality"

	"go.opentelemetry.io/otel/attribute"
)

// TVFeedScanner is the series twin of FeedScanner: it pulls each indexer's RSS
// forward-feed once per tick and grabs items matching wanted episodes. Without
// it a newly aired episode waits for the next tv-missing-search pass (12h by
// default) while a movie in the same library is grabbed within the feed
// interval.
type TVFeedScanner struct {
	store     EligibleEpisodeLister
	indexers  IndexerFeeder
	downloads EpisodeGrabber
}

func NewTVFeedScanner(
	store EligibleEpisodeLister,
	indexers IndexerFeeder,
	downloads EpisodeGrabber,
) *TVFeedScanner {
	return &TVFeedScanner{
		store:     store,
		indexers:  indexers,
		downloads: downloads,
	}
}

func (s *TVFeedScanner) Run(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "rss.tv_feed_scan")
	defer span.End()

	indexers := config.EnabledIndexers()
	if len(indexers) == 0 {
		return nil
	}

	// The no-match cooldown gates indexer *queries*, and a feed item is already
	// in hand — so it is waived here (NotSearchedSince = now matches every row).
	// The failure cap is kept: an episode whose grabs keep failing would
	// otherwise be retried on every tick.
	now := time.Now()
	shows, err := s.store.ListEligibleEpisodesForSync(
		ctx,
		config.Get().Library.MaxGrabFailures,
		now,
		now,
	)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	// Same reasoning as FeedScanner.Run: no client means every grab below fails
	// identically, after pulling each indexer's whole feed. Kept after the query
	// so the span still carries the backlog size.
	if _, ok := config.PickDownloadClient(); !ok {
		span.SetAttributes(attribute.Int("rss.tv_feed_scan.shows", len(shows)))
		slog.InfoContext(ctx, "tv feed-scan: no enabled download client",
			"shows", len(shows))
		return nil
	}

	index := buildEpisodeIndex(shows)
	profiles := resolveShowProfiles(ctx, shows)
	// grabbed tracks episode IDs already attempted this tick so a second indexer
	// carrying the same release doesn't double-grab.
	grabbed := make(map[uint32]struct{})
	var matched int
	for _, idx := range indexers {
		items, err := s.indexers.Feed(ctx, idx.Name)
		if err != nil {
			slog.WarnContext(ctx, "tv feed-scan: indexer failed",
				"indexer", idx.Name, "error", err)
			continue
		}
		matched += s.processItems(ctx, items, index, profiles, grabbed)
	}

	span.SetAttributes(
		attribute.Int("rss.tv_feed_scan.indexers", len(indexers)),
		attribute.Int("rss.tv_feed_scan.matched", matched),
	)
	slog.InfoContext(ctx, "tv feed-scan complete",
		"indexers", len(indexers), "matched", matched)
	return nil
}

func (s *TVFeedScanner) processItems(
	ctx context.Context,
	items []indexer.SearchResult,
	index map[string]*wantedShow,
	profiles map[string]quality.Profile,
	grabbed map[uint32]struct{},
) int {
	matched := 0
	for _, item := range items {
		// A complete-series or multi-season pack imports seasons this pass never
		// looked at, so it is left to a deliberate manual grab.
		if library.IsWholeSeriesPack(item.Title) {
			continue
		}
		parsed := library.Parse(item.Title)
		ws := index[releaseShowKey(parsed)]
		if ws == nil {
			continue
		}
		if res := evaluateRelease(
			profiles[ws.show.QualityProfile], item,
		); res.Rejected {
			slog.DebugContext(ctx, "tv feed-scan: quality rejected",
				"show", ws.show.Title, "release", item.Title,
				"reason", res.RejectReason)
			continue
		}
		if parsed.SeasonPack {
			matched += s.grabPack(ctx, ws, parsed.Season, item, grabbed)
			continue
		}
		e := ws.lookup(parsed)
		if e == nil {
			continue
		}
		if _, already := grabbed[e.ID]; already {
			continue
		}
		grabbed[e.ID] = struct{}{}
		if s.grabOne(ctx, ws.show, e, item) {
			matched++
		}
	}
	return matched
}

// grabPack grabs a season pack against the season's first wanted episode and
// marks every wanted episode in that season downloading — the same 1 record ↔ N
// episodes shape the missing-search pack path produces. Returns how many
// episodes the pack covered.
func (s *TVFeedScanner) grabPack(
	ctx context.Context,
	ws *wantedShow,
	season uint16,
	item indexer.SearchResult,
	grabbed map[uint32]struct{},
) int {
	wanted := make([]*ent.Episode, 0, len(ws.seasons[season]))
	for _, e := range ws.seasons[season] {
		if _, already := grabbed[e.ID]; !already {
			wanted = append(wanted, e)
		}
	}
	if len(wanted) == 0 {
		return 0
	}
	if _, err := s.downloads.GrabEpisode(ctx, item, wanted[0].ID); err != nil {
		slog.WarnContext(ctx, "tv feed-scan: season-pack grab failed",
			"show", ws.show.Title, "season", season,
			"release", item.Title, "error", err)
		return 0
	}
	now := time.Now()
	for _, e := range wanted {
		grabbed[e.ID] = struct{}{}
		markEpisodeDownloading(ctx, s.store, e.ID, now)
	}
	slog.InfoContext(ctx, "tv feed-scan: grabbed season pack",
		"show", ws.show.Title, "season", season,
		"release", item.Title, "episodes", len(wanted))
	return len(wanted)
}

func (s *TVFeedScanner) grabOne(
	ctx context.Context,
	show *ent.TVShow,
	e *ent.Episode,
	item indexer.SearchResult,
) bool {
	if _, err := s.downloads.GrabEpisode(ctx, item, e.ID); err != nil {
		slog.WarnContext(ctx, "tv feed-scan: episode grab failed",
			"show", show.Title, "episode.id", e.ID,
			"release", item.Title, "error", err)
		if ierr := s.store.IncrementEpisodeGrabFailures(ctx, e.ID); ierr != nil {
			slog.WarnContext(ctx, "tv feed-scan: bump episode grab_failures failed",
				"episode.id", e.ID, "error", ierr)
		}
		return false
	}
	if err := s.store.ResetEpisodeGrabFailures(ctx, e.ID); err != nil {
		slog.WarnContext(ctx, "tv feed-scan: reset episode grab_failures failed",
			"episode.id", e.ID, "error", err)
	}
	markEpisodeDownloading(ctx, s.store, e.ID, time.Now())
	slog.InfoContext(ctx, "tv feed-scan: grabbed episode",
		"show", show.Title, "episode.id", e.ID, "release", item.Title)
	return true
}

// wantedShow indexes one show's searchable episodes by every key a release name
// can carry. Only the show's own numbering scheme gets a fallback map: the
// absolute-number and daily-date parses are heuristic, and applied to a
// standard show they would collide with plain season/episode numbering.
type wantedShow struct {
	show     *ent.TVShow
	seasons  map[uint16][]*ent.Episode
	byNumber map[[2]uint16]*ent.Episode
	byAbs    map[uint16]*ent.Episode
	byDate   map[string]*ent.Episode
}

func (w *wantedShow) lookup(p library.ParseResult) *ent.Episode {
	switch {
	case p.Episode > 0:
		return w.byNumber[[2]uint16{p.Season, p.Episode}]
	case p.AbsoluteNumber > 0:
		return w.byAbs[p.AbsoluteNumber]
	case p.AirDate != nil:
		return w.byDate[p.AirDate.Format(time.DateOnly)]
	}
	return nil
}

func buildEpisodeIndex(shows []*ent.TVShow) map[string]*wantedShow {
	index := make(map[string]*wantedShow, len(shows))
	for _, show := range shows {
		w := &wantedShow{
			show:     show,
			seasons:  make(map[uint16][]*ent.Episode),
			byNumber: make(map[[2]uint16]*ent.Episode),
		}
		switch show.Type {
		case tvshow.TypeAnime:
			w.byAbs = make(map[uint16]*ent.Episode)
		case tvshow.TypeDaily:
			w.byDate = make(map[string]*ent.Episode)
		}
		for _, se := range show.Edges.Seasons {
			for _, e := range se.Edges.Episodes {
				w.seasons[se.Number] = append(w.seasons[se.Number], e)
				w.byNumber[[2]uint16{se.Number, e.Number}] = e
				if w.byAbs != nil && e.AbsoluteNumber > 0 {
					w.byAbs[e.AbsoluteNumber] = e
				}
				if w.byDate != nil && !e.AirDate.IsZero() {
					w.byDate[e.AirDate.Format(time.DateOnly)] = e
				}
			}
		}
		index[showKey(show.Title)] = w
		if show.OriginalTitle != "" {
			index[showKey(show.OriginalTitle)] = w
		}
	}
	return index
}

var (
	// The opening bracket of a fansub tag is often already gone by the time a
	// release name reaches here — library.Parse trims leading delimiters off the
	// title — so it is optional, and everything through the first closing
	// bracket goes. No show title carries one.
	fansubTagRe = regexp.MustCompile(`^\[?[^\]]*\]\s*`)
	separatorRe = regexp.MustCompile(`[^a-z0-9]+`)
)

// showKey normalizes a title to its comparable form: a leading fansub tag
// dropped, everything lowercased, every separator collapsed to one space. Both
// sides of the lookup go through it, so "The.Black.Sea" and "The Black Sea"
// land on the same key.
func showKey(title string) string {
	k := fansubTagRe.ReplaceAllString(strings.ToLower(title), "")
	return strings.TrimSpace(separatorRe.ReplaceAllString(k, " "))
}

// releaseShowKey derives the show key from a parsed release name. library.Parse
// cuts the title at the first technical token, which for a season pack or an
// anime release is not the numbering token — "Show.S03.1080p" parses to
// "Show S03" and "[Grp] Show - 18 [1080p]" to "Show 18" — so the number the
// parser already extracted is trimmed back off.
func releaseShowKey(p library.ParseResult) string {
	k := showKey(p.Title)
	if p.SeasonPack {
		k = strings.TrimSuffix(k, fmt.Sprintf(" s%02d", p.Season))
	}
	if p.AbsoluteNumber > 0 {
		k = strings.TrimSuffix(k, fmt.Sprintf(" %d", p.AbsoluteNumber))
	}
	return strings.TrimSpace(k)
}

// resolveShowProfiles resolves each distinct profile name once per tick, for
// the same reason resolveProfiles does on the movie side: a per-item lookup
// recompiles every custom format's regex for every item in every feed.
func resolveShowProfiles(
	ctx context.Context,
	shows []*ent.TVShow,
) map[string]quality.Profile {
	profiles := make(map[string]quality.Profile)
	for _, show := range shows {
		if _, ok := profiles[show.QualityProfile]; ok {
			continue
		}
		profiles[show.QualityProfile] = qualityFor(ctx, show.QualityProfile)
	}
	return profiles
}
