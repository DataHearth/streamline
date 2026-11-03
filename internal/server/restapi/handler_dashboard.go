package restapi

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/datahearth/streamline/ent/mediaevent"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library"
)

func (s *Server) ListActivity(
	ctx context.Context,
	req ListActivityRequestObject,
) (ListActivityResponseObject, error) {
	f := db.ActivityFilter{}

	if req.Params.Type != nil {
		for _, t := range *req.Params.Type {
			f.Types = append(f.Types, mediaevent.Type(t))
		}
	}
	if req.Params.MovieId != nil {
		id := *req.Params.MovieId
		f.MovieID = &id
	}
	if req.Params.SeriesId != nil {
		id := *req.Params.SeriesId
		f.SeriesID = &id
	}
	if req.Params.ArtistId != nil {
		id := *req.Params.ArtistId
		f.ArtistID = &id
	}
	if req.Params.BookId != nil {
		id := *req.Params.BookId
		f.BookID = &id
	}
	if req.Params.BookSeriesId != nil {
		id := *req.Params.BookSeriesId
		f.BookSeriesID = &id
	}
	if req.Params.Since != nil {
		t := *req.Params.Since
		f.Since = &t
	}
	if req.Params.Before != nil {
		t := *req.Params.Before
		f.Before = &t
	}
	limit, ok := limitOr(req.Params.Limit, 0, activityMaxLimit)
	if !ok {
		return ListActivity400JSONResponse{
			BadRequestJSONResponse: errBadRequest(limitRangeMsg(activityMaxLimit)),
		}, nil
	}
	f.Limit = limit
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
	albums, err := s.store.ListUpcomingAlbums(
		ctx,
		req.Params.From,
		req.Params.To,
	)
	if err != nil {
		return nil, err
	}
	books, err := s.store.ListUpcomingBooks(
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
		Albums:   make([]UpcomingAlbum, 0, len(albums)),
		Books:    make([]UpcomingBook, 0, len(books)),
	}
	for _, m := range movies {
		out.Movies = append(out.Movies, toUpcomingMovie(m))
	}
	now := time.Now()
	for _, e := range episodes {
		out.Episodes = append(out.Episodes, toUpcomingEpisode(e, now))
	}
	for _, a := range albums {
		out.Albums = append(out.Albums, toUpcomingAlbum(a))
	}
	for _, b := range books {
		out.Books = append(out.Books, toUpcomingBook(b))
	}
	return ListUpcomingReleases200JSONResponse{
		UpcomingListJSONResponse: UpcomingListJSONResponse(out),
	}, nil
}

func (s *Server) ListLibraryArrivals(
	ctx context.Context,
	req ListLibraryArrivalsRequestObject,
) (ListLibraryArrivalsResponseObject, error) {
	limit, ok := limitOr(req.Params.Limit, 24, arrivalsMaxLimit)
	if !ok {
		return ListLibraryArrivals400JSONResponse{
			BadRequestJSONResponse: errBadRequest(limitRangeMsg(arrivalsMaxLimit)),
		}, nil
	}
	arrivals, err := s.store.RecentArrivals(ctx, int(limit))
	if err != nil {
		return nil, err
	}
	out := LibraryArrivals{Items: make([]LibraryArrival, 0, len(arrivals))}
	for _, a := range arrivals {
		out.Items = append(out.Items, toLibraryArrival(a))
	}
	return ListLibraryArrivals200JSONResponse{
		LibraryArrivalsJSONResponse: LibraryArrivalsJSONResponse(out),
	}, nil
}

func toLibraryArrival(a db.Arrival) LibraryArrival {
	out := LibraryArrival{
		Kind:  LibraryArrivalKind(a.Kind),
		At:    a.At,
		Id:    a.ID,
		Title: a.Title,
	}
	if a.Year != 0 {
		out.Year = &a.Year
	}
	switch a.Kind {
	case db.ArrivalMovie:
		out.Resolution = optString(library.Parse(filepath.Base(a.Path)).Resolution)
	case db.ArrivalSeries:
		out.Addition = seriesAdditionToAPI(a.Addition)
	case db.ArrivalAlbum:
		if a.ArtistID != 0 {
			out.ArtistId = &a.ArtistID
		}
		out.ArtistName = optString(a.ArtistName)
		rt := LibraryArrivalReleaseType(a.ReleaseType)
		out.ReleaseType = &rt
	case db.ArrivalBook:
		out.Author = optString(a.Author)
		bk := LibraryArrivalBookKind(a.BookKind)
		out.BookKind = &bk
		if a.Format != "" {
			f := LibraryArrivalFormat(a.Format)
			out.Format = &f
		}
		if a.SeriesID != 0 {
			out.SeriesId = &a.SeriesID
			out.Volume = a.Volume
		}
	}
	return out
}
