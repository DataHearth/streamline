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

// indexerFromBody reads a create or draft-test body into a config entry.
func indexerFromBody(b IndexerCreate) config.IndexerEntry {
	e := config.IndexerEntry{Name: b.Name, Protocol: "torznab"}
	if b.Protocol != nil {
		e.Protocol = string(*b.Protocol)
	}
	if e.Protocol == "cardigann" {
		c := &config.CardigannIndexer{Settings: map[string]string{}}
		if b.Definition != nil {
			c.Definition = *b.Definition
		}
		if b.Site != nil {
			c.Site = *b.Site
		}
		if b.Settings != nil {
			c.Settings = *b.Settings
		}
		if b.Proxy != nil {
			c.Proxy = *b.Proxy
		}
		e.Cardigann = c
	} else {
		if b.Host != nil {
			e.Host = *b.Host
		}
		if b.Port != nil {
			e.Port = *b.Port
		}
		if b.ApiKey != nil {
			e.APIKey = *b.ApiKey
		}
		if b.Path != nil {
			e.Path = *b.Path
		}
		if b.UseSsl != nil {
			e.UseSSL = *b.UseSsl
		}
	}
	if b.Priority != nil {
		e.Priority = *b.Priority
	}
	if b.Enabled != nil {
		e.Enabled = *b.Enabled
	}
	if b.Private != nil {
		e.Private = *b.Private
	}
	return e
}

func (s *Server) CreateIndexer(
	ctx context.Context,
	request CreateIndexerRequestObject,
) (CreateIndexerResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return CreateIndexer403JSONResponse{ForbiddenJSONResponse: notAdminResp}, nil
	}
	e := indexerFromBody(*request.Body)
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
	return CreateIndexer201JSONResponse(s.indexerWithSession(ctx, e)), nil
}

func (s *Server) UpdateIndexer(
	ctx context.Context,
	request UpdateIndexerRequestObject,
) (UpdateIndexerResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return UpdateIndexer403JSONResponse{ForbiddenJSONResponse: notAdminResp}, nil
	}
	b := request.Body
	patch := config.IndexerPatch{
		Host:     b.Host,
		Port:     b.Port,
		Path:     b.Path,
		UseSSL:   b.UseSsl,
		APIKey:   b.ApiKey,
		Priority: b.Priority,
		Enabled:  b.Enabled,
		Private:  b.Private,
	}
	if b.Protocol != nil {
		p := string(*b.Protocol)
		patch.Protocol = &p
	}
	if b.Definition != nil || b.Site != nil || b.Settings != nil || b.Proxy != nil {
		cp := &config.CardigannPatch{
			Definition: b.Definition, Site: b.Site, Proxy: b.Proxy,
		}
		if b.Settings != nil {
			cp.Settings = *b.Settings
		}
		patch.Cardigann = cp
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
	if e.Protocol == "cardigann" {
		// The credentials may have changed: the next search logs in with
		// what was just saved, not on the old session.
		if err := s.indexers.Forget(ctx, e.Name); err != nil {
			slog.WarnContext(ctx, "could not drop an edited indexer's session",
				"indexer.name", e.Name, "error", err)
		}
	}
	return UpdateIndexer200JSONResponse(s.indexerWithSession(ctx, e)), nil
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
	e := indexerFromBody(*request.Body)
	p := indexer.TestParams{
		Protocol:  e.Protocol,
		Host:      e.Host,
		Port:      e.Port,
		Path:      e.Path,
		UseSSL:    e.UseSSL,
		APIKey:    e.APIKey,
		Cardigann: e.Cardigann,
	}
	if e.Protocol == "cardigann" {
		// The target is one of the definition's own links, never one the
		// caller names; what can be wrong is the definition or a setting.
		if err := config.CheckCardigann(config.Get().CardigannDir(), e); err != nil {
			return TestDraftIndexer422JSONResponse{
				UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
			}, nil
		}
	} else if draftTargetRefused(ctx, e.Host) {
		return TestDraftIndexer422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(
				draftTargetRefusedMessage,
			),
		}, nil
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
