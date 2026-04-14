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
		p.Limit = *req.Params.Limit
	}
	if req.Params.Page != nil && *req.Params.Page > 1 {
		p.Offset = (*req.Params.Page - 1) * p.Limit
	}
	// Reviewers (admin/member) see all requests; request_only sees only theirs.
	if claims.Role == "request_only" {
		p.RequesterID = claims.UserID
	}

	rows, total, err := s.requests.List(ctx, p)
	if err != nil {
		return ListRequests500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
	items := make([]Request, 0, len(rows))
	for _, r := range rows {
		items = append(items, requestToAPI(r))
	}
	page := uint32(1)
	if req.Params.Page != nil && *req.Params.Page > 0 {
		page = *req.Params.Page
	}
	return ListRequests200JSONResponse{
		RequestsListJSONResponse: RequestsListJSONResponse{
			Items: items,
			Total: uint32(total),
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
	r, err := s.requests.Create(
		ctx,
		string(req.Body.MediaType),
		req.Body.MediaId,
		req.Body.Title,
		claims.UserID,
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
			InternalErrorJSONResponse: errInternal(err.Error()),
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
	if err != nil {
		return ApproveRequest500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
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
	if err != nil {
		return DenyRequest500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
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
	if err != nil {
		return ReopenRequest500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
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
			InternalErrorJSONResponse: errInternal(err.Error()),
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
				InternalErrorJSONResponse: errInternal(err.Error()),
			}, nil
		}
		details = movieDetailsToRequestMedia(d)
	case "tvshow":
		d, err := s.metadataTV.GetSeries(ctx, r.MediaID)
		if err != nil {
			return GetRequestMetadata500JSONResponse{
				InternalErrorJSONResponse: errInternal(err.Error()),
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

func movieDetailsToRequestMedia(d *metadata.MovieDetails) RequestMediaDetails {
	out := RequestMediaDetails{Overview: d.Overview}
	if url := metadata.PosterURL(d.PosterPath, "w342"); url != "" {
		out.PosterUrl = &url
	}
	if d.Year != 0 {
		out.Year = &d.Year
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
	return out
}

func seriesDetailsToRequestMedia(d *metadata.TVDetails) RequestMediaDetails {
	out := RequestMediaDetails{Overview: d.Overview}
	if url := metadata.TVDBArtworkURL(d.PosterPath); url != "" {
		out.PosterUrl = &url
	}
	if d.Year != 0 {
		out.Year = &d.Year
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
	return out
}
