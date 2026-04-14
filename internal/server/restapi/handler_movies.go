package restapi

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	entmovie "github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
	moviesvc "github.com/datahearth/streamline/internal/media/movie"
	"github.com/datahearth/streamline/internal/metadata"
)

func (s *Server) ListMovies(
	ctx context.Context,
	request ListMoviesRequestObject,
) (ListMoviesResponseObject, error) {
	page := uint16(*request.Params.Page)
	limit := *request.Params.Limit

	movies, total, err := s.movies.List(ctx, page, limit)
	if err != nil {
		return ListMovies500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}

	items := make([]Movie, 0, len(movies))
	for _, m := range movies {
		items = append(items, movieToAPI(m))
	}

	return ListMovies200JSONResponse{
		Items: items,
		Total: total,
		Page:  uint32(page),
		Limit: limit,
	}, nil
}

func (s *Server) GetMovieCounts(
	ctx context.Context,
	_ GetMovieCountsRequestObject,
) (GetMovieCountsResponseObject, error) {
	counts, err := s.movies.Counts(ctx)
	if err != nil {
		return GetMovieCounts500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
	trend := make([]uint32, len(counts.Trend))
	for i, v := range counts.Trend {
		trend[i] = uint32(v)
	}
	return GetMovieCounts200JSONResponse{
		MovieCountsResponseJSONResponse: MovieCountsResponseJSONResponse{
			Total:       uint32(counts.Total),
			Wanted:      uint32(counts.Wanted),
			Downloading: uint32(counts.Downloading),
			Available:   uint32(counts.Available),
			Trend:       trend,
		},
	}, nil
}

func (s *Server) AddMovie(
	ctx context.Context,
	request AddMovieRequestObject,
) (AddMovieResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return AddMovie403JSONResponse{ForbiddenJSONResponse: requestOnlyResp}, nil
	}

	var qpName string
	if request.Body.QualityProfile != nil {
		qpName = *request.Body.QualityProfile
	}

	m, _, err := s.movies.Add(ctx, request.Body.TmdbId, qpName)
	if err != nil {
		return AddMovie409JSONResponse{
			ConflictJSONResponse: errConflict(err.Error()),
		}, nil
	}

	result := movieToAPI(m)
	return AddMovie201JSONResponse(result), nil
}

