package rss

import (
	"context"
	"log/slog"
	"slices"
	"strings"

	"github.com/datahearth/streamline/ent"
	entbook "github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/media/book"
	"github.com/datahearth/streamline/internal/quality"
)

// The upgrade arms. Music and books are upgraded the way video is, through the
// feed only: nothing issues a query in order to upgrade, so a better release
// is taken when the feed carries one. quality.MusicProfile.Replaces and
// quality.EbookReplaces / AudiobookReplaces are the single predicates.

// albumHeldTier is the worst tier among the files the album's tracks hold.
// known is false when it holds none, or when any file's tier could not be
// established: such a file is never an upgrade target.
func albumHeldTier(a *ent.Album) (quality.AudioTier, bool) {
	var (
		worst quality.AudioTier
		any   bool
	)
	for _, tr := range a.Edges.Tracks {
		for _, mf := range tr.Edges.MediaFiles {
			t, ok := quality.ParseAudioTier(mf.Quality)
			if !ok {
				return 0, false
			}
			if !any || t < worst {
				worst, any = t, true
			}
		}
	}
	return worst, any
}

// tryAlbumUpgrade grabs item as a replacement for the files of the album it
// names when the profile lets its tier beat the worst one on disk. The record
// is flagged replace_mode upgrades so the importer replaces per track, and
// only where the incoming tier beats that track's own. Reports whether it
// grabbed.
func (s *FeedScanner) tryAlbumUpgrade(
	ctx context.Context,
	item indexer.SearchResult,
	parsed library.ParsedMusicRelease,
	artistPart, albumPart string,
	pass *musicPass,
) bool {
	var a *ent.Album
	for _, cand := range pass.upgrades[showKey(artistPart)] {
		if library.TitleNamesSameWork(albumPart, cand.Title) {
			a = cand
			break
		}
	}
	if a == nil {
		return false
	}
	if _, already := pass.grabbed[a.ID]; already {
		return false
	}
	profile := pass.profiles[a.Edges.Artist.QualityProfile]
	if !profile.UpgradeAllowed {
		return false
	}
	if library.ScoreMusicRelease(parsed, profile, library.MusicScopeAlbum) < 0 {
		return false
	}
	have, known := albumHeldTier(a)
	if !profile.Profile().Replaces(have, known, parsed.Tier) {
		slog.DebugContext(ctx, "feed-scan: album upgrade skipped",
			"album", a.Title, "release", item.Title,
			"have", have.String(), "known", known, "incoming", parsed.Tier.String())
		return false
	}

	pass.grabbed[a.ID] = struct{}{}
	if err := s.albums.GrabAlbumRelease(ctx, a.ID, item); err != nil {
		slog.WarnContext(ctx, "feed-scan: album upgrade grab failed",
			"album", a.Title, "release", item.Title, "error", err)
		if !transportFailure(err) {
			if bumpErr := s.store.IncrementAlbumGrabFailures(
				ctx,
				a.ID,
			); bumpErr != nil {
				slog.WarnContext(ctx, "feed-scan: bump album grab_failures failed",
					"album", a.Title, "error", bumpErr)
			}
		}
		return false
	}
	// Without the flag the importer leaves every track that holds a file alone,
	// so the upgrade this run just grabbed can never land.
	if err := s.store.SetLiveAlbumRecordReplaceMode(
		ctx, a.ID, downloadrecord.ReplaceModeUpgrades,
	); err != nil {
		slog.ErrorContext(ctx, "feed-scan: set replace mode failed",
			"album", a.Title, "error", err)
	}
	slog.InfoContext(ctx, "feed-scan: grabbed album upgrade",
		"album", a.Title, "release", item.Title,
		"have", have.String(), "incoming", parsed.Tier.String())
	return true
}

// slotFiles lists the files a book holds for one slot.
func slotFiles(b *ent.Book, kind string) []*ent.MediaFile {
	want := mediafile.BookKindEbook
	if kind == slotAudiobook {
		want = mediafile.BookKindAudiobook
	}
	return slices.DeleteFunc(
		slices.Clone(b.Edges.MediaFiles),
		func(f *ent.MediaFile) bool {
			return f.BookKind != want
		},
	)
}

// slotUpgradable reports whether the book's slot is monitored, holds a file and
// is not mid-grab. The query only says some slot qualifies.
func slotUpgradable(b *ent.Book, kind string) bool {
	if kind == slotAudiobook {
		return b.AudiobookMonitored &&
			b.AudiobookStatus == entbook.AudiobookStatusAvailable
	}
	return b.EbookMonitored && b.EbookStatus == entbook.EbookStatusAvailable
}

