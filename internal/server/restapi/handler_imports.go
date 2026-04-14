package restapi

import (
	"context"
	"errors"

	"github.com/datahearth/streamline/ent"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanfile "github.com/datahearth/streamline/ent/importscanfile"
	entimportscanshow "github.com/datahearth/streamline/ent/importscanshow"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library/bulkimport"
)

func (s *Server) StartImport(
	ctx context.Context,
	req StartImportRequestObject,
) (StartImportResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return StartImport403JSONResponse{ForbiddenJSONResponse: notAdminResp}, nil
	}
	params := bulkimport.StartScanParams{
		SourcePath: req.Body.SourcePath,
		Mode:       entimportscan.Mode(req.Body.Mode),
	}
	if req.Body.ImportMode != nil {
		params.ImportMode = entimportscan.ImportMode(*req.Body.ImportMode)
	}
	scan, err := s.bulkImports.StartScan(ctx, params)
	if err != nil {
		switch {
		case errors.Is(err, bulkimport.ErrInvalidPath),
			errors.Is(err, bulkimport.ErrPathOutsideLibrary):
			return StartImport422JSONResponse{
				UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
			}, nil
		case errors.Is(err, bulkimport.ErrScanRunning):
			return StartImport409JSONResponse{
				ConflictJSONResponse: errConflict(err.Error()),
			}, nil
		default:
			return nil, err
		}
	}
	return StartImport201JSONResponse{
		ImportScanJSONResponse: ImportScanJSONResponse(toAPIImportScan(scan)),
	}, nil
}

func (s *Server) GetImport(
	ctx context.Context,
	req GetImportRequestObject,
) (GetImportResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return GetImport403JSONResponse{ForbiddenJSONResponse: notAdminResp}, nil
	}
	scan, err := s.bulkImports.Get(ctx, req.Id)
	if err != nil {
		if ent.IsNotFound(err) {
			return GetImport404JSONResponse{
				NotFoundJSONResponse: errNotFound(err.Error()),
			}, nil
		}
		return nil, err
	}
	return GetImport200JSONResponse{
		ImportScanJSONResponse: ImportScanJSONResponse(toAPIImportScan(scan)),
	}, nil
}

func (s *Server) ListImports(
	ctx context.Context,
	req ListImportsRequestObject,
) (ListImportsResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return ListImports403JSONResponse{ForbiddenJSONResponse: notAdminResp}, nil
	}
	page, limit := pageOr(req.Params.Page, 1), pageOr(req.Params.Limit, 20)
	items, total, err := s.bulkImports.List(ctx, page, limit)
	if err != nil {
		return nil, err
	}
	apiItems := make([]ImportScan, 0, len(items))
	for _, it := range items {
		apiItems = append(apiItems, toAPIImportScan(it))
	}
	return ListImports200JSONResponse{
		ImportScanListJSONResponse: ImportScanListJSONResponse{
			Items: apiItems,
			Total: total,
		},
	}, nil
}

func (s *Server) CancelImport(
	ctx context.Context,
	req CancelImportRequestObject,
) (CancelImportResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return CancelImport403JSONResponse{ForbiddenJSONResponse: notAdminResp}, nil
	}
	if err := s.bulkImports.Cancel(ctx, req.Id); err != nil {
		switch {
		case errors.Is(err, bulkimport.ErrScanNotFound):
			return CancelImport404JSONResponse{
				NotFoundJSONResponse: errNotFound(err.Error()),
			}, nil
		case errors.Is(err, bulkimport.ErrScanNotCancellable):
			return CancelImport422JSONResponse{
				UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
			}, nil
		default:
			return nil, err
		}
	}
	return CancelImport204Response{}, nil
}

func (s *Server) DeleteImport(
	ctx context.Context,
	req DeleteImportRequestObject,
) (DeleteImportResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return DeleteImport403JSONResponse{ForbiddenJSONResponse: notAdminResp}, nil
	}
	if err := s.bulkImports.Delete(ctx, req.Id); err != nil {
		switch {
		case errors.Is(err, bulkimport.ErrScanNotFound):
			return DeleteImport404JSONResponse{
				NotFoundJSONResponse: errNotFound(err.Error()),
			}, nil
		case errors.Is(err, bulkimport.ErrScanNotDeletable):
			return DeleteImport422JSONResponse{
				UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
			}, nil
		default:
			return nil, err
		}
	}
	return DeleteImport204Response{}, nil
}

