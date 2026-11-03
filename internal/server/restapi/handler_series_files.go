package restapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/media/tvshow"
)

// packFilesFailure is a release-files call that did not produce a list, sorted
// once for both the season and the series route.
type packFilesFailure struct {
	status int
	body   Error
}

// packReleaseFiles runs the pack preview for a season (or the whole show when
// season is nil) and sorts every failure into the status its route answers.
func (s *Server) packReleaseFiles(
	ctx context.Context,
	seriesID uint32,
	season *uint16,
	body *SearchResult,
) (PackFiles, *packFilesFailure) {
	fail := func(status int, e Error) (PackFiles, *packFilesFailure) {
		return PackFiles{}, &packFilesFailure{status: status, body: e}
	}
	if err := requireNotRequestOnly(ctx); err != nil {
		return fail(http.StatusForbidden, Error(requestOnlyResp))
	}
	sr, err := toIndexerResult(body)
	switch {
	case errors.Is(err, errBadReleaseHandle):
		return fail(
			http.StatusUnprocessableEntity,
			Error(errGrabRejected(err.Error())),
		)
	case err != nil:
		return fail(
			http.StatusUnprocessableEntity,
			Error(unprocessableResp(err.Error())),
		)
	}
	view, err := s.tvshows.PackReleaseFiles(ctx, seriesID, season, sr)
	switch {
	case errors.Is(err, tvshow.ErrSeriesNotFound),
		errors.Is(err, tvshow.ErrSeasonNotFound):
		return fail(http.StatusNotFound, Error(errNotFound(err.Error())))
	case errors.Is(err, download.ErrUntrustedSource):
		return fail(
			http.StatusUnprocessableEntity,
			Error(errGrabRejected(err.Error())),
		)
	case errors.Is(err, download.ErrFileListUnavailable):
		return fail(
			http.StatusUnprocessableEntity,
			Error(errFilesUnavailable(err.Error())),
		)
	case err != nil:
		// As for an album: the indexer not handing the .torrent over, or a
		// payload that does not decode, whose text can name the indexer.
		slog.WarnContext(ctx, "release files: fetch failed",
			"tvshow.id", seriesID, "release", sr.Title, "error", err)
		return fail(http.StatusUnprocessableEntity, Error(errTorrentUnreadable(
			"the indexer did not hand over a readable .torrent for this release",
		)))
	}
	return toPackFiles(view), nil
}

func toPackFiles(v *tvshow.PackFilesView) PackFiles {
	out := PackFiles{
		Files:    make([]PackFile, 0, len(v.Files)),
		Episodes: make([]PackEpisode, 0, len(v.Episodes)),
	}
	for _, f := range v.Files {
		pf := PackFile{Path: f.Path, Size: f.Size}
		if f.EpisodeID != 0 {
			pf.EpisodeId = &f.EpisodeID
		}
		out.Files = append(out.Files, pf)
	}
	for _, e := range v.Episodes {
		out.Episodes = append(out.Episodes, PackEpisode{
			Id:        e.ID,
			Season:    e.Season,
			Number:    e.Number,
			Title:     e.Title,
			HasFile:   e.HasFile,
			Wanted:    e.Wanted,
			InRelease: e.InRelease,
		})
	}
	return out
}

func (s *Server) GetSeasonReleaseFiles(
	ctx context.Context,
	request GetSeasonReleaseFilesRequestObject,
) (GetSeasonReleaseFilesResponseObject, error) {
	number := request.Number
	files, f := s.packReleaseFiles(ctx, request.Id, &number, request.Body)
	if f == nil {
		return GetSeasonReleaseFiles200JSONResponse(files), nil
	}
	switch f.status {
	case http.StatusForbidden:
		return GetSeasonReleaseFiles403JSONResponse{
			ForbiddenJSONResponse(f.body),
		}, nil
	case http.StatusNotFound:
		return GetSeasonReleaseFiles404JSONResponse{
			NotFoundJSONResponse(f.body),
		}, nil
	}
	return GetSeasonReleaseFiles422JSONResponse{
		UnprocessableEntityJSONResponse(f.body),
	}, nil
}

func (s *Server) GetSeriesReleaseFiles(
	ctx context.Context,
	request GetSeriesReleaseFilesRequestObject,
) (GetSeriesReleaseFilesResponseObject, error) {
	files, f := s.packReleaseFiles(ctx, request.Id, nil, request.Body)
	if f == nil {
		return GetSeriesReleaseFiles200JSONResponse(files), nil
	}
	switch f.status {
	case http.StatusForbidden:
		return GetSeriesReleaseFiles403JSONResponse{
			ForbiddenJSONResponse(f.body),
		}, nil
	case http.StatusNotFound:
		return GetSeriesReleaseFiles404JSONResponse{
			NotFoundJSONResponse(f.body),
		}, nil
	}
	return GetSeriesReleaseFiles422JSONResponse{
		UnprocessableEntityJSONResponse(f.body),
	}, nil
}
