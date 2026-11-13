package restapi

import (
	"context"
	"errors"
	"mime/multipart"

	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/library/roothealth"
	"github.com/datahearth/streamline/internal/media/book"
	"github.com/datahearth/streamline/internal/media/music"
	"github.com/datahearth/streamline/internal/server/middleware"
	"github.com/datahearth/streamline/internal/upload"
)

const (
	codeUploadRejected = "upload_rejected"
	codeUploadConflict = "upload_conflict"
	codeNoSpace        = "no_space"

	maxUploadPreviewFiles = 5000
)

var errTooManyPreviewFiles = errors.New("too many files to preview")

type uploadFailure int

const (
	failInternal uploadFailure = iota
	failNotFound
	failConflict
	failRejected
	failNoSpace
	failNoProfile
)

func classifyUpload(err error) uploadFailure {
	switch {
	case errors.Is(err, music.ErrAlbumNotFound),
		errors.Is(err, music.ErrArtistNotFound),
		errors.Is(err, music.ErrTrackNotFound),
		errors.Is(err, book.ErrBookNotFound):
		return failNotFound
	case errors.Is(err, upload.ErrConflict):
		return failConflict
	case errors.Is(err, upload.ErrRejected),
		errors.Is(err, errTooManyPreviewFiles),
		errors.Is(err, book.ErrNotHydrated),
		errors.Is(err, book.ErrInvalidSlotKind):
		return failRejected
	case errors.Is(err, upload.ErrNoSpace):
		return failNoSpace
	case errors.Is(err, music.ErrNoQualityProfile),
		errors.Is(err, book.ErrNoQualityProfile):
		return failNoProfile
	}
	return failInternal
}

func errUploadRejected(msg string) UnprocessableEntityJSONResponse {
	code := codeUploadRejected
	return UnprocessableEntityJSONResponse{Message: msg, Code: &code}
}

func errNoSpace(msg string) UnprocessableEntityJSONResponse {
	code := codeNoSpace
	return UnprocessableEntityJSONResponse{Message: msg, Code: &code}
}

func errUploadConflict(msg string) ConflictJSONResponse {
	code := codeUploadConflict
	return ConflictJSONResponse{Message: msg, Code: &code}
}

// unprocessableUpload maps the failures that answer 422; ok is false for the
// ones that do not.
func unprocessableUpload(
	kind uploadFailure,
	err error,
) (UnprocessableEntityJSONResponse, bool) {
	switch kind {
	case failRejected:
		return errUploadRejected(err.Error()), true
	case failNoSpace:
		return errNoSpace(err.Error()), true
	case failNoProfile:
		return errNoQualityProfile(err.Error()), true
	}
	return UnprocessableEntityJSONResponse{}, false
}

// receiver is the body half of an upload, run by the service once it has
// checked the target.
func receiver(
	body *multipart.Reader,
	wav bool,
) func(context.Context, upload.Kind) (*upload.Staged, error) {
	return func(ctx context.Context, kind upload.Kind) (*upload.Staged, error) {
		return upload.Receive(ctx, body, kind, wav)
	}
}

func acceptedUpload(id uint32) UploadAcceptedJSONResponse {
	return UploadAcceptedJSONResponse{RecordId: id}
}

func uploadReplace(p *UploadReplaceExisting) bool {
	return p != nil && *p
}

func uploadTitle(p *UploadTitle) string {
	if p == nil {
		return ""
	}
	return *p
}

func previewFiles(body *UploadPreviewRequest) ([]download.ReleaseFile, error) {
	if body == nil {
		return nil, nil
	}
	if len(body.Files) > maxUploadPreviewFiles {
		return nil, errTooManyPreviewFiles
	}
	out := make([]download.ReleaseFile, len(body.Files))
	for i, f := range body.Files {
		out[i] = download.ReleaseFile{Path: f.Path, Size: f.Size}
	}
	return out, nil
}

func tooManyFiles() UnprocessableEntityJSONResponse {
	return errUploadRejected("at most 5000 files can be previewed at once")
}

func toUploadPreview(views []book.UploadFileView) UploadPreview {
	out := UploadPreview{Files: make([]UploadPreviewOutcome, 0, len(views))}
	for _, v := range views {
		out.Files = append(out.Files, UploadPreviewOutcome{
			Path:    v.Path,
			Size:    v.Size,
			Outcome: UploadPreviewOutcomeOutcome(v.Outcome),
		})
	}
	return out
}

