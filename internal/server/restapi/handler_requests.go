package restapi

import (
	"context"
	"errors"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/request"
	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/metadata"
	requestsvc "github.com/datahearth/streamline/internal/request"
)

func (s *Server) ListRequests(
	ctx context.Context,
	req ListRequestsRequestObject,
) (ListRequestsResponseObject, error) {
	claims := auth.ClaimsFromContext(ctx)
	if claims == nil || claims.UserID == 0 {
		return ListRequests401JSONResponse{
			UnauthorizedJSONResponse: unauthorizedResp("login required"),
		}, nil
	}

	p := db.ListRequestsParams{Limit: 50}
	if req.Params.Status != nil {
		p.Status = string(*req.Params.Status)
	}
	if req.Params.MediaType != nil {
		p.MediaType = string(*req.Params.MediaType)
	}
	if req.Params.Limit != nil && *req.Params.Limit > 0 {
		p.Limit = clampLimit(*req.Params.Limit, requestsMaxLimit)
	}
	if req.Params.Page != nil && *req.Params.Page > 1 {
		p.Offset = uint32(*req.Params.Page-1) * p.Limit
	}
	// Reviewers (admin/member) see all requests; request_only sees only theirs.
	if claims.Role == "request_only" {
		p.RequesterID = claims.UserID
	}

	rows, total, err := s.requests.List(ctx, p)
	if err != nil {
		return ListRequests500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	items := make([]Request, 0, len(rows))
	for _, r := range rows {
		items = append(items, requestToAPI(r))
	}
	page := uint32(1)
	if req.Params.Page != nil && *req.Params.Page > 0 {
		page = uint32(*req.Params.Page)
	}
	return ListRequests200JSONResponse{
		RequestsListJSONResponse: RequestsListJSONResponse{
			Items: items,
			Total: total,
			Page:  page,
			Limit: p.Limit,
		},
	}, nil
}

func (s *Server) CreateRequest(
	ctx context.Context,
	req CreateRequestRequestObject,
) (CreateRequestResponseObject, error) {
	claims := auth.ClaimsFromContext(ctx)
	if claims == nil || claims.UserID == 0 {
		return CreateRequest401JSONResponse{
			UnauthorizedJSONResponse: unauthorizedResp("login required"),
		}, nil
	}
	qualityProfile := ""
	if req.Body.QualityProfile != nil {
		qualityProfile = *req.Body.QualityProfile
	}
	r, err := s.requests.Create(
		ctx,
		string(req.Body.MediaType),
		req.Body.MediaId,
		req.Body.Title,
		claims.UserID,
		qualityProfile,
	)
	if errors.Is(err, requestsvc.ErrDuplicate) {
		return CreateRequest409JSONResponse{
			ConflictJSONResponse: conflictResp(
				"duplicate",
				"already requested or in library",
			),
		}, nil
	}
	if err != nil {
		return CreateRequest500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return CreateRequest201JSONResponse{
		RequestCreatedJSONResponse: RequestCreatedJSONResponse(requestToAPI(r)),
	}, nil
}

func (s *Server) GetRequestCounts(
	ctx context.Context,
	_ GetRequestCountsRequestObject,
) (GetRequestCountsResponseObject, error) {
	if claims := auth.ClaimsFromContext(ctx); claims == nil ||
		claims.UserID == 0 {
		return GetRequestCounts401JSONResponse{
			UnauthorizedJSONResponse: unauthorizedResp("login required"),
		}, nil
	}
	count := func(st request.Status) int {
		n, err := s.store.CountRequestsByStatus(ctx, st)
		if err != nil {
			return 0
		}
		return n
	}
	return GetRequestCounts200JSONResponse{
		RequestCountsResponseJSONResponse: RequestCountsResponseJSONResponse{
			Pending:   count(request.StatusPending),
			Approved:  count(request.StatusApproved),
			Denied:    count(request.StatusDenied),
			Available: count(request.StatusAvailable),
		},
	}, nil
}

func (s *Server) ApproveRequest(
	ctx context.Context,
	req ApproveRequestRequestObject,
) (ApproveRequestResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return ApproveRequest403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	qualityProfile := ""
	if req.Body != nil && req.Body.QualityProfile != nil {
		qualityProfile = *req.Body.QualityProfile
	}
	claims := auth.ClaimsFromContext(ctx)
	r, err := s.requests.Approve(ctx, req.Id, claims.UserID, qualityProfile)
	switch {
	case errors.Is(err, requestsvc.ErrRequestNotFound):
		return ApproveRequest404JSONResponse{
			NotFoundJSONResponse: errNotFound("request not found"),
		}, nil
	case err != nil:
		return ApproveRequest500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return ApproveRequest200JSONResponse{
		RequestDetailJSONResponse: RequestDetailJSONResponse(requestToAPI(r)),
	}, nil
}

func (s *Server) DenyRequest(
	ctx context.Context,
	req DenyRequestRequestObject,
) (DenyRequestResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return DenyRequest403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	claims := auth.ClaimsFromContext(ctx)
	reason := ""
	if req.Body != nil && req.Body.Reason != nil {
		reason = *req.Body.Reason
	}
	r, err := s.requests.Deny(ctx, req.Id, claims.UserID, reason)
	switch {
	case errors.Is(err, requestsvc.ErrRequestNotFound):
		return DenyRequest404JSONResponse{
			NotFoundJSONResponse: errNotFound("request not found"),
		}, nil
	case err != nil:
		return DenyRequest500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return DenyRequest200JSONResponse{
		RequestDetailJSONResponse: RequestDetailJSONResponse(requestToAPI(r)),
	}, nil
}

func (s *Server) ReopenRequest(
	ctx context.Context,
	req ReopenRequestRequestObject,
) (ReopenRequestResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return ReopenRequest403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	r, err := s.requests.Reopen(ctx, req.Id)
	switch {
	case errors.Is(err, requestsvc.ErrRequestNotFound):
		return ReopenRequest404JSONResponse{
			NotFoundJSONResponse: errNotFound("request not found"),
		}, nil
	case err != nil:
		return ReopenRequest500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return ReopenRequest200JSONResponse{
		RequestDetailJSONResponse: RequestDetailJSONResponse(requestToAPI(r)),
	}, nil
}

// GetRequestMetadata fetches poster/overview for the requested item so
// reviewers can judge it. Reviewers (admin/member) see any request; a
// request_only user sees only their own.
func (s *Server) GetRequestMetadata(
	ctx context.Context,
	req GetRequestMetadataRequestObject,
) (GetRequestMetadataResponseObject, error) {
	claims := auth.ClaimsFromContext(ctx)
	if claims == nil || claims.UserID == 0 {
		return GetRequestMetadata401JSONResponse{
			UnauthorizedJSONResponse: unauthorizedResp("login required"),
		}, nil
	}
	r, err := s.requests.Get(ctx, req.Id)
	if ent.IsNotFound(err) {
		return GetRequestMetadata404JSONResponse{
			NotFoundJSONResponse: notFoundResp("request not found"),
		}, nil
	}
	if err != nil {
		return GetRequestMetadata500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	if claims.Role == "request_only" {
		if u := r.Edges.Requester; u == nil || u.ID != claims.UserID {
			return GetRequestMetadata403JSONResponse{
				ForbiddenJSONResponse: forbiddenResp("not your request"),
			}, nil
		}
	}

	var details RequestMediaDetails
	switch r.MediaType {
	case "movie":
		d, err := s.metadata.GetMovie(ctx, r.MediaID)
		if err != nil {
			return GetRequestMetadata500JSONResponse{
				InternalErrorJSONResponse: errInternal(ctx, err),
			}, nil
		}
		details = movieDetailsToRequestMedia(d)
	case "tvshow":
		d, err := s.seriesWithCast(ctx, r.MediaID)
		if err != nil {
			return GetRequestMetadata500JSONResponse{
				InternalErrorJSONResponse: errInternal(ctx, err),
			}, nil
		}
		details = seriesDetailsToRequestMedia(d)
	}
	return GetRequestMetadata200JSONResponse{
		RequestMediaDetailResponseJSONResponse: RequestMediaDetailResponseJSONResponse(
			details,
		),
	}, nil
}

// RequestMediaDetails is LookupDetail plus the poster and year, so the request
// row and the add/request lookup endpoints share one mapping per provider and
// project it down with toLookupDetail.
func movieDetailsToRequestMedia(d *metadata.MovieDetails) RequestMediaDetails {
	out := RequestMediaDetails{}
	if url := metadata.PosterURL(d.PosterPath, "w342"); url != "" {
		out.PosterUrl = &url
	}
	if d.Year != 0 {
		out.Year = &d.Year
	}
	if d.Overview != "" {
		out.Overview = &d.Overview
	}
	if d.Tagline != "" {
		out.Tagline = &d.Tagline
	}
	if d.ReleaseDate != "" {
		out.ReleaseDate = &d.ReleaseDate
	}
	if d.OriginalLanguage != "" {
		out.OriginalLanguage = &d.OriginalLanguage
	}
	if d.IMDbID != "" {
		out.ImdbId = &d.IMDbID
	}
	if d.TMDBID != 0 {
		out.TmdbId = &d.TMDBID
	}
	if d.Rating != 0 {
		out.Rating = &d.Rating
	}
	if d.VoteCount != 0 {
		out.VoteCount = &d.VoteCount
	}
	if d.Runtime != 0 {
		out.Runtime = &d.Runtime
	}
	if len(d.Genres) != 0 {
		out.Genres = &d.Genres
	}
	if cast := castToAPI(d.Cast); len(cast) != 0 {
		out.Cast = &cast
	}
	return out
}

func seriesDetailsToRequestMedia(d *metadata.TVDetails) RequestMediaDetails {
	out := RequestMediaDetails{}
	if url := metadata.TVDBArtworkURL(d.PosterPath); url != "" {
		out.PosterUrl = &url
	}
	if d.Year != 0 {
		out.Year = &d.Year
	}
	if d.Overview != "" {
		out.Overview = &d.Overview
	}
	if d.FirstAired != "" {
		out.ReleaseDate = &d.FirstAired
	}
	if d.IMDbID != "" {
		out.ImdbId = &d.IMDbID
	}
	if d.TVDBID != 0 {
		out.TvdbId = &d.TVDBID
	}
	if d.Network != "" {
		out.Network = &d.Network
	}
	if d.Status != "" {
		out.Status = &d.Status
	}
	if d.Rating != 0 {
		out.Rating = &d.Rating
	}
	if d.Runtime != 0 {
		out.Runtime = &d.Runtime
	}
	if len(d.Genres) != 0 {
		out.Genres = &d.Genres
	}
	if cast := castToAPI(d.Cast); len(cast) != 0 {
		out.Cast = &cast
	}
	if n := len(d.Seasons); n != 0 {
		out.SeasonCount = &n
	}
	if n := len(d.Episodes); n != 0 {
		out.EpisodeCount = &n
	}
	return out
}

func toLookupDetail(d RequestMediaDetails) LookupDetail {
	return LookupDetail{
		Cast:             d.Cast,
		EpisodeCount:     d.EpisodeCount,
		Genres:           d.Genres,
		ImdbId:           d.ImdbId,
		Network:          d.Network,
		OriginalLanguage: d.OriginalLanguage,
		Overview:         d.Overview,
		Rating:           d.Rating,
		ReleaseDate:      d.ReleaseDate,
		Runtime:          d.Runtime,
		SeasonCount:      d.SeasonCount,
		Status:           d.Status,
		Tagline:          d.Tagline,
		TmdbId:           d.TmdbId,
		TvdbId:           d.TvdbId,
		VoteCount:        d.VoteCount,
	}
}
