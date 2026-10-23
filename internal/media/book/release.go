package book

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	entbook "github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/otelx"
)

const slotDownloading = "downloading"

// ReleaseResult is one release offered for a slot, scored against the book's
// profile. Rejected releases stay in the list, flagged and sorted last, so the
// UI can say why they were refused.
type ReleaseResult struct {
	indexer.SearchResult
	// Slot is the slot the release fills, from its parsed container.
	Slot string
	// Container is the upper-case container the name states, empty when none.
	Container   string
	BitrateKbps uint32
	Score       int
	Rejected    bool
	Reason      string
}

// GrabParams describe a grab. Kind is the slot the caller asked for, Slot the
// slot the request body names; either may be empty when the other is set. The
// slot is derived from the release's container and a body that disagrees with
// it is refused, so a forged slot cannot place an audiobook in an ebook slot.
type GrabParams struct {
	Kind            string
	Slot            string
	Result          indexer.SearchResult
	ReplaceExisting bool
}

// ProfileFor resolves the profile that governs a book: its own, else its
// series', else the default of its kind. A name that no longer exists falls to
// the default at use time and is not rewritten.
func ProfileFor(b *ent.Book) (config.BookQualityProfileEntry, bool) {
	name := b.QualityProfile
	if name == "" && b.Edges.Series != nil {
		name = b.Edges.Series.QualityProfile
	}
	return config.ResolveBookQualityProfile(name, string(b.Kind))
}

func (s *Service) findBook(ctx context.Context, id uint32) (*ent.Book, error) {
	row, err := s.db.FindBookByID(ctx, id)
	if ent.IsNotFound(err) {
		return nil, ErrBookNotFound
	}
	return row, err
}

// searchTerms are what the indexers are asked for one slot: the author's names
// and the slot edition's title (the book's own when it has none), with that
// edition's year. The other titles are not queried; a multiplied query would
// cost every rate-limited tracker a request per title.
func searchTerms(b *ent.Book, kind string) (author, title string, year uint16) {
	author, title = b.AuthorName, b.Title
	if ed := slotEdition(b, kind); ed != nil {
		title, year = ed.Title, ed.Year
	}
	if year == 0 && b.ReleaseYear != nil {
		year = *b.ReleaseYear
	}
	return author, title, year
}

// judge scores one search result against a slot. auto marks the unattended
// paths, which also skip a release tagged with a language other than the
// slot edition's; an untagged release is accepted, and an interactive search
// never filters on language.
func judge(
	b *ent.Book,
	kind string,
	profile config.BookQualityProfileEntry,
	r indexer.SearchResult,
	auto bool,
) ReleaseResult {
	parsed := library.ParseBookRelease(r.Title)
	var (
		score int
		why   string
	)
	if kind == slotAudiobook {
		score, why = library.JudgeAudiobookRelease(parsed, profile)
	} else {
		score, why = library.JudgeEbookRelease(parsed, profile)
	}
	slot := kind
	if parsed.Kind != "" {
		slot = parsed.Kind
	}
	res := ReleaseResult{
		SearchResult: r,
		Slot:         slot,
		Container:    parsed.Format,
		BitrateKbps:  parsed.BitrateKbps,
		Score:        score,
	}
	switch {
	case score < 0:
		res.Rejected, res.Reason = true, why
	case parsed.Collection:
		res.Rejected, res.Reason = true, "collection or box set"
	case auto && WrongLanguage(b, kind, parsed):
		res.Rejected = true
		res.Reason = "released in a different language than the edition"
	}
	return res
}

// WrongLanguage reports a release tagged with a language other than the
// slot edition's. An untagged release is never wrong.
func WrongLanguage(b *ent.Book, kind string, p library.ParsedBookRelease) bool {
	ed := slotEdition(b, kind)
	return p.Language != "" && ed != nil && p.Language != ed.Language
}

func sortReleases(out []ReleaseResult) {
	slices.SortStableFunc(out, func(a, b ReleaseResult) int {
		if a.Rejected != b.Rejected {
			if a.Rejected {
				return 1
			}
			return -1
		}
		return cmp.Or(
			cmp.Compare(b.Score, a.Score),
			cmp.Compare(b.Seeders, a.Seeders),
		)
	})
}

