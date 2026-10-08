package book

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/observability"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/posters"
	"github.com/datahearth/streamline/internal/utils/numeric"
)

var tracer = otel.Tracer("github.com/datahearth/streamline/internal/media/book")

var (
	ErrAuthorExists    = errors.New("author already exists")
	ErrAuthorNotFound  = errors.New("author not found")
	ErrBookNotFound    = errors.New("book not found")
	ErrInvalidSlotKind = errors.New("slot kind must be ebook or audiobook")
	ErrNotConfigured   = errors.New("hardcover is not configured")
)

// Manager is the surface the REST handlers use.
type Manager interface {
	Add(ctx context.Context, p AddParams) (*ent.Author, error)
	List(ctx context.Context, page, limit uint16) ([]*ent.Author, uint32, error)
	Get(ctx context.Context, id uint32) (*ent.Author, error)
	GetBook(ctx context.Context, id uint32) (*ent.Book, error)
	SetAuthorMonitored(ctx context.Context, id uint32, m bool) error
	SetBookSlot(ctx context.Context, id uint32, kind string, monitored bool) error
	UpdateAuthor(ctx context.Context, id uint32, p UpdateAuthorParams) error
	Delete(ctx context.Context, id uint32, deleteFiles bool) error
	RefreshOne(ctx context.Context, id uint32) (*ent.Author, error)
	SearchBookReleases(
		ctx context.Context,
		bookID uint32,
		kind string,
	) ([]ReleaseResult, error)
	GrabBookRelease(ctx context.Context, bookID uint32, p GrabParams) error
}

var _ Manager = (*Service)(nil)

type Service struct {
	db       db.Store
	metadata metadata.BookProvider
	posters  posters.Manager
	indexers indexer.Manager
	download download.Downloader
}

func NewService(
	store db.Store,
	meta metadata.BookProvider,
	p posters.Manager,
	idx indexer.Manager,
	dl download.Downloader,
) *Service {
	return &Service{
		db: store, metadata: meta, posters: p, indexers: idx, download: dl,
	}
}

// Adder is the slice of the service the request flow approves through.
type Adder interface {
	Add(ctx context.Context, p AddParams) (*ent.Author, error)
	Get(ctx context.Context, id uint32) (*ent.Author, error)
	SetBookSlot(ctx context.Context, id uint32, kind string, monitored bool) error
}

var _ Adder = (*Service)(nil)

type AddParams struct {
	HardcoverID             uint32
	Monitored               bool
	MonitorPolicy           string // all | future | none
	WantKinds               string // ebook | audiobook | both
	EbookQualityProfile     string
	AudiobookQualityProfile string
}

type UpdateAuthorParams = db.UpdateAuthorParams

const (
	defaultMonitorPolicy = "all"
	defaultWantKinds     = "ebook"
)

// Add fetches the author and bibliography, creates the Author and Book rows
// in one transaction, then fetches cover art in the background.
func (s *Service) Add(ctx context.Context, p AddParams) (*ent.Author, error) {
	ctx, span := tracer.Start(ctx, "book.add",
		trace.WithAttributes(attribute.Int("hardcover_id", int(p.HardcoverID))))
	defer span.End()

	if s.metadata == nil {
		return nil, otelx.RecordSpanError(span, ErrNotConfigured)
	}

	existing, err := s.db.FindAuthorByHardcoverID(ctx, p.HardcoverID)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	if existing != nil {
		return nil, otelx.RecordSpanError(
			span,
			fmt.Errorf("%w: hardcover id %d", ErrAuthorExists, p.HardcoverID),
		)
	}

	details, err := s.metadata.GetAuthor(ctx, p.HardcoverID)
	if err != nil {
		return nil, otelx.RecordSpanError(span, fmt.Errorf("get author: %w", err))
	}

	if p.MonitorPolicy == "" {
		p.MonitorPolicy = defaultMonitorPolicy
	}
	if p.WantKinds == "" {
		p.WantKinds = defaultWantKinds
	}

	now := time.Now()
	seeds := make([]db.BookSeed, len(details.Books))
	covers := make(map[uint32]string, len(details.Books))
	for i, bk := range details.Books {
		ebook, audiobook := applyPolicy(
			p.MonitorPolicy,
			p.WantKinds,
			bk.ReleaseDate,
			now,
		)
		seeds[i] = bookSeed(bk, ebook, audiobook)
		covers[bk.HardcoverID] = bk.CoverURL
	}

	author, err := s.db.CreateAuthor(ctx, db.CreateAuthorParams{
		HardcoverID:             details.HardcoverID,
		Name:                    details.Name,
		SortName:                details.Name,
		Overview:                details.Overview,
		Monitored:               p.Monitored,
		Folder:                  library.SanitizePath(details.Name),
		MonitorPolicy:           p.MonitorPolicy,
		WantKinds:               p.WantKinds,
		EbookQualityProfile:     p.EbookQualityProfile,
		AudiobookQualityProfile: p.AudiobookQualityProfile,
		Books:                   seeds,
	})
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	span.SetAttributes(attribute.Int("author.id", int(author.ID)))

	s.fetchPosters(ctx, author.ID, details.ImageURL, author.Edges.Books, covers)
	slog.InfoContext(ctx, "author added",
		"author.id", author.ID, "hardcover_id", author.HardcoverID,
		"books", len(author.Edges.Books))
	return author, nil
}

