package restapi

import (
	"context"
	"log/slog"

	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/media/tvshow"
	"github.com/datahearth/streamline/internal/metadata"
)

func (s *Server) ListSeries(
	ctx context.Context,
	request ListSeriesRequestObject,
) (ListSeriesResponseObject, error) {
	p := tvshow.FilterParams{Page: 1, Limit: 20}
	if request.Params.Page != nil {
		p.Page = uint16(*request.Params.Page)
	}
	if request.Params.Limit != nil {
		p.Limit = *request.Params.Limit
	}
	if request.Params.Status != nil {
		p.Status = *request.Params.Status
	}
	if request.Params.Type != nil {
		p.Type = *request.Params.Type
	}
	if request.Params.Query != nil {
		p.Query = *request.Params.Query
	}
	if request.Params.Sort != nil {
		p.Sort = *request.Params.Sort
	}

	rows, total, err := s.tvshows.FilterList(ctx, p)
	if err != nil {
		return ListSeries500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
	items := make([]TVShow, 0, len(rows))
	for _, r := range rows {
		items = append(items, tvShowToAPI(r))
	}
	return ListSeries200JSONResponse{SeriesListJSONResponse: SeriesListJSONResponse{
		Items: items,
		Total: total,
		Page:  uint32(p.Page),
		Limit: p.Limit,
	}}, nil
}

func (s *Server) AddSeries(
	ctx context.Context,
	request AddSeriesRequestObject,
) (AddSeriesResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return AddSeries403JSONResponse{ForbiddenJSONResponse: requestOnlyResp}, nil
	}

	qp := ""
	if request.Body.QualityProfile != nil {
		qp = *request.Body.QualityProfile
	}
	show, err := s.tvshows.Add(ctx, request.Body.TvdbId, qp)
	if err != nil {
		return AddSeries409JSONResponse{
			ConflictJSONResponse: errConflict(err.Error()),
		}, nil
	}
	if request.Body.Preset != nil && *request.Body.Preset != "" {
		updated, uerr := s.tvshows.Update(ctx, show.ID, tvshow.UpdateParams{
			Preset: string(*request.Body.Preset),
		})
		if uerr != nil {
			return AddSeries500JSONResponse{
				InternalErrorJSONResponse: errInternal(uerr.Error()),
			}, nil
		}
		show = updated
	}
	return AddSeries201JSONResponse{
		SeriesCreatedJSONResponse: SeriesCreatedJSONResponse(tvShowToAPI(show)),
	}, nil
}