// slotReleases searches the indexers for one slot and judges what comes back.
// A release whose container is the other slot's is left to that slot's search.
func (s *Service) slotReleases(
	ctx context.Context,
	b *ent.Book,
	kind string,
	auto bool,
) ([]ReleaseResult, error) {
	profile, ok := ProfileFor(b)
	if !ok {
		return nil, ErrNoQualityProfile
	}
	author, title, year := searchTerms(b, kind)
	found, err := s.indexers.SearchBook(
		ctx,
		author,
		title,
		year,
		mediafile.BookKind(kind),
	)
	if err != nil {
		return nil, fmt.Errorf("search book: %w", err)
	}
	out := make([]ReleaseResult, 0, len(found))
	for _, r := range found {
		res := judge(b, kind, profile, r, auto)
		if res.Slot != kind {
			continue
		}
		out = append(out, res)
	}
	return out, nil
}

// SearchBookReleases returns every release the indexers offer for the slot,
// or for both slots merged when kind is empty, scored against the book's
// profile.
func (s *Service) SearchBookReleases(
	ctx context.Context,
	bookID uint32,
	kind string,
) ([]ReleaseResult, error) {
	ctx, span := tracer.Start(ctx, "book.search_releases",
		trace.WithAttributes(
			attribute.Int("book.id", int(bookID)),
			attribute.String("book.kind", kind),
		))
	defer span.End()

	kinds := slotKinds
	if kind != "" {
		if !validSlotKind(kind) {
			return nil, otelx.RecordSpanError(span, ErrInvalidSlotKind)
		}
		kinds = []string{kind}
	}
	b, err := s.findBook(ctx, bookID)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}

	var out []ReleaseResult
	for _, k := range kinds {
		found, err := s.slotReleases(ctx, b, k, false)
		if err != nil {
			return nil, otelx.RecordSpanError(span, err)
		}
		out = append(out, found...)
	}
	sortReleases(out)
	span.SetAttributes(attribute.Int("releases", len(out)))
	return out, nil
}

// GrabBookRelease sends one release to the download client for the slot its
// container fills and marks that slot downloading. A slot already available is
// left alone. A grab that replaces a file, by the caller's flag or because the
// slot's edition was changed, tells the importer to remove the old file once
// the new one is placed.
func (s *Service) GrabBookRelease(
	ctx context.Context,
	bookID uint32,
	p GrabParams,
) error {
	ctx, span := tracer.Start(ctx, "book.grab",
		trace.WithAttributes(attribute.Int("book.id", int(bookID))))
	defer span.End()

	parsed := library.ParseBookRelease(p.Result.Title)
	kind := cmp.Or(parsed.Kind, p.Slot, p.Kind)
	span.SetAttributes(attribute.String("book.kind", kind))
	switch {
	case !validSlotKind(kind):
		return otelx.RecordSpanError(span, ErrInvalidSlotKind)
	case parsed.Kind != "" && (p.Slot != "" && p.Slot != parsed.Kind ||
		p.Kind != "" && p.Kind != parsed.Kind):
		return otelx.RecordSpanError(span, ErrSlotMismatch)
	}
	b, err := s.findBook(ctx, bookID)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	if b.LastRefreshedAt == nil {
		return otelx.RecordSpanError(span, ErrNotHydrated)
	}
	rec, err := s.download.GrabBook(ctx, p.Result, bookID, mediafile.BookKind(kind))
	if err != nil {
		return otelx.RecordSpanError(span, fmt.Errorf("grab book: %w", err))
	}

	if from := slotStatus(b, kind); grabbableSlotStatus(kind, from) {
		if err := s.db.SetBookSlotStatus(
			ctx, bookID, kind, from, slotDownloading,
		); err != nil {
			slog.WarnContext(ctx, "grab book: set slot status failed",
				"book.id", bookID, "book.kind", kind, "error", err)
		}
	}
	if p.ReplaceExisting || replacingLanguage(b, kind) != "" {
		if err := s.db.SetLiveBookRecordReplaceMode(
			ctx,
			bookID,
			downloadrecord.BookKind(kind),
			downloadrecord.ReplaceModeAll,
		); err != nil {
			slog.ErrorContext(ctx, "grab book: set replace mode failed",
				"book.id", bookID, "record.id", rec.ID, "error", err)
		}
	}
	slog.InfoContext(ctx, "grabbed book release",
		"release", p.Result.Title, "book.kind", kind)
	return nil
}

func grabbableSlotStatus(kind, status string) bool {
	if kind == slotAudiobook {
		return status == string(entbook.AudiobookStatusWanted) ||
			status == string(entbook.AudiobookStatusSkipped) ||
			status == string(entbook.AudiobookStatusPaused)
	}
	return status == string(entbook.EbookStatusWanted) ||
		status == string(entbook.EbookStatusSkipped) ||
		status == string(entbook.EbookStatusPaused)
}
