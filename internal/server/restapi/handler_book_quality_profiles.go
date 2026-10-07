package restapi

import (
	"context"
	"errors"

	"github.com/datahearth/streamline/internal/config"
)

func ebookQualityProfileToAPI(
	e config.EbookQualityProfileEntry,
) EbookQualityProfile {
	formats := make([]EbookQualityProfileFormats, len(e.Formats))
	for i, f := range e.Formats {
		formats[i] = EbookQualityProfileFormats(f)
	}
	var isDefault bool
	if c := config.Get(); c != nil {
		isDefault = c.EbookQualityDefaultProfile == e.Name
	}
	return EbookQualityProfile{
		Name:           e.Name,
		Formats:        formats,
		Cutoff:         EbookQualityProfileCutoff(e.Cutoff),
		UpgradeAllowed: e.UpgradeAllowed,
		IsDefault:      isDefault,
	}
}

func ebookFormatsFromAPI(in []EbookQualityProfileCreateFormats) []string {
	out := make([]string, len(in))
	for i, f := range in {
		out[i] = string(f)
	}
	return out
}

func (s *Server) ListEbookQualityProfiles(
	_ context.Context,
	_ ListEbookQualityProfilesRequestObject,
) (ListEbookQualityProfilesResponseObject, error) {
	c := config.Get()
	items := make([]EbookQualityProfile, 0, len(c.EbookQualityProfiles))
	for _, p := range c.EbookQualityProfiles {
		items = append(items, ebookQualityProfileToAPI(p))
	}
	return ListEbookQualityProfiles200JSONResponse(items), nil
}

