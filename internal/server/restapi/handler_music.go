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

func (s *Server) ListMusicArtists(
	ctx context.Context,
	request ListMusicArtistsRequestObject,
) (ListMusicArtistsResponseObject, error) {
	page, ok := positiveOr(request.Params.Page, uint16(1))
	if !ok {
		return ListMusicArtists400JSONResponse{
			BadRequestJSONResponse: errBadRequest(msgZeroPage),
		}, nil
	}
	limit, ok := limitOr(request.Params.Limit, 20, musicMaxLimit)
	if !ok {
		return ListMusicArtists400JSONResponse{
			BadRequestJSONResponse: errBadRequest(limitRangeMsg(musicMaxLimit)),
		}, nil
	}
	rows, total, err := s.music.List(ctx, page, limit)
	if err != nil {
		return ListMusicArtists500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	items := make([]MusicArtist, len(rows))
	for i, r := range rows {
		items[i] = musicArtistToAPI(r)
		items[i].Albums = nil
	}
	return ListMusicArtists200JSONResponse{
		Items: items,
		Total: total,
		Page:  uint32(page),
		Limit: limit,
	}, nil
}

func (s *Server) AddMusicArtist(
	ctx context.Context,
	request AddMusicArtistRequestObject,
) (AddMusicArtistResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return AddMusicArtist403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	p := music.AddParams{MBID: request.Body.Mbid, Monitored: true}
	if request.Body.Monitored != nil {
		p.Monitored = *request.Body.Monitored
	}
	if request.Body.QualityProfile != nil {
		p.QualityProfile = *request.Body.QualityProfile
	}
	artist, err := s.music.Add(ctx, p)
	switch {
	case errors.Is(err, music.ErrArtistExists):
		return AddMusicArtist409JSONResponse{
			ConflictJSONResponse: errConflict(err.Error()),
		}, nil
	case err != nil:
		return AddMusicArtist500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return AddMusicArtist201JSONResponse{
		MusicArtistCreatedJSONResponse: MusicArtistCreatedJSONResponse(
			musicArtistToAPI(artist),
		),
	}, nil
}

func (s *Server) GetMusicArtist(
	ctx context.Context,
	request GetMusicArtistRequestObject,
) (GetMusicArtistResponseObject, error) {
	artist, err := s.music.Get(ctx, request.Id)
	switch {
	case errors.Is(err, music.ErrArtistNotFound):
		return GetMusicArtist404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case err != nil:
		return GetMusicArtist500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return GetMusicArtist200JSONResponse{
		MusicArtistDetailJSONResponse: MusicArtistDetailJSONResponse(
			musicArtistToAPI(artist),
		),
	}, nil
}

func (s *Server) PatchMusicArtist(
	ctx context.Context,
	request PatchMusicArtistRequestObject,
) (PatchMusicArtistResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return PatchMusicArtist403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	body := request.Body
	if body.QualityProfile != nil && *body.QualityProfile != "" &&
		!musicProfileExists(*body.QualityProfile) {
		return PatchMusicArtist422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(
				"unknown music quality profile",
			),
		}, nil
	}
	if body.Monitored != nil {
		if err := s.music.SetArtistMonitored(
			ctx,
			request.Id,
			*body.Monitored,
		); err != nil {
			return patchMusicArtistErr(ctx, err), nil
		}
	}
	if body.QualityProfile != nil {
		if err := s.music.SetArtistQualityProfile(
			ctx,
			request.Id,
			*body.QualityProfile,
		); err != nil {
			return patchMusicArtistErr(ctx, err), nil
		}
	}
	artist, err := s.music.Get(ctx, request.Id)
	if err != nil {
		return patchMusicArtistErr(ctx, err), nil
	}
	return PatchMusicArtist200JSONResponse{
		MusicArtistDetailJSONResponse: MusicArtistDetailJSONResponse(
			musicArtistToAPI(artist),
		),
	}, nil
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

func (s *Server) RefreshMusicArtist(
	ctx context.Context,
	request RefreshMusicArtistRequestObject,
) (RefreshMusicArtistResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return RefreshMusicArtist403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	artist, err := s.music.RefreshOne(ctx, request.Id)
	switch {
	case errors.Is(err, music.ErrArtistNotFound):
		return RefreshMusicArtist404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case err != nil:
		return RefreshMusicArtist500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return RefreshMusicArtist200JSONResponse{
		MusicArtistDetailJSONResponse: MusicArtistDetailJSONResponse(
			musicArtistToAPI(artist),
		),
	}, nil
}

func (s *Server) GetMusicAlbum(
	ctx context.Context,
	request GetMusicAlbumRequestObject,
) (GetMusicAlbumResponseObject, error) {
	album, err := s.music.GetAlbum(ctx, request.Id)
	switch {
	case errors.Is(err, music.ErrAlbumNotFound):
		return GetMusicAlbum404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case err != nil:
		return GetMusicAlbum500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return GetMusicAlbum200JSONResponse{
		MusicAlbumDetailJSONResponse: MusicAlbumDetailJSONResponse(
			musicAlbumToAPI(album),
		),
	}, nil
}

func (s *Server) PatchMusicAlbum(
	ctx context.Context,
	request PatchMusicAlbumRequestObject,
) (PatchMusicAlbumResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return PatchMusicAlbum403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	if request.Body.Monitored != nil {
		err := s.music.SetAlbumMonitored(ctx, request.Id, *request.Body.Monitored)
		if err != nil {
			return patchMusicAlbumErr(ctx, err), nil
		}
	}
	album, err := s.music.GetAlbum(ctx, request.Id)
	if err != nil {
		return patchMusicAlbumErr(ctx, err), nil
	}
	return PatchMusicAlbum200JSONResponse{
		MusicAlbumDetailJSONResponse: MusicAlbumDetailJSONResponse(
			musicAlbumToAPI(album),
		),
	}, nil
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

func (s *Server) SearchMusicAlbumReleases(
	ctx context.Context,
	request SearchMusicAlbumReleasesRequestObject,
) (SearchMusicAlbumReleasesResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return SearchMusicAlbumReleases403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	releases, err := s.music.SearchAlbumReleases(ctx, request.Id)
	switch {
	case errors.Is(err, music.ErrAlbumNotFound):
		return SearchMusicAlbumReleases404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case errors.Is(err, music.ErrNoQualityProfile):
		return SearchMusicAlbumReleases422JSONResponse{
			UnprocessableEntityJSONResponse: errNoQualityProfile(err.Error()),
		}, nil
	case err != nil:
		return SearchMusicAlbumReleases500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	private := indexerPrivacy()
	items := make([]AlbumRelease, 0, len(releases))
	for _, r := range releases {
		item := toSearchResult(r.Result)
		score := r.Score
		item.Score = &score
		if p, ok := private[r.Result.ConfiguredIndexer]; ok {
			item.IndexerPrivate = &p
		}
		items = append(items, AlbumRelease{Format: r.Format, Release: item})
	}
	out := AlbumReleaseListJSONResponse{Items: items}
	return SearchMusicAlbumReleases200JSONResponse{
		AlbumReleaseListJSONResponse: out,
	}, nil
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
