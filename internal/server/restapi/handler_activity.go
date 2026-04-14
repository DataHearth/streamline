package restapi

import (
	"context"
	"strings"

	"github.com/datahearth/streamline/ent"
)

func (s *Server) GetDownloadQueue(
	ctx context.Context,
	_ GetDownloadQueueRequestObject,
) (GetDownloadQueueResponseObject, error) {
	snap, err := s.downloads.Queue(ctx)
	if err != nil {
		return nil, err
	}
	out := DownloadQueue{
		Items:       make([]QueueEntry, 0, len(snap.Items)),
		RefreshedAt: snap.RefreshedAt,
	}
	for _, it := range snap.Items {
		out.Items = append(out.Items, toQueueEntry(it))
	}
	return GetDownloadQueue200JSONResponse{
		DownloadQueueJSONResponse: DownloadQueueJSONResponse(out),
	}, nil
}

func (s *Server) CancelQueueItem(
	ctx context.Context,
	request CancelQueueItemRequestObject,
) (CancelQueueItemResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return CancelQueueItem403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	if err := s.downloads.CancelQueueItem(ctx, request.Id); err != nil {
		if ent.IsNotFound(err) {
			return CancelQueueItem404JSONResponse{
				NotFoundJSONResponse: errNotFound(err.Error()),
			}, nil
		}
		return nil, err
	}
	return CancelQueueItem204Response{}, nil
}

func (s *Server) PauseQueueItem(
	ctx context.Context,
	request PauseQueueItemRequestObject,
) (PauseQueueItemResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return PauseQueueItem403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	if err := s.downloads.PauseQueueItem(ctx, request.Id); err != nil {
		if ent.IsNotFound(err) {
			return PauseQueueItem404JSONResponse{
				NotFoundJSONResponse: errNotFound(err.Error()),
			}, nil
		}
		return nil, err
	}
	return PauseQueueItem204Response{}, nil
}

func (s *Server) ResumeQueueItem(
	ctx context.Context,
	request ResumeQueueItemRequestObject,
) (ResumeQueueItemResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return ResumeQueueItem403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	if err := s.downloads.ResumeQueueItem(ctx, request.Id); err != nil {
		if ent.IsNotFound(err) {
			return ResumeQueueItem404JSONResponse{
				NotFoundJSONResponse: errNotFound(err.Error()),
			}, nil
		}
		return nil, err
	}
	return ResumeQueueItem204Response{}, nil
}

func (s *Server) ListDownloadHistory(
	ctx context.Context,
	request ListDownloadHistoryRequestObject,
) (ListDownloadHistoryResponseObject, error) {
	limit := 0
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	cursor := ""
	if request.Params.Cursor != nil {
		cursor = *request.Params.Cursor
	}
	res, err := s.store.ListDownloadHistory(ctx, limit, cursor)
	if err != nil {
		if strings.Contains(err.Error(), "decode cursor") {
			return ListDownloadHistory400JSONResponse{
				BadRequestJSONResponse: errBadRequest("invalid cursor"),
			}, nil
		}
		return nil, err
	}
	out := DownloadHistory{
		Items: make([]HistoryEntry, 0, len(res.Records)),
	}
	for _, r := range res.Records {
		out.Items = append(out.Items, toHistoryEntry(r))
	}
	if res.NextCursor != "" {
		c := res.NextCursor
		out.NextCursor = &c
	}
	return ListDownloadHistory200JSONResponse{
		DownloadHistoryJSONResponse: DownloadHistoryJSONResponse(out),
	}, nil
}

func (s *Server) DeleteHistoryItem(
	ctx context.Context,
	request DeleteHistoryItemRequestObject,
) (DeleteHistoryItemResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return DeleteHistoryItem403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	if err := s.store.DeleteDownloadRecord(ctx, request.Id); err != nil {
		if ent.IsNotFound(err) {
			return DeleteHistoryItem404JSONResponse{
				NotFoundJSONResponse: errNotFound(err.Error()),
			}, nil
		}
		return nil, err
	}
	return DeleteHistoryItem204Response{}, nil
}

func (s *Server) ClearCompletedHistory(
	ctx context.Context,
	_ ClearCompletedHistoryRequestObject,
) (ClearCompletedHistoryResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return ClearCompletedHistory403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	n, err := s.store.DeleteAllCompletedDownloadRecords(ctx)
	if err != nil {
		return nil, err
	}
	return ClearCompletedHistory200JSONResponse{
		ClearCompletedResultJSONResponse: ClearCompletedResultJSONResponse(
			ClearCompletedResult{Deleted: n},
		),
	}, nil
}
