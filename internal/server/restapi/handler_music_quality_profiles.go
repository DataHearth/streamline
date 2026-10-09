package restapi

import (
	"context"
	"errors"

	"github.com/datahearth/streamline/internal/config"
)

func musicProfileToAPI(e config.MusicQualityProfileEntry) MusicQualityProfile {
	tiers := make([]MusicTier, len(e.Tiers))
	for i, t := range e.Tiers {
		tiers[i] = MusicTier(t)
	}
	isDefault := false
	if c := config.Get(); c != nil {
		isDefault = c.MusicQualityDefaultProfile == e.Name
	}
	return MusicQualityProfile{
		Name:           e.Name,
		Tiers:          tiers,
		Preferred:      MusicTier(e.Preferred),
		UpgradeAllowed: e.UpgradeAllowed,
		IsDefault:      isDefault,
	}
}

func musicTiersFromAPI(tiers []MusicTier) []string {
	out := make([]string, len(tiers))
	for i, t := range tiers {
		out[i] = string(t)
	}
	return out
}

func (s *Server) ListMusicQualityProfiles(
	_ context.Context,
	_ ListMusicQualityProfilesRequestObject,
) (ListMusicQualityProfilesResponseObject, error) {
	items := []MusicQualityProfile{}
	if c := config.Get(); c != nil {
		for _, p := range c.MusicQualityProfiles {
			items = append(items, musicProfileToAPI(p))
		}
	}
	return ListMusicQualityProfiles200JSONResponse(items), nil
}

func (s *Server) CreateMusicQualityProfile(
	ctx context.Context,
	request CreateMusicQualityProfileRequestObject,
) (CreateMusicQualityProfileResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return CreateMusicQualityProfile403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	if request.Body == nil {
		return CreateMusicQualityProfile422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable("body required"),
		}, nil
	}
	e := config.MusicQualityProfileEntry{
		Name:           request.Body.Name,
		Tiers:          musicTiersFromAPI(request.Body.Tiers),
		Preferred:      string(request.Body.Preferred),
		UpgradeAllowed: request.Body.UpgradeAllowed,
	}
	switch err := config.AddMusicQualityProfile(ctx, e); {
	case errors.Is(err, config.ErrMusicQualityProfileExists):
		return CreateMusicQualityProfile409JSONResponse{
			ConflictJSONResponse: errConflict(err.Error()),
		}, nil
	case configLocked(err):
		return CreateMusicQualityProfile403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return CreateMusicQualityProfile422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	stored, _ := config.LookupMusicQualityProfile(e.Name)
	return CreateMusicQualityProfile201JSONResponse{
		MusicQualityProfileResponseJSONResponse: MusicQualityProfileResponseJSONResponse(
			musicProfileToAPI(stored),
		),
	}, nil
}

func (s *Server) UpdateMusicQualityProfile(
	ctx context.Context,
	request UpdateMusicQualityProfileRequestObject,
) (UpdateMusicQualityProfileResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return UpdateMusicQualityProfile403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	if request.Body == nil {
		return UpdateMusicQualityProfile422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable("body required"),
		}, nil
	}
	tiers := musicTiersFromAPI(request.Body.Tiers)
	preferred := string(request.Body.Preferred)
	patch := config.MusicQualityProfilePatch{
		Tiers:          &tiers,
		Preferred:      &preferred,
		UpgradeAllowed: &request.Body.UpgradeAllowed,
	}
	switch err := config.UpdateMusicQualityProfile(ctx, request.Name, patch); {
	case errors.Is(err, config.ErrMusicQualityProfileNotFound):
		return UpdateMusicQualityProfile404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case configLocked(err):
		return UpdateMusicQualityProfile403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return UpdateMusicQualityProfile422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	stored, _ := config.LookupMusicQualityProfile(request.Name)
	return UpdateMusicQualityProfile200JSONResponse{
		MusicQualityProfileResponseJSONResponse: MusicQualityProfileResponseJSONResponse(
			musicProfileToAPI(stored),
		),
	}, nil
}

// SetDefaultMusicQualityProfile is also the way out of the 409 a delete
// answers for the current default.
func (s *Server) SetDefaultMusicQualityProfile(
	ctx context.Context,
	request SetDefaultMusicQualityProfileRequestObject,
) (SetDefaultMusicQualityProfileResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return SetDefaultMusicQualityProfile403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	switch err := config.SetDefaultMusicQualityProfile(ctx, request.Name); {
	case errors.Is(err, config.ErrMusicQualityProfileNotFound):
		return SetDefaultMusicQualityProfile404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case configLocked(err):
		return SetDefaultMusicQualityProfile403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return SetDefaultMusicQualityProfile500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return SetDefaultMusicQualityProfile204Response{}, nil
}

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
