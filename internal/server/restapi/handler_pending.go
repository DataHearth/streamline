package restapi

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/library"
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

// reasonIdentified replaces the unidentified reason once an operator has named
// the title. The proposal stays pending — naming it is not accepting it.
const reasonIdentified = "identified, review and import"

func (s *Server) IdentifyPending(
	ctx context.Context,
	request IdentifyPendingRequestObject,
) (IdentifyPendingResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return IdentifyPending403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	rec, err := s.store.FindPendingDownloadRecordByID(ctx, request.Id)
	if err != nil {
		if ent.IsNotFound(err) {
			return IdentifyPending404JSONResponse{
				NotFoundJSONResponse: errNotFound("pending record not found"),
			}, nil
		}
		return nil, err
	}
	if rec.Edges.Movie != nil || rec.Edges.Episode != nil {
		return IdentifyPending409JSONResponse{
			ConflictJSONResponse: errConflict(
				"this proposal is already matched to a title",
			),
		}, nil
	}

	var movieID, episodeID uint32
	switch request.Body.Kind {
	case IdentifyPendingRequestKindSeries:
		ep, resp := s.identifySeries(ctx, rec, request.Body.ProviderId)
		if resp != nil {
			return resp, nil
		}
		episodeID = ep
	default:
		m, resp := s.identifyMovie(ctx, request.Body.ProviderId)
		if resp != nil {
			return resp, nil
		}
		movieID = m
	}

	if err := s.store.IdentifyDownloadRecord(
		ctx, request.Id, movieID, episodeID, reasonIdentified,
	); err != nil {
		return IdentifyPending500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return IdentifyPending204Response{}, nil
}

// identifySeries resolves tvdbID to a show — adding it when the library does
// not have it — and returns the episode the record should anchor to. A non-nil
// response is the caller's return value.
func (s *Server) identifySeries(
	ctx context.Context, rec *ent.DownloadRecord, tvdbID uint32,
) (uint32, IdentifyPendingResponseObject) {
	// FindTVShowByTVDBID reports "not in the library" as a nil row with a nil
	// error, so the row is what decides, not the error.
	existing, err := s.store.FindTVShowByTVDBID(ctx, tvdbID)
	if err != nil {
		return 0, IdentifyPending500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}
	}
	id := uint32(0)
	if existing != nil {
		id = existing.ID
	} else {
		added, aerr := s.tvshows.Add(ctx, tvdbID, "")
		if aerr != nil {
			return 0, IdentifyPending422JSONResponse{
				UnprocessableEntityJSONResponse: errUnprocessable(
					fmt.Sprintf("could not add that series: %v", aerr),
				),
			}
		}
		id = added.ID
	}

	// Load the tree fresh either way: Add returns the row it created without
	// the seasons the episode anchor is resolved against.
	show, err := s.tvshows.Get(ctx, id)
	if err != nil {
		return 0, IdentifyPending500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}
	}
	parsed := library.Parse(rec.Title)
	ep := download.AdoptionEpisode(parsed, show)
	if ep == nil {
		return 0, IdentifyPending422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(fmt.Sprintf(
				"%s has no season %d to file this release against",
				show.Title, parsed.Season,
			)),
		}
	}
	return ep.ID, nil
}

func (s *Server) identifyMovie(
	ctx context.Context, tmdbID uint32,
) (uint32, IdentifyPendingResponseObject) {
	// GetByTMDBID reports "not in the library" as a nil row with a nil error,
	// like FindTVShowByTVDBID above — the row decides, not the error.
	existing, err := s.movies.GetByTMDBID(ctx, tmdbID)
	if err != nil {
		return 0, IdentifyPending500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}
	}
	if existing != nil {
		return existing.ID, nil
	}
	added, _, err := s.movies.Add(ctx, tmdbID, "")
	if err != nil {
		return 0, IdentifyPending422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(
				fmt.Sprintf("could not add that movie: %v", err),
			),
		}
	}
	return added.ID, nil
}

func (s *Server) ImportPending(
	ctx context.Context,
	request ImportPendingRequestObject,
) (ImportPendingResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return ImportPending403JSONResponse{ForbiddenJSONResponse: notAdminResp}, nil
	}
	rec, err := s.store.FindPendingDownloadRecordByID(ctx, request.Id)
	if err != nil {
		if ent.IsNotFound(err) {
			return ImportPending404JSONResponse{
				NotFoundJSONResponse: errNotFound("pending record not found"),
			}, nil
		}
		return nil, err
	}
	if rec.Edges.Movie == nil && rec.Edges.Episode == nil {
		return ImportPending409JSONResponse{
			ConflictJSONResponse: errConflict(
				"identify this proposal before importing it",
			),
		}, nil
	}
	// Flip to importing; the import_scan safety-net job re-enqueues it.
	if err := s.store.UpdateDownloadRecordStatus(
		ctx, request.Id, downloadrecord.StatusImporting,
	); err != nil {
		return ImportPending500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
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
	if rec.Edges.Movie == nil && rec.Edges.Episode == nil {
		return ReplacePending409JSONResponse{
			ConflictJSONResponse: errConflict(
				"identify this proposal before replacing anything",
			),
		}, nil
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
			InternalErrorJSONResponse: errInternal(ctx, err),
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
				InternalErrorJSONResponse: errInternal(ctx, err),
			}
		}
		for _, f := range files {
			if err := s.movies.DeleteFile(
				ctx, rec.Edges.Movie.ID, f.ID, moviesvc.DeleteFileOptions{},
			); err != nil {
				return ReplacePending500JSONResponse{
					InternalErrorJSONResponse: errInternal(ctx, err),
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
				InternalErrorJSONResponse: errInternal(ctx, err),
			}
		}
		if err := s.tvshows.DeleteEpisodeFile(
			ctx, rec.Edges.Episode.ID, tvshow.DeleteFileOptions{},
		); err != nil {
			return ReplacePending500JSONResponse{
				InternalErrorJSONResponse: errInternal(ctx, err),
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
		ctx, old.DownloadClientName, old.TorrentHash, false,
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
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	if request.Body != nil &&
		request.Body.RemoveTorrent != nil &&
		*request.Body.RemoveTorrent &&
		rec.TorrentHash != "" && rec.DownloadClientName != "" {
		if err := s.downloads.RemoveTorrent(
			ctx, rec.DownloadClientName, rec.TorrentHash, false,
		); err != nil {
			slog.WarnContext(ctx, "ignore pending: remove torrent failed",
				"hash", rec.TorrentHash, "error", err)
		}
	}
	return IgnorePending204Response{}, nil
}