func (s *Server) UploadMusicAlbum(
	ctx context.Context,
	request UploadMusicAlbumRequestObject,
) (UploadMusicAlbumResponseObject, error) {
	if err := roothealth.Require(ctx, db.RootMusic); err != nil {
		return nil, err
	}
	err := upload.CheckSpace(middleware.UploadLength(ctx))
	var rec uint32
	if err == nil {
		o := music.UploadOptions{
			Replace: uploadReplace(request.Params.ReplaceExisting),
			Title:   uploadTitle(request.Params.Title),
		}
		r, uerr := s.music.UploadAlbum(
			ctx, request.Id, o,
			receiver(request.Body, library.ConvertsWAV(s.prober)),
		)
		err = uerr
		if r != nil {
			rec = r.ID
		}
	}
	kind := classifyUpload(err)
	if err == nil {
		s.importer.Enqueue(rec)
		return UploadMusicAlbum202JSONResponse{
			UploadAcceptedJSONResponse: acceptedUpload(rec),
		}, nil
	}
	if r, ok := unprocessableUpload(kind, err); ok {
		return UploadMusicAlbum422JSONResponse{
			UnprocessableEntityJSONResponse: r,
		}, nil
	}
	switch kind {
	case failNotFound:
		return UploadMusicAlbum404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case failConflict:
		return UploadMusicAlbum409JSONResponse{
			ConflictJSONResponse: errUploadConflict(err.Error()),
		}, nil
	}
	return UploadMusicAlbum500JSONResponse{
		InternalErrorJSONResponse: errInternal(ctx, err),
	}, nil
}

func (s *Server) UploadMusicTrack(
	ctx context.Context,
	request UploadMusicTrackRequestObject,
) (UploadMusicTrackResponseObject, error) {
	if err := roothealth.Require(ctx, db.RootMusic); err != nil {
		return nil, err
	}
	err := upload.CheckSpace(middleware.UploadLength(ctx))
	var rec uint32
	if err == nil {
		o := music.UploadOptions{
			Replace: uploadReplace(request.Params.ReplaceExisting),
			Title:   uploadTitle(request.Params.Title),
		}
		r, uerr := s.music.UploadTrack(
			ctx, request.Id, o,
			receiver(request.Body, library.ConvertsWAV(s.prober)),
		)
		err = uerr
		if r != nil {
			rec = r.ID
		}
	}
	kind := classifyUpload(err)
	if err == nil {
		s.importer.Enqueue(rec)
		return UploadMusicTrack202JSONResponse{
			UploadAcceptedJSONResponse: acceptedUpload(rec),
		}, nil
	}
	if r, ok := unprocessableUpload(kind, err); ok {
		return UploadMusicTrack422JSONResponse{
			UnprocessableEntityJSONResponse: r,
		}, nil
	}
	switch kind {
	case failNotFound:
		return UploadMusicTrack404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case failConflict:
		return UploadMusicTrack409JSONResponse{
			ConflictJSONResponse: errUploadConflict(err.Error()),
		}, nil
	}
	return UploadMusicTrack500JSONResponse{
		InternalErrorJSONResponse: errInternal(ctx, err),
	}, nil
}

func (s *Server) UploadMusicArtist(
	ctx context.Context,
	request UploadMusicArtistRequestObject,
) (UploadMusicArtistResponseObject, error) {
	if err := roothealth.Require(ctx, db.RootMusic); err != nil {
		return nil, err
	}
	err := upload.CheckSpace(middleware.UploadLength(ctx))
	var rec uint32
	var skipped []string
	if err == nil {
		o := music.UploadOptions{
			Replace: uploadReplace(request.Params.ReplaceExisting),
			Title:   uploadTitle(request.Params.Title),
		}
		r, sk, uerr := s.music.UploadArtist(
			ctx, request.Id, o,
			receiver(request.Body, library.ConvertsWAV(s.prober)),
		)
		err = uerr
		skipped = sk
		if r != nil {
			rec = r.ID
		}
	}
	kind := classifyUpload(err)
	if err == nil {
		s.importer.Enqueue(rec)
		out := UploadAcceptedJSONResponse{RecordId: rec}
		if len(skipped) > 0 {
			out.SkippedFolders = &skipped
		}
		return UploadMusicArtist202JSONResponse{UploadAcceptedJSONResponse: out}, nil
	}
	if r, ok := unprocessableUpload(kind, err); ok {
		return UploadMusicArtist422JSONResponse{
			UnprocessableEntityJSONResponse: r,
		}, nil
	}
	switch kind {
	case failNotFound:
		return UploadMusicArtist404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case failConflict:
		return UploadMusicArtist409JSONResponse{
			ConflictJSONResponse: errUploadConflict(err.Error()),
		}, nil
	}
	return UploadMusicArtist500JSONResponse{
		InternalErrorJSONResponse: errInternal(ctx, err),
	}, nil
}

