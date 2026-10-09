package restapi

import (
	"context"
	"errors"

	"github.com/datahearth/streamline/internal/config"
)

func (s *Server) DeleteMusicQualityProfile(
	ctx context.Context,
	request DeleteMusicQualityProfileRequestObject,
) (DeleteMusicQualityProfileResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return DeleteMusicQualityProfile403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	switch err := config.DeleteMusicQualityProfile(ctx, request.Name); {
	case errors.Is(err, config.ErrMusicQualityProfileNotFound):
		return DeleteMusicQualityProfile404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case errors.Is(err, config.ErrMusicQualityProfileInUseAsDefault):
		return DeleteMusicQualityProfile409JSONResponse{
			ConflictJSONResponse: errConflict(err.Error()),
		}, nil
	case configLocked(err):
		return DeleteMusicQualityProfile403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return DeleteMusicQualityProfile500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return DeleteMusicQualityProfile204Response{}, nil
}