func (s *Server) GetMovie(
	ctx context.Context,
	request GetMovieRequestObject,
) (GetMovieResponseObject, error) {
	m, err := s.movies.Get(ctx, request.Id)
	if err != nil {
		return GetMovie404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	}
	files, err := s.store.ListMediaFilesByMovieID(ctx, m.ID)
	if err != nil {
		return GetMovie500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
	result := movieToAPI(m)
	if len(files) > 0 {
		apiFiles := make([]MediaFile, 0, len(files))
		for _, f := range files {
			apiFiles = append(apiFiles, mediaFileToAPI(f))
		}
		result.MediaFiles = &apiFiles
	}
	// Cast, genres and rating are fetched live from TMDB on the detail
	// view. A failure here (no API key, transport error) must not fail
	// the whole response — those sections degrade to empty instead.
	if details, derr := s.metadata.GetMovie(ctx, m.TmdbID); derr != nil {
		slog.WarnContext(ctx, "movie detail: metadata fetch failed",
			"movie.id", m.ID, "movie.tmdb_id", m.TmdbID, "error", derr)
	} else {
		if len(details.Cast) > 0 {
			cast := castToAPI(details.Cast)
			result.Cast = &cast
		}
		if len(details.Genres) > 0 {
			genres := details.Genres
			result.Genres = &genres
		}
		if details.Rating > 0 {
			rating := details.Rating
			result.Rating = &rating
		}
	}
	return GetMovie200JSONResponse(result), nil
}

func (s *Server) PatchMovie(
	ctx context.Context,
	request PatchMovieRequestObject,
) (PatchMovieResponseObject, error) {
	var params moviesvc.UpdateParams
	if request.Body.Status != nil {
		st := entmovie.Status(*request.Body.Status)
		params.Status = &st
	}
	params.QualityProfile = request.Body.QualityProfile
	params.Monitored = request.Body.Monitored

	m, err := s.movies.Update(ctx, request.Id, params)
	if err != nil {
		return PatchMovie404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	}

	return PatchMovie200JSONResponse(movieToAPI(m)), nil
}

func (s *Server) DeleteMovie(
	ctx context.Context,
	request DeleteMovieRequestObject,
) (DeleteMovieResponseObject, error) {
	opts := moviesvc.DeleteOptions{}
	if request.Params.DeleteFiles != nil {
		opts.DeleteFiles = *request.Params.DeleteFiles
	}
	if err := s.movies.Delete(ctx, request.Id, opts); err != nil {
		return DeleteMovie404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	}
	return DeleteMovie204Response{}, nil
}

func (s *Server) DeleteMovieFile(
	ctx context.Context,
	request DeleteMovieFileRequestObject,
) (DeleteMovieFileResponseObject, error) {
	remove := request.Body != nil &&
		request.Body.RemoveTorrent != nil &&
		*request.Body.RemoveTorrent
	if err := s.movies.DeleteFile(ctx, request.Id, request.FileId,
		moviesvc.DeleteFileOptions{RemoveTorrent: remove}); err != nil {
		return DeleteMovieFile404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	}
	return DeleteMovieFile204Response{}, nil
}

func (s *Server) SearchMovieNow(
	ctx context.Context,
	request SearchMovieNowRequestObject,
) (SearchMovieNowResponseObject, error) {
	m, err := s.movies.Get(ctx, request.Id)
	if err != nil {
		return SearchMovieNow404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	}
	if s.missingSearcher == nil {
		return SearchMovieNow500JSONResponse{
			InternalErrorJSONResponse: errInternal("search not configured"),
		}, nil
	}
	if err := s.missingSearcher.SearchOne(ctx, m); err != nil {
		return SearchMovieNow500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
	return SearchMovieNow202JSONResponse{
		MovieSearchAcceptedJSONResponse: MovieSearchAcceptedJSONResponse{
			MovieId:      request.Id,
			DispatchedAt: time.Now().UTC(),
		},
	}, nil
}

func (s *Server) SearchMovie(
	ctx context.Context,
	request SearchMovieRequestObject,
) (SearchMovieResponseObject, error) {
	m, err := s.movies.Get(ctx, request.Id)
	if err != nil {
		return SearchMovie404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	}

	results, err := s.indexers.SearchMovie(
		ctx,
		[]string{m.Title, m.OriginalTitle},
		m.TmdbID,
	)
	if err != nil {
		return SearchMovie500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}

	items := make([]SearchResult, 0, len(results))
	for _, r := range results {
		item := SearchResult{
			Title:       r.Title,
			DownloadUrl: r.Download,
			Size:        r.Size,
			Seeders:     r.Seeders,
		}
		parsed := library.Parse(filepath.Base(r.Title))
		if r.InfoURL != "" {
			item.InfoUrl = &r.InfoURL
		}
		if r.Leechers > 0 {
			item.Leechers = &r.Leechers
		}
		if !r.PublishDate.IsZero() {
			pub := r.PublishDate
			item.PublishedAt = &pub
		}
		if r.Indexer != "" {
			idx := r.Indexer
			item.Indexer = &idx
		}
		if parsed.Group != "" {
			g := parsed.Group
			item.ReleaseGroup = &g
		}
		if parsed.Resolution != "" {
			res := parsed.Resolution
			item.Resolution = &res
		}
		if parsed.Source != "" {
			src := parsed.Source
			item.Source = &src
		}
		if parsed.Codec != "" {
			cdc := parsed.Codec
			item.Codec = &cdc
		}
		items = append(items, item)
	}

	return SearchMovie200JSONResponse(items), nil
}

func (s *Server) GetMoviePlayOnLinks(
	ctx context.Context,
	request GetMoviePlayOnLinksRequestObject,
) (GetMoviePlayOnLinksResponseObject, error) {
	m, err := s.movies.Get(ctx, request.Id)
	if err != nil {
		return GetMoviePlayOnLinks404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	}
	if s.deepLinker == nil {
		return GetMoviePlayOnLinks500JSONResponse{
			InternalErrorJSONResponse: errInternal(
				"play-on resolver not configured",
			),
		}, nil
	}
	results := s.deepLinker.Resolve(ctx, m.TmdbID, m.Title, m.Year)
	items := make([]PlayOnLink, 0, len(results))
	for _, r := range results {
		items = append(items, playOnToAPI(r))
	}
	return GetMoviePlayOnLinks200JSONResponse{
		MoviePlayOnLinksJSONResponse: MoviePlayOnLinksJSONResponse{Items: items},
	}, nil
}

func (s *Server) GrabMovieRelease(
	ctx context.Context,
	request GrabMovieReleaseRequestObject,
) (GrabMovieReleaseResponseObject, error) {
	m, err := s.movies.Get(ctx, request.Id)
	if err != nil {
		return GrabMovieRelease404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	}
	if request.Body == nil ||
		request.Body.DownloadUrl == "" ||
		request.Body.Title == "" {
		return GrabMovieRelease422JSONResponse{
			UnprocessableEntityJSONResponse: unprocessableResp(
				"release title and download_url are required",
			),
		}, nil
	}
	sr := indexer.SearchResult{
		Title:    request.Body.Title,
		Download: request.Body.DownloadUrl,
		Size:     request.Body.Size,
		Seeders:  request.Body.Seeders,
	}
	if request.Body.InfoUrl != nil {
		sr.InfoURL = *request.Body.InfoUrl
	}
	if request.Body.Leechers != nil {
		sr.Leechers = *request.Body.Leechers
	}
	rec, err := s.downloads.Grab(ctx, sr, m.ID)
	if err != nil {
		return GrabMovieRelease500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
	if replaceExisting(request.Body) {
		if err := s.store.MarkDownloadRecordReplaceExisting(
			ctx,
			rec.ID,
		); err != nil {
			slog.WarnContext(ctx, "grab movie: mark replace-existing failed",
				"download_record.id", rec.ID, "error", err)
		}
	}
	return GrabMovieRelease202JSONResponse{
		MovieSearchAcceptedJSONResponse: MovieSearchAcceptedJSONResponse{
			MovieId:      request.Id,
			DispatchedAt: time.Now().UTC(),
		},
	}, nil
}

func (s *Server) RefreshMovieMetadata(
	ctx context.Context,
	request RefreshMovieMetadataRequestObject,
) (RefreshMovieMetadataResponseObject, error) {
	m, err := s.movies.RefreshOne(ctx, request.Id)
	if err != nil {
		return RefreshMovieMetadata500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
	if m == nil {
		return RefreshMovieMetadata404JSONResponse{
			NotFoundJSONResponse: errNotFound("movie not found"),
		}, nil
	}
	return RefreshMovieMetadata200JSONResponse{
		MovieRefreshedJSONResponse: MovieRefreshedJSONResponse(movieToAPI(m)),
	}, nil
}

func (s *Server) RenameMovieFiles(
	ctx context.Context,
	request RenameMovieFilesRequestObject,
) (RenameMovieFilesResponseObject, error) {
	if s.renamer == nil {
		return RenameMovieFiles500JSONResponse{
			InternalErrorJSONResponse: errInternal("renamer not configured"),
		}, nil
	}
	preview := request.Params.Preview != nil && *request.Params.Preview
	var plan library.RenamePlan
	var err error
	if preview {
		plan, err = s.renamer.Preview(ctx, request.Id)
	} else {
		plan, err = s.renamer.Apply(ctx, request.Id)
	}
	if err != nil {
		return RenameMovieFiles500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
	out := RenamePlan{
		MovieId:    request.Id,
		Operations: make([]RenameOperation, 0, len(plan.Operations)),
	}
	for _, op := range plan.Operations {
		out.Operations = append(out.Operations, RenameOperation{
			MediaFileId: op.MediaFileID,
			From:        op.From,
			To:          op.To,
		})
	}
	return RenameMovieFiles200JSONResponse{
		MovieRenamePlanJSONResponse: MovieRenamePlanJSONResponse(out),
	}, nil
}

func (s *Server) GetMovieRecommendations(
	ctx context.Context,
	request GetMovieRecommendationsRequestObject,
) (GetMovieRecommendationsResponseObject, error) {
	m, err := s.movies.Get(ctx, request.Id)
	if err != nil {
		return GetMovieRecommendations404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	}

	results, err := s.metadata.Recommendations(ctx, m.TmdbID)
	if err != nil {
		return GetMovieRecommendations500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}

	items := make([]TMDBMovieResult, 0, len(results))
	for _, r := range results {
		item := TMDBMovieResult{
			TmdbId:        r.TMDBID,
			Title:         r.Title,
			OriginalTitle: r.OriginalTitle,
			Year:          r.Year,
		}
		if r.Overview != "" {
			item.Overview = &r.Overview
		}
		if url := metadata.PosterURL(r.PosterPath, "w342"); url != "" {
			item.PosterUrl = &url
		}
		items = append(items, item)
	}

	return GetMovieRecommendations200JSONResponse{
		MovieRecommendationsJSONResponse: MovieRecommendationsJSONResponse{
			Items: items,
		},
	}, nil
}

func (s *Server) SearchTMDBMovie(
	ctx context.Context,
	request SearchTMDBMovieRequestObject,
) (SearchTMDBMovieResponseObject, error) {
	var year uint16
	if request.Params.Year != nil {
		year = *request.Params.Year
	}

	results, err := s.metadata.SearchMovie(ctx, request.Params.Q, year)
	if err != nil {
		return SearchTMDBMovie500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}

	items := make([]TMDBMovieResult, 0, len(results))
	for _, r := range results {
		item := TMDBMovieResult{
			TmdbId:        r.TMDBID,
			Title:         r.Title,
			OriginalTitle: r.OriginalTitle,
			Year:          r.Year,
		}
		if r.Overview != "" {
			item.Overview = &r.Overview
		}
		if url := metadata.PosterURL(r.PosterPath, "w185"); url != "" {
			item.PosterUrl = &url
		}
		items = append(items, item)
	}

	return SearchTMDBMovie200JSONResponse(items), nil
}
