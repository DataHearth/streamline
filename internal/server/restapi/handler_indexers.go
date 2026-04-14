package restapi

import (
	"context"
	"errors"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/indexer"
)

func (s *Server) ListIndexers(
	ctx context.Context,
	_ ListIndexersRequestObject,
) (ListIndexersResponseObject, error) {
	c := config.Get()
	items := make([]Indexer, 0, len(c.Indexers))
	for _, e := range c.Indexers {
		items = append(items, indexerToAPI(e))
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
	patch := config.IndexerPatch{
		Host:     &request.Body.Host,
		Port:     &request.Body.Port,
		Path:     request.Body.Path,
		UseSSL:   request.Body.UseSsl,
		APIKey:   &request.Body.ApiKey,
		Priority: request.Body.Priority,
		Enabled:  request.Body.Enabled,
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
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
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
	switch {
	case err == nil:
		return TestIndexer200Response{}, nil
	case errors.Is(err, config.ErrIndexerNotFound):
		return TestIndexer404JSONResponse{
			NotFoundJSONResponse: errNotFound("indexer not found"),
		}, nil
	case errors.Is(err, indexer.ErrUnreachable),
		errors.Is(err, indexer.ErrUnauthorized),
		errors.Is(err, indexer.ErrUnexpectedStatus),
		errors.Is(err, indexer.ErrBadResponse):
		return TestIndexer422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	default:
		return TestIndexer500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
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

	switch err := s.indexers.Test(ctx, p); {
	case err == nil:
		return TestDraftIndexer200Response{}, nil
	case errors.Is(err, indexer.ErrUnreachable),
		errors.Is(err, indexer.ErrUnauthorized),
		errors.Is(err, indexer.ErrUnexpectedStatus),
		errors.Is(err, indexer.ErrBadResponse):
		return TestDraftIndexer422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	default:
		return TestDraftIndexer500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
}