// heldBookFormat is the best format among a slot's files, ranked on the ladder
// (the slot's files are one format for an ebook and one folder for an
// audiobook). An empty answer means a file whose format is not on the ladder.
func heldBookFormat(files []*ent.MediaFile, ladder []string) string {
	best := -1
	for _, f := range files {
		i := slices.Index(ladder, strings.ToUpper(f.Quality))
		if i < 0 {
			continue
		}
		if best < 0 || i < best {
			best = i
		}
	}
	if best < 0 {
		return ""
	}
	return ladder[best]
}

// heldAudiobookKbps is the measured rate of the folder's first probed file, 0
// when none was probed.
func heldAudiobookKbps(files []*ent.MediaFile) uint32 {
	for _, f := range files {
		if f.Bitrate > 0 {
			return f.Bitrate / 1000
		}
	}
	return 0
}

// tryBookUpgrade grabs item as a replacement for the slot's current file when
// the profile's upgrade predicate takes it. Reports whether it grabbed.
func (s *FeedScanner) tryBookUpgrade(
	ctx context.Context,
	item indexer.SearchResult,
	parsed library.ParsedBookRelease,
	kind string,
	pass *bookPass,
) bool {
	b := pass.upgrades.find(item.Title, parsed, func(c *ent.Book) bool {
		return slotUpgradable(c, kind)
	})
	if b == nil || book.WrongLanguage(b, kind, parsed) {
		return false
	}
	slot := bookSlot{id: b.ID, kind: kind}
	if _, already := pass.grabbed[slot]; already {
		return false
	}
	profile, ok := bookProfile(ctx, b)
	if !ok {
		return false
	}
	files := slotFiles(b, kind)
	var replaces bool
	if kind == slotAudiobook {
		if library.ScoreAudiobookRelease(parsed, profile) < 0 {
			return false
		}
		replaces = quality.AudiobookReplaces(
			profile.AudiobookProfile(),
			heldBookFormat(files, quality.AudiobookLadder), heldAudiobookKbps(files),
			parsed.Format, parsed.BitrateKbps,
		)
	} else {
		if library.ScoreEbookRelease(parsed, profile) < 0 {
			return false
		}
		replaces = quality.EbookReplaces(
			profile.EbookProfile(),
			heldBookFormat(files, quality.EbookLadder), parsed.Format,
		)
	}
	if !replaces {
		slog.DebugContext(ctx, "feed-scan: book upgrade skipped",
			"book", b.Title, "kind", kind, "release", item.Title)
		return false
	}

	pass.grabbed[slot] = struct{}{}
	if err := s.books.GrabBookRelease(
		ctx, b.ID, book.GrabParams{Kind: kind, Result: item},
	); err != nil {
		slog.WarnContext(ctx, "feed-scan: book upgrade grab failed",
			"book", b.Title, "kind", kind, "release", item.Title, "error", err)
		if !transportFailure(err) {
			if bumpErr := s.store.IncrementBookSlotGrabFailures(
				ctx, b.ID, kind,
			); bumpErr != nil {
				slog.WarnContext(ctx, "feed-scan: bump book grab_failures failed",
					"book", b.Title, "kind", kind, "error", bumpErr)
			}
		}
		return false
	}
	recordKind := downloadrecord.BookKindEbook
	if kind == slotAudiobook {
		recordKind = downloadrecord.BookKindAudiobook
	}
	// Without the flag the importer refuses to overwrite the slot's file, so
	// the upgrade this run just grabbed can never land.
	if err := s.store.SetLiveBookRecordReplaceMode(
		ctx, b.ID, recordKind, downloadrecord.ReplaceModeUpgrades,
	); err != nil {
		slog.ErrorContext(ctx, "feed-scan: set replace mode failed",
			"book", b.Title, "kind", kind, "error", err)
	}
	slog.InfoContext(ctx, "feed-scan: grabbed book upgrade",
		"book", b.Title, "kind", kind, "release", item.Title)
	return true
}

// bookProfile resolves the profile that governs a book, logging when it
// cannot: a book is never judged under a profile nobody can read.
func bookProfile(
	ctx context.Context,
	b *ent.Book,
) (config.BookQualityProfileEntry, bool) {
	p, ok := book.ProfileFor(b)
	if !ok {
		slog.WarnContext(ctx, "feed-scan: book quality profile unresolved",
			"book", b.Title, "profile", b.QualityProfile)
	}
	return p, ok
}
