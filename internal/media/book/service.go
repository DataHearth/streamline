// Package book is the books vertical. A Book is the root entity and a shelf
// item on its own, or a volume of a BookSeries; people are Author rows joined
// to either through a role. Hardcover is called in batches of twenty: a book
// add is one request, a series add is one for the skeleton plus one per twenty
// volumes of which only the first batch is on the HTTP request, and the rest
// is hydrated by a background worker.
package book

import (
	"context"
	"errors"
	"sync/atomic"

	"go.opentelemetry.io/otel"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/mediaserver"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/posters"
)

var tracer = otel.Tracer("github.com/datahearth/streamline/internal/media/book")

var (
	ErrBookNotFound      = errors.New("book not found")
	ErrSeriesNotFound    = errors.New("series not found")
	ErrBookExists        = errors.New("book already in the library")
	ErrSeriesExists      = errors.New("series already in the library")
	ErrHardcoverNotFound = errors.New("hardcover has no such title")
	ErrInvalidSlotKind   = errors.New("slot kind must be ebook or audiobook")
	ErrSlotMismatch      = errors.New(
		"the release is not of the slot it was grabbed for",
	)
	ErrNoQualityProfile = errors.New("no quality profile configured for this slot")
	ErrNotConfigured    = errors.New("hardcover is not configured")
	ErrUnknownProfile   = errors.New("unknown book quality profile")
	ErrInvalidMonitor   = errors.New("unknown monitor value")
	ErrInvalidLanguage  = errors.New(
		"preferred_language must be a two-letter ISO 639-1 code",
	)
	ErrInvalidKind     = errors.New("unknown book kind")
	ErrEditionMismatch = errors.New("format and edition_id go together")
	ErrUnknownEdition  = errors.New("no such edition")
	ErrSeriesVolume    = errors.New("a volume is removed with its series")
	ErrNotHydrated     = errors.New(
		"the book has not been read from hardcover yet",
	)
)

// Book monitor values and series policies, as the wire spells them.
const (
	MonitorBoth      = "both"
	MonitorEbook     = "ebook"
	MonitorAudiobook = "audiobook"
	MonitorNone      = "none"

	PolicyAll    = "all"
	PolicyFuture = "future"
	PolicyNone   = "none"
)

// Manager is the surface the REST handlers use.
type Manager interface {
	Lookup(ctx context.Context, query, kind string) ([]LookupHit, error)
	LookupDetail(
		ctx context.Context,
		kind string,
		hardcoverID uint32,
	) (*LookupDetail, error)
	AddBook(ctx context.Context, p AddBookParams) (*ent.Book, error)
	AddSeries(ctx context.Context, p AddSeriesParams) (*ent.BookSeries, error)
	List(ctx context.Context, p ListParams) (ShelfPage, error)
	Counts(ctx context.Context, p ListParams) (db.ShelfCounts, error)
	GetBook(ctx context.Context, id uint32) (*ent.Book, error)
	GetSeries(ctx context.Context, id uint32) (*ent.BookSeries, error)
	// Progress is the live progress, 0 to 100, of each slot being downloaded.
	Progress(ctx context.Context, bookIDs []uint32) map[uint32]map[string]float64
	PatchBook(ctx context.Context, id uint32, p PatchBookParams) (*ent.Book, error)
	PatchSeries(
		ctx context.Context,
		id uint32,
		p PatchSeriesParams,
	) (*ent.BookSeries, error)
	DeleteBook(ctx context.Context, id uint32, deleteFiles bool) error
	DeleteSeries(ctx context.Context, id uint32, deleteFiles bool) error
	RefreshBook(ctx context.Context, id uint32) (*ent.Book, error)
	RefreshSeries(ctx context.Context, id uint32) (*ent.BookSeries, error)
	SearchBookReleases(
		ctx context.Context,
		bookID uint32,
		kind string,
	) ([]ReleaseResult, error)
	GrabBookRelease(ctx context.Context, bookID uint32, p GrabParams) error
	SearchNowBook(ctx context.Context, id uint32, kind string) (uint32, error)
	SearchNowSeries(ctx context.Context, id uint32) (uint32, error)
	RenameBook(
		ctx context.Context,
		id uint32,
		preview bool,
	) (library.RenamePlan, error)
	RenameSeries(
		ctx context.Context,
		id uint32,
		preview bool,
	) (library.RenamePlan, error)
}

var _ Manager = (*Service)(nil)

type Service struct {
	db       db.Store
	metadata metadata.BookProvider
	posters  posters.Manager
	indexers indexer.Manager
	download download.Downloader
	ms       mediaserver.Refresher

	hydrating atomic.Bool
	details   detailMemo
}

func NewService(
	store db.Store,
	meta metadata.BookProvider,
	p posters.Manager,
	idx indexer.Manager,
	dl download.Downloader,
	ms mediaserver.Refresher,
) *Service {
	return &Service{
		db: store, metadata: meta, posters: p, indexers: idx, download: dl, ms: ms,
	}
}

func (s *Service) provider() (metadata.BookProvider, error) {
	if s.metadata == nil {
		return nil, ErrNotConfigured
	}
	return s.metadata, nil
}