func bookSeed(bk metadata.BookInfo, ebook, audiobook bool) db.BookSeed {
	return db.BookSeed{
		HardcoverID:        bk.HardcoverID,
		Title:              bk.Title,
		SortTitle:          bk.Title,
		ReleaseDate:        bk.ReleaseDate,
		SeriesName:         bk.SeriesName,
		SeriesPosition:     bk.SeriesPosition,
		EbookMonitored:     ebook,
		AudiobookMonitored: audiobook,
	}
}

// applyPolicy reports which slots a new book starts monitored in.
func applyPolicy(
	policy, wantKinds string,
	release *time.Time,
	authorAddedAt time.Time,
) (ebook, audiobook bool) {
	monitor := policy == "all" ||
		(policy == "future" && release != nil && release.After(authorAddedAt))
	if !monitor {
		return false, false
	}
	return wantKinds == "ebook" || wantKinds == "both",
		wantKinds == "audiobook" || wantKinds == "both"
}

// fetchPosters is best-effort and runs after the commit: a missing cover is
// routine and must not fail the add. authorImage is empty for a refresh.
func (s *Service) fetchPosters(
	ctx context.Context,
	authorID uint32,
	authorImage string,
	books []*ent.Book,
	covers map[uint32]string,
) {
	if authorImage == "" && len(books) == 0 {
		return
	}
	bg := context.WithoutCancel(ctx)
	go func() {
		defer observability.RecoverPanic(bg, "book.fetch_posters", nil)
		if authorImage != "" {
			if err := s.posters.Fetch(
				bg,
				"authors",
				authorID,
				authorImage,
			); err != nil {
				slog.WarnContext(bg, "author image fetch failed",
					"author.id", authorID, "error", err)
			}
		}
		for _, b := range books {
			src := covers[b.HardcoverID]
			if src == "" {
				continue
			}
			if err := s.posters.Fetch(bg, "books", b.ID, src); err != nil {
				slog.WarnContext(bg, "book cover fetch failed",
					"book.id", b.ID, "error", err)
			}
		}
	}()
}

func (s *Service) List(
	ctx context.Context,
	page, limit uint16,
) ([]*ent.Author, uint32, error) {
	ctx, span := tracer.Start(ctx, "book.list")
	defer span.End()
	if page == 0 {
		page = 1
	}
	if limit == 0 {
		limit = 20
	}
	total, err := s.db.CountAuthors(ctx)
	if err != nil {
		return nil, 0, otelx.RecordSpanError(span, err)
	}
	rows, err := s.db.ListAuthors(ctx, uint32(page-1)*uint32(limit), uint32(limit))
	if err != nil {
		return nil, 0, otelx.RecordSpanError(span, err)
	}
	return rows, numeric.SaturateU32(total), nil
}

func (s *Service) Get(ctx context.Context, id uint32) (*ent.Author, error) {
	ctx, span := tracer.Start(ctx, "book.get",
		trace.WithAttributes(attribute.Int("author.id", int(id))))
	defer span.End()
	author, err := s.db.FindAuthorByID(ctx, id)
	if err != nil {
		return nil, otelx.RecordSpanError(span, notFound(err))
	}
	return author, nil
}

func (s *Service) GetBook(ctx context.Context, id uint32) (*ent.Book, error) {
	ctx, span := tracer.Start(ctx, "book.get_book",
		trace.WithAttributes(attribute.Int("book.id", int(id))))
	defer span.End()
	row, err := s.db.FindBookByID(ctx, id)
	if ent.IsNotFound(err) {
		err = ErrBookNotFound
	}
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	return row, nil
}

func (s *Service) SetAuthorMonitored(ctx context.Context, id uint32, m bool) error {
	ctx, span := tracer.Start(ctx, "book.set_author_monitored",
		trace.WithAttributes(
			attribute.Int("author.id", int(id)),
			attribute.Bool("monitored", m),
		))
	defer span.End()
	return otelx.RecordSpanError(span, notFound(s.db.SetAuthorMonitored(ctx, id, m)))
}

func (s *Service) UpdateAuthor(
	ctx context.Context,
	id uint32,
	p UpdateAuthorParams,
) error {
	ctx, span := tracer.Start(ctx, "book.update_author",
		trace.WithAttributes(attribute.Int("author.id", int(id))))
	defer span.End()
	return otelx.RecordSpanError(span, notFound(s.db.UpdateAuthor(ctx, id, p)))
}

