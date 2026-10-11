package restapi

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/indexer"
)

func (s *Server) ListIndexers(
	ctx context.Context,
	_ ListIndexersRequestObject,
) (ListIndexersResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return ListIndexers403JSONResponse{ForbiddenJSONResponse: notAdminResp}, nil
	}
	c := config.Get()
	items := make([]Indexer, 0, len(c.Indexers))
	for _, e := range c.Indexers {
		items = append(items, s.indexerWithSession(ctx, e))
	}
	return ListIndexers200JSONResponse(items), nil
}

func (s *Server) CreateIndexer(
	ctx context.Context,
	request CreateIndexerRequestObject,
) (CreateIndexerResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return CreateIndexer403JSONResponse{ForbiddenJSONResponse: notAdminResp}, nil
	}
	e := config.IndexerEntry{
		Name:     request.Body.Name,
		Host:     request.Body.Host,
		Port:     request.Body.Port,
		APIKey:   request.Body.ApiKey,
		Protocol: "torznab",
	}
	if request.Body.Path != nil {
		e.Path = *request.Body.Path
	}
	if request.Body.UseSsl != nil {
		e.UseSSL = *request.Body.UseSsl
	}
	if request.Body.Priority != nil {
		e.Priority = *request.Body.Priority
	}
	if request.Body.Enabled != nil {
		e.Enabled = *request.Body.Enabled
	}
	if request.Body.Private != nil {
		e.Private = *request.Body.Private
	}
	if request.Body.Protocol != nil {
		e.Protocol = string(*request.Body.Protocol)
	}

	switch err := config.AddIndexer(ctx, e); {
	case errors.Is(err, config.ErrIndexerExists):
		return CreateIndexer409JSONResponse{
			ConflictJSONResponse: errConflict("indexer name already exists"),
		}, nil
	case configLocked(err):
		return CreateIndexer403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return CreateIndexer422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	return CreateIndexer201JSONResponse(indexerToAPI(e)), nil
}

func (s *Server) UpdateIndexer(
	ctx context.Context,
	request UpdateIndexerRequestObject,
) (UpdateIndexerResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return UpdateIndexer403JSONResponse{ForbiddenJSONResponse: notAdminResp}, nil
	}
	if e, ok := config.FindIndexer(request.Name); ok && e.Protocol == "cardigann" {
		return UpdateIndexer422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(
				"a cardigann indexer is configured in the config file",
			),
		}, nil
	}
	patch := config.IndexerPatch{
		Host:     &request.Body.Host,
		Port:     &request.Body.Port,
		Path:     request.Body.Path,
		UseSSL:   request.Body.UseSsl,
		APIKey:   &request.Body.ApiKey,
		Priority: request.Body.Priority,
		Enabled:  request.Body.Enabled,
		Private:  request.Body.Private,
	}
	if request.Body.Protocol != nil {
		p := string(*request.Body.Protocol)
		patch.Protocol = &p
	}

	switch err := config.UpdateIndexer(ctx, request.Name, patch); {
	case errors.Is(err, config.ErrIndexerNotFound):
		return UpdateIndexer404JSONResponse{
			NotFoundJSONResponse: errNotFound("indexer not found"),
		}, nil
	case configLocked(err):
		return UpdateIndexer403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return UpdateIndexer422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	e, _ := config.FindIndexer(request.Name)
	return UpdateIndexer200JSONResponse(indexerToAPI(e)), nil
}

func (s *Server) DeleteIndexer(
	ctx context.Context,
	request DeleteIndexerRequestObject,
) (DeleteIndexerResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return DeleteIndexer403JSONResponse{ForbiddenJSONResponse: notAdminResp}, nil
	}
	e, found := config.FindIndexer(request.Name)
	switch err := config.DeleteIndexer(ctx, request.Name); {
	case errors.Is(err, config.ErrIndexerNotFound):
		return DeleteIndexer404JSONResponse{
			NotFoundJSONResponse: errNotFound("indexer not found"),
		}, nil
	case configLocked(err):
		return DeleteIndexer403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return DeleteIndexer500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	if found && e.Protocol == "cardigann" {
		if err := s.indexers.Forget(ctx, e.Name); err != nil {
			slog.WarnContext(ctx, "could not drop a deleted indexer's session",
				"indexer.name", e.Name, "error", err)
		}
	}
	return DeleteIndexer204Response{}, nil
}

func (s *Server) TestIndexer(
	ctx context.Context,
	request TestIndexerRequestObject,
) (TestIndexerResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return TestIndexer403JSONResponse{ForbiddenJSONResponse: notAdminResp}, nil
	}
	err := s.indexers.TestByName(ctx, request.Name)
	if msg, ok := indexerProbeFailure(err); ok {
		return TestIndexer422JSONResponse{
			UnprocessableEntityJSONResponse: errConnectionFailed(msg),
		}, nil
	}
	switch {
	case err == nil:
		return TestIndexer200Response{}, nil
	case errors.Is(err, config.ErrIndexerNotFound):
		return TestIndexer404JSONResponse{
			NotFoundJSONResponse: errNotFound("indexer not found"),
		}, nil
	default:
		return TestIndexer500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
}

