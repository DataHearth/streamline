package restapi

import (
	"context"

	"github.com/datahearth/streamline/ent/request"
	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/utils/numeric"
)

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
