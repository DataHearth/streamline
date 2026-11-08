package restapi

import (
	"context"
	"errors"

	"github.com/datahearth/streamline/internal/cardigann/definitions"
	"github.com/datahearth/streamline/internal/cardigann/update"
)

func (s *Server) ListCardigannUpdates(
	ctx context.Context,
	_ ListCardigannUpdatesRequestObject,
) (ListCardigannUpdatesResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return ListCardigannUpdates403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	pending := update.List()
	out := CardigannUpdates{Pending: make([]CardigannUpdate, 0, len(pending))}
	if set := definitions.Current(); set != nil {
		up := set.Upstream()
		out.Upstream = CardigannUpstream{Commit: up.Commit, Date: up.Date}
	}
	for _, p := range pending {
		reasons := p.Reasons
		if reasons == nil {
			reasons = []string{}
		}
		out.Pending = append(out.Pending, CardigannUpdate{
			File: p.File, Name: p.Name, Reasons: reasons, Indexers: p.Indexers,
			Diff: p.Diff, Commit: p.Commit,
		})
	}
	return ListCardigannUpdates200JSONResponse(out), nil
}

func (s *Server) ApproveCardigannUpdate(
	ctx context.Context,
	request ApproveCardigannUpdateRequestObject,
) (ApproveCardigannUpdateResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return ApproveCardigannUpdate403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	switch err := update.Approve(ctx, request.File); {
	case errors.Is(err, update.ErrNoUpdate):
		return ApproveCardigannUpdate404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case configLocked(err):
		return ApproveCardigannUpdate403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return ApproveCardigannUpdate500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return ApproveCardigannUpdate204Response{}, nil
}

func (s *Server) RejectCardigannUpdate(
	ctx context.Context,
	request RejectCardigannUpdateRequestObject,
) (RejectCardigannUpdateResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return RejectCardigannUpdate403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	if err := update.Reject(request.File); errors.Is(err, update.ErrNoUpdate) {
		return RejectCardigannUpdate404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	}
	return RejectCardigannUpdate204Response{}, nil
}
