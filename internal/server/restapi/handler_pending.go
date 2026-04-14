package restapi

import (
	"context"
	"log/slog"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	moviesvc "github.com/datahearth/streamline/internal/media/movie"
	"github.com/datahearth/streamline/internal/media/tvshow"
)

func (s *Server) ListPending(
	ctx context.Context,
	_ ListPendingRequestObject,
) (ListPendingResponseObject, error) {
	records, err := s.store.ListPendingDownloadRecords(ctx)
	if err != nil {
		return nil, err
	}
	out := PendingList{Items: make([]PendingItem, 0, len(records))}
	for _, r := range records {
		out.Items = append(out.Items, toPendingItem(r))
	}
	return ListPending200JSONResponse{
		PendingListJSONResponse: PendingListJSONResponse(out),
	}, nil
}

func (s *Server) ImportPending(
	ctx context.Context,
	request ImportPendingRequestObject,
) (ImportPendingResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return ImportPending403JSONResponse{ForbiddenJSONResponse: notAdminResp}, nil
	}
	if _, err := s.store.FindPendingDownloadRecordByID(ctx, request.Id); err != nil {
		if ent.IsNotFound(err) {
			return ImportPending404JSONResponse{
				NotFoundJSONResponse: errNotFound("pending record not found"),
			}, nil
		}
		return nil, err
	}
	// Flip to importing; the import_scan safety-net job re-enqueues it.
	if err := s.store.UpdateDownloadRecordStatus(
		ctx, request.Id, downloadrecord.StatusImporting,
	); err != nil {
		return ImportPending500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
	return ImportPending204Response{}, nil
}

func (s *Server) ReplacePending(
	ctx context.Context,
	request ReplacePendingRequestObject,
) (ReplacePendingResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return ReplacePending403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	rec, err := s.store.FindPendingDownloadRecordByID(ctx, request.Id)
	if err != nil {
		if ent.IsNotFound(err) {
			return ReplacePending404JSONResponse{
				NotFoundJSONResponse: errNotFound("pending record not found"),
			}, nil
		}
		return nil, err
	}

	// Clear the existing file(s) without touching torrents — the proposed
	// torrent must survive to be imported.
	if resp := s.clearExistingFile(ctx, rec); resp != nil {
		return resp, nil
	}

	removeOld := request.Body != nil &&
		request.Body.RemoveOldTorrent != nil &&
		*request.Body.RemoveOldTorrent
	if removeOld {
		s.removeOldTorrent(ctx, rec)
	}

	if err := s.store.UpdateDownloadRecordStatus(
		ctx, request.Id, downloadrecord.StatusImporting,
	); err != nil {
		return ReplacePending500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
	return ReplacePending204Response{}, nil
}

// clearExistingFile deletes the matched media's current file(s) and reverts it
// to wanted. Returns a non-nil 500 response on failure, nil on success (incl.
// "nothing to delete").
func (s *Server) clearExistingFile(
	ctx context.Context, rec *ent.DownloadRecord,
) ReplacePendingResponseObject {
	switch {
	case rec.Edges.Movie != nil:
		files, err := s.store.ListMediaFilesByMovieID(ctx, rec.Edges.Movie.ID)
		if err != nil {
			return ReplacePending500JSONResponse{
				InternalErrorJSONResponse: errInternal(err.Error()),
			}
		}
		for _, f := range files {
			if err := s.movies.DeleteFile(
				ctx, rec.Edges.Movie.ID, f.ID, moviesvc.DeleteFileOptions{},
			); err != nil {
				return ReplacePending500JSONResponse{
					InternalErrorJSONResponse: errInternal(err.Error()),
				}
			}
		}
	case rec.Edges.Episode != nil:
		if _, err := s.store.FindMediaFileByEpisodeID(
			ctx, rec.Edges.Episode.ID,
		); ent.IsNotFound(err) {
			return nil // no file to replace
		} else if err != nil {
			return ReplacePending500JSONResponse{
				InternalErrorJSONResponse: errInternal(err.Error()),
			}
		}
		if err := s.tvshows.DeleteEpisodeFile(
			ctx, rec.Edges.Episode.ID, tvshow.DeleteFileOptions{},
		); err != nil {
			return ReplacePending500JSONResponse{
				InternalErrorJSONResponse: errInternal(err.Error()),
			}
		}
	}
	return nil
}

// removeOldTorrent best-effort removes the torrent that produced the existing
// file when it is still tracked and distinct from the proposal.
func (s *Server) removeOldTorrent(ctx context.Context, pending *ent.DownloadRecord) {
	var (
		old *ent.DownloadRecord
		err error
	)
	switch {
	case pending.Edges.Movie != nil:
		old, err = s.store.LatestImportedRecordForMovie(ctx, pending.Edges.Movie.ID)
	case pending.Edges.Episode != nil:
		old, err = s.store.LatestImportedRecordForEpisode(
			ctx,
			pending.Edges.Episode.ID,
		)
	default:
		return
	}
	if err != nil {
		return // ent.NotFound or transient: nothing to remove
	}
	// ponytail: the latest hash-carrying record is usually the pending proposal
	// itself; act only on a distinct, still-tracked older torrent. Upgrade
	// path: a status=completed-scoped "previous torrent" query.
	if old.TorrentHash == "" ||
		old.TorrentHash == pending.TorrentHash ||
		old.DownloadClientName == "" {
		return
	}
	if err := s.downloads.RemoveTorrent(
		ctx, old.DownloadClientName, old.TorrentHash,
	); err != nil {
		slog.WarnContext(ctx, "replace pending: remove old torrent failed",
			"hash", old.TorrentHash, "error", err)
	}
}

func (s *Server) IgnorePending(
	ctx context.Context,
	request IgnorePendingRequestObject,
) (IgnorePendingResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return IgnorePending403JSONResponse{ForbiddenJSONResponse: notAdminResp}, nil
	}
	rec, err := s.store.FindPendingDownloadRecordByID(ctx, request.Id)
	if err != nil {
		if ent.IsNotFound(err) {
			return IgnorePending404JSONResponse{
				NotFoundJSONResponse: errNotFound("pending record not found"),
			}, nil
		}
		return nil, err
	}
	if err := s.store.UpdateDownloadRecordStatus(
		ctx, request.Id, downloadrecord.StatusDismissed,
	); err != nil {
		return IgnorePending500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
	if request.Body != nil &&
		request.Body.RemoveTorrent != nil &&
		*request.Body.RemoveTorrent &&
		rec.TorrentHash != "" && rec.DownloadClientName != "" {
		if err := s.downloads.RemoveTorrent(
			ctx, rec.DownloadClientName, rec.TorrentHash,
		); err != nil {
			slog.WarnContext(ctx, "ignore pending: remove torrent failed",
				"hash", rec.TorrentHash, "error", err)
		}
	}
	return IgnorePending204Response{}, nil
}
