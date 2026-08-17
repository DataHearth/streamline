package restapi

import (
	"context"
	"errors"
	"log/slog"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/media/tvshow"
	"github.com/datahearth/streamline/internal/metadata"
)

func (s *Server) ListSeries(
	ctx context.Context,
	request ListSeriesRequestObject,
) (ListSeriesResponseObject, error) {
	// The service coerces page=0 itself, but pageOr keeps the response's
	// echoed Page field at 1 rather than parroting the 0 — same as /movies.
	p := tvshow.FilterParams{
		Page:  pageOr(request.Params.Page, 1),
		Limit: 20,
	}
	if request.Params.Limit != nil {
		p.Limit = clampLimit(*request.Params.Limit, seriesMaxLimit)
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
			InternalErrorJSONResponse: errInternal(ctx, err),
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
				InternalErrorJSONResponse: errInternal(ctx, uerr),
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
			InternalErrorJSONResponse: errInternal(ctx, err),
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
			InternalErrorJSONResponse: errInternal(ctx, err),
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

func (s *Server) GetSeriesLookupDetail(
	ctx context.Context,
	request GetSeriesLookupDetailRequestObject,
) (GetSeriesLookupDetailResponseObject, error) {
	d, err := s.seriesWithCast(ctx, request.TvdbId)
	if err != nil {
		return GetSeriesLookupDetail500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return GetSeriesLookupDetail200JSONResponse{
		LookupDetailResponseJSONResponse: LookupDetailResponseJSONResponse(
			toLookupDetail(seriesDetailsToRequestMedia(d)),
		),
	}, nil
}

// seriesWithCast is GetSeries plus the top-billed actors GetSeries leaves out,
// as both the add/request lookup panel and the expanded request row show them.
//
// ponytail: second hit on the same /series/{id}/extended record GetSeries
// already fetched — it drops `characters`. Parse cast inside GetSeries if this
// ever shows up hot.
func (s *Server) seriesWithCast(
	ctx context.Context,
	tvdbID uint32,
) (*metadata.TVDetails, error) {
	d, err := s.metadataTV.GetSeries(ctx, tvdbID)
	if err != nil {
		return nil, err
	}
	cast, err := s.metadataTV.GetSeriesCast(ctx, tvdbID)
	if err != nil {
		return nil, err
	}
	d.Cast = cast
	return d, nil
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
	if len(show.Cast) > 0 {
		apiCast := storedCastToAPI(show.Cast)
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
			InternalErrorJSONResponse: errInternal(ctx, err),
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
			InternalErrorJSONResponse: errInternal(ctx, err),
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
				InternalErrorJSONResponse: errInternal(ctx, err),
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
			InternalErrorJSONResponse: errInternal(ctx, err),
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
			InternalErrorJSONResponse: errInternal(ctx, errTVSearchNotConfigured),
		}, nil
	}
	if err := s.tvSearcher.SearchShow(ctx, request.Id); err != nil {
		return SearchSeries500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
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
			InternalErrorJSONResponse: errInternal(ctx, err),
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
	switch {
	case errors.Is(err, download.ErrUntrustedSource):
		return GrabEpisodeRelease422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	case err != nil:
		return GrabEpisodeRelease500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
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
			InternalErrorJSONResponse: errInternal(ctx, err),
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
	err := s.tvshows.GrabSeasonRelease(
		ctx, request.Id, request.Number, sr, replaceExisting(request.Body),
	)
	switch {
	case errors.Is(err, download.ErrUntrustedSource):
		return GrabSeasonRelease422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	case err != nil:
		return GrabSeasonRelease500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
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
			InternalErrorJSONResponse: errInternal(ctx, err),
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
	err := s.tvshows.GrabSeriesRelease(
		ctx, request.Id, sr, replaceExisting(request.Body),
	)
	switch {
	case errors.Is(err, download.ErrUntrustedSource):
		return GrabSeriesRelease422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	case err != nil:
		return GrabSeriesRelease500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return GrabSeriesRelease202Response{}, nil
}

func (s *Server) GetSeriesPlayOnLinks(
	ctx context.Context,
	request GetSeriesPlayOnLinksRequestObject,
) (GetSeriesPlayOnLinksResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return GetSeriesPlayOnLinks403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	show, err := s.tvshows.Get(ctx, request.Id)
	if err != nil {
		return GetSeriesPlayOnLinks404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	}
	if s.deepLinker == nil {
		return GetSeriesPlayOnLinks500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, errPlayOnNotConfigured),
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

// ApplySpecialsToExisting retro-applies library.monitor_specials to series
// already in the library. Admin only — it is a library-wide bulk mutation
// driven from the settings page.
func (s *Server) ApplySpecialsToExisting(
	ctx context.Context,
	_ ApplySpecialsToExistingRequestObject,
) (ApplySpecialsToExistingResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return ApplySpecialsToExisting403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	n, err := s.tvshows.ApplySpecialsToExisting(ctx)
	if err != nil {
		return ApplySpecialsToExisting500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return ApplySpecialsToExisting200JSONResponse{
		SpecialsMonitoredJSONResponse: SpecialsMonitoredJSONResponse{
			SeasonsUpdated: n,
			Monitored:      config.Get().Library.MonitorSpecials,
		},
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
			InternalErrorJSONResponse: errInternal(ctx, err),
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
			InternalErrorJSONResponse: errInternal(ctx, errRenamerNotConfigured),
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
	switch {
	case errors.Is(err, tvshow.ErrSeriesNotFound):
		return RenameSeriesFiles404JSONResponse{
			NotFoundJSONResponse: errNotFound("series not found"),
		}, nil
	case err != nil:
		return RenameSeriesFiles500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
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