func (s *Server) TestDraftIndexer(
	ctx context.Context,
	request TestDraftIndexerRequestObject,
) (TestDraftIndexerResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return TestDraftIndexer403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	b := request.Body
	if draftTargetRefused(ctx, b.Host) {
		return TestDraftIndexer422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(
				draftTargetRefusedMessage,
			),
		}, nil
	}
	p := indexer.TestParams{
		Protocol: "torznab",
		Host:     b.Host,
		Port:     b.Port,
		APIKey:   b.ApiKey,
	}
	if b.Protocol != nil {
		p.Protocol = string(*b.Protocol)
	}
	if b.Path != nil {
		p.Path = *b.Path
	}
	if b.UseSsl != nil {
		p.UseSSL = *b.UseSsl
	}

	err := s.indexers.Test(ctx, p)
	if msg, ok := indexerProbeFailure(err); ok {
		return TestDraftIndexer422JSONResponse{
			UnprocessableEntityJSONResponse: errConnectionFailed(msg),
		}, nil
	}
	if err != nil {
		return TestDraftIndexer500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return TestDraftIndexer200Response{}, nil
}

// indexerProbeFailure reports the typed connection-test failure by its own
// sentinel text. The probe error itself is never echoed to the caller: torznab
// authenticates through an `apikey` query parameter, so a wrapped *url.Error
// would hand the indexer's key to anyone who can reach the endpoint. A
// built-in indexer's login failures are the exception: their text is the
// tracker's own message, which carries no URL.
func indexerProbeFailure(err error) (string, bool) {
	for _, sentinel := range []error{
		indexer.ErrLoginFailed,
		indexer.ErrCaptchaRequired,
		indexer.ErrLoginBackoff,
		indexer.ErrCaptchaExpired,
		indexer.ErrChallenge,
	} {
		if errors.Is(err, sentinel) {
			return err.Error(), true
		}
	}
	for _, sentinel := range []error{
		indexer.ErrUnreachable,
		indexer.ErrUnauthorized,
		indexer.ErrUnexpectedStatus,
		indexer.ErrBadResponse,
	} {
		if errors.Is(err, sentinel) {
			return sentinel.Error(), true
		}
	}
	return "", false
}

// indexerWithSession is indexerToAPI plus a built-in indexer's login state.
func (s *Server) indexerWithSession(
	ctx context.Context,
	e config.IndexerEntry,
) Indexer {
	i := indexerToAPI(e)
	if e.Protocol != "cardigann" {
		return i
	}
	st, err := s.indexers.Session(ctx, e.Name)
	if err != nil {
		slog.WarnContext(ctx, "could not read an indexer's session",
			"indexer.name", e.Name, "error", err)
		return i
	}
	i.Session = &IndexerSession{
		LoginRequired: st.LoginRequired,
		Captcha:       st.Captcha,
		LoggedIn:      st.LoggedIn,
		ExpiresAt:     st.ExpiresAt,
		RetryAt:       st.RetryAt,
	}
	if st.LastError != "" {
		i.Session.LastError = &st.LastError
	}
	return i
}

func (s *Server) GetIndexerCaptcha(
	ctx context.Context,
	request GetIndexerCaptchaRequestObject,
) (GetIndexerCaptchaResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return GetIndexerCaptcha403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	token, img, err := s.indexers.Captcha(ctx, request.Name)
	if msg, ok := indexerProbeFailure(err); ok {
		return GetIndexerCaptcha422JSONResponse{
			UnprocessableEntityJSONResponse: errConnectionFailed(msg),
		}, nil
	}
	switch {
	case errors.Is(err, config.ErrIndexerNotFound):
		return GetIndexerCaptcha404JSONResponse{
			NotFoundJSONResponse: errNotFound("indexer not found"),
		}, nil
	case errors.Is(err, indexer.ErrNotBuiltin):
		return GetIndexerCaptcha422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	case err != nil:
		return GetIndexerCaptcha500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	case img == nil:
		return GetIndexerCaptcha204Response{}, nil
	}
	// The page is the tracker's: the type it claims is not trusted into a
	// data: URL the SPA renders.
	ct := http.DetectContentType(img.Data)
	if !strings.HasPrefix(ct, "image/") {
		return GetIndexerCaptcha422JSONResponse{
			UnprocessableEntityJSONResponse: errConnectionFailed(
				"the tracker's captcha is not an image",
			),
		}, nil
	}
	return GetIndexerCaptcha200JSONResponse{
		Token: token,
		Image: "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(
			img.Data,
		),
	}, nil
}

func (s *Server) LoginIndexer(
	ctx context.Context,
	request LoginIndexerRequestObject,
) (LoginIndexerResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return LoginIndexer403JSONResponse{ForbiddenJSONResponse: notAdminResp}, nil
	}
	var token, answer string
	if b := request.Body; b != nil {
		if b.Token != nil {
			token = *b.Token
		}
		if b.Answer != nil {
			answer = *b.Answer
		}
	}
	err := s.indexers.Login(ctx, request.Name, token, answer)
	if msg, ok := indexerProbeFailure(err); ok {
		return LoginIndexer422JSONResponse{
			UnprocessableEntityJSONResponse: errConnectionFailed(msg),
		}, nil
	}
	switch {
	case errors.Is(err, config.ErrIndexerNotFound):
		return LoginIndexer404JSONResponse{
			NotFoundJSONResponse: errNotFound("indexer not found"),
		}, nil
	case errors.Is(err, indexer.ErrNotBuiltin):
		return LoginIndexer422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	case err != nil:
		return LoginIndexer500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	e, _ := config.FindIndexer(request.Name)
	return LoginIndexer200JSONResponse(s.indexerWithSession(ctx, e)), nil
}