func (s *Service) SetBookSlot(
	ctx context.Context,
	id uint32,
	kind string,
	monitored bool,
) error {
	ctx, span := tracer.Start(ctx, "book.set_book_slot",
		trace.WithAttributes(
			attribute.Int("book.id", int(id)),
			attribute.String("kind", kind),
			attribute.Bool("monitored", monitored),
		))
	defer span.End()
	if kind != string(mediafile.BookKindEbook) &&
		kind != string(mediafile.BookKindAudiobook) {
		return otelx.RecordSpanError(span, ErrInvalidSlotKind)
	}
	err := s.db.SetBookSlot(ctx, id, kind, monitored)
	if ent.IsNotFound(err) {
		err = ErrBookNotFound
	}
	return otelx.RecordSpanError(span, err)
}

// Delete removes the author; books and media-file rows cascade in the schema.
// Files on disk are removed only when deleteFiles is set.
func (s *Service) Delete(ctx context.Context, id uint32, deleteFiles bool) error {
	ctx, span := tracer.Start(ctx, "book.delete",
		trace.WithAttributes(
			attribute.Int("author.id", int(id)),
			attribute.Bool("delete_files", deleteFiles),
		))
	defer span.End()

	author, err := s.db.FindAuthorByID(ctx, id)
	if err != nil {
		return otelx.RecordSpanError(span, notFound(err))
	}
	if deleteFiles {
		lib := config.Get().Library
		for _, b := range author.Edges.Books {
			for _, f := range b.Edges.MediaFiles {
				root := lib.EbookPath
				if f.BookKind == mediafile.BookKindAudiobook {
					root = lib.AudiobookPath
				}
				if err := library.RemoveMediaFile(ctx, f.Path, root); err != nil {
					slog.ErrorContext(ctx, "book file was not deleted from disk",
						"author.id", id, "path", f.Path, "error", err)
				}
			}
		}
	}
	if err := s.db.DeleteAuthor(ctx, id); err != nil {
		return otelx.RecordSpanError(span, notFound(err))
	}
	for _, b := range author.Edges.Books {
		if err := s.posters.Remove("books", b.ID); err != nil {
			slog.WarnContext(ctx, "book poster was not removed",
				"book.id", b.ID, "error", err)
		}
	}
	if err := s.posters.Remove("authors", id); err != nil {
		slog.WarnContext(ctx, "author poster was not removed",
			"author.id", id, "error", err)
	}
	slog.InfoContext(ctx, "author deleted", "author.id", id)
	return nil
}

// RefreshOne re-fetches the bibliography. Books new to the author take the
// author's monitor policy; existing books update metadata only, so slot flags
// and statuses stay as the user left them.
func (s *Service) RefreshOne(ctx context.Context, id uint32) (*ent.Author, error) {
	ctx, span := tracer.Start(ctx, "book.refresh_one",
		trace.WithAttributes(attribute.Int("author.id", int(id))))
	defer span.End()

	if s.metadata == nil {
		return nil, otelx.RecordSpanError(span, ErrNotConfigured)
	}

	author, err := s.db.FindAuthorByID(ctx, id)
	if err != nil {
		return nil, otelx.RecordSpanError(span, notFound(err))
	}
	details, err := s.metadata.GetAuthor(ctx, author.HardcoverID)
	if err != nil {
		return nil, otelx.RecordSpanError(span, fmt.Errorf("get author: %w", err))
	}

	known := make(map[uint32]bool, len(author.Edges.Books))
	for _, b := range author.Edges.Books {
		known[b.HardcoverID] = true
	}
	covers := make(map[uint32]string, len(details.Books))
	seeds := make([]db.BookSeed, len(details.Books))
	for i, bk := range details.Books {
		covers[bk.HardcoverID] = bk.CoverURL
		var ebook, audiobook bool
		if !known[bk.HardcoverID] {
			ebook, audiobook = applyPolicy(
				string(author.MonitorPolicy),
				string(author.WantKinds),
				bk.ReleaseDate,
				author.CreateTime,
			)
		}
		seeds[i] = bookSeed(bk, ebook, audiobook)
	}

	if err := s.db.RefreshAuthor(ctx, id, db.RefreshAuthorParams{
		Name:        details.Name,
		SortName:    details.Name,
		Overview:    details.Overview,
		Books:       seeds,
		RefreshedAt: time.Now(),
	}); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}

	updated, err := s.db.FindAuthorByID(ctx, id)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	var added []*ent.Book
	for _, b := range updated.Edges.Books {
		if !known[b.HardcoverID] {
			added = append(added, b)
		}
	}
	s.fetchPosters(ctx, id, "", added, covers)
	return updated, nil
}

func notFound(err error) error {
	if ent.IsNotFound(err) {
		return ErrAuthorNotFound
	}
	return err
}
