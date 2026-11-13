package rss

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/tvshow"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/library/roothealth"
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
	ctx, span := tracer.Start(download.Automatic(ctx), "rss.tv_feed_scan")
	defer span.End()

	indexers := config.EnabledIndexers()
	if len(indexers) == 0 {
		return nil
	}
	status, err := roothealth.Offline(ctx, db.RootSeries)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	if status != nil {
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
	// Which show a name belongs to is decided over the whole library: a show
	// absent from both listings this tick still owns its title, and another
	// show's alias must not take its releases.
	library, err := s.store.ListShowNames(ctx)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	names := resolveShowNames(ctx, library, shows, upgradable)
	// Every grab below is automatic; without the blocklist any of them could
	// be a release the user rejected.
	blocklist, err := s.store.RejectedReleases(ctx)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}

	// Unfiltered season lengths: the two indexes above are narrowed to the
	// episodes this pass cares about, and a pack costs us every episode it
	// holds regardless. A failure here only unscales the size bounds, which is
	// how they behaved before they scaled at all, so the tick continues.
	seasonCounts, err := s.store.SeasonEpisodeCounts(ctx, showIDs(shows, upgradable))
	if err != nil {
		slog.WarnContext(ctx, "tv feed-scan: season counts unavailable",
			"error", err)
		seasonCounts = nil
	}

	pass := &tvPass{
		names:        names,
		wanted:       buildEpisodeIndex(shows),
		upgrades:     buildEpisodeIndex(upgradable),
		profiles:     resolveShowProfiles(ctx, shows, upgradable),
		seasonCounts: seasonCounts,
		rejected:     blocklist,
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

// tvPass carries one tick's state: which show a release name belongs to, the
// two show indexes by show ID, the profiles resolved for the shows in them,
// and the episode IDs already grabbed. A release resolves to one show ID
// first, so it names the same show in both indexes or neither.
type tvPass struct {
	names    showNames
	wanted   map[uint32]*wantedShow
	upgrades map[uint32]*wantedShow
	profiles map[string]quality.Profile
	// seasonCounts is show id -> season number -> episode total, unfiltered.
	// Sizes a pack; nil when the lookup failed, which leaves size bounds
	// unscaled rather than blocking the tick.
	seasonCounts map[uint32]map[uint16]int
	grabbed      map[uint32]struct{}
	// rejected is the releases the user rejected, matched per show.
	rejected *db.ReleaseBlocklist
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
		id, ok := pass.names.resolve(releaseShowKey(parsed), parsed.Year)
		if !ok {
			continue
		}
		ws, us := pass.wanted[id], pass.upgrades[id]
		// Both indexes hold the same row for a show with wanted *and* on-disk
		// episodes, so the profile is one lookup either way.
		show := indexedShow(ws, us)
		if show == nil {
			continue
		}
		// Both branches below grab for this show, wanted and upgrade alike.
		if rejected(ctx, item, pass.rejected.Show, show.ID) {
			continue
		}
		p := pass.profiles[show.QualityProfile]
		episodes := releaseEpisodes(item.Title, show.ID, pass.seasonCounts)
		res := evaluateRelease(p, item, episodes)
		if res.Rejected {
			slog.DebugContext(ctx, "tv feed-scan: quality rejected",
				"show", show.Title, "release", item.Title,
				"reason", res.RejectReason)
			continue
		}
		// Built once and threaded through both branches below: it depends only
		// on the release and the library's season length, not on which show
		// index (wanted/upgrade) ends up using it.
		release := qualityctx.ContextFromRelease(
			item.Title, item.Size, item.Seeders, item.HasSeeders, episodes,
		)
		if n := s.grabWanted(ctx, ws, us, parsed, item, p, release, pass); n > 0 {
			matched += n
			continue
		}
		// The wanted branch reports 0 both for "no such episode here" and for
		// "the grab failed"; either way the file on disk, if any, is still the
		// one to beat, and the upgrade branch re-checks `grabbed` itself.
		matched += s.grabUpgrade(ctx, us, parsed, item, p, release, pass)
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
	us *wantedShow,
	parsed library.ParseResult,
	item indexer.SearchResult,
	p quality.Profile,
	release quality.ReleaseContext,
	pass *tvPass,
) int {
	if ws == nil {
		return 0
	}
	if parsed.SeasonPack {
		return s.grabPack(ctx, ws, us, parsed.Season, item, p, release, pass)
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

// beatEpisodes returns the episodes of us's season, excluding any already
// grabbed this tick, whose current file the release beats. grabPack (a
// gap-fill pack that also replaces what it beats) and grabUpgrade (an
// upgrade-only pack) share this one scoring loop so the "beats" rule can
// never read differently between the two paths.
func beatEpisodes(
	us *wantedShow,
	season uint16,
	p quality.Profile,
	release quality.ReleaseContext,
	grabbed map[uint32]struct{},
) []*ent.Episode {
	if us == nil {
		return nil
	}
	var beat []*ent.Episode
	for _, e := range us.seasons[season] {
		if _, already := grabbed[e.ID]; already {
			continue
		}
		files := db.PresentFiles(e.Edges.MediaFiles)
		if len(files) == 0 {
			continue
		}
		file := qualityctx.ContextFromRow(files[0])
		if quality.ReplacesFile(p, file, release) {
			beat = append(beat, e)
		}
	}
	return beat
}

// grabPack grabs a season pack against the season's first still-open episode.
// The wanted set is the season's missing episodes plus, under an
// upgrade-permitting profile, the on-disk episodes the same release beats —
// one grab covers both the gap and what it replaces. Only the missing
// episodes are marked downloading; a beaten episode keeps its file's status
// ("available") since the importer, not the marker, decides it per file via
// replace_mode upgrades. Returns how many episodes the pack covered.
func (s *TVFeedScanner) grabPack(
	ctx context.Context,
	ws *wantedShow,
	us *wantedShow,
	season uint16,
	item indexer.SearchResult,
	p quality.Profile,
	release quality.ReleaseContext,
	pass *tvPass,
) int {
	missing := make([]*ent.Episode, 0, len(ws.seasons[season]))
	for _, e := range ws.seasons[season] {
		if _, already := pass.grabbed[e.ID]; !already {
			missing = append(missing, e)
		}
	}
	var beat []*ent.Episode
	if p.UpgradeAllowed {
		beat = beatEpisodes(us, season, p, release, pass.grabbed)
	}
	wanted := append(missing, beat...)
	if len(wanted) == 0 {
		return 0
	}
	wantedIDs := make([]uint32, len(wanted))
	for i, e := range wanted {
		wantedIDs[i] = e.ID
	}
	rec, err := s.downloads.GrabEpisode(ctx, item, wanted[0].ID, wantedIDs)
	if err != nil {
		slog.WarnContext(ctx, "tv feed-scan: season-pack grab failed",
			"show", ws.show.Title, "season", season,
			"release", item.Title, "error", err)
		// ErrNoWantedFiles means the release holds nothing for these episodes,
		// so the anchor is no closer to being filled than before — the same
		// standing the single-episode path counts. Left uncounted, a season
		// whose only packs are all mismatches would be re-tried on every feed
		// tick without ever reaching the max_grab_failures ceiling.
		if errors.Is(err, download.ErrNoWantedFiles) {
			if ierr := s.store.IncrementEpisodeGrabFailures(
				ctx, wanted[0].ID,
			); ierr != nil {
				slog.WarnContext(ctx,
					"tv feed-scan: bump episode grab_failures failed",
					"episode.id", wanted[0].ID, "error", ierr)
			}
		}
		return 0
	}
	// The pack is grabbed for the gap, but it may also beat files already on
	// disk. The importer decides that per episode, and upgrades is what lets
	// it.
	if err := s.store.SetDownloadRecordReplaceMode(
		ctx, rec.ID, downloadrecord.ReplaceModeUpgrades,
	); err != nil {
		slog.ErrorContext(ctx, "tv feed-scan: set replace mode failed",
			"show", ws.show.Title, "record.id", rec.ID, "error", err)
	}
	now := time.Now()
	for _, e := range missing {
		pass.grabbed[e.ID] = struct{}{}
		markEpisodeDownloading(ctx, s.store, e.ID, now)
	}
	for _, e := range beat {
		pass.grabbed[e.ID] = struct{}{}
	}
	slog.InfoContext(ctx, "tv feed-scan: grabbed season pack",
		"show", ws.show.Title, "season", season,
		"release", item.Title, "filled", len(missing), "replaced", len(beat))
	return len(wanted)
}

// grabUpgrade grabs item as a replacement for the files it beats, flagging
// the record so the importer overwrites instead of skipping. A pack is judged
// per episode: only the episodes the release actually outscores are grabbed
// and marked. Reports how many episodes it covered.
func (s *TVFeedScanner) grabUpgrade(
	ctx context.Context,
	us *wantedShow,
	parsed library.ParseResult,
	item indexer.SearchResult,
	p quality.Profile,
	release quality.ReleaseContext,
	pass *tvPass,
) int {
	if us == nil || !p.UpgradeAllowed {
		return 0
	}
	var selected []*ent.Episode
	if parsed.SeasonPack {
		selected = beatEpisodes(us, parsed.Season, p, release, pass.grabbed)
	} else if e := us.lookup(parsed); e != nil {
		files := db.PresentFiles(e.Edges.MediaFiles)
		if _, already := pass.grabbed[e.ID]; !already && len(files) > 0 {
			file := qualityctx.ContextFromRow(files[0])
			if quality.ReplacesFile(p, file, release) {
				selected = []*ent.Episode{e}
			}
		}
	}
	if len(selected) == 0 {
		return 0
	}
	selectedIDs := make([]uint32, len(selected))
	for i, e := range selected {
		selectedIDs[i] = e.ID
	}
	rec, err := s.downloads.GrabEpisode(ctx, item, selected[0].ID, selectedIDs)
	if err != nil {
		slog.WarnContext(ctx, "tv feed-scan: upgrade grab failed",
			"show", us.show.Title, "release", item.Title, "error", err)
		if !transportFailure(err) {
			if ierr := s.store.IncrementEpisodeGrabFailures(
				ctx, selected[0].ID,
			); ierr != nil {
				slog.WarnContext(
					ctx,
					"tv feed-scan: bump episode grab_failures failed",
					"episode.id",
					selected[0].ID,
					"error",
					ierr,
				)
			}
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
	// selected episodes already have a file — this grab replaces it, it does
	// not fill a gap — so status stays "available" and last_search_at (a
	// missing-search cooldown input, meaningless once status leaves "wanted")
	// is left alone. grabbed still records the attempt, which is what keeps a
	// second indexer's copy of this release from grabbing the same episode
	// twice in one tick.
	for _, e := range selected {
		pass.grabbed[e.ID] = struct{}{}
	}
	slog.InfoContext(ctx, "tv feed-scan: grabbed upgrade",
		"show", us.show.Title, "release", item.Title,
		"episodes", len(selected))
	return len(selected)
}

func (s *TVFeedScanner) grabOne(
	ctx context.Context,
	show *ent.TVShow,
	e *ent.Episode,
	item indexer.SearchResult,
) bool {
	if _, err := s.downloads.GrabEpisode(
		ctx,
		item,
		e.ID,
		[]uint32{e.ID},
	); err != nil {
		slog.WarnContext(ctx, "tv feed-scan: episode grab failed",
			"show", show.Title, "episode.id", e.ID,
			"release", item.Title, "error", err)
		if !transportFailure(err) {
			if ierr := s.store.IncrementEpisodeGrabFailures(ctx, e.ID); ierr != nil {
				slog.WarnContext(
					ctx,
					"tv feed-scan: bump episode grab_failures failed",
					"episode.id",
					e.ID,
					"error",
					ierr,
				)
			}
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

// newWantedShow indexes one show's loaded episode edges. The feed scan indexes
// every show of a listing up front; the missing search indexes the one show it
// is already working on, so the construction lives apart from the map that
// holds it.
func newWantedShow(show *ent.TVShow) *wantedShow {
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
	return w
}

// titleYearRe is the year TVDB writes after a title to tell same-named shows
// apart ("Foundation (2021)"). library.Parse takes the year out of a release
// name's title, so it has to come off the show's too.
var titleYearRe = regexp.MustCompile(`\s*\(((?:19|20)\d{2})\)\s*$`)

// splitTitleYear returns title without a trailing "(YYYY)", and that year.
func splitTitleYear(title string) (string, uint16) {
	m := titleYearRe.FindStringSubmatchIndex(title)
	if m == nil {
		return title, 0
	}
	// Four digits from the pattern: always a valid uint16.
	year, _ := strconv.ParseUint(title[m[2]:m[3]], 10, 16)
	return title[:m[0]], uint16(year)
}

// showNameKeys returns the keys a release may name show by, in two tiers —
// its title and original title, then its aliases — and the years any of them
// states. Empty keys are left out.
func showNameKeys(show *ent.TVShow) (titles, aliases []string, years []uint16) {
	key := func(name string) string {
		bare, year := splitTitleYear(name)
		if year != 0 && !slices.Contains(years, year) {
			years = append(years, year)
		}
		return showKey(bare)
	}
	for _, t := range []string{show.Title, show.OriginalTitle} {
		if k := key(t); k != "" && !slices.Contains(titles, k) {
			titles = append(titles, k)
		}
	}
	for _, a := range show.Aliases {
		if k := key(a); k != "" && !slices.Contains(titles, k) &&
			!slices.Contains(aliases, k) {
			aliases = append(aliases, k)
		}
	}
	return titles, aliases, years
}

// showNames is which show each release name belongs to, for one tick.
type showNames struct {
	// claims is every key a show answers to, with the IDs of the shows
	// claiming it in the tier that holds it.
	claims map[string][]uint32
	// years is every year a show answers to: its own and any its names state.
	years map[uint32][]uint16
}

// resolve returns the show a release keyed key, carrying year (0 when it
// states none), is for. A key one show claims is that show's whatever the
// year — a daily show's air date reads as a year too. A key several shows
// claim is the one show among them answering to the release's year, if
// exactly one does.
func (n showNames) resolve(key string, year uint16) (uint32, bool) {
	ids := n.claims[key]
	if len(ids) == 1 {
		return ids[0], true
	}
	if len(ids) == 0 || year == 0 {
		return 0, false
	}
	var found uint32
	matches := 0
	for _, id := range ids {
		if slices.Contains(n.years[id], year) {
			found = id
			matches++
		}
	}
	return found, matches == 1
}

// resolveShowNames decides which shows each key names, over every show of
// every list (the same show may appear in several; it is told apart by ID).
//
// Titles outrank aliases: an alias claims only a key no show's title holds,
// so another show's translated title can never take a show's releases. A key
// claimed by more than one show within its tier is told apart by the
// release's year only; a release it leaves unresolved is left to the missing
// search, which queries per show.
func resolveShowNames(ctx context.Context, lists ...[]*ent.TVShow) showNames {
	titles := make(map[string][]uint32)
	aliases := make(map[string][]uint32)
	names := showNames{
		claims: make(map[string][]uint32),
		years:  make(map[uint32][]uint16),
	}
	claim := func(tier map[string][]uint32, key string, id uint32) {
		if !slices.Contains(tier[key], id) {
			tier[key] = append(tier[key], id)
		}
	}
	for _, list := range lists {
		for _, show := range list {
			ts, _, years := showNameKeys(show)
			for _, k := range ts {
				claim(titles, k, show.ID)
			}
			if show.Year != 0 {
				years = append(years, show.Year)
			}
			for _, y := range years {
				if !slices.Contains(names.years[show.ID], y) {
					names.years[show.ID] = append(names.years[show.ID], y)
				}
			}
		}
	}
	for _, list := range lists {
		for _, show := range list {
			_, as, _ := showNameKeys(show)
			for _, k := range as {
				if _, taken := titles[k]; !taken {
					claim(aliases, k, show.ID)
				}
			}
		}
	}
	for _, tier := range []map[string][]uint32{titles, aliases} {
		for key, ids := range tier {
			names.claims[key] = ids
			if len(ids) > 1 {
				slog.DebugContext(
					ctx,
					"tv feed-scan: name shared by several shows, told apart by year only",
					"key",
					key,
					"show.ids",
					slices.Sorted(slices.Values(ids)),
				)
			}
		}
	}
	return names
}

// buildEpisodeIndex indexes shows by ID.
func buildEpisodeIndex(shows []*ent.TVShow) map[uint32]*wantedShow {
	index := make(map[uint32]*wantedShow, len(shows))
	for _, show := range shows {
		index[show.ID] = newWantedShow(show)
	}
	return index
}

// The opening bracket of a fansub tag is often already gone by the time a
// release name reaches here — library.Parse trims leading delimiters off the
// title — so it is optional, and everything through the first closing bracket
// goes. No show title carries one.
var fansubTagRe = regexp.MustCompile(`^\[?[^\]]*\]\s*`)

// latinLetters spells the Latin letters NFKD leaves whole the way a release
// name writes them.
var latinLetters = map[rune]string{
	'ß': "ss", 'æ': "ae", 'œ': "oe", 'ø': "o", 'đ': "d", 'ð': "d",
	'ł': "l", 'þ': "th", 'ı': "i",
}

// showKey normalizes a title to its comparable form: a leading fansub tag
// dropped, everything lowercased, every run of anything but a letter or a
// digit collapsed to one space. Both sides of the lookup go through it, so
// "The.Black.Sea" and "The Black Sea" land on the same key.
//
// Letters of every script are kept: a title in Cyrillic or Japanese keys to
// itself, never to the empty key every such title would share. NFKD splits an
// accent off its letter, and on a Latin, Greek or Cyrillic letter the accent
// is dropped, since a release name usually leaves it out ("Shōgun" and
// "Shogun" are one key); in other scripts the mark is part of the letter and
// stays. NFKD also folds compatibility forms, so full-width "１" is "1".
func showKey(title string) string {
	var key strings.Builder
	var base rune
	sep := false
	for _, r := range norm.NFKD.String(fansubTagRe.ReplaceAllString(title, "")) {
		r = unicode.ToLower(r)
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if sep && key.Len() > 0 {
				key.WriteByte(' ')
			}
			sep = false
			base = r
			if s, ok := latinLetters[r]; ok {
				key.WriteString(s)
			} else {
				key.WriteRune(r)
			}
		case unicode.IsMark(r):
			// A mark belongs to the letter before it; one with no letter to
			// attach to, or an accent the release name would not spell, goes.
			if base == 0 || sep ||
				unicode.In(base, unicode.Latin, unicode.Greek, unicode.Cyrillic) {
				continue
			}
			key.WriteRune(r)
		default:
			sep = true
		}
	}
	return key.String()
}

// releaseShowKey derives the show key from a parsed release name. library.Parse
// cuts the title at the first technical token, which for a season pack or an
// anime release is not the numbering token — "Show.S03.1080p" parses to
// "Show S03" and "[Grp] Show - 18 [1080p]" to "Show 18" — so the number the
// parser already extracted is trimmed back off.
func releaseShowKey(p library.ParseResult) string {
	k := showKey(p.Title)
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
			profiles[show.QualityProfile] = qualityFor(
				ctx,
				config.MediaSeries,
				show.QualityProfile,
			)
		}
	}
	return profiles
}
