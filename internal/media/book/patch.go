package book

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/media/book/pick"
	"github.com/datahearth/streamline/internal/otelx"
)

// PatchBookParams are the optional changes of a book. Format and EditionID go
// together.
type PatchBookParams struct {
	Monitor           *string
	PreferredLanguage *string
	QualityProfile    *string
	Kind              *string
	Format            *string
	EditionID         *uint32
}

// PatchSeriesParams are the optional changes of a series.
type PatchSeriesParams struct {
	Monitor        *string
	QualityProfile *string
	Edition        *string
}

func (p PatchBookParams) validate(b *ent.Book) error {
	if p.Monitor != nil {
		if _, _, err := slotFlags(*p.Monitor); err != nil {
			return err
		}
	}
	if p.PreferredLanguage != nil && !validLanguage(*p.PreferredLanguage) {
		return ErrInvalidLanguage
	}
	if p.QualityProfile != nil {
		if err := checkProfile(*p.QualityProfile); err != nil {
			return err
		}
	}
	if p.Kind != nil && !validKind(*p.Kind) {
		return fmt.Errorf("%w: %q", ErrInvalidKind, *p.Kind)
	}
	if (p.Format == nil) != (p.EditionID == nil) {
		return ErrEditionMismatch
	}
	if p.Format != nil {
		if !validSlotKind(*p.Format) {
			return ErrInvalidSlotKind
		}
		ok := slices.ContainsFunc(b.Edges.Editions, func(e *ent.BookEdition) bool {
			return e.ID == *p.EditionID && string(e.Format) == *p.Format
		})
		if !ok {
			return fmt.Errorf("%w: %d", ErrUnknownEdition, *p.EditionID)
		}
	}
	return nil
}

// PatchBook applies the changes in the order they depend on each other: the
// profile and kind, the preferred language (which re-derives the titles and
// re-picks every slot without a file), the edition of a slot, then monitoring.
// The answer is the book as it now stands.
func (s *Service) PatchBook(
	ctx context.Context,
	id uint32,
	p PatchBookParams,
) (*ent.Book, error) {
	ctx, span := tracer.Start(ctx, "book.patch",
		trace.WithAttributes(attribute.Int("book.id", int(id))))
	defer span.End()

	row, err := s.GetBook(ctx, id)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	if err := p.validate(row); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}

	if p.QualityProfile != nil {
		if err := s.db.SetBookQualityProfile(
			ctx,
			id,
			*p.QualityProfile,
		); err != nil {
			return nil, otelx.RecordSpanError(span, err)
		}
	}
	if p.Kind != nil {
		if err := s.db.SetBookKind(ctx, id, *p.Kind); err != nil {
			return nil, otelx.RecordSpanError(span, err)
		}
	}
	if p.PreferredLanguage != nil {
		if err := s.setPreferredLanguage(
			ctx,
			row,
			*p.PreferredLanguage,
		); err != nil {
			return nil, otelx.RecordSpanError(span, err)
		}
	}
	if p.Format != nil {
		if err := s.setSlotEdition(ctx, row, *p.Format, *p.EditionID); err != nil {
			return nil, otelx.RecordSpanError(span, err)
		}
	}
	if p.Monitor != nil {
		fresh, err := s.GetBook(ctx, id)
		if err != nil {
			return nil, otelx.RecordSpanError(span, err)
		}
		ebook, audiobook, _ := slotFlags(*p.Monitor)
		if err := s.setMonitored(ctx, fresh, slotEbook, ebook); err != nil {
			return nil, otelx.RecordSpanError(span, err)
		}
		if err := s.setMonitored(ctx, fresh, slotAudiobook, audiobook); err != nil {
			return nil, otelx.RecordSpanError(span, err)
		}
	}
	out, err := s.GetBook(ctx, id)
	return out, otelx.RecordSpanError(span, err)
}

// setPreferredLanguage stores the language, recomputes the titles from the
// stored editions and re-picks the edition of every slot that holds no file;
// a slot with a file keeps its edition.
func (s *Service) setPreferredLanguage(
	ctx context.Context,
	b *ent.Book,
	language string,
) error {
	if err := s.db.SetBookPreferredLanguage(ctx, b.ID, language); err != nil {
		return err
	}
	views := entEditionViews(b.Edges.Editions)
	title, original := pick.Titles(views, language, b.Title)
	if err := s.db.SetBookTitles(ctx, b.ID, title, original); err != nil {
		return err
	}
	for _, kind := range slotKinds {
		if hasSlotFile(b, kind) {
			continue
		}
		ed, ok := pick.Slot(views, kind, language)
		if !ok {
			continue
		}
		if err := s.db.SetBookSlotEdition(ctx, b.ID, kind, ed.ID); err != nil {
			return err
		}
	}
	return nil
}

