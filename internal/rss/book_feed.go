package rss

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/datahearth/streamline/ent"
	entbook "github.com/datahearth/streamline/ent/book"
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

// bookPass carries one tick's state for the book branch: wanted books indexed
// by author key, and the slots already attempted. A book's two slots are
// separate grabs, so grabbed is keyed on the pair.
type bookPass struct {
	wanted  map[string][]*ent.Book
	grabbed map[bookSlot]struct{}
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

func (s *FeedScanner) newBookPass(ctx context.Context) (*bookPass, error) {
	books, err := s.store.ListWantedBooks(
		ctx, config.Get().Library.MaxGrabFailures,
	)
	if err != nil {
		return nil, err
	}
	pass := &bookPass{
		wanted:  make(map[string][]*ent.Book),
		grabbed: make(map[bookSlot]struct{}),
	}
	for _, b := range books {
		if b.Edges.Author == nil {
			continue
		}
		key := showKey(b.Edges.Author.Name)
		pass.wanted[key] = append(pass.wanted[key], b)
	}
	return pass, nil
}

func (s *FeedScanner) processBookItems(
	ctx context.Context,
	items []indexer.SearchResult,
	pass *bookPass,
) int {
	if len(pass.wanted) == 0 {
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
		authorPart, titlePart, ok := splitCreatorTitle(item.Title)
		if !ok {
			continue
		}
		var b *ent.Book
		for _, cand := range pass.wanted[showKey(authorPart)] {
			if slotWanted(cand, kind, maxFailures) &&
				library.TitleNamesSameWork(titlePart, cand.Title) {
				b = cand
				break
			}
		}
		if b == nil {
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

// bookScore scores the release against the author's profile for the slot, or
// -1 when it does not fit; an unresolvable profile rejects too, so a book is
// never grabbed under a profile nobody can read.
func bookScore(
	ctx context.Context,
	b *ent.Book,
	kind string,
	parsed library.ParsedBookRelease,
) int {
	a := b.Edges.Author
	if kind == slotAudiobook {
		p, ok := config.ResolveAudiobookQualityProfile(a.AudiobookQualityProfile)
		if !ok {
			slog.WarnContext(ctx, "feed-scan: audiobook quality profile unresolved",
				"author", a.Name, "profile", a.AudiobookQualityProfile)
			return -1
		}
		return library.ScoreAudiobookRelease(parsed, p)
	}
	p, ok := config.ResolveEbookQualityProfile(a.EbookQualityProfile)
	if !ok {
		slog.WarnContext(ctx, "feed-scan: ebook quality profile unresolved",
			"author", a.Name, "profile", a.EbookQualityProfile)
		return -1
	}
	return library.ScoreEbookRelease(parsed, p)
}