func (s *Server) CreateEbookQualityProfile(
	ctx context.Context,
	request CreateEbookQualityProfileRequestObject,
) (CreateEbookQualityProfileResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return CreateEbookQualityProfile403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	body := request.Body
	e := config.EbookQualityProfileEntry{
		Name:           body.Name,
		Formats:        ebookFormatsFromAPI(body.Formats),
		Cutoff:         string(body.Cutoff),
		UpgradeAllowed: body.UpgradeAllowed,
	}
	switch err := config.AddEbookQualityProfile(ctx, e); {
	case errors.Is(err, config.ErrEbookQualityProfileExists):
		return CreateEbookQualityProfile409JSONResponse{
			ConflictJSONResponse: errConflict(err.Error()),
		}, nil
	case configLocked(err):
		return CreateEbookQualityProfile403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return CreateEbookQualityProfile422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	return CreateEbookQualityProfile201JSONResponse{
		EbookQualityProfileResponseJSONResponse: EbookQualityProfileResponseJSONResponse(
			ebookQualityProfileToAPI(e),
		),
	}, nil
}

func (s *Server) UpdateEbookQualityProfile(
	ctx context.Context,
	request UpdateEbookQualityProfileRequestObject,
) (UpdateEbookQualityProfileResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return UpdateEbookQualityProfile403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	body := request.Body
	formats := ebookFormatsFromAPI(body.Formats)
	cutoff := string(body.Cutoff)
	patch := config.EbookQualityProfilePatch{
		Formats:        &formats,
		Cutoff:         &cutoff,
		UpgradeAllowed: &body.UpgradeAllowed,
	}
	switch err := config.UpdateEbookQualityProfile(ctx, request.Name, patch); {
	case errors.Is(err, config.ErrEbookQualityProfileNotFound):
		return UpdateEbookQualityProfile404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case configLocked(err):
		return UpdateEbookQualityProfile403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return UpdateEbookQualityProfile422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	e, _ := config.ResolveEbookQualityProfile(request.Name)
	return UpdateEbookQualityProfile200JSONResponse{
		EbookQualityProfileResponseJSONResponse: EbookQualityProfileResponseJSONResponse(
			ebookQualityProfileToAPI(e),
		),
	}, nil
}

func (s *Server) DeleteEbookQualityProfile(
	ctx context.Context,
	request DeleteEbookQualityProfileRequestObject,
) (DeleteEbookQualityProfileResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return DeleteEbookQualityProfile403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	switch err := config.DeleteEbookQualityProfile(ctx, request.Name); {
	case errors.Is(err, config.ErrEbookQualityProfileNotFound):
		return DeleteEbookQualityProfile404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case errors.Is(err, config.ErrEbookQualityProfileInUseAsDefault):
		return DeleteEbookQualityProfile409JSONResponse{
			ConflictJSONResponse: errConflict(err.Error()),
		}, nil
	case configLocked(err):
		return DeleteEbookQualityProfile403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return DeleteEbookQualityProfile500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return DeleteEbookQualityProfile204Response{}, nil
}

func audiobookQualityProfileToAPI(
	e config.AudiobookQualityProfileEntry,
) AudiobookQualityProfile {
	formats := make([]AudiobookQualityProfileFormats, len(e.Formats))
	for i, f := range e.Formats {
		formats[i] = AudiobookQualityProfileFormats(f)
	}
	var isDefault bool
	if c := config.Get(); c != nil {
		isDefault = c.AudiobookQualityDefaultProfile == e.Name
	}
	return AudiobookQualityProfile{
		Name:           e.Name,
		Formats:        formats,
		Cutoff:         AudiobookQualityProfileCutoff(e.Cutoff),
		UpgradeAllowed: e.UpgradeAllowed,
		IsDefault:      isDefault,
	}
}

func audiobookFormatsFromAPI(in []AudiobookQualityProfileCreateFormats) []string {
	out := make([]string, len(in))
	for i, f := range in {
		out[i] = string(f)
	}
	return out
}

func (s *Server) ListAudiobookQualityProfiles(
	_ context.Context,
	_ ListAudiobookQualityProfilesRequestObject,
) (ListAudiobookQualityProfilesResponseObject, error) {
	c := config.Get()
	items := make([]AudiobookQualityProfile, 0, len(c.AudiobookQualityProfiles))
	for _, p := range c.AudiobookQualityProfiles {
		items = append(items, audiobookQualityProfileToAPI(p))
	}
	return ListAudiobookQualityProfiles200JSONResponse(items), nil
}

func (s *Server) CreateAudiobookQualityProfile(
	ctx context.Context,
	request CreateAudiobookQualityProfileRequestObject,
) (CreateAudiobookQualityProfileResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return CreateAudiobookQualityProfile403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	body := request.Body
	e := config.AudiobookQualityProfileEntry{
		Name:           body.Name,
		Formats:        audiobookFormatsFromAPI(body.Formats),
		Cutoff:         string(body.Cutoff),
		UpgradeAllowed: body.UpgradeAllowed,
	}
	switch err := config.AddAudiobookQualityProfile(ctx, e); {
	case errors.Is(err, config.ErrAudiobookQualityProfileExists):
		return CreateAudiobookQualityProfile409JSONResponse{
			ConflictJSONResponse: errConflict(err.Error()),
		}, nil
	case configLocked(err):
		return CreateAudiobookQualityProfile403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return CreateAudiobookQualityProfile422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	return CreateAudiobookQualityProfile201JSONResponse{
		AudiobookQualityProfileResponseJSONResponse: AudiobookQualityProfileResponseJSONResponse(
			audiobookQualityProfileToAPI(e),
		),
	}, nil
}

func (s *Server) UpdateAudiobookQualityProfile(
	ctx context.Context,
	request UpdateAudiobookQualityProfileRequestObject,
) (UpdateAudiobookQualityProfileResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return UpdateAudiobookQualityProfile403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	body := request.Body
	formats := audiobookFormatsFromAPI(body.Formats)
	cutoff := string(body.Cutoff)
	patch := config.AudiobookQualityProfilePatch{
		Formats:        &formats,
		Cutoff:         &cutoff,
		UpgradeAllowed: &body.UpgradeAllowed,
	}
	switch err := config.UpdateAudiobookQualityProfile(ctx, request.Name, patch); {
	case errors.Is(err, config.ErrAudiobookQualityProfileNotFound):
		return UpdateAudiobookQualityProfile404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case configLocked(err):
		return UpdateAudiobookQualityProfile403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return UpdateAudiobookQualityProfile422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	e, _ := config.ResolveAudiobookQualityProfile(request.Name)
	return UpdateAudiobookQualityProfile200JSONResponse{
		AudiobookQualityProfileResponseJSONResponse: AudiobookQualityProfileResponseJSONResponse(
			audiobookQualityProfileToAPI(e),
		),
	}, nil
}

func (s *Server) DeleteAudiobookQualityProfile(
	ctx context.Context,
	request DeleteAudiobookQualityProfileRequestObject,
) (DeleteAudiobookQualityProfileResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return DeleteAudiobookQualityProfile403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	switch err := config.DeleteAudiobookQualityProfile(ctx, request.Name); {
	case errors.Is(err, config.ErrAudiobookQualityProfileNotFound):
		return DeleteAudiobookQualityProfile404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case errors.Is(err, config.ErrAudiobookQualityProfileInUseAsDefault):
		return DeleteAudiobookQualityProfile409JSONResponse{
			ConflictJSONResponse: errConflict(err.Error()),
		}, nil
	case configLocked(err):
		return DeleteAudiobookQualityProfile403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return DeleteAudiobookQualityProfile500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return DeleteAudiobookQualityProfile204Response{}, nil
}
