package rss

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/tvshow"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/quality"
	"github.com/datahearth/streamline/internal/quality/qualityctx"

	"go.opentelemetry.io/otel/attribute"
)

// TVFeedScanner is the series twin of FeedScanner: it pulls each indexer's RSS
// forward-feed once per tick and grabs items matching wanted episodes. Without
// it a newly aired episode waits for the next tv-missing-search pass (12h by
// default) while a movie in the same library is grabbed within the feed
// interval. Episodes already on disk are matched too, and grabbed as an
// upgrade when the release outscores what is there.
type TVFeedScanner struct {
	store     TVFeedStore
	indexers  IndexerFeeder
	downloads EpisodeGrabber
}

func NewTVFeedScanner(
	store TVFeedStore,
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

	upgradable, err := s.store.ListUpgradeCandidateShows(ctx)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}

	pass := &tvPass{
		wanted:   buildEpisodeIndex(shows),
		upgrades: buildEpisodeIndex(upgradable),
		profiles: resolveShowProfiles(ctx, shows, upgradable),
		// grabbed tracks episode IDs already attempted this tick so a second
		// indexer carrying the same release doesn't double-grab, and so a wanted
		// grab and an upgrade grab never both fire for one episode.
		grabbed: make(map[uint32]struct{}),
	}
	var matched int
	for _, idx := range indexers {
		items, err := s.indexers.Feed(ctx, idx.Name)
		if err != nil {
			slog.WarnContext(ctx, "tv feed-scan: indexer failed",
				"indexer", idx.Name, "error", err)
			continue
		}
		matched += s.processItems(ctx, items, pass)
	}

	span.SetAttributes(
		attribute.Int("rss.tv_feed_scan.indexers", len(indexers)),
		attribute.Int("rss.tv_feed_scan.matched", matched),
	)
	slog.InfoContext(ctx, "tv feed-scan complete",
		"indexers", len(indexers), "matched", matched)
	return nil
}

// tvPass carries one tick's state: the two show indexes items are matched
// against, the profiles resolved for the shows in them, and the episode IDs
// already grabbed.
type tvPass struct {
	wanted   map[string]*wantedShow
	upgrades map[string]*wantedShow
	profiles map[string]quality.Profile
	grabbed  map[uint32]struct{}
}

func (s *TVFeedScanner) processItems(
	ctx context.Context,
	items []indexer.SearchResult,
	pass *tvPass,
) int {
	matched := 0
	for _, item := range items {
		// A complete-series or multi-season pack imports seasons this pass never
		// looked at, so it is left to a deliberate manual grab.
		if library.IsWholeSeriesPack(item.Title) {
			continue
		}
		parsed := library.Parse(item.Title)
		key := releaseShowKey(parsed)
		ws, us := pass.wanted[key], pass.upgrades[key]
		// Both indexes hold the same row for a show with wanted *and* on-disk
		// episodes, so the profile is one lookup either way.
		show := indexedShow(ws, us)
		if show == nil {
			continue
		}
		p := pass.profiles[show.QualityProfile]
		res := evaluateRelease(p, item)
		if res.Rejected {
			slog.DebugContext(ctx, "tv feed-scan: quality rejected",
				"show", show.Title, "release", item.Title,
				"reason", res.RejectReason)
			continue
		}
		if n := s.grabWanted(ctx, ws, parsed, item, p, pass); n > 0 {
			matched += n
			continue
		}
		// The wanted branch reports 0 both for "no such episode here" and for
		// "the grab failed"; either way the file on disk, if any, is still the
		// one to beat, and the upgrade branch re-checks `grabbed` itself.
		matched += s.grabUpgrade(ctx, us, parsed, item, p, res, pass)
	}
	return matched
}

func indexedShow(indexes ...*wantedShow) *ent.TVShow {
	for _, w := range indexes {
		if w != nil {
			return w.show
		}
	}
	return nil
}