// setSlotEdition moves a slot to another edition. A slot with a file keeps it
// until the new edition arrives: the old edition's language is remembered, the
// slot goes back to wanted, and the next grab replaces the file once the new
// one is placed.
func (s *Service) setSlotEdition(
	ctx context.Context,
	b *ent.Book,
	kind string,
	edition uint32,
) error {
	cur := slotEdition(b, kind)
	if hasSlotFile(b, kind) && (cur == nil || cur.ID != edition) {
		old := b.PreferredLanguage
		if cur != nil {
			old = cur.Language
		}
		if err := s.db.SetBookReplacing(ctx, b.ID, kind, old); err != nil {
			return err
		}
		if err := s.db.SetBookSlotStatus(
			ctx, b.ID, kind, "available", "wanted",
		); err != nil {
			return err
		}
	}
	return s.db.SetBookSlotEdition(ctx, b.ID, kind, edition)
}

// setMonitored monitors or unmonitors one slot. Monitoring picks an edition
// when the slot has none; a hydrated book with no edition of that format stays
// unmonitored, the patch silently leaving it so.
func (s *Service) setMonitored(
	ctx context.Context,
	b *ent.Book,
	kind string,
	on bool,
) error {
	if on && slotEdition(b, kind) == nil && b.LastRefreshedAt != nil {
		ed, ok := pick.Slot(
			entEditionViews(b.Edges.Editions),
			kind,
			b.PreferredLanguage,
		)
		if !ok {
			return nil
		}
		if err := s.db.SetBookSlotEdition(ctx, b.ID, kind, ed.ID); err != nil {
			return err
		}
	}
	return s.db.SetBookSlot(ctx, b.ID, kind, on)
}

// EditionOption is one (language, publisher) a series can be switched to.
type EditionOption struct {
	Label     string
	Language  string
	Publisher string
	// Volumes is how many volumes have a stored ebook edition of it.
	Volumes int
}

// SeriesEditions lists the editions a series can be switched to: every
// distinct (language, publisher) with a publisher among the stored ebook
// editions of its volumes, the ones covering most volumes first. Every label
// is a real choice for at least one volume.
func SeriesEditions(s *ent.BookSeries) []EditionOption {
	type key struct{ language, publisher string }
	counts := map[key]int{}
	for _, v := range s.Edges.Volumes {
		seen := map[key]bool{}
		for _, e := range v.Edges.Editions {
			k := key{e.Language, e.Publisher}
			if e.Format != "ebook" || e.Publisher == "" || seen[k] {
				continue
			}
			seen[k] = true
			counts[k]++
		}
	}
	out := make([]EditionOption, 0, len(counts))
	for k, n := range counts {
		out = append(out, EditionOption{
			Label:     editionLabel(k.language, k.publisher),
			Language:  k.language,
			Publisher: k.publisher,
			Volumes:   n,
		})
	}
	slices.SortFunc(out, func(a, b EditionOption) int {
		return cmp.Or(
			cmp.Compare(b.Volumes, a.Volumes),
			cmp.Compare(a.Label, b.Label),
		)
	})
	return out
}

// SeriesEditionLabel is the label of the series' current choice.
func SeriesEditionLabel(s *ent.BookSeries) string {
	if s.EditionLanguage == "" {
		return ""
	}
	return editionLabel(s.EditionLanguage, s.EditionPublisher)
}

// PatchSeries stores the monitor policy, the profile (written through to every
// volume) and the edition, and applies each to the volumes. The answer is the
// series as it now stands.
func (s *Service) PatchSeries(
	ctx context.Context,
	id uint32,
	p PatchSeriesParams,
) (*ent.BookSeries, error) {
	ctx, span := tracer.Start(ctx, "book.patch_series",
		trace.WithAttributes(attribute.Int("series.id", int(id))))
	defer span.End()

	row, err := s.GetSeries(ctx, id)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	if p.Monitor != nil && !validPolicy(*p.Monitor) {
		return nil, otelx.RecordSpanError(
			span, fmt.Errorf("%w: %q", ErrInvalidMonitor, *p.Monitor),
		)
	}
	if p.QualityProfile != nil {
		if err := checkProfile(*p.QualityProfile); err != nil {
			return nil, otelx.RecordSpanError(span, err)
		}
	}
	var chosen *EditionOption
	if p.Edition != nil {
		for _, o := range SeriesEditions(row) {
			if o.Label == *p.Edition {
				chosen = &o
				break
			}
		}
		if chosen == nil && *p.Edition == SeriesEditionLabel(row) {
			chosen = &EditionOption{
				Language: row.EditionLanguage, Publisher: row.EditionPublisher,
			}
		}
		if chosen == nil {
			return nil, otelx.RecordSpanError(
				span, fmt.Errorf("%w: %q", ErrUnknownEdition, *p.Edition),
			)
		}
	}

	if p.QualityProfile != nil {
		if err := s.db.SetSeriesQualityProfile(
			ctx,
			id,
			*p.QualityProfile,
		); err != nil {
			return nil, otelx.RecordSpanError(span, err)
		}
	}
	if p.Monitor != nil {
		if err := s.db.SetSeriesMonitor(
			ctx,
			id,
			*p.Monitor,
			time.Now(),
		); err != nil {
			return nil, otelx.RecordSpanError(span, err)
		}
	}
	if chosen != nil {
		if err := s.db.SetSeriesEdition(
			ctx,
			id,
			chosen.Language,
			chosen.Publisher,
		); err != nil {
			return nil, otelx.RecordSpanError(span, err)
		}
	}
	out, err := s.GetSeries(ctx, id)
	return out, otelx.RecordSpanError(span, err)
}
