package restapi

import (
	"context"
	"errors"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/metadatachange"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/metadata"
)

const metadataChangesMaxLimit = 100

var mediaOwnerTypes = map[ListMetadataChangesParamsMedia][]db.ChangeOwnerType{
	ListMetadataChangesParamsMediaMusic: {
		db.ChangeOwnerArtist,
		db.ChangeOwnerAlbum,
	},
	ListMetadataChangesParamsMediaSeries: {db.ChangeOwnerTVShow},
	ListMetadataChangesParamsMediaMovies: {db.ChangeOwnerMovie},
	ListMetadataChangesParamsMediaBooks:  {db.ChangeOwnerBook},
}

func (s *Server) ListMetadataChanges(
	ctx context.Context,
	req ListMetadataChangesRequestObject,
) (ListMetadataChangesResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return ListMetadataChanges403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	page, ok := positiveOr(req.Params.Page, uint16(1))
	if !ok {
		return ListMetadataChanges400JSONResponse{
			BadRequestJSONResponse: errBadRequest(msgZeroPage),
		}, nil
	}
	limit, ok := limitOr(req.Params.Limit, 50, metadataChangesMaxLimit)
	if !ok {
		return ListMetadataChanges400JSONResponse{
			BadRequestJSONResponse: errBadRequest(
				limitRangeMsg(metadataChangesMaxLimit),
			),
		}, nil
	}
	var f db.ChangeFilter
	if m := req.Params.Media; m != nil {
		types, known := mediaOwnerTypes[*m]
		if !known {
			return ListMetadataChanges400JSONResponse{
				BadRequestJSONResponse: errBadRequest("unknown media " + string(*m)),
			}, nil
		}
		f.Types = types
	}
	if k := req.Params.Kind; k != nil {
		kind := metadatachange.Kind(*k)
		if metadatachange.KindValidator(kind) != nil {
			return ListMetadataChanges400JSONResponse{
				BadRequestJSONResponse: errBadRequest("unknown kind " + string(*k)),
			}, nil
		}
		f.Kinds = []metadatachange.Kind{kind}
	}
	rows, total, err := s.store.ListChanges(ctx, f, limit, uint32(page-1)*limit)
	if err != nil {
		return nil, err
	}
	out := MetadataChangeList{
		Items: make([]MetadataChange, 0, len(rows)),
		Total: total,
	}
	for _, r := range rows {
		out.Items = append(out.Items, toMetadataChange(r))
	}
	return ListMetadataChanges200JSONResponse{
		MetadataChangeListJSONResponse: MetadataChangeListJSONResponse(out),
	}, nil
}

func (s *Server) GetMetadataChangeCounts(
	ctx context.Context,
	_ GetMetadataChangeCountsRequestObject,
) (GetMetadataChangeCountsResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return GetMetadataChangeCounts403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	c, err := s.store.CountChanges(ctx)
	if err != nil {
		return nil, err
	}
	out := MetadataChangeCounts{
		Total:  c.Total,
		Music:  c.ByType[db.ChangeOwnerArtist] + c.ByType[db.ChangeOwnerAlbum],
		Series: c.ByType[db.ChangeOwnerTVShow],
		Movies: c.ByType[db.ChangeOwnerMovie],
		Books:  c.ByType[db.ChangeOwnerBook],
		Kinds:  make(map[string]uint32, len(c.ByKind)),
	}
	for k, n := range c.ByKind {
		out.Kinds[string(k)] = n
	}
	return GetMetadataChangeCounts200JSONResponse{
		MetadataChangeCountsJSONResponse: MetadataChangeCountsJSONResponse(out),
	}, nil
}

func (s *Server) KeepMetadataChange(
	ctx context.Context,
	req KeepMetadataChangeRequestObject,
) (KeepMetadataChangeResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return KeepMetadataChange403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	err := s.store.KeepChange(ctx, req.Id)
	if ent.IsNotFound(err) {
		return KeepMetadataChange404JSONResponse{
			NotFoundJSONResponse: errNotFound("metadata change not found"),
		}, nil
	}
	if err != nil {
		return nil, err
	}
	return KeepMetadataChange204Response{}, nil
}

func (s *Server) FollowMetadataChange(
	ctx context.Context,
	req FollowMetadataChangeRequestObject,
) (FollowMetadataChangeResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return FollowMetadataChange403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	c, err := s.store.FindChange(ctx, req.Id)
	if ent.IsNotFound(err) ||
		(err == nil && c.State != metadatachange.StatePending) {
		return FollowMetadataChange404JSONResponse{
			NotFoundJSONResponse: errNotFound("metadata change not found"),
		}, nil
	}
	if err != nil {
		return nil, err
	}
	switch db.ChangeOwnerOf(c).Type {
	case db.ChangeOwnerArtist, db.ChangeOwnerAlbum:
		err = s.music.FollowChange(ctx, c)
	default:
		err = db.ErrNotFollowable
	}
	switch {
	case err == nil:
		return FollowMetadataChange204Response{}, nil
	case errors.Is(err, db.ErrNotFollowable), errors.Is(err, db.ErrChangeConflict):
		code := "change_conflict"
		if errors.Is(err, db.ErrNotFollowable) {
			code = "not_followable"
		}
		return FollowMetadataChange409JSONResponse{
			Message: err.Error(),
			Code:    &code,
		}, nil
	case errors.Is(err, metadata.ErrRateLimited):
		return FollowMetadataChange429JSONResponse{
			RateLimitedJSONResponse: errMusicBrainzRateLimited(err),
		}, nil
	}
	return nil, err
}

func toMetadataChange(c *ent.MetadataChange) MetadataChange {
	o := db.ChangeOwnerOf(c)
	owner := MetadataChangeOwner{Type: MetadataChangeOwnerType(o.Type), Id: o.ID}
	switch {
	case c.Edges.Artist != nil:
		owner.Title = c.Edges.Artist.Name
		owner.ArtistId = &c.Edges.Artist.ID
	case c.Edges.Album != nil:
		owner.Title = c.Edges.Album.Title
		if a := c.Edges.Album.Edges.Artist; a != nil {
			owner.ArtistId, owner.ArtistName = &a.ID, &a.Name
		}
	case c.Edges.TvShow != nil:
		owner.Title = c.Edges.TvShow.Title
		owner.Year = nonZero(c.Edges.TvShow.Year)
	case c.Edges.Movie != nil:
		owner.Title = c.Edges.Movie.Title
		owner.Year = nonZero(c.Edges.Movie.Year)
	case c.Edges.Book != nil:
		owner.Title = c.Edges.Book.Title
	}
	details := c.Payload
	if details == nil {
		details = map[string]any{}
	}
	return MetadataChange{
		Id:         c.ID,
		Kind:       MetadataChangeKind(c.Kind),
		Owner:      owner,
		Details:    details,
		DetectedAt: c.UpdateTime,
	}
}

func nonZero(v uint16) *uint16 {
	if v == 0 {
		return nil
	}
	return &v
}