// grabWanted grabs item for the episodes of ws it fills, as a pack or a single
// episode. Reports how many episodes it covered.
func (s *TVFeedScanner) grabWanted(
	ctx context.Context,
	ws *wantedShow,
	parsed library.ParseResult,
	item indexer.SearchResult,
	p quality.Profile,
	pass *tvPass,
) int {
	if ws == nil {
		return 0
	}
	if parsed.SeasonPack {
		return s.grabPack(ctx, ws, parsed.Season, item, p, pass.grabbed)
	}
	e := ws.lookup(parsed)
	if e == nil {
		return 0
	}
	if _, already := pass.grabbed[e.ID]; already {
		return 0
	}
	pass.grabbed[e.ID] = struct{}{}
	if !s.grabOne(ctx, ws.show, e, item) {
		return 0
	}
	return 1
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
	p quality.Profile,
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
	rec, err := s.downloads.GrabEpisode(ctx, item, wanted[0].ID)
	if err != nil {
		slog.WarnContext(ctx, "tv feed-scan: season-pack grab failed",
			"show", ws.show.Title, "season", season,
			"release", item.Title, "error", err)
		return 0
	}
	// The pack is grabbed for the gap, but it may also beat files already on
	// disk. Under the default per-episode rule the importer decides that per
	// episode, so upgrades is what lets it. A profile that opted into
	// replace_whole_season wants no per-episode veto here either — writing
	// upgrades would let this gap-filling grab quietly replace whatever
	// individual files it beats, exactly the mixed-source patchwork the key
	// exists to prevent, so it gets none instead: the files already on disk
	// are left alone, matching the old bool-based behaviour.
	mode := downloadrecord.ReplaceModeUpgrades
	if p.ReplaceWholeSeason {
		mode = downloadrecord.ReplaceModeNone
	}
	if err := s.store.SetDownloadRecordReplaceMode(
		ctx, rec.ID, mode,
	); err != nil {
		slog.ErrorContext(ctx, "tv feed-scan: set replace mode failed",
			"show", ws.show.Title, "record.id", rec.ID, "error", err)
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

// grabUpgrade grabs item as a replacement for the files it beats, flagging
// the record so the importer overwrites instead of skipping. A pack is
// judged per episode by default — only the episodes the release actually
// outscores are grabbed and marked; see grabWholeSeasonUpgrade for the
// all-or-nothing rule a profile can opt into. Reports how many episodes it
// covered.
func (s *TVFeedScanner) grabUpgrade(
	ctx context.Context,
	us *wantedShow,
	parsed library.ParseResult,
	item indexer.SearchResult,
	p quality.Profile,
	rel quality.Result,
	pass *tvPass,
) int {
	if us == nil || !p.UpgradeAllowed {
		return 0
	}
	var targets []*ent.Episode
	if parsed.SeasonPack {
		for _, e := range us.seasons[parsed.Season] {
			if _, already := pass.grabbed[e.ID]; !already {
				targets = append(targets, e)
			}
		}
	} else if e := us.lookup(parsed); e != nil {
		if _, already := pass.grabbed[e.ID]; !already {
			targets = append(targets, e)
		}
	}
	if len(targets) == 0 {
		return 0
	}
	if p.ReplaceWholeSeason {
		return s.grabWholeSeasonUpgrade(ctx, us, targets, item, p, rel, pass)
	}
	release := qualityctx.ContextFromRelease(item.Title, item.Size, item.Seeders)
	var selected []*ent.Episode
	for _, e := range targets {
		if len(e.Edges.MediaFiles) == 0 {
			continue
		}
		mf := e.Edges.MediaFiles[0]
		file := qualityctx.ContextFromFile(
			filepath.Base(mf.Path), mf.Size, int(mf.Width), mf.VideoCodec,
		)
		if quality.ReplacesFile(p, file, release) {
			selected = append(selected, e)
		}
	}
	if len(selected) == 0 {
		return 0
	}

	rec, err := s.downloads.GrabEpisode(ctx, item, selected[0].ID)
	if err != nil {
		slog.WarnContext(ctx, "tv feed-scan: upgrade grab failed",
			"show", us.show.Title, "release", item.Title, "error", err)
		if ierr := s.store.IncrementEpisodeGrabFailures(
			ctx, selected[0].ID,
		); ierr != nil {
			slog.WarnContext(ctx, "tv feed-scan: bump episode grab_failures failed",
				"episode.id", selected[0].ID, "error", ierr)
		}
		return 0
	}
	// Without the flag the importer skips every episode that already has a file,
	// so the upgrade this run just grabbed can never land: actionable, not noise.
	if err := s.store.SetDownloadRecordReplaceMode(
		ctx, rec.ID, downloadrecord.ReplaceModeUpgrades,
	); err != nil {
		slog.ErrorContext(ctx, "tv feed-scan: set replace mode failed",
			"show", us.show.Title, "record.id", rec.ID, "error", err)
	}
	now := time.Now()
	for _, e := range selected {
		pass.grabbed[e.ID] = struct{}{}
		markEpisodeDownloading(ctx, s.store, e.ID, now)
	}
	slog.InfoContext(ctx, "tv feed-scan: grabbed upgrade",
		"show", us.show.Title, "release", item.Title,
		"episodes", len(selected), "considered", len(targets))
	return len(selected)
}

// grabWholeSeasonUpgrade is the ReplaceWholeSeason escape hatch: a pack must
// beat the season's *best* file, since there is no per-episode veto once it
// is grabbed, and beating the worst would replace every other episode with
// something worse. Reports how many episodes it covered.
func (s *TVFeedScanner) grabWholeSeasonUpgrade(
	ctx context.Context,
	us *wantedShow,
	targets []*ent.Episode,
	item indexer.SearchResult,
	p quality.Profile,
	rel quality.Result,
	pass *tvPass,
) int {
	best := 0
	for _, e := range targets {
		if len(e.Edges.MediaFiles) == 0 {
			return 0
		}
		mf := e.Edges.MediaFiles[0]
		file := qualityctx.ContextFromFile(
			filepath.Base(mf.Path), mf.Size, int(mf.Width), mf.VideoCodec,
		)
		if !p.UpgradableFrom(file.Resolution) {
			slog.DebugContext(
				ctx,
				"tv feed-scan: upgrade skipped, file outside band",
				"show",
				us.show.Title,
				"episode.id",
				e.ID,
				"file.resolution",
				file.Resolution,
				"release",
				item.Title,
			)
			return 0
		}
		if score := quality.Evaluate(p, file).Score; score > best {
			best = score
		}
	}
	if !p.ShouldUpgrade(best, rel.Score) {
		return 0
	}

	rec, err := s.downloads.GrabEpisode(ctx, item, targets[0].ID)
	if err != nil {
		slog.WarnContext(ctx, "tv feed-scan: upgrade grab failed",
			"show", us.show.Title, "release", item.Title, "error", err)
		if ierr := s.store.IncrementEpisodeGrabFailures(
			ctx, targets[0].ID,
		); ierr != nil {
			slog.WarnContext(ctx, "tv feed-scan: bump episode grab_failures failed",
				"episode.id", targets[0].ID, "error", ierr)
		}
		return 0
	}
	// Without the flag the importer skips every episode that already has a file,
	// so the upgrade this run just grabbed can never land: actionable, not noise.
	if err := s.store.SetDownloadRecordReplaceMode(
		ctx, rec.ID, downloadrecord.ReplaceModeAll,
	); err != nil {
		slog.ErrorContext(ctx, "tv feed-scan: set replace mode failed",
			"show", us.show.Title, "record.id", rec.ID, "error", err)
	}
	now := time.Now()
	for _, e := range targets {
		pass.grabbed[e.ID] = struct{}{}
		markEpisodeDownloading(ctx, s.store, e.ID, now)
	}
	slog.InfoContext(ctx, "tv feed-scan: grabbed upgrade",
		"show", us.show.Title, "release", item.Title, "episodes", len(targets),
		"file_score", best, "release_score", rel.Score)
	return len(targets)
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
	lists ...[]*ent.TVShow,
) map[string]quality.Profile {
	profiles := make(map[string]quality.Profile)
	for _, list := range lists {
		for _, show := range list {
			if _, ok := profiles[show.QualityProfile]; ok {
				continue
			}
			profiles[show.QualityProfile] = qualityFor(ctx, show.QualityProfile)
		}
	}
	return profiles
}
