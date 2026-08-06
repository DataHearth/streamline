package restapi

import (
	"context"
	"errors"

	"github.com/datahearth/streamline/internal/library/pathmigrate"
)

func (s *Server) GetPathMigration(
	ctx context.Context,
	_ GetPathMigrationRequestObject,
) (GetPathMigrationResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return GetPathMigration403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	return GetPathMigration200JSONResponse{
		PathMigrationJSONResponse: PathMigrationJSONResponse(
			toAPIPathMigration(s.pathMigrations.Status()),
		),
	}, nil
}

func (s *Server) GetPathMigrationRoots(
	ctx context.Context,
	_ GetPathMigrationRootsRequestObject,
) (GetPathMigrationRootsResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return GetPathMigrationRoots403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	roots, err := s.pathMigrations.Roots(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]PathMigrationRoot, 0, len(roots))
	for _, r := range roots {
		items = append(items, PathMigrationRoot{
			Root:    PathMigrationRootRoot(r.Root),
			Path:    r.Path,
			Tracked: r.Tracked,
			Total:   r.Total,
		})
	}
	return GetPathMigrationRoots200JSONResponse{
		PathMigrationRootListJSONResponse: PathMigrationRootListJSONResponse{
			Items: items,
		},
	}, nil
}

func (s *Server) StartPathMigration(
	ctx context.Context,
	req StartPathMigrationRequestObject,
) (StartPathMigrationResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return StartPathMigration403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	status, err := s.pathMigrations.Start(ctx, toPathMigrateParams(*req.Body))
	if err != nil {
		switch {
		case errors.Is(err, pathmigrate.ErrMigrationRunning):
			return StartPathMigration409JSONResponse{
				ConflictJSONResponse: errConflict(err.Error()),
			}, nil
		case badMigrationParams(err):
			return StartPathMigration422JSONResponse{
				UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
			}, nil
		default:
			return nil, err
		}
	}
	return StartPathMigration202JSONResponse{
		PathMigrationJSONResponse: PathMigrationJSONResponse(
			toAPIPathMigration(status),
		),
	}, nil
}

func (s *Server) PreviewPathMigration(
	ctx context.Context,
	req PreviewPathMigrationRequestObject,
) (PreviewPathMigrationResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return PreviewPathMigration403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	preview, err := s.pathMigrations.Preview(
		ctx,
		toPathMigrateParams(*req.Body),
	)
	if err != nil {
		if !badMigrationParams(err) {
			return nil, err
		}
		return PreviewPathMigration422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	samples := make([]PathRewrite, 0, len(preview.Samples))
	for _, e := range preview.Samples {
		samples = append(samples, PathRewrite{From: e.From, To: e.To})
	}
	return PreviewPathMigration200JSONResponse{
		PathMigrationPreviewJSONResponse: PathMigrationPreviewJSONResponse{
			Root:    PathMigrationPreviewRoot(preview.Root),
			From:    preview.From,
			To:      preview.To,
			Total:   preview.Total,
			Skipped: preview.Skipped,
			CanMove: preview.CanMove,
			Samples: samples,
		},
	}, nil
}

// badMigrationParams separates "the operator asked for something invalid"
// from an infrastructure failure while collecting the affected rows — the
// latter is a 500, not a 422 the operator could fix by retyping a path.
func badMigrationParams(err error) bool {
	return errors.Is(err, pathmigrate.ErrInvalidPath) ||
		errors.Is(err, pathmigrate.ErrUnknownRoot) ||
		errors.Is(err, pathmigrate.ErrMoveUnsupported)
}

func toPathMigrateParams(body PathMigrationRequest) pathmigrate.Params {
	p := pathmigrate.Params{
		Root: pathmigrate.Root(body.Root),
		To:   body.To,
	}
	if body.From != nil {
		p.From = *body.From
	}
	if body.MoveFiles != nil {
		p.MoveFiles = *body.MoveFiles
	}
	return p
}

func toAPIPathMigration(s pathmigrate.Status) PathMigration {
	out := PathMigration{
		Running:   s.Running,
		Root:      string(s.Root),
		From:      s.From,
		To:        s.To,
		MoveFiles: s.MoveFiles,
		Total:     s.Total,
		Done:      s.Done,
		Skipped:   s.Skipped,
		Current:   s.Current,
	}
	if s.Error != "" {
		out.Error = &s.Error
	}
	if !s.StartedAt.IsZero() {
		out.StartedAt = &s.StartedAt
	}
	if !s.FinishedAt.IsZero() {
		out.FinishedAt = &s.FinishedAt
	}
	return out
}
