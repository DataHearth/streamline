package restapi

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"path/filepath"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	enttvshow "github.com/datahearth/streamline/ent/tvshow"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/quality"
	"github.com/datahearth/streamline/internal/quality/qualityctx"
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

// PreviewPending answers what importing the proposal's torrent would do. A
// pack proposal links exactly one episode — the anchor — which says nothing
// about the other episodes in the torrent, so the operator was deciding blind
// on anything wider than a single file.
func (s *Server) PreviewPending(
	ctx context.Context,
	request PreviewPendingRequestObject,
) (PreviewPendingResponseObject, error) {
	rec, err := s.store.FindPendingDownloadRecordByID(ctx, request.Id)
	if err != nil {
		if ent.IsNotFound(err) {
			return PreviewPending404JSONResponse{
				NotFoundJSONResponse: errNotFound("pending record not found"),
			}, nil
		}
		return nil, err
	}
	ep := rec.Edges.Episode
	if ep == nil || ep.Edges.Season == nil ||
		ep.Edges.Season.Edges.TvShow == nil {
		return PreviewPending409JSONResponse{
			ConflictJSONResponse: errConflict(
				"only a proposal matched to an episode can be previewed",
			),
		}, nil
	}
	// The record's own episode edge carries one season; the pack is matched
	// against the whole tree, exactly as the importer matches it.
	show, err := s.store.FindTVShowByID(ctx, ep.Edges.Season.Edges.TvShow.ID)
	if err != nil {
		return PreviewPending500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	files, err := s.downloads.ListTorrentFiles(
		ctx, rec.DownloadClientName, rec.TorrentHash,
	)
	if err != nil {
		return PreviewPending422JSONResponse{
			UnprocessableEntityJSONResponse: errConnectionFailed(
				fmt.Sprintf("could not list the torrent's files: %v", err),
			),
		}, nil
	}
	return PreviewPending200JSONResponse{
		PendingPreviewJSONResponse: PendingPreviewJSONResponse(
			previewPack(files, show, rec.Title),
		),
	}, nil
}

// previewPack buckets a torrent's files by what each action would do with
// them: fill an empty episode, beat an episode that already has a file, or
// lose to it. It applies the same extension / size / sample filters the
// importer's own file walk does, and reaches the same per-file verdict through
// qualityctx.Replaces, so the counts cannot promise a file the import then
// skips. Two files landing on one episode count once — the importer imports
// whichever it reaches first.
//
// The one thing it cannot mirror is the probe: the files are still in the
// download client, so upgrades/keeps are decided on filename and size alone
// and the import may land on a slightly different split.
func previewPack(
	files []download.TorrentFile, show *ent.TVShow, releaseTitle string,
) PendingPreview {
	out := PendingPreview{
		Imports:  []PendingPreviewEpisode{},
		Upgrades: []PendingPreviewEpisode{},
		Keeps:    []PendingPreviewEpisode{},
	}
	anime := show.Type == enttvshow.TypeAnime
	profile, hasProfile := config.ResolveScoredProfile(show.QualityProfile)
	seen := map[uint32]bool{}
	for _, f := range files {
		name := path.Base(f.Path)
		if !library.MediaExts[strings.ToLower(filepath.Ext(name))] ||
			f.Size < library.MinEpisodeSize ||
			library.SampleRe.MatchString(name) {
			continue
		}
		season, ep := library.MatchEpisodeInSeason(
			library.Parse(name), show.Edges.Seasons, anime,
		)
		if ep == nil {
			out.Unmatched++
			continue
		}
		if seen[ep.ID] {
			continue
		}
		seen[ep.ID] = true
		item := PendingPreviewEpisode{Season: season, Episode: ep.Number}
		if ep.Title != "" {
			item.Title = &ep.Title
		}
		switch {
		case len(ep.Edges.MediaFiles) == 0:
			out.Imports = append(out.Imports, item)
		case hasProfile && replacesEpisodeFile(
			profile, ep.Edges.MediaFiles[0], name, f.Size, releaseTitle,
		):
			out.Upgrades = append(out.Upgrades, item)
		default:
			// No profile resolves, or the file on disk wins. takesPackFile
			// answers false for both, so neither action touches this episode.
			out.Keeps = append(out.Keeps, item)
		}
	}
	return out
}

// replacesEpisodeFile is takesPackFile's "upgrades" arm minus the probe, which
// the preview cannot have.
func replacesEpisodeFile(
	profile quality.Profile,
	existing *ent.MediaFile,
	name string,
	size int64,
	releaseTitle string,
) bool {
	return qualityctx.Replaces(
		profile,
		qualityctx.ContextFromFile(
			filepath.Base(existing.Path), existing.Size,
			int(existing.Width), existing.VideoCodec,
		),
		qualityctx.ContextFromPackFile(name, size, 0, "", releaseTitle),
	)
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

	// The importer owns the replacement, and this used to pre-delete instead:
	// one file, the record's *anchor*, which on a pack is a handle rather than
	// anything the torrent holds — so Replace could delete a file the release
	// had no copy of, and skip the other N because the record was still
	// replace_mode "none". Worse on every shape, the delete ran here, before
	// anything was probed: a held import had already destroyed the only copy on
	// disk, which is exactly the ordering importSingleEpisode/importMovieRecord
	// go out of their way to avoid.
	//
	// "upgrades" rather than "all" because a pack proposal is not a release the
	// operator inspected: it is hundreds of files, and quality.ReplacesFile is
	// what decides, per episode, against that episode's own file. A movie or a
	// single episode has no second opinion to reach, so the importer's
	// `!= none` check reads it as the plain overwrite it is. It can therefore
	// legitimately replace nothing — the preview says so before the click.
	if err := s.store.SetDownloadRecordReplaceMode(
		ctx, request.Id, downloadrecord.ReplaceModeUpgrades,
	); err != nil {
		return ReplacePending500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
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
