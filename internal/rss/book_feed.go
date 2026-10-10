package rss

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/datahearth/streamline/ent"
	entbook "github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/bookcontribution"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/media/book"
)

const (
	slotEbook     = "ebook"
	slotAudiobook = "audiobook"

	ebookCategoryMin = 7000
	ebookCategoryMax = 8000
)

type bookSlot struct {
	id   uint32
	kind string
}

// bookIndex finds the books a release could be for. A standalone book is
// reached by its makers, the way a release names it ("Author - Title"); a
// series volume is also reached by the series it belongs to and the volume
// number, since a manga release rarely carries its author.
type bookIndex struct {
	byCreator map[string][]*ent.Book
	volumes   []*ent.Book
}

// bookPass carries one tick's state for the book branch: the wanted books and
// the books holding files (upgrade candidates), and the slots already
// attempted. A book's two slots are separate grabs, so grabbed is keyed on the
// pair.
type bookPass struct {
	wanted   bookIndex
	upgrades bookIndex
	grabbed  map[bookSlot]struct{}
}

// bookKindForCategory maps a Torznab category to the slot it fills: exactly
// 3030 is audiobooks, 7000-7999 is ebooks, anything else (including an empty
// or unparsable category) is not a book.
func bookKindForCategory(cat string) string {
	n, err := strconv.Atoi(cat)
	switch {
	case err != nil:
		return ""
	case n == audiobookCategory:
		return slotAudiobook
	case n >= ebookCategoryMin && n < ebookCategoryMax:
		return slotEbook
	}
	return ""
}

// slotWanted reports whether the book's slot of the given kind is monitored,
// wanted and under the failure cap.
func slotWanted(b *ent.Book, kind string, maxGrabFailures uint8) bool {
	if kind == slotAudiobook {
		return b.AudiobookMonitored &&
			b.AudiobookStatus == entbook.AudiobookStatusWanted &&
			b.AudiobookGrabFailures < maxGrabFailures
	}
	return b.EbookMonitored &&
		b.EbookStatus == entbook.EbookStatusWanted &&
		b.EbookGrabFailures < maxGrabFailures
}

// creatorKeys are the names a release may credit a book under: the display
// string and each maker.
func creatorKeys(b *ent.Book) []string {
	keys := []string{showKey(b.AuthorName)}
	for _, c := range b.Edges.Contributions {
		a := c.Edges.Author
		if a == nil {
			continue
		}
		switch c.Role {
		case bookcontribution.RoleAuthor,
			bookcontribution.RoleWriter,
			bookcontribution.RoleArtist:
			keys = append(keys, showKey(a.Name))
		}
	}
	return keys
}

// titleForms are the names a release may use for a book: its title, its
// original title and the title of every edition.
func titleForms(b *ent.Book) []string {
	forms := []string{b.Title}
	if b.OriginalTitle != "" {
		forms = append(forms, b.OriginalTitle)
	}
	for _, e := range b.Edges.Editions {
		forms = append(forms, e.Title)
	}
	for _, e := range []*ent.BookEdition{b.Edges.EbookEdition, b.Edges.AudiobookEdition} {
		if e != nil {
			forms = append(forms, e.Title)
		}
	}
	return forms
}

func indexBooks(books []*ent.Book) bookIndex {
	idx := bookIndex{byCreator: make(map[string][]*ent.Book)}
	for _, b := range books {
		seen := map[string]bool{}
		for _, k := range creatorKeys(b) {
			if k == "" || seen[k] {
				continue
			}
			seen[k] = true
			idx.byCreator[k] = append(idx.byCreator[k], b)
		}
		if b.Edges.Series != nil {
			idx.volumes = append(idx.volumes, b)
		}
	}
	return idx
}

func (i bookIndex) empty() bool {
	return len(i.byCreator) == 0 && len(i.volumes) == 0
}

// find returns the first book the release is for among those keep accepts: a
// standalone match on "Author - Title", else a series volume on the series name
// and the parsed volume number.
func (i bookIndex) find(
	itemTitle string,
	parsed library.ParsedBookRelease,
	keep func(*ent.Book) bool,
) *ent.Book {
	if creator, title, ok := splitCreatorTitle(itemTitle); ok {
		for _, cand := range i.byCreator[showKey(creator)] {
			if !keep(cand) {
				continue
			}
			for _, form := range titleForms(cand) {
				if library.TitleNamesSameWork(title, form) {
					return cand
				}
			}
		}
	}
	if parsed.Volume == nil {
		return nil
	}
	for _, v := range i.volumes {
		if !keep(v) || v.SeriesPosition == nil ||
			*v.SeriesPosition != *parsed.Volume {
			continue
		}
		sr := v.Edges.Series
		if library.TitleNamesSameWork(parsed.VolumePrefix, sr.Title) ||
			(sr.OriginalTitle != "" &&
				library.TitleNamesSameWork(parsed.VolumePrefix, sr.OriginalTitle)) {
			return v
		}
	}
	return nil
}

