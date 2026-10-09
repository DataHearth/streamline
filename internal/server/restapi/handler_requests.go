package restapi

import (
	"context"
	"errors"
	"fmt"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/request"
	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/media/book"
	"github.com/datahearth/streamline/internal/metadata"
	requestsvc "github.com/datahearth/streamline/internal/request"
	"github.com/datahearth/streamline/internal/utils/numeric"
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

	page, ok := positiveOr(req.Params.Page, uint16(1))
	if !ok {
		return ListRequests400JSONResponse{
			BadRequestJSONResponse: errBadRequest(msgZeroPage),
		}, nil
	}
	limit, ok := limitOr(req.Params.Limit, 50, requestsMaxLimit)
	if !ok {
		return ListRequests400JSONResponse{
			BadRequestJSONResponse: errBadRequest(limitRangeMsg(requestsMaxLimit)),
		}, nil
	}
	p := db.ListRequestsParams{Limit: limit}
	if req.Params.Status != nil {
		p.Status = string(*req.Params.Status)
	}
	if req.Params.MediaType != nil {
		for _, t := range *req.Params.MediaType {
			if !t.Valid() {
				return ListRequests400JSONResponse{
					BadRequestJSONResponse: errBadRequest(
						"unknown media_type " + string(t),
					),
				}, nil
			}
			p.MediaTypes = append(p.MediaTypes, string(t))
		}
	}
	p.Offset = uint32(page-1) * p.Limit
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
	return ListRequests200JSONResponse{
		Items: items,
		Total: numeric.SaturateU32(total),
		Page:  uint32(page),
		Limit: p.Limit,
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
	if !req.Body.MediaType.Valid() {
		return CreateRequest400JSONResponse{
			BadRequestJSONResponse: errBadRequest("unknown media_type"),
		}, nil
	}
	var mediaID uint32
	if req.Body.MediaId != nil {
		mediaID = *req.Body.MediaId
	}
	r, err := s.requests.Create(ctx, requestsvc.CreateParams{
		MediaType:      string(req.Body.MediaType),
		MediaID:        mediaID,
		MediaMBID:      deref(req.Body.MediaMbid),
		Title:          req.Body.Title,
		RequesterID:    claims.UserID,
		QualityProfile: deref(req.Body.QualityProfile),
	})
	switch {
	case errors.Is(err, requestsvc.ErrDuplicate):
		return CreateRequest409JSONResponse{
			ConflictJSONResponse: conflictResp(
				"duplicate",
				"already requested or in library",
			),
		}, nil
	case errors.Is(err, requestsvc.ErrInvalidRequest):
		return CreateRequest400JSONResponse{
			BadRequestJSONResponse: errBadRequest(err.Error()),
		}, nil
	case err != nil:
		return CreateRequest500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return CreateRequest201JSONResponse{
		RequestCreatedJSONResponse: RequestCreatedJSONResponse(requestToAPI(r)),
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
	if req.Body != nil {
		qualityProfile = deref(req.Body.QualityProfile)
	}
	claims := auth.ClaimsFromContext(ctx)
	r, err := s.requests.Approve(ctx, req.Id, claims.UserID, qualityProfile)
	switch {
	case errors.Is(err, requestsvc.ErrRequestNotFound):
		return ApproveRequest404JSONResponse{
			NotFoundJSONResponse: errNotFound("request not found"),
		}, nil
	case errors.Is(err, requestsvc.ErrUnknownProfile):
		return ApproveRequest422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	case errors.Is(err, metadata.ErrRateLimited):
		return ApproveRequest429JSONResponse{
			RateLimitedJSONResponse: errRateLimited(err),
		}, nil
	case errors.Is(err, book.ErrNotConfigured):
		return ApproveRequest503JSONResponse{
			ServiceUnavailableJSONResponse: errServiceUnavailable(err.Error()),
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
	if req.Body != nil {
		reason = deref(req.Body.Reason)
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

// requestToAPI maps a request row. Requester and approver come from the edges
// the query loaded.
func requestToAPI(r *ent.Request) Request {
	out := Request{
		Id:        r.ID,
		MediaType: RequestMediaType(r.MediaType),
		MediaId:   r.MediaID,
		Title:     r.Title,
		Status:    RequestStatus(r.Status),
		CreatedAt: r.CreateTime,
		UpdatedAt: r.UpdateTime,
	}
	if r.Reason != "" {
		out.Reason = &r.Reason
	}
	if r.QualityProfile != "" {
		out.QualityProfile = &r.QualityProfile
	}
	if r.MediaMbid != "" {
		out.MediaMbid = &r.MediaMbid
	}
	if u := r.Edges.Requester; u != nil {
		out.Requester = requestUserToAPI(u)
	}
	if u := r.Edges.ApprovedBy; u != nil {
		au := requestUserToAPI(u)
		out.ApprovedBy = &au
	}
	return out
}

// GetRequestMetadata fetches poster/overview for the requested item so
// reviewers can judge it. Reviewers (admin/member) see any request; a
// request_only user sees only their own. An artist, book or series request is
// answered by the same operation that serves its lookup detail, so the request
// panel and the add flow can never disagree, and share its cache.
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

	var out RequestMetadata
	switch r.MediaType {
	case "movie":
		d, err := s.metadata.GetMovie(ctx, r.MediaID)
		if err != nil {
			return GetRequestMetadata500JSONResponse{
				InternalErrorJSONResponse: errInternal(ctx, err),
			}, nil
		}
		err = out.FromRequestMediaDetails(movieDetailsToRequestMedia(d))
		if err != nil {
			return GetRequestMetadata500JSONResponse{
				InternalErrorJSONResponse: errInternal(ctx, err),
			}, nil
		}
	case "tvshow":
		d, err := s.seriesWithCast(ctx, r.MediaID)
		if err != nil {
			return GetRequestMetadata500JSONResponse{
				InternalErrorJSONResponse: errInternal(ctx, err),
			}, nil
		}
		err = out.FromRequestMediaDetails(seriesDetailsToRequestMedia(d))
		if err != nil {
			return GetRequestMetadata500JSONResponse{
				InternalErrorJSONResponse: errInternal(ctx, err),
			}, nil
		}
	case "artist":
		resp, err := s.GetMusicArtistLookup(ctx, GetMusicArtistLookupRequestObject{
			Mbid: r.MediaMbid,
		})
		if err != nil {
			return GetRequestMetadata500JSONResponse{
				InternalErrorJSONResponse: errInternal(ctx, err),
			}, nil
		}
		switch v := resp.(type) {
		case GetMusicArtistLookup200JSONResponse:
			err = out.FromMusicArtistLookupDetail(
				MusicArtistLookupDetail(
					v.MusicArtistLookupDetailResponseJSONResponse,
				),
			)
			if err != nil {
				return GetRequestMetadata500JSONResponse{
					InternalErrorJSONResponse: errInternal(ctx, err),
				}, nil
			}
		case GetMusicArtistLookup404JSONResponse:
			return GetRequestMetadata404JSONResponse(v), nil
		case GetMusicArtistLookup429JSONResponse:
			return GetRequestMetadata429JSONResponse(v), nil
		case GetMusicArtistLookup500JSONResponse:
			return GetRequestMetadata500JSONResponse(v), nil
		default:
			return GetRequestMetadata500JSONResponse{
				InternalErrorJSONResponse: errInternal(
					ctx, fmt.Errorf("unexpected artist lookup response %T", resp),
				),
			}, nil
		}
	case "book", "book_series":
		lookupType := BookShelfTypeBook
		if r.MediaType == "book_series" {
			lookupType = BookShelfTypeSeries
		}
		resp, err := s.GetBookLookup(ctx, GetBookLookupRequestObject{
			HardcoverId: r.MediaID,
			Params:      GetBookLookupParams{Type: lookupType},
		})
		if err != nil {
			return GetRequestMetadata500JSONResponse{
				InternalErrorJSONResponse: errInternal(ctx, err),
			}, nil
		}
		switch v := resp.(type) {
		case GetBookLookup200JSONResponse:
			err = out.FromBookLookupDetail(
				BookLookupDetail(v.BookLookupDetailResponseJSONResponse),
			)
			if err != nil {
				return GetRequestMetadata500JSONResponse{
					InternalErrorJSONResponse: errInternal(ctx, err),
				}, nil
			}
		case GetBookLookup404JSONResponse:
			return GetRequestMetadata404JSONResponse(v), nil
		case GetBookLookup429JSONResponse:
			return GetRequestMetadata429JSONResponse(v), nil
		case GetBookLookup503JSONResponse:
			return GetRequestMetadata503JSONResponse(v), nil
		case GetBookLookup500JSONResponse:
			return GetRequestMetadata500JSONResponse(v), nil
		default:
			return GetRequestMetadata500JSONResponse{
				InternalErrorJSONResponse: errInternal(
					ctx, fmt.Errorf("unexpected book lookup response %T", resp),
				),
			}, nil
		}
	}
	return GetRequestMetadata200JSONResponse{
		RequestMetadataResponseJSONResponse: RequestMetadataResponseJSONResponse(
			out,
		),
	}, nil
}

func (s *Server) GetRequestCounts(
	ctx context.Context,
	_ GetRequestCountsRequestObject,
) (GetRequestCountsResponseObject, error) {
	claims := auth.ClaimsFromContext(ctx)
	if claims == nil || claims.UserID == 0 {
		return GetRequestCounts401JSONResponse{
			UnauthorizedJSONResponse: unauthorizedResp("login required"),
		}, nil
	}
	// Scoped like ListRequests, or the badges count rows the list never shows.
	var requesterID uint32
	if claims.Role == "request_only" {
		requesterID = claims.UserID
	}
	count := func(st request.Status) int {
		n, err := s.store.CountRequestsByStatus(ctx, st, requesterID)
		if err != nil {
			return 0
		}
		return n
	}
	return GetRequestCounts200JSONResponse{
		Pending:   count(request.StatusPending),
		Approved:  count(request.StatusApproved),
		Denied:    count(request.StatusDenied),
		Available: count(request.StatusAvailable),
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
		c := numeric.SaturateU16(n)
		out.SeasonCount = &c
	}
	if n := len(d.Episodes); n != 0 {
		c := numeric.SaturateU16(n)
		out.EpisodeCount = &c
	}
	return out
}

func toLookupDetail(d RequestMediaDetails) LookupDetail {
	return LookupDetail{
		Cast:             d.Cast,
		EpisodeCount:     d.EpisodeCount,
		Genres:           d.Genres,
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
