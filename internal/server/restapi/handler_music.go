package restapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/media/music"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/utils/numeric"
)

const (
	defaultMusicLimit = 20

	releaseSearchMinQuery = 2
	releaseSearchMaxQuery = 200
	releaseSearchMaxLimit = 20

	msgMusicParam = "unknown value for a music filter"
)

// musicLang is the overview locale of a request; the spec admits en and fr and
// the service falls back to English for anything else.
func musicLang[T ~string](lang *T) string {
	if lang == nil {
		return "en"
	}
	return string(*lang)
}

func (s *Server) SearchMusicArtists(
	ctx context.Context,
	request SearchMusicArtistsRequestObject,
) (SearchMusicArtistsResponseObject, error) {
	hits, err := s.music.SearchArtists(ctx, request.Params.Query)
	switch {
	case errors.Is(err, metadata.ErrRateLimited):
		return SearchMusicArtists429JSONResponse{
			RateLimitedJSONResponse: errMusicBrainzRateLimited(err),
		}, nil
	case err != nil:
		return SearchMusicArtists500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	items := make([]MusicArtistSearchResult, len(hits))
	for i, h := range hits {
		items[i] = toMusicSearchHit(h)
	}
	return SearchMusicArtists200JSONResponse{
		Items: items,
	}, nil
}

func (s *Server) SearchMusicReleases(
	ctx context.Context,
	request SearchMusicReleasesRequestObject,
) (SearchMusicReleasesResponseObject, error) {
	query := strings.TrimSpace(request.Params.Query)
	if n := utf8.RuneCountInString(query); n < releaseSearchMinQuery ||
		n > releaseSearchMaxQuery {
		return SearchMusicReleases400JSONResponse{
			BadRequestJSONResponse: errBadRequest(fmt.Sprintf(
				"query must be %d to %d characters",
				releaseSearchMinQuery, releaseSearchMaxQuery,
			)),
		}, nil
	}
	limit, ok := limitOr(
		request.Params.Limit,
		releaseSearchMaxLimit,
		releaseSearchMaxLimit,
	)
	if !ok {
		return SearchMusicReleases400JSONResponse{
			BadRequestJSONResponse: errBadRequest(
				limitRangeMsg(releaseSearchMaxLimit),
			),
		}, nil
	}
	hits, err := s.music.SearchReleaseGroups(ctx, query)
	switch {
	case errors.Is(err, metadata.ErrRateLimited):
		return SearchMusicReleases429JSONResponse{
			RateLimitedJSONResponse: errMusicBrainzRateLimited(err),
		}, nil
	case err != nil:
		return SearchMusicReleases500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	items := make([]MusicReleaseSearchResult, 0, min(len(hits), limit))
	for _, h := range hits[:min(len(hits), limit)] {
		items = append(items, toMusicReleaseHit(h))
	}
	return SearchMusicReleases200JSONResponse{Items: items}, nil
}

func toMusicReleaseHit(h music.ReleaseHit) MusicReleaseSearchResult {
	out := MusicReleaseSearchResult{
		Mbid:         h.MBID,
		Title:        h.Title,
		Artist:       h.ArtistName,
		ArtistMbid:   h.ArtistMBID,
		AlreadyAdded: h.AlreadyAdded,
	}
	if h.ReleaseDate != nil {
		y := numeric.SaturateU16(h.ReleaseDate.Year())
		out.Year = &y
	}
	if h.Type != "" {
		t := MusicAlbumType(h.Type)
		out.Type = &t
	}
	return out
}

func (s *Server) GetMusicArtistLookup(
	ctx context.Context,
	request GetMusicArtistLookupRequestObject,
) (GetMusicArtistLookupResponseObject, error) {
	detail, err := s.music.LookupArtist(
		ctx, request.Mbid, musicLang(request.Params.Lang),
	)
	switch {
	case errors.Is(err, metadata.ErrNotFound):
		return GetMusicArtistLookup404JSONResponse{
			NotFoundJSONResponse: errNotFound("artist not found"),
		}, nil
	case errors.Is(err, metadata.ErrRateLimited):
		return GetMusicArtistLookup429JSONResponse{
			RateLimitedJSONResponse: errMusicBrainzRateLimited(err),
		}, nil
	case err != nil:
		return GetMusicArtistLookup500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return GetMusicArtistLookup200JSONResponse{
		MusicArtistLookupDetailResponseJSONResponse: MusicArtistLookupDetailResponseJSONResponse(
			toMusicLookupDetail(detail),
		),
	}, nil
}

func (s *Server) ListMusicArtists(
	ctx context.Context,
	request ListMusicArtistsRequestObject,
) (ListMusicArtistsResponseObject, error) {
	p := request.Params
	page, ok := positiveOr(p.Page, 1)
	if !ok {
		return ListMusicArtists400JSONResponse{
			BadRequestJSONResponse: errBadRequest(msgZeroPage),
		}, nil
	}
	limit, ok := limitOr(p.Limit, defaultMusicLimit, musicMaxLimit)
	if !ok {
		return ListMusicArtists400JSONResponse{
			BadRequestJSONResponse: errBadRequest(limitRangeMsg(musicMaxLimit)),
		}, nil
	}
	if (p.Status != nil && !p.Status.Valid()) ||
		(p.Monitored != nil && !p.Monitored.Valid()) ||
		(p.Sort != nil && !p.Sort.Valid()) ||
		(p.Order != nil && !p.Order.Valid()) {
		return ListMusicArtists400JSONResponse{
			BadRequestJSONResponse: errBadRequest(msgMusicParam),
		}, nil
	}
	params := db.ListArtistsParams{
		Status:    paramString(p.Status),
		Monitored: paramString(p.Monitored),
		Query:     paramString(p.Query),
		Sort:      paramString(p.Sort),
		Order:     paramString(p.Order),
		Offset:    uint32(page-1) * uint32(limit),
		Limit:     limit,
	}
	res, err := s.music.List(ctx, params)
	if err != nil {
		return ListMusicArtists500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	items := make([]MusicArtist, len(res.Items))
	for i, v := range res.Items {
		items[i] = toMusicArtist(v)
	}
	return ListMusicArtists200JSONResponse{
		Items: items,
		Limit: limit,
		Page:  uint32(page),
		Total: res.Total,
	}, nil
}

func paramString[T ~string](v *T) string {
	if v == nil {
		return ""
	}
	return string(*v)
}

func (s *Server) GetMusicArtistCounts(
	ctx context.Context,
	request GetMusicArtistCountsRequestObject,
) (GetMusicArtistCountsResponseObject, error) {
	p := request.Params
	counts, err := s.music.Counts(ctx, db.ListArtistsParams{
		Status:    paramString(p.Status),
		Monitored: paramString(p.Monitored),
		Query:     paramString(p.Query),
	})
	if err != nil {
		return GetMusicArtistCounts500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return GetMusicArtistCounts200JSONResponse{
		MusicArtistCountsResponseJSONResponse: MusicArtistCountsResponseJSONResponse(
			toMusicCounts(counts),
		),
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
	if request.Body == nil || request.Body.Mbid == "" {
		return AddMusicArtist422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable("mbid is required"),
		}, nil
	}
	monitor := ""
	if request.Body.Monitor != nil {
		monitor = string(*request.Body.Monitor)
	}
	row, err := s.music.Add(ctx, music.AddParams{
		MBID:           request.Body.Mbid,
		Monitor:        monitor,
		QualityProfile: deref(request.Body.QualityProfile),
	})
	switch {
	case errors.Is(err, music.ErrArtistExists):
		return AddMusicArtist409JSONResponse{
			ConflictJSONResponse: errConflict(err.Error()),
		}, nil
	case errors.Is(err, music.ErrUnknownProfile),
		errors.Is(err, music.ErrInvalidMonitor):
		return AddMusicArtist422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	case errors.Is(err, metadata.ErrRateLimited):
		return AddMusicArtist429JSONResponse{
			RateLimitedJSONResponse: errMusicBrainzRateLimited(err),
		}, nil
	case err != nil:
		return AddMusicArtist500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	view, err := s.music.Detail(ctx, row.ID, "en")
	if err != nil {
		return AddMusicArtist500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return AddMusicArtist201JSONResponse{
		MusicArtistCreatedJSONResponse: MusicArtistCreatedJSONResponse(
			toMusicArtistDetail(view),
		),
	}, nil
}

func (s *Server) GetMusicArtist(
	ctx context.Context,
	request GetMusicArtistRequestObject,
) (GetMusicArtistResponseObject, error) {
	view, err := s.music.Detail(ctx, request.Id, musicLang(request.Params.Lang))
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
			toMusicArtistDetail(view),
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
	if request.Body == nil {
		return PatchMusicArtist422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable("body required"),
		}, nil
	}
	var err error
	if request.Body.Monitor != nil {
		err = s.music.SetArtistMonitor(
			ctx,
			request.Id,
			string(*request.Body.Monitor),
		)
	}
	if err == nil && request.Body.QualityProfile != nil {
		err = s.music.SetArtistQualityProfile(
			ctx, request.Id, *request.Body.QualityProfile,
		)
	}
	switch {
	case errors.Is(err, music.ErrArtistNotFound):
		return PatchMusicArtist404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case errors.Is(err, music.ErrUnknownProfile),
		errors.Is(err, music.ErrInvalidMonitor):
		return PatchMusicArtist422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	case err != nil:
		return PatchMusicArtist500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	view, err := s.music.Detail(ctx, request.Id, musicLang(request.Params.Lang))
	if err != nil {
		return PatchMusicArtist500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return PatchMusicArtist200JSONResponse{
		MusicArtistDetailJSONResponse: MusicArtistDetailJSONResponse(
			toMusicArtistDetail(view),
		),
	}, nil
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
	_, err := s.music.RefreshOne(ctx, request.Id)
	switch {
	case errors.Is(err, music.ErrArtistNotFound):
		return RefreshMusicArtist404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case errors.Is(err, metadata.ErrRateLimited):
		return RefreshMusicArtist429JSONResponse{
			RateLimitedJSONResponse: errMusicBrainzRateLimited(err),
		}, nil
	case err != nil:
		return RefreshMusicArtist500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	view, err := s.music.Detail(ctx, request.Id, musicLang(request.Params.Lang))
	if err != nil {
		return RefreshMusicArtist500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return RefreshMusicArtist200JSONResponse{
		MusicArtistDetailJSONResponse: MusicArtistDetailJSONResponse(
			toMusicArtistDetail(view),
		),
	}, nil
}

func (s *Server) RenameMusicArtistFiles(
	ctx context.Context,
	request RenameMusicArtistFilesRequestObject,
) (RenameMusicArtistFilesResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return RenameMusicArtistFiles403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	if s.musicRenamer == nil {
		return RenameMusicArtistFiles500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, errRenamerNotConfigured),
		}, nil
	}
	var (
		plan library.RenamePlan
		err  error
	)
	if request.Params.Preview != nil && *request.Params.Preview {
		plan, err = s.musicRenamer.Preview(ctx, request.Id)
	} else {
		plan, err = s.musicRenamer.Apply(ctx, request.Id)
	}
	switch {
	case errors.Is(err, music.ErrArtistNotFound):
		return RenameMusicArtistFiles404JSONResponse{
			NotFoundJSONResponse: errNotFound("artist not found"),
		}, nil
	case err != nil:
		return RenameMusicArtistFiles500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	out := MusicRenamePlan{
		ArtistId:   request.Id,
		Operations: make([]RenameOperation, 0, len(plan.Operations)),
	}
	for _, op := range plan.Operations {
		out.Operations = append(out.Operations, RenameOperation{
			MediaFileId: op.MediaFileID, From: op.From, To: op.To,
		})
	}
	return RenameMusicArtistFiles200JSONResponse{
		MusicRenamePlanResponseJSONResponse: MusicRenamePlanResponseJSONResponse(
			out,
		),
	}, nil
}

func (s *Server) SearchMusicArtistNow(
	ctx context.Context,
	request SearchMusicArtistNowRequestObject,
) (SearchMusicArtistNowResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return SearchMusicArtistNow403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	err := s.music.SearchArtistNow(ctx, request.Id)
	switch {
	case errors.Is(err, music.ErrArtistNotFound):
		return SearchMusicArtistNow404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case err != nil:
		return SearchMusicArtistNow500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return SearchMusicArtistNow202Response{}, nil
}

// toMusicSearchItems maps judged releases to API items, stamping each with its
// indexer's privacy.
func toMusicSearchItems(releases []music.AlbumRelease) []SearchResult {
	private := indexerPrivacy()
	items := make([]SearchResult, 0, len(releases))
	for _, r := range releases {
		item := toMusicSearchResult(r.Result, r.Parsed, r.Score, r.Reason)
		if p, ok := private[r.Result.ConfiguredIndexer]; ok {
			item.IndexerPrivate = &p
		}
		items = append(items, item)
	}
	return items
}

func (s *Server) BrowseMusicArtistReleases(
	ctx context.Context,
	request BrowseMusicArtistReleasesRequestObject,
) (BrowseMusicArtistReleasesResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return BrowseMusicArtistReleases403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	releases, err := s.music.BrowseArtistReleases(ctx, request.Id)
	switch {
	case errors.Is(err, music.ErrArtistNotFound):
		return BrowseMusicArtistReleases404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case errors.Is(err, music.ErrNoQualityProfile):
		return BrowseMusicArtistReleases422JSONResponse{
			UnprocessableEntityJSONResponse: errNoQualityProfile(err.Error()),
		}, nil
	case err != nil:
		return BrowseMusicArtistReleases500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return BrowseMusicArtistReleases200JSONResponse{
		SearchResultsJSONResponse: SearchResultsJSONResponse(
			SearchResultList{Items: toMusicSearchItems(releases)},
		),
	}, nil
}

// grabRefused reports whether err is the download manager refusing the grab
// itself rather than failing at it.
func grabRefused(err error) bool {
	return errors.Is(err, download.ErrUntrustedSource) ||
		errors.Is(err, download.ErrNoWantedFiles) ||
		errors.Is(err, download.ErrClientFull) ||
		errors.Is(err, download.ErrUnsafeTorrentName)
}

func (s *Server) GrabMusicArtistRelease(
	ctx context.Context,
	request GrabMusicArtistReleaseRequestObject,
) (GrabMusicArtistReleaseResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return GrabMusicArtistRelease403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	sr, err := toIndexerResult(request.Body)
	switch {
	case errors.Is(err, errBadReleaseHandle):
		return GrabMusicArtistRelease422JSONResponse{
			UnprocessableEntityJSONResponse: errGrabRejected(err.Error()),
		}, nil
	case err != nil:
		return GrabMusicArtistRelease422JSONResponse{
			UnprocessableEntityJSONResponse: unprocessableResp(err.Error()),
		}, nil
	}
	err = s.music.GrabArtistRelease(
		ctx,
		request.Id,
		sr,
		replaceExisting(request.Body),
	)
	switch {
	case errors.Is(err, music.ErrArtistNotFound):
		return GrabMusicArtistRelease404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case errors.Is(err, music.ErrNoQualityProfile):
		return GrabMusicArtistRelease422JSONResponse{
			UnprocessableEntityJSONResponse: errNoQualityProfile(err.Error()),
		}, nil
	case grabRefused(err):
		return GrabMusicArtistRelease422JSONResponse{
			UnprocessableEntityJSONResponse: errGrabRejected(err.Error()),
		}, nil
	case err != nil:
		return GrabMusicArtistRelease500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return GrabMusicArtistRelease202Response{}, nil
}

func (s *Server) GetMusicAlbum(
	ctx context.Context,
	request GetMusicAlbumRequestObject,
) (GetMusicAlbumResponseObject, error) {
	view, err := s.music.AlbumDetail(ctx, request.Id)
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
			toMusicAlbum(*view, view.Album.Edges.Artist.ID),
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
	var err error
	if request.Body != nil && request.Body.Monitored != nil {
		err = s.music.SetAlbumMonitored(ctx, request.Id, *request.Body.Monitored)
	}
	var view *music.AlbumView
	if err == nil {
		view, err = s.music.AlbumDetail(ctx, request.Id)
	}
	switch {
	case errors.Is(err, music.ErrAlbumNotFound):
		return PatchMusicAlbum404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case err != nil:
		return PatchMusicAlbum500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return PatchMusicAlbum200JSONResponse{
		MusicAlbumDetailJSONResponse: MusicAlbumDetailJSONResponse(
			toMusicAlbum(*view, view.Album.Edges.Artist.ID),
		),
	}, nil
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
	return SearchMusicAlbumReleases200JSONResponse{
		SearchResultsJSONResponse: SearchResultsJSONResponse(
			SearchResultList{Items: toMusicSearchItems(releases)},
		),
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
	err = s.music.GrabAlbum(ctx, request.Id, sr, replaceExisting(request.Body))
	switch {
	case errors.Is(err, music.ErrAlbumNotFound):
		return GrabMusicAlbumRelease404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case errors.Is(err, music.ErrNoQualityProfile):
		return GrabMusicAlbumRelease422JSONResponse{
			UnprocessableEntityJSONResponse: errNoQualityProfile(err.Error()),
		}, nil
	case grabRefused(err):
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

func (s *Server) SearchMusicAlbumNow(
	ctx context.Context,
	request SearchMusicAlbumNowRequestObject,
) (SearchMusicAlbumNowResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return SearchMusicAlbumNow403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	err := s.music.SearchAlbumNow(ctx, request.Id)
	switch {
	case errors.Is(err, music.ErrAlbumNotFound):
		return SearchMusicAlbumNow404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case err != nil:
		return SearchMusicAlbumNow500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return SearchMusicAlbumNow202Response{}, nil
}

func (s *Server) SearchMusicTrackNow(
	ctx context.Context,
	request SearchMusicTrackNowRequestObject,
) (SearchMusicTrackNowResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return SearchMusicTrackNow403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	err := s.music.SearchTrackNow(ctx, request.Id)
	switch {
	case errors.Is(err, music.ErrTrackNotFound),
		errors.Is(err, music.ErrAlbumNotFound):
		return SearchMusicTrackNow404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case err != nil:
		return SearchMusicTrackNow500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return SearchMusicTrackNow202Response{}, nil
}

func (s *Server) DeleteMusicTrackFile(
	ctx context.Context,
	request DeleteMusicTrackFileRequestObject,
) (DeleteMusicTrackFileResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return DeleteMusicTrackFile403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	err := s.music.DeleteTrackFile(ctx, request.Id)
	switch {
	case errors.Is(err, music.ErrTrackNotFound):
		return DeleteMusicTrackFile404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case errors.Is(err, music.ErrOutsideRoot):
		return DeleteMusicTrackFile409JSONResponse{
			ConflictJSONResponse: errConflict(
				"a file of this track is outside the music library; nothing was deleted",
			),
		}, nil
	case err != nil:
		return DeleteMusicTrackFile500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return DeleteMusicTrackFile204Response{}, nil
}
