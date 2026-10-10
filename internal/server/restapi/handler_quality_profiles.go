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
	defaultFor := []QualityDefaultMedia{}
	if c := config.Get(); c != nil {
		for _, m := range c.DefaultFor(e.Name) {
			defaultFor = append(defaultFor, QualityDefaultMedia(m))
		}
	}
	out := QualityProfile{
		Name:       e.Name,
		IsDefault:  len(defaultFor) > 0,
		DefaultFor: defaultFor,
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
	out.Transcode = transcodePolicyToAPI(e.Transcode)
	return out
}

// transcodePolicyToAPI maps a profile's transcode block into the generated
// view. Nil in, nil out: a profile the worker never touches carries no field.
func transcodePolicyToAPI(p *config.TranscodePolicy) *TranscodePolicy {
	if p == nil {
		return nil
	}
	out := &TranscodePolicy{
		To: TranscodeTo{
			Container:  TranscodeToContainer(p.To.Container),
			VideoCodec: TranscodeToVideoCodec(p.To.VideoCodec),
			Preset:     TranscodeToPreset(p.To.Preset),
			AudioCodec: TranscodeToAudioCodec(p.To.AudioCodec),
		},
	}
	if p.To.CRF != 0 {
		crf := p.To.CRF
		out.To.Crf = &crf
	}
	if len(p.To.AudioPassthrough) > 0 {
		ap := p.To.AudioPassthrough
		out.To.AudioPassthrough = &ap
	}
	if len(p.If.VideoCodecs) > 0 ||
		len(p.If.Containers) > 0 ||
		p.If.MaxVideoBitrate != "" ||
		p.If.MinVideoBitrate != "" {
		cond := &TranscodeIf{}
		if len(p.If.VideoCodecs) > 0 {
			codecs := make([]TranscodeIfVideoCodecs, len(p.If.VideoCodecs))
			for i, c := range p.If.VideoCodecs {
				codecs[i] = TranscodeIfVideoCodecs(c)
			}
			cond.VideoCodecs = &codecs
		}
		if len(p.If.Containers) > 0 {
			containers := make([]TranscodeIfContainers, len(p.If.Containers))
			for i, c := range p.If.Containers {
				containers[i] = TranscodeIfContainers(c)
			}
			cond.Containers = &containers
		}
		if p.If.MaxVideoBitrate != "" {
			b := p.If.MaxVideoBitrate
			cond.MaxVideoBitrate = &b
		}
		if p.If.MinVideoBitrate != "" {
			b := p.If.MinVideoBitrate
			cond.MinVideoBitrate = &b
		}
		out.If = cond
	}
	return out
}

// transcodePolicyFromAPI is the inverse, used by create/update requests.
func transcodePolicyFromAPI(p *TranscodePolicy) *config.TranscodePolicy {
	if p == nil {
		return nil
	}
	out := &config.TranscodePolicy{
		To: config.TranscodeTo{
			Container:  string(p.To.Container),
			VideoCodec: string(p.To.VideoCodec),
			Preset:     string(p.To.Preset),
			AudioCodec: string(p.To.AudioCodec),
		},
	}
	if p.To.Crf != nil {
		out.To.CRF = *p.To.Crf
	}
	if p.To.AudioPassthrough != nil {
		out.To.AudioPassthrough = *p.To.AudioPassthrough
	}
	if p.If == nil {
		return out
	}
	if p.If.VideoCodecs != nil {
		codecs := make([]string, len(*p.If.VideoCodecs))
		for i, c := range *p.If.VideoCodecs {
			codecs[i] = string(c)
		}
		out.If.VideoCodecs = codecs
	}
	if p.If.Containers != nil {
		containers := make([]string, len(*p.If.Containers))
		for i, c := range *p.If.Containers {
			containers[i] = string(c)
		}
		out.If.Containers = containers
	}
	if p.If.MaxVideoBitrate != nil {
		out.If.MaxVideoBitrate = *p.If.MaxVideoBitrate
	}
	if p.If.MinVideoBitrate != nil {
		out.If.MinVideoBitrate = *p.If.MinVideoBitrate
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
func qualityProfileFromCreate(b QualityProfileCreate) config.QualityProfileEntry {
	e := config.QualityProfileEntry{
		Name:                b.Name,
		PreferredResolution: string(b.PreferredResolution),
		MinResolution:       string(b.PreferredResolution),
	}
	if b.MinResolution != nil {
		e.MinResolution = string(*b.MinResolution)
	}
	if b.UpgradeAllowed != nil {
		e.UpgradeAllowed = *b.UpgradeAllowed
	}
	if b.AllowedCodecs != nil {
		e.AllowedCodecs = *b.AllowedCodecs
	}
	if b.Formats != nil {
		e.Formats = formatScoresFromAPI(*b.Formats)
	}
	if b.MinScore != nil {
		e.MinScore = *b.MinScore
	}
	if b.UpgradeUntilScore != nil {
		e.UpgradeUntilScore = *b.UpgradeUntilScore
	}
	e.Transcode = transcodePolicyFromAPI(b.Transcode)
	return e
}

func qualityProfileCreateFromEntry(
	e config.QualityProfileEntry,
) QualityProfileCreate {
	minRes := QualityProfileCreateMinResolution(e.MinResolution)
	formats := formatScoresToAPI(e.Formats)
	return QualityProfileCreate{
		Name: e.Name,
		PreferredResolution: QualityProfileCreatePreferredResolution(
			e.PreferredResolution,
		),
		MinResolution:     &minRes,
		UpgradeAllowed:    &e.UpgradeAllowed,
		AllowedCodecs:     &e.AllowedCodecs,
		Formats:           &formats,
		MinScore:          &e.MinScore,
		UpgradeUntilScore: &e.UpgradeUntilScore,
		Transcode:         transcodePolicyToAPI(e.Transcode),
	}
}

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
	e := qualityProfileFromCreate(*request.Body)

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
	// Nil leaves the stored policy alone, so there is deliberately no way to
	// remove one over the API — clearing it is a config-file edit.
	patch.Transcode = transcodePolicyFromAPI(request.Body.Transcode)

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
	e, _ := config.LookupQualityProfile(request.Name)
	return UpdateQualityProfile200JSONResponse(qualityProfileToAPI(e)), nil
}

// SetDefaultQualityProfile points the movie and/or series default at the named
// profile; an absent media sets both, as the single v1 default did. Also the
// way out of DeleteQualityProfile's 409: a current default cannot be deleted
// while it holds the role.
func (s *Server) SetDefaultQualityProfile(
	ctx context.Context,
	request SetDefaultQualityProfileRequestObject,
) (SetDefaultQualityProfileResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return SetDefaultQualityProfile403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	media := []config.Media{config.MediaMovie, config.MediaSeries}
	if request.Params.Media != nil {
		media = []config.Media{config.Media(*request.Params.Media)}
	}
	switch err := config.SetDefaultQualityProfile(ctx, request.Name, media...); {
	case errors.Is(err, config.ErrQualityProfileNotFound):
		return SetDefaultQualityProfile404JSONResponse{
			NotFoundJSONResponse: errNotFound("quality profile not found"),
		}, nil
	case configLocked(err):
		return SetDefaultQualityProfile403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return SetDefaultQualityProfile500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return SetDefaultQualityProfile204Response{}, nil
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
