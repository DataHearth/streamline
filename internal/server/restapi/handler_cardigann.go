package restapi

import (
	"context"
	"errors"

	"github.com/datahearth/streamline/internal/cardigann/definitions"
	"github.com/datahearth/streamline/internal/cardigann/update"
	"github.com/datahearth/streamline/internal/config"
)

// definitionUsers maps a definition file to the configured indexers that
// run it.
func definitionUsers() map[string][]string {
	out := map[string][]string{}
	for _, ix := range config.Get().Indexers {
		if ix.Protocol == "cardigann" && ix.Cardigann != nil {
			out[ix.Cardigann.Definition] = append(
				out[ix.Cardigann.Definition],
				ix.Name,
			)
		}
	}
	return out
}

func (s *Server) ListCardigannUpdates(
	ctx context.Context,
	_ ListCardigannUpdatesRequestObject,
) (ListCardigannUpdatesResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return ListCardigannUpdates403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	dir := config.Get().CardigannDir()
	snap, err := definitions.Snapshot(dir)
	if err != nil {
		return ListCardigannUpdates500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	pending, err := update.List(dir)
	if err != nil {
		return ListCardigannUpdates500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	users := definitionUsers()
	out := CardigannUpdates{
		Upstream: CardigannUpstream{
			Commit: snap.Upstream.Commit,
			Date:   snap.Upstream.Date,
		},
		Pending: make([]CardigannUpdate, 0, len(pending)),
	}
	for _, p := range pending {
		diff, err := update.Diff(dir, p.File)
		if err != nil {
			return ListCardigannUpdates500JSONResponse{
				InternalErrorJSONResponse: errInternal(ctx, err),
			}, nil
		}
		ixs := users[p.File]
		if ixs == nil {
			ixs = []string{}
		}
		out.Pending = append(out.Pending, CardigannUpdate{
			File: p.File, Name: p.Name, Reasons: p.Reasons, Removed: p.Removed,
			Indexers: ixs, Diff: diff,
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
	switch err := update.Approve(config.Get().CardigannDir(), request.File); {
	case errors.Is(err, update.ErrNoUpdate):
		return ApproveCardigannUpdate404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
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
	switch err := update.Reject(config.Get().CardigannDir(), request.File); {
	case errors.Is(err, update.ErrNoUpdate):
		return RejectCardigannUpdate404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case err != nil:
		return RejectCardigannUpdate500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return RejectCardigannUpdate204Response{}, nil
}