func (s *Server) CommitImport(
	ctx context.Context,
	req CommitImportRequestObject,
) (CommitImportResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return CommitImport403JSONResponse{ForbiddenJSONResponse: notAdminResp}, nil
	}
	if err := s.bulkImports.Commit(ctx, req.Id); err != nil {
		switch {
		case errors.Is(err, bulkimport.ErrScanNotFound):
			return CommitImport404JSONResponse{
				NotFoundJSONResponse: errNotFound(err.Error()),
			}, nil
		case errors.Is(err, bulkimport.ErrScanNotReviewable):
			return CommitImport422JSONResponse{
				UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
			}, nil
		default:
			return nil, err
		}
	}
	scan, err := s.bulkImports.Get(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return CommitImport202JSONResponse{
		ImportScanJSONResponse: ImportScanJSONResponse(toAPIImportScan(scan)),
	}, nil
}

func (s *Server) ListImportFiles(
	ctx context.Context,
	req ListImportFilesRequestObject,
) (ListImportFilesResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return ListImportFiles403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	page, limit := pageOr(req.Params.Page, 1), pageOr(req.Params.Limit, 50)
	cls := entimportscanfile.Classification("")
	if req.Params.Classification != nil {
		cls = entimportscanfile.Classification(*req.Params.Classification)
	}
	q := ""
	if req.Params.Q != nil {
		q = *req.Params.Q
	}
	items, total, err := s.bulkImports.Files(ctx, bulkimport.FilesParams{
		ScanID: req.Id, Classification: cls, Query: q, Page: page, Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	apiItems := make([]ImportScanFile, 0, len(items))
	for _, it := range items {
		apiItems = append(apiItems, toAPIImportScanFile(it))
	}
	return ListImportFiles200JSONResponse{
		ImportScanFileListJSONResponse: ImportScanFileListJSONResponse{
			Items: apiItems,
			Total: total,
		},
	}, nil
}

func (s *Server) UpdateImportFileDecision(
	ctx context.Context,
	req UpdateImportFileDecisionRequestObject,
) (UpdateImportFileDecisionResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return UpdateImportFileDecision403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	var tmdbID *uint32
	if req.Body.TmdbId != nil {
		v := *req.Body.TmdbId
		tmdbID = &v
	}
	f, err := s.bulkImports.UpdateFileDecision(
		ctx,
		req.Id,
		req.FileId,
		entimportscanfile.Decision(req.Body.Decision),
		tmdbID,
	)
	if err != nil {
		if errors.Is(err, bulkimport.ErrScanNotFound) {
			return UpdateImportFileDecision404JSONResponse{
				NotFoundJSONResponse: errNotFound(err.Error()),
			}, nil
		}
		return nil, err
	}
	return UpdateImportFileDecision200JSONResponse{
		ImportScanFileJSONResponse: ImportScanFileJSONResponse(
			toAPIImportScanFile(f),
		),
	}, nil
}

func (s *Server) ListImportShows(
	ctx context.Context,
	req ListImportShowsRequestObject,
) (ListImportShowsResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return ListImportShows403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	page, limit := pageOr(req.Params.Page, 1), pageOr(req.Params.Limit, 50)
	cls := entimportscanshow.Classification("")
	if req.Params.Classification != nil {
		cls = entimportscanshow.Classification(*req.Params.Classification)
	}
	items, total, err := s.store.ListImportScanShows(
		ctx,
		db.ListImportScanShowsParams{
			ScanID:         req.Id,
			Classification: cls,
			Offset:         uint32(page-1) * uint32(limit),
			Limit:          uint32(limit),
		},
	)
	if err != nil {
		return nil, err
	}
	apiItems := make([]ImportScanShow, 0, len(items))
	for _, it := range items {
		apiItems = append(apiItems, toAPIImportScanShow(it))
	}
	return ListImportShows200JSONResponse{
		ImportScanShowListJSONResponse: ImportScanShowListJSONResponse{
			Items: apiItems,
			Total: total,
		},
	}, nil
}

func (s *Server) UpdateImportShowDecision(
	ctx context.Context,
	req UpdateImportShowDecisionRequestObject,
) (UpdateImportShowDecisionResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return UpdateImportShowDecision403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	var tvdbID *uint32
	if req.Body.TvdbId != nil {
		v := *req.Body.TvdbId
		tvdbID = &v
	}
	if err := s.store.UpdateImportScanShowDecision(
		ctx,
		req.ShowId,
		entimportscanshow.Decision(req.Body.Decision),
		tvdbID,
	); err != nil {
		return nil, err
	}
	row, err := s.store.FindImportScanShow(ctx, req.Id, req.ShowId)
	if err != nil {
		if ent.IsNotFound(err) {
			return UpdateImportShowDecision404JSONResponse{
				NotFoundJSONResponse: errNotFound(err.Error()),
			}, nil
		}
		return nil, err
	}
	return UpdateImportShowDecision200JSONResponse{
		ImportScanShowJSONResponse: ImportScanShowJSONResponse(
			toAPIImportScanShow(row),
		),
	}, nil
}

func pageOr(v *uint16, def uint16) uint16 {
	if v != nil && *v > 0 {
		return *v
	}
	return def
}
