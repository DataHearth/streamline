package restapi

import (
	"context"
	"errors"

	"github.com/datahearth/streamline/internal/config"
)

func bookProfileToAPI(e config.BookQualityProfileEntry) BookQualityProfile {
	ebook := make([]EbookFormat, len(e.Ebook.Formats))
	for i, f := range e.Ebook.Formats {
		ebook[i] = EbookFormat(f)
	}
	audio := make([]AudiobookFormat, len(e.Audiobook.Formats))
	for i, f := range e.Audiobook.Formats {
		audio[i] = AudiobookFormat(f)
	}
	defaultFor := []BookKind{}
	if c := config.Get(); c != nil {
		for _, k := range c.BookDefaultFor(e.Name) {
			defaultFor = append(defaultFor, BookKind(k))
		}
	}
	return BookQualityProfile{
		Name:           e.Name,
		UpgradeAllowed: e.UpgradeAllowed,
		Ebook: EbookProfileSlot{
			Formats:   ebook,
			Preferred: EbookFormat(e.Ebook.Preferred),
		},
		Audiobook: AudiobookProfileSlot{
			Formats:    audio,
			Preferred:  AudiobookFormat(e.Audiobook.Preferred),
			MinBitrate: e.Audiobook.MinBitrate,
		},
		DefaultFor: defaultFor,
	}
}

func ebookSlotFromAPI(s EbookProfileSlot) config.EbookSlot {
	formats := make([]string, len(s.Formats))
	for i, f := range s.Formats {
		formats[i] = string(f)
	}
	return config.EbookSlot{Formats: formats, Preferred: string(s.Preferred)}
}

func audiobookSlotFromAPI(s AudiobookProfileSlot) config.AudiobookSlot {
	formats := make([]string, len(s.Formats))
	for i, f := range s.Formats {
		formats[i] = string(f)
	}
	return config.AudiobookSlot{
		Formats:    formats,
		Preferred:  string(s.Preferred),
		MinBitrate: s.MinBitrate,
	}
}

func (s *Server) ListBookQualityProfiles(
	_ context.Context,
	_ ListBookQualityProfilesRequestObject,
) (ListBookQualityProfilesResponseObject, error) {
	items := []BookQualityProfile{}
	if c := config.Get(); c != nil {
		for _, p := range c.BookQualityProfiles {
			items = append(items, bookProfileToAPI(p))
		}
	}
	return ListBookQualityProfiles200JSONResponse(items), nil
}

func (s *Server) CreateBookQualityProfile(
	ctx context.Context,
	request CreateBookQualityProfileRequestObject,
) (CreateBookQualityProfileResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return CreateBookQualityProfile403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	if request.Body == nil {
		return CreateBookQualityProfile422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable("body required"),
		}, nil
	}
	e := config.BookQualityProfileEntry{
		Name:           request.Body.Name,
		UpgradeAllowed: request.Body.UpgradeAllowed,
		Ebook:          ebookSlotFromAPI(request.Body.Ebook),
		Audiobook:      audiobookSlotFromAPI(request.Body.Audiobook),
	}
	switch err := config.AddBookQualityProfile(ctx, e); {
	case errors.Is(err, config.ErrBookQualityProfileExists):
		return CreateBookQualityProfile409JSONResponse{
			ConflictJSONResponse: errConflict(err.Error()),
		}, nil
	case configLocked(err):
		return CreateBookQualityProfile403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return CreateBookQualityProfile422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	stored, _ := config.LookupBookQualityProfile(e.Name)
	return CreateBookQualityProfile201JSONResponse{
		BookQualityProfileResponseJSONResponse: BookQualityProfileResponseJSONResponse(
			bookProfileToAPI(stored),
		),
	}, nil
}

func (s *Server) UpdateBookQualityProfile(
	ctx context.Context,
	request UpdateBookQualityProfileRequestObject,
) (UpdateBookQualityProfileResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return UpdateBookQualityProfile403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	if request.Body == nil {
		return UpdateBookQualityProfile422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable("body required"),
		}, nil
	}
	ebook := ebookSlotFromAPI(request.Body.Ebook)
	audiobook := audiobookSlotFromAPI(request.Body.Audiobook)
	patch := config.BookQualityProfilePatch{
		UpgradeAllowed: &request.Body.UpgradeAllowed,
		Ebook:          &ebook,
		Audiobook:      &audiobook,
	}
	switch err := config.UpdateBookQualityProfile(ctx, request.Name, patch); {
	case errors.Is(err, config.ErrBookQualityProfileNotFound):
		return UpdateBookQualityProfile404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case configLocked(err):
		return UpdateBookQualityProfile403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return UpdateBookQualityProfile422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	stored, _ := config.LookupBookQualityProfile(request.Name)
	return UpdateBookQualityProfile200JSONResponse{
		BookQualityProfileResponseJSONResponse: BookQualityProfileResponseJSONResponse(
			bookProfileToAPI(stored),
		),
	}, nil
}

// SetDefaultBookQualityProfile points one book kind's default at the named
// profile; it is also the way out of the 409 a delete answers while a profile
// is the default for any kind.
func (s *Server) SetDefaultBookQualityProfile(
	ctx context.Context,
	request SetDefaultBookQualityProfileRequestObject,
) (SetDefaultBookQualityProfileResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return SetDefaultBookQualityProfile403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	err := config.SetDefaultBookQualityProfile(
		ctx, request.Name, string(request.Params.Kind),
	)
	switch {
	case errors.Is(err, config.ErrBookQualityProfileNotFound):
		return SetDefaultBookQualityProfile404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case configLocked(err):
		return SetDefaultBookQualityProfile403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return SetDefaultBookQualityProfile500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return SetDefaultBookQualityProfile204Response{}, nil
}

func (s *Server) DeleteBookQualityProfile(
	ctx context.Context,
	request DeleteBookQualityProfileRequestObject,
) (DeleteBookQualityProfileResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return DeleteBookQualityProfile403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	switch err := config.DeleteBookQualityProfile(ctx, request.Name); {
	case errors.Is(err, config.ErrBookQualityProfileNotFound):
		return DeleteBookQualityProfile404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case errors.Is(err, config.ErrBookQualityProfileInUseAsDefault):
		return DeleteBookQualityProfile409JSONResponse{
			ConflictJSONResponse: errConflict(err.Error()),
		}, nil
	case configLocked(err):
		return DeleteBookQualityProfile403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return DeleteBookQualityProfile500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return DeleteBookQualityProfile204Response{}, nil
}
