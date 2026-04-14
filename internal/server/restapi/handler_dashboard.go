package restapi

import (
	"context"
	"strings"

	"github.com/datahearth/streamline/ent/movieevent"
	"github.com/datahearth/streamline/internal/db"
)

func (s *Server) ListActivity(
	ctx context.Context,
	req ListActivityRequestObject,
) (ListActivityResponseObject, error) {
	f := db.ActivityFilter{}

	if req.Params.Type != nil {
		for _, t := range *req.Params.Type {
			f.Types = append(f.Types, movieevent.Type(t))
		}
	}
	if req.Params.MovieId != nil {
		id := *req.Params.MovieId
		f.MovieID = &id
	}
	if req.Params.Since != nil {
		t := *req.Params.Since
		f.Since = &t
	}
	if req.Params.Before != nil {
		t := *req.Params.Before
		f.Before = &t
	}
	if req.Params.Limit != nil {
		f.Limit = *req.Params.Limit
	}
	if req.Params.Cursor != nil {
		f.Cursor = *req.Params.Cursor
	}

	res, err := s.store.RecentActivity(ctx, f)
	if err != nil {
		// db.RecentActivity wraps cursor decode failures with the literal
		// "decode cursor". Translate to 400 — anything else is a true 5xx.
		if strings.Contains(err.Error(), "decode cursor") {
			return ListActivity400JSONResponse{
				BadRequestJSONResponse: errBadRequest("invalid cursor"),
			}, nil
		}
		return nil, err
	}

	out := ActivityList{
		Events: make([]ActivityEvent, 0, len(res.Events)),
	}
	for _, e := range res.Events {
		out.Events = append(out.Events, toActivityEvent(e))
	}
	if res.NextCursor != "" {
		c := res.NextCursor
		out.NextCursor = &c
	}
	return ListActivity200JSONResponse{
		ActivityListJSONResponse: ActivityListJSONResponse(out),
	}, nil
}

func (s *Server) ListUpcomingReleases(
	ctx context.Context,
	req ListUpcomingReleasesRequestObject,
) (ListUpcomingReleasesResponseObject, error) {
	if !req.Params.From.Before(req.Params.To) {
		return ListUpcomingReleases400JSONResponse{
			BadRequestJSONResponse: errBadRequest("from must be before to"),
		}, nil
	}
	movies, err := s.store.UpcomingReleases(ctx, req.Params.From, req.Params.To)
	if err != nil {
		return nil, err
	}
	episodes, err := s.store.ListUpcomingEpisodes(
		ctx,
		req.Params.From,
		req.Params.To,
	)
	if err != nil {
		return nil, err
	}
	out := UpcomingList{
		Movies:   make([]UpcomingMovie, 0, len(movies)),
		Episodes: make([]UpcomingEpisode, 0, len(episodes)),
	}
	for _, m := range movies {
		out.Movies = append(out.Movies, toUpcomingMovie(m))
	}
	for _, e := range episodes {
		out.Episodes = append(out.Episodes, toUpcomingEpisode(e))
	}
	return ListUpcomingReleases200JSONResponse{
		UpcomingListJSONResponse: UpcomingListJSONResponse(out),
	}, nil
}