func (s *Server) GetSeriesCounts(
	ctx context.Context,
	request GetSeriesCountsRequestObject,
) (GetSeriesCountsResponseObject, error) {
	c, err := s.tvshows.Counts(ctx)
	if err != nil {
		return GetSeriesCounts500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
	return GetSeriesCounts200JSONResponse{
		SeriesCountsResponseJSONResponse: SeriesCountsResponseJSONResponse{
			Total:          c.Total,
			Continuing:     c.Continuing,
			Ended:          c.Ended,
			WantedEpisodes: c.WantedEpisodes,
		},
	}, nil
}

func (s *Server) LookupSeries(
	ctx context.Context,
	request LookupSeriesRequestObject,
) (LookupSeriesResponseObject, error) {
	results, err := s.metadataTV.SearchSeries(ctx, request.Params.Query)
	if err != nil {
		return LookupSeries500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
	out := make([]SeriesLookupResult, 0, len(results))
	for _, r := range results {
		item := SeriesLookupResult{TvdbId: r.TVDBID, Title: r.Title, Year: r.Year}
		if r.Network != "" {
			n := r.Network
			item.Network = &n
		}
		if r.Overview != "" {
			o := r.Overview
			item.Overview = &o
		}
		if url := metadata.TVDBArtworkURL(r.PosterPath); url != "" {
			item.PosterUrl = &url
		}
		if existing, _ := s.store.FindTVShowByTVDBID(
			ctx,
			r.TVDBID,
		); existing != nil {
			added := true
			item.AlreadyAdded = &added
		}
		out = append(out, item)
	}
	return LookupSeries200JSONResponse{
		SeriesLookupResultsJSONResponse: SeriesLookupResultsJSONResponse{Items: out},
	}, nil
}

func (s *Server) GetSeries(
	ctx context.Context,
	request GetSeriesRequestObject,
) (GetSeriesResponseObject, error) {
	show, err := s.tvshows.Get(ctx, request.Id)
	if err != nil {
		return GetSeries404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	}
	result := tvShowToAPI(show)
	// Cast is fetched live from TVDB on the detail view. A failure here (no API
	// key, transport error) must not fail the response — the section degrades
	// to empty instead, mirroring GetMovie.
	if cast, cerr := s.metadataTV.GetSeriesCast(ctx, show.TvdbID); cerr != nil {
		slog.WarnContext(ctx, "series detail: cast fetch failed",
			"series.id", show.ID, "series.tvdb_id", show.TvdbID, "error", cerr)
	} else if len(cast) > 0 {
		apiCast := castToAPI(cast)
		result.Cast = &apiCast
	}
	return GetSeries200JSONResponse{
		SeriesDetailJSONResponse: SeriesDetailJSONResponse(result),
	}, nil
}

func (s *Server) PatchSeries(
	ctx context.Context,
	request PatchSeriesRequestObject,
) (PatchSeriesResponseObject, error) {
	if _, err := s.tvshows.Get(ctx, request.Id); err != nil {
		return PatchSeries404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	}
	p := tvshow.UpdateParams{
		Monitored:      request.Body.Monitored,
		QualityProfile: request.Body.QualityProfile,
	}
	if request.Body.Preset != nil {
		p.Preset = string(*request.Body.Preset)
	}
	show, err := s.tvshows.Update(ctx, request.Id, p)
	if err != nil {
		return PatchSeries500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
	return PatchSeries200JSONResponse{
		SeriesDetailJSONResponse: SeriesDetailJSONResponse(tvShowToAPI(show)),
	}, nil
}

func (s *Server) DeleteSeries(
	ctx context.Context,
	request DeleteSeriesRequestObject,
) (DeleteSeriesResponseObject, error) {
	if _, err := s.tvshows.Get(ctx, request.Id); err != nil {
		return DeleteSeries404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	}
	deleteFiles := request.Params.DeleteFiles != nil && *request.Params.DeleteFiles
	if err := s.tvshows.Delete(
		ctx,
		request.Id,
		tvshow.DeleteOptions{DeleteFiles: deleteFiles},
	); err != nil {
		return DeleteSeries500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
	return DeleteSeries204Response{}, nil
}

func (s *Server) DeleteEpisodeFile(
	ctx context.Context,
	request DeleteEpisodeFileRequestObject,
) (DeleteEpisodeFileResponseObject, error) {
	remove := request.Body != nil &&
		request.Body.RemoveTorrent != nil &&
		*request.Body.RemoveTorrent
	if err := s.tvshows.DeleteEpisodeFile(ctx, request.EpisodeId,
		tvshow.DeleteFileOptions{RemoveTorrent: remove}); err != nil {
		return DeleteEpisodeFile404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	}
	return DeleteEpisodeFile204Response{}, nil
}

func (s *Server) PatchSeason(
	ctx context.Context,
	request PatchSeasonRequestObject,
) (PatchSeasonResponseObject, error) {
	show, err := s.tvshows.Get(ctx, request.Id)
	if err != nil {
		return PatchSeason404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	}
	for _, se := range show.Edges.Seasons {
		if se.Number != request.Number {
			continue
		}
		if err := s.tvshows.SetSeasonMonitored(
			ctx,
			se.ID,
			request.Body.Monitored,
		); err != nil {
			return PatchSeason500JSONResponse{
				InternalErrorJSONResponse: errInternal(err.Error()),
			}, nil
		}
		return PatchSeason204Response{}, nil
	}
	return PatchSeason404JSONResponse{
		NotFoundJSONResponse: errNotFound("season not found"),
	}, nil
}

func (s *Server) PatchEpisode(
	ctx context.Context,
	request PatchEpisodeRequestObject,
) (PatchEpisodeResponseObject, error) {
	if _, err := s.tvshows.Get(ctx, request.Id); err != nil {
		return PatchEpisode404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	}
	if err := s.tvshows.SetEpisodeMonitored(
		ctx,
		request.EpisodeId,
		request.Body.Monitored,
	); err != nil {
		return PatchEpisode500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
	return PatchEpisode204Response{}, nil
}

func (s *Server) SearchSeries(
	ctx context.Context,
	request SearchSeriesRequestObject,
) (SearchSeriesResponseObject, error) {
	if _, err := s.tvshows.Get(ctx, request.Id); err != nil {
		return SearchSeries404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	}
	if s.tvSearcher == nil {
		return SearchSeries500JSONResponse{
			InternalErrorJSONResponse: errInternal("tv search not configured"),
		}, nil
	}
	if err := s.tvSearcher.SearchShow(ctx, request.Id); err != nil {
		return SearchSeries500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
	return SearchSeries202Response{}, nil
}

func (s *Server) BrowseEpisodeReleases(
	ctx context.Context,
	request BrowseEpisodeReleasesRequestObject,
) (BrowseEpisodeReleasesResponseObject, error) {
	show, err := s.tvshows.Get(ctx, request.Id)
	if err != nil {
		return BrowseEpisodeReleases404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	}
	var season, episode uint16
	found := false
	for _, se := range show.Edges.Seasons {
		for _, ep := range se.Edges.Episodes {
			if ep.ID == request.EpisodeId {
				season = se.Number
				episode = ep.Number
				found = true
			}
		}
	}
	if !found {
		return BrowseEpisodeReleases404JSONResponse{
			NotFoundJSONResponse: errNotFound("episode not found"),
		}, nil
	}
	results, err := s.indexers.SearchEpisode(
		ctx,
		[]string{show.Title},
		show.TvdbID,
		season,
		episode,
	)
	if err != nil {
		return BrowseEpisodeReleases500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
	items := make([]SearchResult, 0, len(results))
	for _, r := range results {
		items = append(items, toSearchResult(r))
	}
	return BrowseEpisodeReleases200JSONResponse{
		SearchResultsJSONResponse: SearchResultsJSONResponse{Items: items},
	}, nil
}

// toIndexerResult validates a grab request body (title + download_url required)
// and maps it to an indexer.SearchResult. The bool reports whether the body was
// acceptable; the caller emits the operation-specific 422 on false.
func toIndexerResult(body *SearchResult) (indexer.SearchResult, bool) {
	if body == nil || body.DownloadUrl == "" || body.Title == "" {
		return indexer.SearchResult{}, false
	}
	sr := indexer.SearchResult{
		Title:    body.Title,
		Download: body.DownloadUrl,
		Size:     body.Size,
		Seeders:  body.Seeders,
	}
	if body.InfoUrl != nil {
		sr.InfoURL = *body.InfoUrl
	}
	if body.Leechers != nil {
		sr.Leechers = *body.Leechers
	}
	return sr, true
}

// replaceExisting reports whether a manual-grab body asked to overwrite
// already-present file(s) for the covered media.
func replaceExisting(body *SearchResult) bool {
	return body != nil && body.ReplaceExisting != nil && *body.ReplaceExisting
}

func (s *Server) GrabEpisodeRelease(
	ctx context.Context,
	request GrabEpisodeReleaseRequestObject,
) (GrabEpisodeReleaseResponseObject, error) {
	if _, err := s.tvshows.Get(ctx, request.Id); err != nil {
		return GrabEpisodeRelease404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	}
	sr, ok := toIndexerResult(request.Body)
	if !ok {
		return GrabEpisodeRelease422JSONResponse{
			UnprocessableEntityJSONResponse: unprocessableResp(
				"release title and download_url are required",
			),
		}, nil
	}
	rec, err := s.downloads.GrabEpisode(ctx, sr, request.EpisodeId)
	if err != nil {
		return GrabEpisodeRelease500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
	if replaceExisting(request.Body) {
		if err := s.store.MarkDownloadRecordReplaceExisting(
			ctx,
			rec.ID,
		); err != nil {
			slog.WarnContext(ctx, "grab episode: mark replace-existing failed",
				"download_record.id", rec.ID, "error", err)
		}
	}
	return GrabEpisodeRelease202Response{}, nil
}

func (s *Server) BrowseSeasonReleases(
	ctx context.Context,
	request BrowseSeasonReleasesRequestObject,
) (BrowseSeasonReleasesResponseObject, error) {
	show, err := s.tvshows.Get(ctx, request.Id)
	if err != nil {
		return BrowseSeasonReleases404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	}
	results, err := s.indexers.SearchSeason(
		ctx, []string{show.Title}, show.TvdbID, request.Number,
	)
	if err != nil {
		return BrowseSeasonReleases500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
	items := make([]SearchResult, 0, len(results))
	for _, r := range results {
		items = append(items, toSearchResult(r))
	}
	return BrowseSeasonReleases200JSONResponse{
		SearchResultsJSONResponse: SearchResultsJSONResponse{Items: items},
	}, nil
}

func (s *Server) GrabSeasonRelease(
	ctx context.Context,
	request GrabSeasonReleaseRequestObject,
) (GrabSeasonReleaseResponseObject, error) {
	if _, err := s.tvshows.Get(ctx, request.Id); err != nil {
		return GrabSeasonRelease404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	}
	sr, ok := toIndexerResult(request.Body)
	if !ok {
		return GrabSeasonRelease422JSONResponse{
			UnprocessableEntityJSONResponse: unprocessableResp(
				"release title and download_url are required",
			),
		}, nil
	}
	if err := s.tvshows.GrabSeasonRelease(
		ctx, request.Id, request.Number, sr, replaceExisting(request.Body),
	); err != nil {
		return GrabSeasonRelease500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
	return GrabSeasonRelease202Response{}, nil
}

func (s *Server) BrowseSeriesReleases(
	ctx context.Context,
	request BrowseSeriesReleasesRequestObject,
) (BrowseSeriesReleasesResponseObject, error) {
	show, err := s.tvshows.Get(ctx, request.Id)
	if err != nil {
		return BrowseSeriesReleases404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	}
	results, err := s.indexers.SearchSeries(ctx, []string{show.Title}, show.TvdbID)
	if err != nil {
		return BrowseSeriesReleases500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
	items := make([]SearchResult, 0, len(results))
	for _, r := range results {
		items = append(items, toSearchResult(r))
	}
	return BrowseSeriesReleases200JSONResponse{
		SearchResultsJSONResponse: SearchResultsJSONResponse{Items: items},
	}, nil
}

func (s *Server) GrabSeriesRelease(
	ctx context.Context,
	request GrabSeriesReleaseRequestObject,
) (GrabSeriesReleaseResponseObject, error) {
	if _, err := s.tvshows.Get(ctx, request.Id); err != nil {
		return GrabSeriesRelease404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	}
	sr, ok := toIndexerResult(request.Body)
	if !ok {
		return GrabSeriesRelease422JSONResponse{
			UnprocessableEntityJSONResponse: unprocessableResp(
				"release title and download_url are required",
			),
		}, nil
	}
	if err := s.tvshows.GrabSeriesRelease(
		ctx, request.Id, sr, replaceExisting(request.Body),
	); err != nil {
		return GrabSeriesRelease500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
	return GrabSeriesRelease202Response{}, nil
}

func (s *Server) GetSeriesPlayOnLinks(
	ctx context.Context,
	request GetSeriesPlayOnLinksRequestObject,
) (GetSeriesPlayOnLinksResponseObject, error) {
	show, err := s.tvshows.Get(ctx, request.Id)
	if err != nil {
		return GetSeriesPlayOnLinks404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	}
	if s.deepLinker == nil {
		return GetSeriesPlayOnLinks500JSONResponse{
			InternalErrorJSONResponse: errInternal(
				"play-on resolver not configured",
			),
		}, nil
	}
	results := s.deepLinker.ResolveTV(ctx, show.TvdbID, show.Title, show.Year)
	items := make([]PlayOnLink, 0, len(results))
	for _, r := range results {
		items = append(items, playOnToAPI(r))
	}
	return GetSeriesPlayOnLinks200JSONResponse{
		SeriesPlayOnLinksJSONResponse: SeriesPlayOnLinksJSONResponse{Items: items},
	}, nil
}

func (s *Server) RefreshSeriesMetadata(
	ctx context.Context,
	request RefreshSeriesMetadataRequestObject,
) (RefreshSeriesMetadataResponseObject, error) {
	if _, err := s.tvshows.Get(ctx, request.Id); err != nil {
		return RefreshSeriesMetadata404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	}
	show, err := s.tvshows.RefreshOne(ctx, request.Id)
	if err != nil {
		return RefreshSeriesMetadata500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
	return RefreshSeriesMetadata200JSONResponse{
		SeriesDetailJSONResponse: SeriesDetailJSONResponse(tvShowToAPI(show)),
	}, nil
}

func (s *Server) RenameSeriesFiles(
	ctx context.Context,
	request RenameSeriesFilesRequestObject,
) (RenameSeriesFilesResponseObject, error) {
	if s.seriesRenamer == nil {
		return RenameSeriesFiles500JSONResponse{
			InternalErrorJSONResponse: errInternal("renamer not configured"),
		}, nil
	}
	preview := request.Params.Preview != nil && *request.Params.Preview
	var plan library.RenamePlan
	var err error
	if preview {
		plan, err = s.seriesRenamer.Preview(ctx, request.Id)
	} else {
		plan, err = s.seriesRenamer.Apply(ctx, request.Id)
	}
	if err != nil {
		return RenameSeriesFiles500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
	out := SeriesRenamePlan{
		SeriesId:   request.Id,
		Operations: make([]RenameOperation, 0, len(plan.Operations)),
	}
	for _, op := range plan.Operations {
		out.Operations = append(out.Operations, RenameOperation{
			MediaFileId: op.MediaFileID,
			From:        op.From,
			To:          op.To,
		})
	}
	return RenameSeriesFiles200JSONResponse{
		SeriesRenamePlanJSONResponse: SeriesRenamePlanJSONResponse(out),
	}, nil
}
