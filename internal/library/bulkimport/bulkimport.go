package bulkimport

import (
	"context"

	"github.com/datahearth/streamline/ent"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanfile "github.com/datahearth/streamline/ent/importscanfile"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/media/movie"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/posters"
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
	importSvc   *library.ImportService
	movieSvc    *movie.Service
	seriesAdder SeriesAdder
	posters     posters.Manager
	libraryDir  string
}

// NewService constructs the bulk-import service.
func NewService(
	store db.Store,
	meta metadata.Provider,
	importSvc *library.ImportService,
	movieSvc *movie.Service,
	seriesAdder SeriesAdder,
	posters posters.Manager,
	libraryDir string,
) *Service {
	return &Service{
		store:       store,
		metadata:    meta,
		importSvc:   importSvc,
		movieSvc:    movieSvc,
		seriesAdder: seriesAdder,
		posters:     posters,
		libraryDir:  libraryDir,
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
			return nil, ErrScanNotFound
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
		fileID,
		decision,
		tmdbID,
	); err != nil {
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
