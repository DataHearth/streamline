package restapi

import (
	"context"
	"errors"
	"slices"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/media/music"
)

func (s *Server) SearchMusicArtists(
	ctx context.Context,
	request SearchMusicArtistsRequestObject,
) (SearchMusicArtistsResponseObject, error) {
	results, err := s.metadataMusic.SearchArtists(ctx, request.Params.Query)
	if err != nil {
		return SearchMusicArtists500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	items := make([]MusicArtistSearchResult, 0, len(results))
	for _, r := range results {
		existing, err := s.store.FindArtistByMBID(ctx, r.MBID)
		if err != nil {
			return SearchMusicArtists500JSONResponse{
				InternalErrorJSONResponse: errInternal(ctx, err),
			}, nil
		}
		items = append(items, MusicArtistSearchResult{
			Mbid:           r.MBID,
			Name:           r.Name,
			SortName:       r.SortName,
			Disambiguation: optString(r.Disambiguation),
			Score:          r.Score,
			AlreadyAdded:   existing != nil,
		})
	}
	return SearchMusicArtists200JSONResponse{Items: items}, nil
}

func patchMusicArtistErr(
	ctx context.Context,
	err error,
) PatchMusicArtistResponseObject {
	if errors.Is(err, music.ErrArtistNotFound) {
		return PatchMusicArtist404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}
	}
	return PatchMusicArtist500JSONResponse{
		InternalErrorJSONResponse: errInternal(ctx, err),
	}
}

// ResolveMusicQualityProfile falls back to the default profile, so it cannot
// tell a typo from a real name.
func musicProfileExists(name string) bool {
	c := config.Get()
	if c == nil {
		return false
	}
	return slices.ContainsFunc(
		c.MusicQualityProfiles,
		func(p config.MusicQualityProfileEntry) bool { return p.Name == name },
	)
}

func (s *Server) DeleteMusicArtist(
	ctx context.Context,
	request DeleteMusicArtistRequestObject,
) (DeleteMusicArtistResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return DeleteMusicArtist403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	deleteFiles := request.Params.DeleteFiles != nil && *request.Params.DeleteFiles
	err := s.music.Delete(ctx, request.Id, deleteFiles)
	switch {
	case errors.Is(err, music.ErrArtistNotFound):
		return DeleteMusicArtist404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case err != nil:
		return DeleteMusicArtist500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return DeleteMusicArtist204Response{}, nil
}

func patchMusicAlbumErr(
	ctx context.Context,
	err error,
) PatchMusicAlbumResponseObject {
	if errors.Is(err, music.ErrAlbumNotFound) {
		return PatchMusicAlbum404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}
	}
	return PatchMusicAlbum500JSONResponse{
		InternalErrorJSONResponse: errInternal(ctx, err),
	}
}

func (s *Server) GrabMusicAlbumRelease(
	ctx context.Context,
	request GrabMusicAlbumReleaseRequestObject,
) (GrabMusicAlbumReleaseResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return GrabMusicAlbumRelease403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	sr, err := toIndexerResult(request.Body)
	switch {
	case errors.Is(err, errBadReleaseHandle):
		return GrabMusicAlbumRelease422JSONResponse{
			UnprocessableEntityJSONResponse: errGrabRejected(err.Error()),
		}, nil
	case err != nil:
		return GrabMusicAlbumRelease422JSONResponse{
			UnprocessableEntityJSONResponse: unprocessableResp(err.Error()),
		}, nil
	}
	err = s.music.GrabAlbumRelease(ctx, request.Id, sr)
	switch {
	case errors.Is(err, music.ErrAlbumNotFound):
		return GrabMusicAlbumRelease404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case errors.Is(err, music.ErrNoQualityProfile):
		return GrabMusicAlbumRelease422JSONResponse{
			UnprocessableEntityJSONResponse: errNoQualityProfile(err.Error()),
		}, nil
	case errors.Is(err, download.ErrUntrustedSource),
		errors.Is(err, download.ErrClientFull),
		errors.Is(err, download.ErrUnsafeTorrentName):
		return GrabMusicAlbumRelease422JSONResponse{
			UnprocessableEntityJSONResponse: errGrabRejected(err.Error()),
		}, nil
	case err != nil:
		return GrabMusicAlbumRelease500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return GrabMusicAlbumRelease202Response{}, nil
}
