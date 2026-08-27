package restapi

import (
	"context"
	"errors"

	"github.com/datahearth/streamline/internal/config"
)

// qualityProfileToAPI maps config.QualityProfileEntry into the generated
// view. AllowedCodecs and Formats are always non-nil slices so the fields
// serialize as [] rather than null when unset.
func qualityProfileToAPI(e config.QualityProfileEntry) QualityProfile {
	codecs := e.AllowedCodecs
	if codecs == nil {
		codecs = []string{}
	}
	formats := formatScoresToAPI(e.Formats)
	var isDefault bool
	if c := config.Get(); c != nil {
		isDefault = c.QualityDefaultProfile == e.Name
	}
	out := QualityProfile{
		Name:      e.Name,
		IsDefault: isDefault,
		PreferredResolution: QualityProfilePreferredResolution(
			e.PreferredResolution,
		),
		MinResolution:  QualityProfileMinResolution(e.MinResolution),
		UpgradeAllowed: e.UpgradeAllowed,
		AllowedCodecs:  &codecs,
		Formats:        &formats,
	}
	if e.MinScore != 0 {
		v := e.MinScore
		out.MinScore = &v
	}
	if e.UpgradeUntilScore != 0 {
		v := e.UpgradeUntilScore
		out.UpgradeUntilScore = &v
	}
	return out
}

// formatScoresToAPI maps a profile's scored-format list into the generated
// view.
func formatScoresToAPI(
	scores []config.QualityProfileFormatScore,
) []QualityProfileFormatScore {
	out := make([]QualityProfileFormatScore, len(scores))
	for i, fs := range scores {
		score := fs.Score
		out[i] = QualityProfileFormatScore{Name: fs.Name, Score: &score}
	}
	return out
}

// formatScoresFromAPI is the inverse of formatScoresToAPI, used by create/
// update requests.
func formatScoresFromAPI(
	scores []QualityProfileFormatScore,
) []config.QualityProfileFormatScore {
	out := make([]config.QualityProfileFormatScore, len(scores))
	for i, fs := range scores {
		var score int
		if fs.Score != nil {
			score = *fs.Score
		}
		out[i] = config.QualityProfileFormatScore{Name: fs.Name, Score: score}
	}
	return out
}

func (s *Server) ListQualityProfiles(
	ctx context.Context,
	_ ListQualityProfilesRequestObject,
) (ListQualityProfilesResponseObject, error) {
	c := config.Get()
	items := make([]QualityProfile, 0, len(c.QualityProfiles))
	for _, p := range c.QualityProfiles {
		items = append(items, qualityProfileToAPI(p))
	}
	return ListQualityProfiles200JSONResponse(items), nil
}

func (s *Server) CreateQualityProfile(
	ctx context.Context,
	request CreateQualityProfileRequestObject,
) (CreateQualityProfileResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return CreateQualityProfile403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	e := config.QualityProfileEntry{
		Name:                request.Body.Name,
		PreferredResolution: string(request.Body.PreferredResolution),
	}
	if request.Body.MinResolution != nil {
		e.MinResolution = string(*request.Body.MinResolution)
	} else {
		e.MinResolution = e.PreferredResolution
	}
	if request.Body.UpgradeAllowed != nil {
		e.UpgradeAllowed = *request.Body.UpgradeAllowed
	}
	if request.Body.AllowedCodecs != nil {
		e.AllowedCodecs = *request.Body.AllowedCodecs
	}
	if request.Body.Formats != nil {
		e.Formats = formatScoresFromAPI(*request.Body.Formats)
	}
	if request.Body.MinScore != nil {
		e.MinScore = *request.Body.MinScore
	}
	if request.Body.UpgradeUntilScore != nil {
		e.UpgradeUntilScore = *request.Body.UpgradeUntilScore
	}

	switch err := config.AddQualityProfile(ctx, e); {
	case errors.Is(err, config.ErrQualityProfileExists):
		return CreateQualityProfile409JSONResponse{
			ConflictJSONResponse: errConflict("quality profile name already exists"),
		}, nil
	case configLocked(err):
		return CreateQualityProfile403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return CreateQualityProfile422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	return CreateQualityProfile201JSONResponse(qualityProfileToAPI(e)), nil
}

func (s *Server) UpdateQualityProfile(
	ctx context.Context,
	request UpdateQualityProfileRequestObject,
) (UpdateQualityProfileResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return UpdateQualityProfile403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	pref := string(request.Body.PreferredResolution)
	patch := config.QualityProfilePatch{
		PreferredResolution: &pref,
		UpgradeAllowed:      request.Body.UpgradeAllowed,
	}
	if request.Body.MinResolution != nil {
		mr := string(*request.Body.MinResolution)
		patch.MinResolution = &mr
	}
	if request.Body.AllowedCodecs != nil {
		patch.AllowedCodecs = request.Body.AllowedCodecs
	}
	if request.Body.Formats != nil {
		fs := formatScoresFromAPI(*request.Body.Formats)
		patch.Formats = &fs
	}
	if request.Body.MinScore != nil {
		patch.MinScore = request.Body.MinScore
	}
	if request.Body.UpgradeUntilScore != nil {
		patch.UpgradeUntilScore = request.Body.UpgradeUntilScore
	}

	switch err := config.UpdateQualityProfile(ctx, request.Name, patch); {
	case errors.Is(err, config.ErrQualityProfileNotFound):
		return UpdateQualityProfile404JSONResponse{
			NotFoundJSONResponse: errNotFound("quality profile not found"),
		}, nil
	case configLocked(err):
		return UpdateQualityProfile403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return UpdateQualityProfile422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	e, _ := config.ResolveQualityProfile(request.Name)
	return UpdateQualityProfile200JSONResponse(qualityProfileToAPI(e)), nil
}

func (s *Server) DeleteQualityProfile(
	ctx context.Context,
	request DeleteQualityProfileRequestObject,
) (DeleteQualityProfileResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return DeleteQualityProfile403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	switch err := config.DeleteQualityProfile(ctx, request.Name); {
	case errors.Is(err, config.ErrQualityProfileNotFound):
		return DeleteQualityProfile404JSONResponse{
			NotFoundJSONResponse: errNotFound("quality profile not found"),
		}, nil
	case errors.Is(err, config.ErrQualityProfileInUseAsDefault):
		return DeleteQualityProfile409JSONResponse{
			ConflictJSONResponse: errConflict(err.Error()),
		}, nil
	case configLocked(err):
		return DeleteQualityProfile403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return DeleteQualityProfile500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return DeleteQualityProfile204Response{}, nil
}
