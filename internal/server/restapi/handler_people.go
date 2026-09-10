package restapi

import (
	"context"
	"errors"

	"github.com/datahearth/streamline/internal/db"
)

func (s *Server) ListPeople(
	ctx context.Context,
	request ListPeopleRequestObject,
) (ListPeopleResponseObject, error) {
	limit, ok := limitOr(request.Params.Limit, 20, peopleMaxLimit)
	if !ok {
		return ListPeople400JSONResponse{
			BadRequestJSONResponse: errBadRequest(limitRangeMsg(peopleMaxLimit)),
		}, nil
	}
	p := db.ListPeopleParams{Limit: limit}
	if request.Params.Query != nil {
		p.Query = *request.Params.Query
	}
	if request.Params.Offset != nil {
		p.Offset = *request.Params.Offset
	}

	people, total, err := s.store.ListPeople(ctx, p)
	if err != nil {
		return ListPeople500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}

	items := make([]Person, 0, len(people))
	for _, person := range people {
		item := Person{
			Id:      person.ID,
			TmdbId:  person.TMDBID,
			TvdbId:  person.TVDBID,
			Name:    person.Name,
			Credits: person.Credits,
		}
		if person.ProfileURL != "" {
			url := person.ProfileURL
			item.ProfileUrl = &url
		}
		items = append(items, item)
	}

	return ListPeople200JSONResponse{
		Items:  items,
		Total:  total,
		Limit:  p.Limit,
		Offset: p.Offset,
	}, nil
}

func (s *Server) GetPerson(
	ctx context.Context,
	request GetPersonRequestObject,
) (GetPersonResponseObject, error) {
	credits, err := s.store.PersonCredits(ctx, request.Id)
	if errors.Is(err, db.ErrPersonNotFound) {
		return GetPerson404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	}
	if err != nil {
		return GetPerson500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}

	movies := make([]PersonMovieCredit, 0, len(credits.Movies))
	for _, c := range credits.Movies {
		movies = append(movies, PersonMovieCredit{
			Movie:     movieToAPI(c.Movie),
			Character: c.Character,
		})
	}
	// tvShowBaseToAPI, not tvShowToAPI: the credit query leaves the
	// season/episode tree unloaded, and the full converter would render its
	// absence as a show with zero seasons rather than as a show not asked for.
	series := make([]PersonSeriesCredit, 0, len(credits.Series))
	for _, c := range credits.Series {
		series = append(series, PersonSeriesCredit{
			Series:    tvShowBaseToAPI(c.Series),
			Character: c.Character,
		})
	}

	out := PersonCredits{
		Id:     credits.ID,
		TmdbId: credits.TMDBID,
		TvdbId: credits.TVDBID,
		Name:   credits.Name,
		Movies: movies,
		Series: series,
	}
	if credits.ProfileURL != "" {
		url := credits.ProfileURL
		out.ProfileUrl = &url
	}
	return GetPerson200JSONResponse(out), nil
}
