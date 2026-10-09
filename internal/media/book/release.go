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
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/utils/numeric"
)

const slotDownloading = "downloading"

type ReleaseResult struct {
	indexer.SearchResult
	Parsed   library.ParsedBookRelease
	Format   string
	Score    int
	Rejected bool
	Reason   string
}

type GrabParams struct {
	Kind   string
	Result indexer.SearchResult
}

func validSlotKind(kind string) bool {
	return kind == string(mediafile.BookKindEbook) ||
		kind == string(mediafile.BookKindAudiobook)
}

func (s *Service) findBook(ctx context.Context, id uint32) (*ent.Book, error) {
	row, err := s.db.FindBookByID(ctx, id)
	if ent.IsNotFound(err) {
		return nil, ErrBookNotFound
	}
	return row, err
}

// SearchBookReleases returns every release the indexers offer for one slot,
// scored against the author's profile for that slot. Rejected releases stay in
// the list, flagged and sorted last, so the UI can say why they were refused.
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

	if !validSlotKind(kind) {
		return nil, otelx.RecordSpanError(span, ErrInvalidSlotKind)
	}
	b, err := s.findBook(ctx, bookID)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	author := b.Edges.Author

	var judge func(library.ParsedBookRelease) (int, string)
	if kind == string(mediafile.BookKindEbook) {
		profile, ok := config.ResolveBookQualityProfile(
			author.EbookQualityProfile,
			"",
		)
		if !ok {
			return nil, otelx.RecordSpanError(span, ErrNoQualityProfile)
		}
		judge = func(p library.ParsedBookRelease) (int, string) {
			return library.JudgeEbookRelease(p, profile)
		}
	} else {
		profile, ok := config.ResolveBookQualityProfile(
			author.AudiobookQualityProfile, "",
		)
		if !ok {
			return nil, otelx.RecordSpanError(span, ErrNoQualityProfile)
		}
		judge = func(p library.ParsedBookRelease) (int, string) {
			return library.JudgeAudiobookRelease(p, profile)
		}
	}

	var year uint16
	if b.ReleaseDate != nil {
		year = numeric.SaturateU16(b.ReleaseDate.Year())
	}
	found, err := s.indexers.SearchBook(
		ctx, author.Name, b.Title, year, mediafile.BookKind(kind),
	)
	if err != nil {
		return nil, otelx.RecordSpanError(span, fmt.Errorf("search book: %w", err))
	}

	out := make([]ReleaseResult, len(found))
	for i, r := range found {
		parsed := library.ParseBookRelease(r.Title)
		score, reason := judge(parsed)
		res := ReleaseResult{
			SearchResult: r,
			Parsed:       parsed,
			Format:       parsed.Format,
			Score:        score,
		}
		switch {
		case score < 0:
			res.Rejected = true
			res.Reason = reason
		case parsed.Collection:
			res.Rejected = true
			res.Reason = "collection or box set"
		}
		out[i] = res
	}
	slices.SortStableFunc(out, func(a, b ReleaseResult) int {
		if a.Rejected != b.Rejected {
			if a.Rejected {
				return 1
			}
			return -1
		}
		if c := cmp.Compare(b.Score, a.Score); c != 0 {
			return c
		}
		return cmp.Compare(b.Seeders, a.Seeders)
	})
	span.SetAttributes(attribute.Int("releases", len(out)))
	return out, nil
}

// GrabBookRelease sends one release to the download client for one slot and
// marks that slot downloading. A slot already available is left alone.
func (s *Service) GrabBookRelease(
	ctx context.Context,
	bookID uint32,
	p GrabParams,
) error {
	ctx, span := tracer.Start(ctx, "book.grab",
		trace.WithAttributes(
			attribute.Int("book.id", int(bookID)),
			attribute.String("book.kind", p.Kind),
		))
	defer span.End()

	if !validSlotKind(p.Kind) {
		return otelx.RecordSpanError(span, ErrInvalidSlotKind)
	}
	b, err := s.findBook(ctx, bookID)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	if _, err := s.download.GrabBook(
		ctx, p.Result, bookID, mediafile.BookKind(p.Kind),
	); err != nil {
		return otelx.RecordSpanError(span, fmt.Errorf("grab book: %w", err))
	}

	if from := slotStatus(b, p.Kind); grabbableSlotStatus(p.Kind, from) {
		if err := s.db.SetBookSlotStatus(
			ctx, bookID, p.Kind, from, slotDownloading,
		); err != nil {
			slog.WarnContext(ctx, "grab book: set slot status failed",
				"book.id", bookID, "book.kind", p.Kind, "error", err)
		}
	}
	slog.InfoContext(ctx, "grabbed book release",
		"release", p.Result.Title, "book.kind", p.Kind)
	return nil
}

func slotStatus(b *ent.Book, kind string) string {
	if kind == string(mediafile.BookKindEbook) {
		return string(b.EbookStatus)
	}
	return string(b.AudiobookStatus)
}

func grabbableSlotStatus(kind, status string) bool {
	if kind == string(mediafile.BookKindEbook) {
		return status == string(entbook.EbookStatusWanted) ||
			status == string(entbook.EbookStatusSkipped) ||
			status == string(entbook.EbookStatusPaused)
	}
	return status == string(entbook.AudiobookStatusWanted) ||
		status == string(entbook.AudiobookStatusSkipped) ||
		status == string(entbook.AudiobookStatusPaused)
}