func (s *Server) UploadBook(
	ctx context.Context,
	request UploadBookRequestObject,
) (UploadBookResponseObject, error) {
	if err := roothealth.Require(
		ctx,
		db.BookRoot(string(request.Params.Slot)),
	); err != nil {
		return nil, err
	}
	err := upload.CheckSpace(middleware.UploadLength(ctx))
	var rec uint32
	if err == nil {
		o := book.UploadOptions{
			Replace: uploadReplace(request.Params.ReplaceExisting),
			Title:   uploadTitle(request.Params.Title),
		}
		r, uerr := s.books.UploadBook(
			ctx,
			request.Id,
			string(request.Params.Slot),
			o,
			receiver(request.Body, false),
		)
		err = uerr
		if r != nil {
			rec = r.ID
		}
	}
	kind := classifyUpload(err)
	if err == nil {
		s.importer.Enqueue(rec)
		return UploadBook202JSONResponse{
			UploadAcceptedJSONResponse: acceptedUpload(rec),
		}, nil
	}
	if r, ok := unprocessableUpload(kind, err); ok {
		return UploadBook422JSONResponse{UnprocessableEntityJSONResponse: r}, nil
	}
	switch kind {
	case failNotFound:
		return UploadBook404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case failConflict:
		return UploadBook409JSONResponse{
			ConflictJSONResponse: errUploadConflict(err.Error()),
		}, nil
	}
	return UploadBook500JSONResponse{
		InternalErrorJSONResponse: errInternal(ctx, err),
	}, nil
}

func (s *Server) PreviewMusicAlbumUpload(
	ctx context.Context,
	request PreviewMusicAlbumUploadRequestObject,
) (PreviewMusicAlbumUploadResponseObject, error) {
	files, err := previewFiles(request.Body)
	if err != nil {
		return PreviewMusicAlbumUpload422JSONResponse{
			UnprocessableEntityJSONResponse: tooManyFiles(),
		}, nil
	}
	view, err := s.music.AlbumUploadPreview(ctx, request.Id, files)
	if err == nil {
		out := toReleaseFiles(view)
		if view.TracksPending {
			out.TracksPending = &view.TracksPending
		}
		return PreviewMusicAlbumUpload200JSONResponse(out), nil
	}
	kind := classifyUpload(err)
	if r, ok := unprocessableUpload(kind, err); ok {
		return PreviewMusicAlbumUpload422JSONResponse{
			UnprocessableEntityJSONResponse: r,
		}, nil
	}
	switch kind {
	case failNotFound:
		return PreviewMusicAlbumUpload404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case failConflict:
		return PreviewMusicAlbumUpload409JSONResponse{
			ConflictJSONResponse: errUploadConflict(err.Error()),
		}, nil
	}
	return PreviewMusicAlbumUpload500JSONResponse{
		InternalErrorJSONResponse: errInternal(ctx, err),
	}, nil
}

func (s *Server) PreviewMusicArtistUpload(
	ctx context.Context,
	request PreviewMusicArtistUploadRequestObject,
) (PreviewMusicArtistUploadResponseObject, error) {
	files, err := previewFiles(request.Body)
	if err != nil {
		return PreviewMusicArtistUpload422JSONResponse{
			UnprocessableEntityJSONResponse: tooManyFiles(),
		}, nil
	}
	view, err := s.music.ArtistUploadPreview(ctx, request.Id, files)
	if err == nil {
		return PreviewMusicArtistUpload200JSONResponse(toDiscographyFiles(view)), nil
	}
	kind := classifyUpload(err)
	if r, ok := unprocessableUpload(kind, err); ok {
		return PreviewMusicArtistUpload422JSONResponse{
			UnprocessableEntityJSONResponse: r,
		}, nil
	}
	switch kind {
	case failNotFound:
		return PreviewMusicArtistUpload404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case failConflict:
		return PreviewMusicArtistUpload409JSONResponse{
			ConflictJSONResponse: errUploadConflict(err.Error()),
		}, nil
	}
	return PreviewMusicArtistUpload500JSONResponse{
		InternalErrorJSONResponse: errInternal(ctx, err),
	}, nil
}

func (s *Server) PreviewBookUpload(
	ctx context.Context,
	request PreviewBookUploadRequestObject,
) (PreviewBookUploadResponseObject, error) {
	files, err := previewFiles(request.Body)
	if err != nil {
		return PreviewBookUpload422JSONResponse{
			UnprocessableEntityJSONResponse: tooManyFiles(),
		}, nil
	}
	views, err := s.books.BookUploadPreview(
		ctx, request.Id, string(request.Params.Slot), files,
	)
	if err == nil {
		return PreviewBookUpload200JSONResponse(toUploadPreview(views)), nil
	}
	kind := classifyUpload(err)
	if r, ok := unprocessableUpload(kind, err); ok {
		return PreviewBookUpload422JSONResponse{
			UnprocessableEntityJSONResponse: r,
		}, nil
	}
	switch kind {
	case failNotFound:
		return PreviewBookUpload404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case failConflict:
		return PreviewBookUpload409JSONResponse{
			ConflictJSONResponse: errUploadConflict(err.Error()),
		}, nil
	}
	return PreviewBookUpload500JSONResponse{
		InternalErrorJSONResponse: errInternal(ctx, err),
	}, nil
}
