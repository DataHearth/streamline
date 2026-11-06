package restapi

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/importer"
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
		if errors.Is(err, download.ErrRecordHeld) {
			return CancelQueueItem409JSONResponse{
				ConflictJSONResponse: errConflict(
					"download is held; resolve it via POST /downloads/{id}/resolve",
				),
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
		if errors.Is(err, download.ErrRecordHeld) {
			return PauseQueueItem409JSONResponse{
				ConflictJSONResponse: errConflict(
					"download is held; resolve it via POST /downloads/{id}/resolve",
				),
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
		if errors.Is(err, download.ErrRecordHeld) {
			return ResumeQueueItem409JSONResponse{
				ConflictJSONResponse: errConflict(
					"download is held; resolve it via POST /downloads/{id}/resolve",
				),
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
	limit, ok := limitOr(request.Params.Limit, 0, activityMaxLimit)
	if !ok {
		return ListDownloadHistory400JSONResponse{
			BadRequestJSONResponse: errBadRequest(limitRangeMsg(activityMaxLimit)),
		}, nil
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
		Total: res.Total,
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

// RetryFailedImport hands a terminally-failed record back to the importer.
// Nothing else can: resolve only accepts a held record and the importer's scan
// only picks up records already importing, so before this a record that
// exhausted library.import_max_attempts was unreachable — including after the
// operator fixed whatever caused it.
func (s *Server) RetryFailedImport(
	ctx context.Context,
	request RetryFailedImportRequestObject,
) (RetryFailedImportResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return RetryFailedImport403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	rec, err := s.store.FindDownloadRecordByID(ctx, request.Id)
	if err != nil {
		if ent.IsNotFound(err) {
			return RetryFailedImport404JSONResponse{
				NotFoundJSONResponse: errNotFound("download record not found"),
			}, nil
		}
		return nil, err
	}
	if rec.Status != downloadrecord.StatusFailed {
		return RetryFailedImport409JSONResponse{
			ConflictJSONResponse: errConflict("download record has not failed"),
		}, nil
	}
	if err := s.store.RetryFailedDownloadRecord(ctx, rec.ID); err != nil {
		return nil, err
	}
	s.importer.Enqueue(rec.ID)
	slog.InfoContext(ctx, "retrying a failed import",
		"record.id", rec.ID, "save_path", rec.SavePath)
	return RetryFailedImport204Response{}, nil
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
	// A record whose torrent still seeds is kept, or the adoption pass files
	// that torrent as an unidentified proposal on its next tick.
	live := s.downloads.SeedingTorrents(ctx)
	if !live.Complete {
		return ClearCompletedHistory503JSONResponse{
			ServiceUnavailableJSONResponse: errServiceUnavailable(
				"a download client could not be listed, so nothing was cleared: " +
					"the records of torrents it still seeds cannot be told apart",
			),
		}, nil
	}
	n, err := s.store.DeleteAllCompletedDownloadRecords(ctx, live.Hashes())
	if err != nil {
		return nil, err
	}
	return ClearCompletedHistory200JSONResponse{
		ClearCompletedResultJSONResponse: ClearCompletedResultJSONResponse(
			ClearCompletedResult{Deleted: n},
		),
	}, nil
}

// ResolveHeldDownload releases a download held by import verification. import
// re-runs the import with verification bypassed; regrab and delete both remove
// the torrent with its files, differing only in whether the media goes back to
// wanted for another search.
func (s *Server) ResolveHeldDownload(
	ctx context.Context,
	request ResolveHeldDownloadRequestObject,
) (ResolveHeldDownloadResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return ResolveHeldDownload403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	// The generated wrapper only json-decodes, so an unknown or absent action
	// arrives here as a value no branch below claims — and the tail of this
	// function is the destructive one.
	switch request.Body.Action {
	case ResolveHeldRequestActionImport,
		ResolveHeldRequestActionRegrab,
		ResolveHeldRequestActionDelete:
	default:
		return ResolveHeldDownload400JSONResponse{
			BadRequestJSONResponse: errBadRequest(
				"action must be one of import, regrab, delete",
			),
		}, nil
	}

	rec, err := s.store.FindDownloadRecordByID(ctx, request.Id)
	if err != nil {
		if ent.IsNotFound(err) {
			return ResolveHeldDownload404JSONResponse{
				NotFoundJSONResponse: errNotFound("download record not found"),
			}, nil
		}
		return nil, err
	}
	if rec.Status != downloadrecord.StatusHeld {
		return ResolveHeldDownload409JSONResponse{
			ConflictJSONResponse: errConflict("download record is not held"),
		}, nil
	}

	if request.Body.Action == ResolveHeldRequestActionImport {
		var assignments map[string]uint32
		if request.Body.TrackAssignments != nil {
			plan, err := s.trackPlans.AlbumTrackPlan(ctx, rec.ID)
			if errors.Is(err, importer.ErrNotSingleAlbum) {
				return ResolveHeldDownload409JSONResponse{
					ConflictJSONResponse: errConflict(err.Error()),
				}, nil
			}
			if err != nil {
				return nil, err
			}
			var problem string
			assignments, problem = checkTrackAssignments(
				plan,
				*request.Body.TrackAssignments,
			)
			if problem != "" {
				return ResolveHeldDownload422JSONResponse{
					UnprocessableEntityJSONResponse: errTrackAssignmentRejected(
						problem,
					),
				}, nil
			}
		}
		if err := s.store.ReleaseHeldDownloadRecord(
			ctx,
			rec.ID,
			assignments,
		); err != nil {
			return nil, err
		}
		s.importer.Enqueue(rec.ID)
		return ResolveHeldDownload204Response{}, nil
	}

	if rec.TorrentHash != "" {
		if err := s.downloads.RemoveTorrent(
			ctx, rec.DownloadClientName, rec.TorrentHash, true,
		); err != nil {
			slog.WarnContext(ctx, "resolve held: remove torrent failed",
				"hash", rec.TorrentHash, "error", err)
		}
	}
	requeue := request.Body.Action == ResolveHeldRequestActionRegrab
	reason := "rejected by user"
	if requeue {
		reason = "rejected by user (re-grab)"
	}
	if err := s.store.FailHeldDownloadRecord(
		ctx, rec.ID, reason, requeue,
	); err != nil {
		return nil, err
	}
	return ResolveHeldDownload204Response{}, nil
}

// checkTrackAssignments turns a held album's chosen tracks into what the import
// reads, or says why it cannot: the paths come from the client, and only the
// record's own files may reach the importer.
func checkTrackAssignments(
	plan importer.TrackPlan,
	chosen []TrackAssignment,
) (map[string]uint32, string) {
	if len(chosen) == 0 {
		return nil, "choose a track for at least one file"
	}
	files := make(map[string]struct{}, len(plan.Files))
	for _, f := range plan.Files {
		files[f.Path] = struct{}{}
	}
	tracks := make(map[uint32]string, len(plan.Tracks))
	for _, t := range plan.Tracks {
		tracks[t.ID] = t.Title
	}
	out := make(map[string]uint32, len(chosen))
	taken := make(map[uint32]struct{}, len(chosen))
	for _, c := range chosen {
		if _, ok := files[c.File]; !ok {
			return nil, c.File + " is not a file of this download"
		}
		title, ok := tracks[c.TrackId]
		if !ok {
			return nil, c.File + " is assigned a track that is not on this album"
		}
		if _, dup := taken[c.TrackId]; dup {
			return nil, "two files are assigned to " + title
		}
		taken[c.TrackId] = struct{}{}
		out[c.File] = c.TrackId
	}
	return out, ""
}

// GetHeldDownloadTrackPlan lists a held album's files and tracks.
func (s *Server) GetHeldDownloadTrackPlan(
	ctx context.Context,
	request GetHeldDownloadTrackPlanRequestObject,
) (GetHeldDownloadTrackPlanResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return GetHeldDownloadTrackPlan403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	rec, err := s.store.FindDownloadRecordByID(ctx, request.Id)
	if err != nil {
		if ent.IsNotFound(err) {
			return GetHeldDownloadTrackPlan404JSONResponse{
				NotFoundJSONResponse: errNotFound("download record not found"),
			}, nil
		}
		return nil, err
	}
	if rec.Status != downloadrecord.StatusHeld {
		return GetHeldDownloadTrackPlan409JSONResponse{
			ConflictJSONResponse: errConflict("download record is not held"),
		}, nil
	}
	plan, err := s.trackPlans.AlbumTrackPlan(ctx, rec.ID)
	if errors.Is(err, importer.ErrNotSingleAlbum) {
		return GetHeldDownloadTrackPlan409JSONResponse{
			ConflictJSONResponse: errConflict(err.Error()),
		}, nil
	}
	if err != nil {
		return nil, err
	}
	out := GetHeldDownloadTrackPlan200JSONResponse{
		Files:  make([]TrackPlanFile, 0, len(plan.Files)),
		Tracks: make([]TrackPlanTrack, 0, len(plan.Tracks)),
	}
	for _, f := range plan.Files {
		row := TrackPlanFile{File: f.Path}
		if f.Track != nil {
			row.TrackId = &f.Track.ID
		}
		out.Files = append(out.Files, row)
	}
	for _, t := range plan.Tracks {
		out.Tracks = append(out.Tracks, TrackPlanTrack{
			Id: t.ID, Disc: t.Disc, Position: t.Position, Title: t.Title,
			HasFile: len(t.Edges.MediaFiles) > 0,
		})
	}
	return out, nil
}
