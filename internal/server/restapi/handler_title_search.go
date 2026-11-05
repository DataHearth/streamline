package restapi

import (
	"context"
	"fmt"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/datahearth/streamline/ent"
	entalbum "github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/internal/db"
)

// titleSearchParams reads the query/limit/offset trio both title searches
// share. ok is false for a limit outside [1, titleSearchMaxLimit].
func titleSearchParams(
	query *string,
	limit *uint16,
	offset *uint32,
) (db.TitleSearchParams, bool) {
	l, ok := limitOr(limit, 20, titleSearchMaxLimit)
	if !ok {
		return db.TitleSearchParams{}, false
	}
	p := db.TitleSearchParams{Limit: l}
	if query != nil {
		p.Query = *query
	}
	if offset != nil {
		p.Offset = *offset
	}
	return p, true
}

func (s *Server) ListMusicAlbums(
	ctx context.Context,
	request ListMusicAlbumsRequestObject,
) (ListMusicAlbumsResponseObject, error) {
	prm := request.Params
	p, ok := titleSearchParams(prm.Query, prm.Limit, prm.Offset)
	if !ok {
		return ListMusicAlbums400JSONResponse{
			BadRequestJSONResponse: errBadRequest(
				limitRangeMsg(titleSearchMaxLimit),
			),
		}, nil
	}
	var types []entalbum.Type
	if prm.Type != nil {
		for _, t := range *prm.Type {
			at := entalbum.Type(t)
			if entalbum.TypeValidator(at) != nil {
				return ListMusicAlbums400JSONResponse{
					BadRequestJSONResponse: errBadRequest(
						fmt.Sprintf("unknown release type %q", t),
					),
				}, nil
			}
			types = append(types, at)
		}
	}
	albums, err := s.store.SearchAlbums(ctx, p, types)
	if err != nil {
		return ListMusicAlbums500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	items := make([]AlbumSearchHit, 0, len(albums))
	for _, a := range albums {
		items = append(items, toAlbumSearchHit(a))
	}
	return ListMusicAlbums200JSONResponse{
		Items:  items,
		Limit:  p.Limit,
		Offset: p.Offset,
	}, nil
}

func (s *Server) ListEpisodes(
	ctx context.Context,
	request ListEpisodesRequestObject,
) (ListEpisodesResponseObject, error) {
	prm := request.Params
	p, ok := titleSearchParams(prm.Query, prm.Limit, prm.Offset)
	if !ok {
		return ListEpisodes400JSONResponse{
			BadRequestJSONResponse: errBadRequest(
				limitRangeMsg(titleSearchMaxLimit),
			),
		}, nil
	}
	episodes, err := s.store.SearchEpisodes(ctx, p)
	if err != nil {
		return ListEpisodes500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	items := make([]EpisodeSearchHit, 0, len(episodes))
	for _, e := range episodes {
		// An episode is always loaded with its season and show; one missing
		// either edge could not be named or linked, so it is left out.
		if hit, ok := toEpisodeSearchHit(e); ok {
			items = append(items, hit)
		}
	}
	return ListEpisodes200JSONResponse{
		Items:  items,
		Limit:  p.Limit,
		Offset: p.Offset,
	}, nil
}

func toAlbumSearchHit(a *ent.Album) AlbumSearchHit {
	out := AlbumSearchHit{
		Id:    a.ID,
		Title: a.Title,
		Type:  MusicAlbumType(a.Type),
	}
	if a.ReleaseDate != nil {
		out.ReleaseDate = &openapi_types.Date{Time: a.ReleaseDate.UTC()}
	}
	if ar := a.Edges.Artist; ar != nil {
		out.ArtistId = ar.ID
		out.ArtistName = ar.Name
	}
	return out
}

func toEpisodeSearchHit(e *ent.Episode) (EpisodeSearchHit, bool) {
	se := e.Edges.Season
	if se == nil || se.Edges.TvShow == nil {
		return EpisodeSearchHit{}, false
	}
	show := se.Edges.TvShow
	out := EpisodeSearchHit{
		Id:          e.ID,
		Title:       e.Title,
		SeriesId:    show.ID,
		SeriesTitle: show.Title,
		SeriesType:  EpisodeSearchHitSeriesType(show.Type),
		Season:      se.Number,
		Episode:     e.Number,
	}
	if !e.AirDate.IsZero() {
		out.AirDate = &e.AirDate
	}
	return out, true
}

func (s *Server) ListMusicTracks(
	ctx context.Context,
	request ListMusicTracksRequestObject,
) (ListMusicTracksResponseObject, error) {
	prm := request.Params
	p, ok := titleSearchParams(prm.Query, prm.Limit, prm.Offset)
	if !ok {
		return ListMusicTracks400JSONResponse{
			BadRequestJSONResponse: errBadRequest(
				limitRangeMsg(titleSearchMaxLimit),
			),
		}, nil
	}
	tracks, err := s.store.SearchTracks(ctx, p)
	if err != nil {
		return ListMusicTracks500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	items := make([]TrackSearchHit, 0, len(tracks))
	for _, t := range tracks {
		// A track is always loaded with its album and artist; one missing
		// either edge could not be named or linked, so it is left out.
		if hit, ok := toTrackSearchHit(t); ok {
			items = append(items, hit)
		}
	}
	return ListMusicTracks200JSONResponse{
		Items:  items,
		Limit:  p.Limit,
		Offset: p.Offset,
	}, nil
}

func toTrackSearchHit(t *ent.Track) (TrackSearchHit, bool) {
	al := t.Edges.Album
	if al == nil || al.Edges.Artist == nil {
		return TrackSearchHit{}, false
	}
	return TrackSearchHit{
		Id:         t.ID,
		Title:      t.Title,
		AlbumId:    al.ID,
		AlbumTitle: al.Title,
		AlbumType:  MusicAlbumType(al.Type),
		ArtistId:   al.Edges.Artist.ID,
		ArtistName: al.Edges.Artist.Name,
	}, true
}
