package bulkimport

import (
	"context"
	"errors"

	"github.com/datahearth/streamline/ent"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanfile "github.com/datahearth/streamline/ent/importscanfile"
	entimportscanshow "github.com/datahearth/streamline/ent/importscanshow"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/media/movie"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Manager is the consumer-facing interface for the bulk-import service.
type Manager interface {
	StartScan(ctx context.Context, p StartScanParams) (*ent.ImportScan, error)
	Get(ctx context.Context, id uint32) (*ent.ImportScan, error)
	List(ctx context.Context, page, limit uint16) ([]*ent.ImportScan, uint32, error)
	Files(ctx context.Context, p FilesParams) ([]*ent.ImportScanFile, uint32, error)
	GetFile(ctx context.Context, scanID, fileID uint32) (*ent.ImportScanFile, error)
	UpdateFileDecision(
		ctx context.Context,
		scanID, fileID uint32,
		decision entimportscanfile.Decision,
		tmdbID *uint32,
	) (*ent.ImportScanFile, error)
	Cancel(ctx context.Context, id uint32) error
	Commit(ctx context.Context, id uint32) error
	Delete(ctx context.Context, id uint32) error
	BulkDecide(ctx context.Context, p BulkDecisionParams) (int, error)
	AbortInflight(ctx context.Context) (uint32, error)
}

// FilesParams is the input for Manager.Files.
type FilesParams struct {
	ScanID         uint32
	Classification entimportscanfile.Classification // empty = all
	Query          string
	Page           uint16
	Limit          uint16
}

// SeriesAdder creates a TV show (with its seasons and episodes) from a TVDB id.
// Satisfied by *tvshow.Service; used to adopt shows on series-scan commit.
type SeriesAdder interface {
	Add(
		ctx context.Context,
		tvdbID uint32,
		qualityProfile string,
	) (*ent.TVShow, error)
}

// Service implements Manager.
type Service struct {
	store       db.Store
	metadata    metadata.Provider
	tvmeta      metadata.TVProvider
	importSvc   *library.ImportService
	movieSvc    *movie.Service
	seriesAdder SeriesAdder
	moviePath   string
	seriesPath  string
}

// NewService constructs the bulk-import service.
func NewService(
	store db.Store,
	meta metadata.Provider,
	tvmeta metadata.TVProvider,
	importSvc *library.ImportService,
	movieSvc *movie.Service,
	seriesAdder SeriesAdder,
	moviePath string,
	seriesPath string,
) *Service {
	return &Service{
		store:       store,
		metadata:    meta,
		tvmeta:      tvmeta,
		importSvc:   importSvc,
		movieSvc:    movieSvc,
		seriesAdder: seriesAdder,
		moviePath:   moviePath,
		seriesPath:  seriesPath,
	}
}

// AbortInflight is the boot-time helper. Called from wire.go before the HTTP server starts.
func (s *Service) AbortInflight(ctx context.Context) (uint32, error) {
	return s.store.AbortInflightImportScans(ctx, failureMessageOnRestart)
}

func (s *Service) Get(ctx context.Context, id uint32) (*ent.ImportScan, error) {
	return s.store.FindImportScan(ctx, id)
}

func (s *Service) List(
	ctx context.Context,
	page, limit uint16,
) ([]*ent.ImportScan, uint32, error) {
	if page == 0 {
		page = 1
	}
	if limit == 0 {
		limit = historyPageSize
	}
	return s.store.ListImportScans(ctx, uint32(page-1)*uint32(limit), uint32(limit))
}

func (s *Service) Files(
	ctx context.Context,
	p FilesParams,
) ([]*ent.ImportScanFile, uint32, error) {
	if _, err := s.store.FindImportScan(ctx, p.ScanID); err != nil {
		if ent.IsNotFound(err) {
			return nil, 0, ErrScanNotFound
		}
		return nil, 0, err
	}
	page := p.Page
	if page == 0 {
		page = 1
	}
	limit := p.Limit
	if limit == 0 {
		limit = reviewPageSize
	}
	return s.store.FilterImportScanFiles(ctx, db.FilterImportScanFilesParams{
		ScanID:         p.ScanID,
		Classification: p.Classification,
		Query:          p.Query,
		Offset:         uint32(page-1) * uint32(limit),
		Limit:          uint32(limit),
	})
}

func (s *Service) GetFile(
	ctx context.Context,
	scanID, fileID uint32,
) (*ent.ImportScanFile, error) {
	row, err := s.store.FindImportScanFile(ctx, scanID, fileID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrScanFileNotFound
		}
		return nil, err
	}
	return row, nil
}

