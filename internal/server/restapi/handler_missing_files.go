package restapi

import (
	"context"
	"fmt"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
)

const (
	missingFilesDefaultLimit = 50
	forgetMaxIDs             = 100
)

func (s *Server) ListMissingFiles(
	ctx context.Context,
	req ListMissingFilesRequestObject,
) (ListMissingFilesResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return ListMissingFiles403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	limit, ok := limitOr[uint16](
		req.Params.Limit, missingFilesDefaultLimit, missingFilesMaxLimit,
	)
	if !ok {
		return ListMissingFiles400JSONResponse{
			BadRequestJSONResponse: errBadRequest(
				limitRangeMsg(missingFilesMaxLimit),
			),
		}, nil
	}
	page, ok := positiveOr[uint32](req.Params.Page, 1)
	if !ok {
		return ListMissingFiles400JSONResponse{
			BadRequestJSONResponse: errBadRequest(msgZeroPage),
		}, nil
	}
	rows, total, err := s.store.ListMissingFiles(
		ctx, int(limit), (int(page)-1)*int(limit),
	)
	if err != nil {
		return nil, err
	}
	items := make([]MissingFile, 0, len(rows))
	for _, row := range rows {
		items = append(items, toAPIMissingFile(row))
	}
	return ListMissingFiles200JSONResponse{
		Items: items,
		Total: total,
	}, nil
}

func (s *Server) ForgetMissingFiles(
	ctx context.Context,
	req ForgetMissingFilesRequestObject,
) (ForgetMissingFilesResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return ForgetMissingFiles403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	if n := len(req.Body.Ids); n == 0 || n > forgetMaxIDs {
		return ForgetMissingFiles400JSONResponse{
			BadRequestJSONResponse: errBadRequest(
				fmt.Sprintf("ids must hold between 1 and %d files", forgetMaxIDs),
			),
		}, nil
	}
	forgotten, err := s.hygiene.ForgetMissing(ctx, req.Body.Ids)
	if err != nil {
		return nil, err
	}
	return ForgetMissingFiles200JSONResponse{
		Forgotten: forgotten,
	}, nil
}

func toAPIMissingFile(row *ent.MediaFile) MissingFile {
	out := MissingFile{
		Id:   row.ID,
		Path: row.Path,
		Size: row.Size,
	}
	if root, owned := db.RootOfFile(row); owned {
		out.Root = MissingFileRoot(root)
	}
	if row.MissingSince != nil {
		out.MissingSince = *row.MissingSince
	}
	if wait, never := config.Get().Library.MissingFileWait(); !never &&
		row.MissingConfirmedAt != nil {
		at := row.MissingConfirmedAt.Add(wait)
		out.RedownloadAt = &at
	}
	switch {
	case row.Edges.Movie != nil:
		out.OwnerKind = MissingFileOwnerKindMovie
		out.OwnerId = row.Edges.Movie.ID
		out.Title = row.Edges.Movie.Title
	case row.Edges.Episode != nil:
		fillEpisodeOwner(&out, row.Edges.Episode)
	case row.Edges.Track != nil:
		fillTrackOwner(&out, row.Edges.Track)
	case row.Edges.Book != nil:
		out.OwnerKind = MissingFileOwnerKindBook
		out.OwnerId = row.Edges.Book.ID
		out.Title = row.Edges.Book.Title
	}
	return out
}

func fillEpisodeOwner(out *MissingFile, ep *ent.Episode) {
	out.OwnerKind = MissingFileOwnerKindSeries
	season := ep.Edges.Season
	if season == nil || season.Edges.TvShow == nil {
		out.Title = ep.Title
		return
	}
	out.OwnerId = season.Edges.TvShow.ID
	out.Title = fmt.Sprintf(
		"%s S%02dE%02d", season.Edges.TvShow.Title, season.Number, ep.Number,
	)
}

func fillTrackOwner(out *MissingFile, tr *ent.Track) {
	out.OwnerKind = MissingFileOwnerKindArtist
	alb := tr.Edges.Album
	if alb == nil || alb.Edges.Artist == nil {
		out.Title = tr.Title
		return
	}
	out.OwnerId = alb.Edges.Artist.ID
	out.Title = fmt.Sprintf(
		"%s - %s - %02d %s", alb.Edges.Artist.Name, alb.Title, tr.Position, tr.Title,
	)
}