func (s *FeedScanner) newBookPass(ctx context.Context) (*bookPass, error) {
	books, err := s.store.ListWantedBooks(
		ctx, config.Get().Library.MaxGrabFailures,
	)
	if err != nil {
		return nil, err
	}
	upgradable, err := s.store.ListUpgradeCandidateBooks(ctx)
	if err != nil {
		return nil, err
	}
	return &bookPass{
		wanted:   indexBooks(books),
		upgrades: indexBooks(upgradable),
		grabbed:  make(map[bookSlot]struct{}),
	}, nil
}

func (s *FeedScanner) processBookItems(
	ctx context.Context,
	items []indexer.SearchResult,
	pass *bookPass,
) int {
	if pass.wanted.empty() && pass.upgrades.empty() {
		return 0
	}
	maxFailures := config.Get().Library.MaxGrabFailures
	matched := 0
	for _, item := range items {
		kind := bookKindForCategory(item.Category)
		if kind == "" {
			continue
		}
		parsed := library.ParseBookRelease(item.Title)
		if parsed.Collection {
			continue
		}
		b := pass.wanted.find(item.Title, parsed, func(c *ent.Book) bool {
			return slotWanted(c, kind, maxFailures)
		})
		if b == nil {
			if s.tryBookUpgrade(ctx, item, parsed, kind, pass) {
				matched++
			}
			continue
		}
		slot := bookSlot{id: b.ID, kind: kind}
		if _, already := pass.grabbed[slot]; already {
			continue
		}
		if bookScore(ctx, b, kind, parsed) < 0 {
			slog.DebugContext(ctx, "feed-scan: book format rejected",
				"book", b.Title, "kind", kind, "release", item.Title,
				"format", parsed.Format)
			continue
		}
		pass.grabbed[slot] = struct{}{}
		err := s.books.GrabBookRelease(
			ctx, b.ID, book.GrabParams{Kind: kind, Result: item},
		)
		if err != nil {
			slog.WarnContext(ctx, "feed-scan: book grab failed",
				"book", b.Title, "kind", kind, "release", item.Title,
				"error", err)
			if !transportFailure(err) {
				if bumpErr := s.store.IncrementBookSlotGrabFailures(
					ctx, b.ID, kind,
				); bumpErr != nil {
					slog.WarnContext(ctx,
						"feed-scan: bump book grab_failures failed",
						"book", b.Title, "kind", kind, "error", bumpErr)
				}
			}
			continue
		}
		matched++
		if err := s.store.ResetBookSlotGrabFailures(ctx, b.ID, kind); err != nil {
			slog.WarnContext(ctx, "feed-scan: reset book grab_failures failed",
				"book", b.Title, "kind", kind, "error", err)
		}
		if err := s.store.SetBookSlotLastSearchAt(
			ctx, b.ID, kind, time.Now(),
		); err != nil {
			slog.WarnContext(ctx, "feed-scan: set book last_search_at failed",
				"book", b.Title, "kind", kind, "error", err)
		}
		slog.InfoContext(ctx, "feed-scan: grabbed book",
			"book", b.Title, "kind", kind, "release", item.Title)
	}
	return matched
}

// bookScore scores the release against the book's profile for the slot, or -1
// when it does not fit: a format the profile refuses, or a release tagged with
// another language than the slot's edition. An unresolvable profile rejects
// too, so a book is never grabbed under a profile nobody can read.
func bookScore(
	ctx context.Context,
	b *ent.Book,
	kind string,
	parsed library.ParsedBookRelease,
) int {
	p, ok := bookProfile(ctx, b)
	if !ok {
		return -1
	}
	if book.WrongLanguage(b, kind, parsed) {
		return -1
	}
	if kind == slotAudiobook {
		return library.ScoreAudiobookRelease(parsed, p)
	}
	return library.ScoreEbookRelease(parsed, p)
}