func (s *Service) UpdateFileDecision(
	ctx context.Context,
	scanID, fileID uint32,
	decision entimportscanfile.Decision,
	tmdbID *uint32,
) (*ent.ImportScanFile, error) {
	if err := s.store.UpdateImportScanFileDecision(
		ctx,
		scanID,
		fileID,
		decision,
		tmdbID,
	); err != nil {
		if errors.Is(err, db.ErrImportScanFileNotFound) {
			return nil, ErrScanFileNotFound
		}
		return nil, err
	}
	return s.GetFile(ctx, scanID, fileID)
}

func (s *Service) Cancel(ctx context.Context, id uint32) error {
	scan, err := s.store.FindImportScan(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrScanNotFound
		}
		return err
	}
	if scan.Status != entimportscan.StatusRunning &&
		scan.Status != entimportscan.StatusCommitting {
		return ErrScanNotCancellable
	}
	return s.store.UpdateImportScanStatus(
		ctx,
		id,
		entimportscan.StatusCancelled,
		db.UpdateScanStatusOpts{},
	)
}

func (s *Service) Delete(ctx context.Context, id uint32) error {
	scan, err := s.store.FindImportScan(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrScanNotFound
		}
		return err
	}
	if scan.Status == entimportscan.StatusRunning ||
		scan.Status == entimportscan.StatusCommitting {
		return ErrScanNotDeletable
	}
	return s.store.DeleteImportScan(ctx, id)
}

// BulkDecisionParams narrows a bulk decision. An empty Classification and no
// IDs means every row in the scan.
type BulkDecisionParams struct {
	ScanID         uint32
	Decision       string
	Classification string
	IDs            []uint32
}

// BulkDecide applies one decision across a scan's rows and returns how many
// changed. It dispatches on the scan's kind, so one call serves both movie
// files and series shows — reviewing a 465-file scan through the per-row
// endpoint took 465 requests.
func (s *Service) BulkDecide(
	ctx context.Context,
	p BulkDecisionParams,
) (int, error) {
	ctx, span := tracer.Start(ctx, "bulkimport.bulk_decide",
		trace.WithAttributes(
			attribute.Int64("scan.id", int64(p.ScanID)),
			attribute.String("decision", p.Decision),
		))
	defer span.End()

	scan, err := s.store.FindImportScan(ctx, p.ScanID)
	if err != nil {
		if ent.IsNotFound(err) {
			return 0, otelx.RecordSpanError(span, ErrScanNotFound)
		}
		return 0, otelx.RecordSpanError(span, err)
	}
	if scan.Status != entimportscan.StatusAwaitingReview {
		return 0, otelx.RecordSpanError(span, ErrScanNotReviewable)
	}

	if scan.Kind == entimportscan.KindSeries {
		n, err := s.store.BulkUpdateImportScanShowDecisions(
			ctx, p.ScanID,
			entimportscanshow.Decision(p.Decision),
			entimportscanshow.Classification(p.Classification),
			p.IDs,
		)
		return n, otelx.RecordSpanError(span, err)
	}
	n, err := s.store.BulkUpdateImportScanFileDecisions(
		ctx, p.ScanID,
		entimportscanfile.Decision(p.Decision),
		entimportscanfile.Classification(p.Classification),
		p.IDs,
	)
	return n, otelx.RecordSpanError(span, err)
}
