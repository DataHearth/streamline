package restapi

import (
	"context"
	"errors"

	"github.com/datahearth/streamline/internal/config"
)

func musicQualityProfileToAPI(
	e config.MusicQualityProfileEntry,
) MusicQualityProfile {
	formats := make([]MusicQualityProfileFormats, len(e.Formats))
	for i, f := range e.Formats {
		formats[i] = MusicQualityProfileFormats(f)
	}
	var isDefault bool
	if c := config.Get(); c != nil {
		isDefault = c.MusicQualityDefaultProfile == e.Name
	}
	return MusicQualityProfile{
		Name:           e.Name,
		Formats:        formats,
		Cutoff:         MusicQualityProfileCutoff(e.Cutoff),
		UpgradeAllowed: e.UpgradeAllowed,
		IsDefault:      isDefault,
	}
}

func musicFormatsFromAPI(in []MusicQualityProfileCreateFormats) []string {
	out := make([]string, len(in))
	for i, f := range in {
		out[i] = string(f)
	}
	return out
}

func (s *Server) ListMusicQualityProfiles(
	_ context.Context,
	_ ListMusicQualityProfilesRequestObject,
) (ListMusicQualityProfilesResponseObject, error) {
	c := config.Get()
	items := make([]MusicQualityProfile, 0, len(c.MusicQualityProfiles))
	for _, p := range c.MusicQualityProfiles {
		items = append(items, musicQualityProfileToAPI(p))
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
	body := request.Body
	e := config.MusicQualityProfileEntry{
		Name:           body.Name,
		Formats:        musicFormatsFromAPI(body.Formats),
		Cutoff:         string(body.Cutoff),
		UpgradeAllowed: body.UpgradeAllowed,
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
	return CreateMusicQualityProfile201JSONResponse{
		MusicQualityProfileResponseJSONResponse: MusicQualityProfileResponseJSONResponse(
			musicQualityProfileToAPI(e),
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
	body := request.Body
	formats := musicFormatsFromAPI(body.Formats)
	cutoff := string(body.Cutoff)
	patch := config.MusicQualityProfilePatch{
		Formats:        &formats,
		Cutoff:         &cutoff,
		UpgradeAllowed: &body.UpgradeAllowed,
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
	e, _ := config.ResolveMusicQualityProfile(request.Name)
	return UpdateMusicQualityProfile200JSONResponse{
		MusicQualityProfileResponseJSONResponse: MusicQualityProfileResponseJSONResponse(
			musicQualityProfileToAPI(e),
		),
	}, nil
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
