package restapi

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/media/book"
	"github.com/datahearth/streamline/internal/metadata"
)

const (
	invalidSlotKindMsg = "kind must be ebook or audiobook"

	// codeSeriesVolume marks a 409 raised because a volume is removed with its
	// series, not on its own.
	codeSeriesVolume = "series_volume"

	minBookQueryLen = 2
)

var errBookProviderMissing = errors.New(
	"hardcover is not configured: set metadata.hardcover_api_key",
)

// providerUnavailable reports whether err means Hardcover cannot be reached
// with the configured credentials, which is the operator's to fix and not a
// fault in the request.
func providerUnavailable(err error) bool {
	return errors.Is(err, book.ErrNotConfigured) ||
		errors.Is(err, metadata.ErrHardcoverKeyMissing) ||
		errors.Is(err, metadata.ErrHardcoverUnauthorized)
}

func rateLimited(err error) bool { return errors.Is(err, metadata.ErrRateLimited) }

func errSeriesVolume(msg string) ConflictJSONResponse {
	code := codeSeriesVolume
	return ConflictJSONResponse{Message: msg, Code: &code}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefOr[T ~string](v *T) string {
	if v == nil {
		return ""
	}
	return string(*v)
}

// invalidInput reports an input the service refused as unprocessable.
func invalidInput(err error) bool {
	return errors.Is(err, book.ErrInvalidMonitor) ||
		errors.Is(err, book.ErrUnknownProfile) ||
		errors.Is(err, book.ErrInvalidLanguage) ||
		errors.Is(err, book.ErrInvalidKind) ||
		errors.Is(err, book.ErrEditionMismatch) ||
		errors.Is(err, book.ErrUnknownEdition) ||
		errors.Is(err, book.ErrInvalidSlotKind)
}

// parseShelfKinds validates the comma list of kinds; an unknown one is the
// caller's to fix.
func parseShelfKinds(in *BookShelfKindParam) ([]string, error) {
	if in == nil {
		return nil, nil
	}
	out := make([]string, 0, len(*in))
	for _, k := range *in {
		if !slices.Contains(config.BookKinds, string(k)) {
			return nil, fmt.Errorf("unknown kind %q", k)
		}
		out = append(out, string(k))
	}
	return out, nil
}

func (s *Server) SearchBooks(
	ctx context.Context,
	req SearchBooksRequestObject,
) (SearchBooksResponseObject, error) {
	if len(strings.TrimSpace(req.Params.Query)) < minBookQueryLen {
		return SearchBooks400JSONResponse{
			BadRequestJSONResponse: errBadRequest(
				"query must be at least 2 characters",
			),
		}, nil
	}
	kind := "all"
	if t := req.Params.Type; t != nil {
		if *t != "all" && *t != "book" && *t != "series" {
			return SearchBooks400JSONResponse{
				BadRequestJSONResponse: errBadRequest(
					"type must be book, series or all",
				),
			}, nil
		}
		kind = string(*t)
	}
	hits, err := s.books.Lookup(ctx, strings.TrimSpace(req.Params.Query), kind)
	switch {
	case rateLimited(err):
		return SearchBooks429JSONResponse{
			RateLimitedJSONResponse: errRateLimited(err),
		}, nil
	case providerUnavailable(err):
		return SearchBooks503JSONResponse{
			ServiceUnavailableJSONResponse: errServiceUnavailable(
				errBookProviderMissing.Error(),
			),
		}, nil
	case err != nil:
		return SearchBooks500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	items := make([]BookLookupHit, 0, len(hits))
	for _, h := range hits {
		items = append(items, toAPILookupHit(h))
	}
	return SearchBooks200JSONResponse{
		Items: items,
	}, nil
}

func (s *Server) GetBookLookup(
	ctx context.Context,
	req GetBookLookupRequestObject,
) (GetBookLookupResponseObject, error) {
	if t := req.Params.Type; t != BookShelfTypeBook && t != BookShelfTypeSeries {
		return GetBookLookup400JSONResponse{
			BadRequestJSONResponse: errBadRequest("type must be book or series"),
		}, nil
	}
	d, err := s.books.LookupDetail(ctx, string(req.Params.Type), req.HardcoverId)
	switch {
	case errors.Is(err, book.ErrHardcoverNotFound):
		return GetBookLookup404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case rateLimited(err):
		return GetBookLookup429JSONResponse{
			RateLimitedJSONResponse: errRateLimited(err),
		}, nil
	case providerUnavailable(err):
		return GetBookLookup503JSONResponse{
			ServiceUnavailableJSONResponse: errServiceUnavailable(
				errBookProviderMissing.Error(),
			),
		}, nil
	case err != nil:
		return GetBookLookup500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return GetBookLookup200JSONResponse{
		BookLookupDetailResponseJSONResponse: BookLookupDetailResponseJSONResponse(
			toAPILookupDetail(d),
		),
	}, nil
}

// shelfParams validates and assembles the shelf filters shared by the list and
// the counts. msg is the 400 body when a value is refused.
func shelfParams(
	status, author, query *string,
	format *BookFormat,
	kinds *BookShelfKindParam,
) (book.ListParams, string) {
	p := book.ListParams{}
	if status != nil && *status != "" && *status != "all" {
		switch *status {
		case "available", "wanted", "downloading":
			p.Status = *status
		default:
			return p, "status must be all, available, wanted or downloading"
		}
	}
	if author != nil {
		p.Author = *author
	}
	if query != nil {
		p.Query = *query
	}
	if format != nil {
		if *format != BookFormatEbook && *format != BookFormatAudiobook {
			return p, invalidSlotKindMsg
		}
		p.Format = string(*format)
	}
	ks, err := parseShelfKinds(kinds)
	if err != nil {
		return p, err.Error()
	}
	p.Kinds = ks
	return p, ""
}

func strOf[T ~string](v *T) *string {
	if v == nil {
		return nil
	}
	s := string(*v)
	return &s
}

func (s *Server) ListBooks(
	ctx context.Context,
	req ListBooksRequestObject,
) (ListBooksResponseObject, error) {
	page, ok := positiveOr(req.Params.Page, uint16(1))
	if !ok {
		return ListBooks400JSONResponse{
			BadRequestJSONResponse: errBadRequest(msgZeroPage),
		}, nil
	}
	limit, ok := limitOr(req.Params.Limit, 20, bookMaxLimit)
	if !ok {
		return ListBooks400JSONResponse{
			BadRequestJSONResponse: errBadRequest(limitRangeMsg(bookMaxLimit)),
		}, nil
	}
	p, msg := shelfParams(
		strOf(req.Params.Status), req.Params.Author, req.Params.Query,
		req.Params.Format, req.Params.Kind,
	)
	if msg != "" {
		return ListBooks400JSONResponse{
			BadRequestJSONResponse: errBadRequest(msg),
		}, nil
	}
	p.Page, p.Limit = uint32(page), uint32(limit)
	p.Sort = "added"
	p.Desc = true
	if sort := req.Params.Sort; sort != nil {
		p.Sort = string(*sort)
		p.Desc = p.Sort == "added"
	}
	if order := req.Params.Order; order != nil {
		p.Desc = *order == ListBooksParamsOrderDesc
	}

	res, err := s.books.List(ctx, p)
	if err != nil {
		return ListBooks500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	items := make([]ShelfItem, 0, len(res.Rows))
	for _, r := range res.Rows {
		items = append(items, toShelfItem(r, res.Progress))
	}
	return ListBooks200JSONResponse{
		Items: items, Total: res.Total, Page: uint32(page), Limit: limit,
	}, nil
}

func (s *Server) GetBookCounts(
	ctx context.Context,
	req GetBookCountsRequestObject,
) (GetBookCountsResponseObject, error) {
	p, msg := shelfParams(
		strOf(req.Params.Status), req.Params.Author, req.Params.Query,
		req.Params.Format, req.Params.Kind,
	)
	if msg != "" {
		return GetBookCounts400JSONResponse{
			BadRequestJSONResponse: errBadRequest(msg),
		}, nil
	}
	c, err := s.books.Counts(ctx, p)
	if err != nil {
		return GetBookCounts500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return GetBookCounts200JSONResponse{
		BookCountsResponseJSONResponse: BookCountsResponseJSONResponse(
			toBookCounts(c),
		),
	}, nil
}

func (s *Server) AddBook(
	ctx context.Context,
	req AddBookRequestObject,
) (AddBookResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return AddBook403JSONResponse{ForbiddenJSONResponse: requestOnlyResp}, nil
	}
	row, err := s.books.AddBook(ctx, book.AddBookParams{
		HardcoverID:    req.Body.HardcoverId,
		Monitor:        derefOr(req.Body.Monitor),
		QualityProfile: deref(req.Body.QualityProfile),
	})
	switch {
	case err == nil:
	case errors.Is(err, book.ErrBookExists):
		return AddBook409JSONResponse{
			ConflictJSONResponse: errConflict(err.Error()),
		}, nil
	case errors.Is(err, book.ErrHardcoverNotFound):
		return AddBook404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case invalidInput(err):
		return AddBook422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	case rateLimited(err):
		return AddBook429JSONResponse{
			RateLimitedJSONResponse: errRateLimited(err),
		}, nil
	case providerUnavailable(err):
		return AddBook503JSONResponse{
			ServiceUnavailableJSONResponse: errServiceUnavailable(
				errBookProviderMissing.Error(),
			),
		}, nil
	default:
		return AddBook500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return AddBook201JSONResponse{
		BookCreatedJSONResponse: BookCreatedJSONResponse(toAPIBook(row, nil)),
	}, nil
}

func (s *Server) AddBookSeries(
	ctx context.Context,
	req AddBookSeriesRequestObject,
) (AddBookSeriesResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return AddBookSeries403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	row, err := s.books.AddSeries(ctx, book.AddSeriesParams{
		HardcoverID:    req.Body.HardcoverId,
		Monitor:        derefOr(req.Body.Monitor),
		QualityProfile: deref(req.Body.QualityProfile),
	})
	switch {
	case err == nil:
	case errors.Is(err, book.ErrSeriesExists):
		return AddBookSeries409JSONResponse{
			ConflictJSONResponse: errConflict(err.Error()),
		}, nil
	case errors.Is(err, book.ErrHardcoverNotFound):
		return AddBookSeries404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case invalidInput(err):
		return AddBookSeries422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	case rateLimited(err):
		return AddBookSeries429JSONResponse{
			RateLimitedJSONResponse: errRateLimited(err),
		}, nil
	case providerUnavailable(err):
		return AddBookSeries503JSONResponse{
			ServiceUnavailableJSONResponse: errServiceUnavailable(
				errBookProviderMissing.Error(),
			),
		}, nil
	default:
		return AddBookSeries500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return AddBookSeries201JSONResponse{
		BookSeriesCreatedJSONResponse: BookSeriesCreatedJSONResponse(
			toAPISeries(row),
		),
	}, nil
}

func (s *Server) GetBook(
	ctx context.Context,
	req GetBookRequestObject,
) (GetBookResponseObject, error) {
	row, err := s.books.GetBook(ctx, req.Id)
	switch {
	case errors.Is(err, book.ErrBookNotFound):
		return GetBook404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case err != nil:
		return GetBook500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	progress := s.books.Progress(ctx, []uint32{row.ID})[row.ID]
	return GetBook200JSONResponse{
		BookDetailJSONResponse: BookDetailJSONResponse(toAPIBook(row, progress)),
	}, nil
}

func (s *Server) GetBookSeries(
	ctx context.Context,
	req GetBookSeriesRequestObject,
) (GetBookSeriesResponseObject, error) {
	row, err := s.books.GetSeries(ctx, req.Id)
	switch {
	case errors.Is(err, book.ErrSeriesNotFound):
		return GetBookSeries404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case err != nil:
		return GetBookSeries500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return GetBookSeries200JSONResponse{
		BookSeriesDetailJSONResponse: BookSeriesDetailJSONResponse(toAPISeries(row)),
	}, nil
}

func (s *Server) PatchBook(
	ctx context.Context,
	req PatchBookRequestObject,
) (PatchBookResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return PatchBook403JSONResponse{ForbiddenJSONResponse: requestOnlyResp}, nil
	}
	b := req.Body
	row, err := s.books.PatchBook(ctx, req.Id, book.PatchBookParams{
		Monitor:           strOf(b.Monitor),
		PreferredLanguage: b.PreferredLanguage,
		QualityProfile:    b.QualityProfile,
		Kind:              strOf(b.Kind),
		Format:            strOf(b.Format),
		EditionID:         b.EditionId,
	})
	switch {
	case err == nil:
	case errors.Is(err, book.ErrBookNotFound):
		return PatchBook404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case invalidInput(err):
		return PatchBook422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	default:
		return PatchBook500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	progress := s.books.Progress(ctx, []uint32{row.ID})[row.ID]
	return PatchBook200JSONResponse{
		BookDetailJSONResponse: BookDetailJSONResponse(toAPIBook(row, progress)),
	}, nil
}

func (s *Server) PatchBookSeries(
	ctx context.Context,
	req PatchBookSeriesRequestObject,
) (PatchBookSeriesResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return PatchBookSeries403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	b := req.Body
	row, err := s.books.PatchSeries(ctx, req.Id, book.PatchSeriesParams{
		Monitor:        strOf(b.Monitor),
		QualityProfile: b.QualityProfile,
		Edition:        b.Edition,
	})
	switch {
	case err == nil:
	case errors.Is(err, book.ErrSeriesNotFound):
		return PatchBookSeries404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case invalidInput(err):
		return PatchBookSeries422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	default:
		return PatchBookSeries500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return PatchBookSeries200JSONResponse{
		BookSeriesDetailJSONResponse: BookSeriesDetailJSONResponse(toAPISeries(row)),
	}, nil
}

func (s *Server) DeleteBook(
	ctx context.Context,
	req DeleteBookRequestObject,
) (DeleteBookResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return DeleteBook403JSONResponse{ForbiddenJSONResponse: requestOnlyResp}, nil
	}
	deleteFiles := req.Params.DeleteFiles != nil && *req.Params.DeleteFiles
	err := s.books.DeleteBook(ctx, req.Id, deleteFiles)
	switch {
	case err == nil:
		return DeleteBook204Response{}, nil
	case errors.Is(err, book.ErrBookNotFound):
		return DeleteBook404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case errors.Is(err, book.ErrSeriesVolume):
		return DeleteBook409JSONResponse{
			ConflictJSONResponse: errSeriesVolume(err.Error()),
		}, nil
	}
	return DeleteBook500JSONResponse{
		InternalErrorJSONResponse: errInternal(ctx, err),
	}, nil
}

func (s *Server) DeleteBookSeries(
	ctx context.Context,
	req DeleteBookSeriesRequestObject,
) (DeleteBookSeriesResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return DeleteBookSeries403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	deleteFiles := req.Params.DeleteFiles != nil && *req.Params.DeleteFiles
	err := s.books.DeleteSeries(ctx, req.Id, deleteFiles)
	switch {
	case err == nil:
		return DeleteBookSeries204Response{}, nil
	case errors.Is(err, book.ErrSeriesNotFound):
		return DeleteBookSeries404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	}
	return DeleteBookSeries500JSONResponse{
		InternalErrorJSONResponse: errInternal(ctx, err),
	}, nil
}

func (s *Server) RefreshBook(
	ctx context.Context,
	req RefreshBookRequestObject,
) (RefreshBookResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return RefreshBook403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	row, err := s.books.RefreshBook(ctx, req.Id)
	switch {
	case err == nil:
	case errors.Is(err, book.ErrBookNotFound),
		errors.Is(err, book.ErrHardcoverNotFound):
		return RefreshBook404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case rateLimited(err):
		return RefreshBook429JSONResponse{
			RateLimitedJSONResponse: errRateLimited(err),
		}, nil
	case providerUnavailable(err):
		return RefreshBook503JSONResponse{
			ServiceUnavailableJSONResponse: errServiceUnavailable(
				errBookProviderMissing.Error(),
			),
		}, nil
	default:
		return RefreshBook500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	progress := s.books.Progress(ctx, []uint32{row.ID})[row.ID]
	return RefreshBook200JSONResponse{
		BookDetailJSONResponse: BookDetailJSONResponse(toAPIBook(row, progress)),
	}, nil
}

func (s *Server) RefreshBookSeries(
	ctx context.Context,
	req RefreshBookSeriesRequestObject,
) (RefreshBookSeriesResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return RefreshBookSeries403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	row, err := s.books.RefreshSeries(ctx, req.Id)
	switch {
	case err == nil:
	case errors.Is(err, book.ErrSeriesNotFound),
		errors.Is(err, book.ErrHardcoverNotFound):
		return RefreshBookSeries404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case rateLimited(err):
		return RefreshBookSeries429JSONResponse{
			RateLimitedJSONResponse: errRateLimited(err),
		}, nil
	case providerUnavailable(err):
		return RefreshBookSeries503JSONResponse{
			ServiceUnavailableJSONResponse: errServiceUnavailable(
				errBookProviderMissing.Error(),
			),
		}, nil
	default:
		return RefreshBookSeries500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return RefreshBookSeries200JSONResponse{
		BookSeriesDetailJSONResponse: BookSeriesDetailJSONResponse(toAPISeries(row)),
	}, nil
}

func (s *Server) SearchBookReleases(
	ctx context.Context,
	req SearchBookReleasesRequestObject,
) (SearchBookReleasesResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return SearchBookReleases403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	results, err := s.books.SearchBookReleases(ctx, req.Id, derefOr(req.Params.Kind))
	switch {
	case err == nil:
	case errors.Is(err, book.ErrInvalidSlotKind):
		return SearchBookReleases400JSONResponse{
			BadRequestJSONResponse: errBadRequest(invalidSlotKindMsg),
		}, nil
	case errors.Is(err, book.ErrBookNotFound):
		return SearchBookReleases404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case errors.Is(err, book.ErrNoQualityProfile):
		return SearchBookReleases422JSONResponse{
			UnprocessableEntityJSONResponse: errNoQualityProfile(err.Error()),
		}, nil
	default:
		return SearchBookReleases500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	private := indexerPrivacy()
	items := make([]SearchResult, 0, len(results))
	for _, r := range results {
		items = append(items, toBookRelease(r, private))
	}
	return SearchBookReleases200JSONResponse{
		Items: items,
	}, nil
}

func (s *Server) GrabBookRelease(
	ctx context.Context,
	req GrabBookReleaseRequestObject,
) (GrabBookReleaseResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return GrabBookRelease403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	query, slot := derefOr(req.Params.Kind), ""
	if req.Body != nil && req.Body.Slot != nil {
		slot = string(*req.Body.Slot)
	}
	if query != "" && slot != "" && query != slot {
		return GrabBookRelease400JSONResponse{
			BadRequestJSONResponse: errBadRequest(
				"kind and the release's slot name different slots",
			),
		}, nil
	}
	sr, err := toIndexerResult(req.Body)
	switch {
	case errors.Is(err, errBadReleaseHandle):
		return GrabBookRelease422JSONResponse{
			UnprocessableEntityJSONResponse: errGrabRejected(err.Error()),
		}, nil
	case err != nil:
		return GrabBookRelease422JSONResponse{
			UnprocessableEntityJSONResponse: unprocessableResp(err.Error()),
		}, nil
	}
	err = s.books.GrabBookRelease(ctx, req.Id, book.GrabParams{
		Kind:            query,
		Slot:            slot,
		Result:          sr,
		ReplaceExisting: replaceExisting(req.Body),
	})
	switch {
	case err == nil:
		return GrabBookRelease202Response{}, nil
	case errors.Is(err, book.ErrBookNotFound):
		return GrabBookRelease404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case errors.Is(err, book.ErrInvalidSlotKind):
		return GrabBookRelease400JSONResponse{
			BadRequestJSONResponse: errBadRequest(invalidSlotKindMsg),
		}, nil
	case errors.Is(err, book.ErrNoQualityProfile):
		return GrabBookRelease422JSONResponse{
			UnprocessableEntityJSONResponse: errNoQualityProfile(err.Error()),
		}, nil
	case errors.Is(err, book.ErrSlotMismatch),
		errors.Is(err, book.ErrNotHydrated),
		errors.Is(err, download.ErrUntrustedSource),
		errors.Is(err, download.ErrClientFull),
		errors.Is(err, download.ErrUnsafeTorrentName):
		return GrabBookRelease422JSONResponse{
			UnprocessableEntityJSONResponse: errGrabRejected(err.Error()),
		}, nil
	}
	return GrabBookRelease500JSONResponse{
		InternalErrorJSONResponse: errInternal(ctx, err),
	}, nil
}

func (s *Server) SearchBookNow(
	ctx context.Context,
	req SearchBookNowRequestObject,
) (SearchBookNowResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return SearchBookNow403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	queued, err := s.books.SearchNowBook(ctx, req.Id, derefOr(req.Params.Kind))
	switch {
	case err == nil:
	case errors.Is(err, book.ErrBookNotFound):
		return SearchBookNow404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	default:
		return SearchBookNow500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return SearchBookNow202JSONResponse{
		Queued: queued,
	}, nil
}

func (s *Server) SearchBookSeriesNow(
	ctx context.Context,
	req SearchBookSeriesNowRequestObject,
) (SearchBookSeriesNowResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return SearchBookSeriesNow403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	queued, err := s.books.SearchNowSeries(ctx, req.Id)
	switch {
	case err == nil:
	case errors.Is(err, book.ErrSeriesNotFound):
		return SearchBookSeriesNow404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	default:
		return SearchBookSeriesNow500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return SearchBookSeriesNow202JSONResponse{
		Queued: queued,
	}, nil
}

func (s *Server) RenameBookFiles(
	ctx context.Context,
	req RenameBookFilesRequestObject,
) (RenameBookFilesResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return RenameBookFiles403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	preview := req.Params.Preview != nil && *req.Params.Preview
	plan, err := s.books.RenameBook(ctx, req.Id, preview)
	switch {
	case err == nil:
	case errors.Is(err, book.ErrBookNotFound):
		return RenameBookFiles404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	default:
		return RenameBookFiles500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return RenameBookFiles200JSONResponse{
		BookId:     req.Id,
		Operations: toRenameOperations(plan.Operations),
	}, nil
}

func (s *Server) RenameBookSeriesFiles(
	ctx context.Context,
	req RenameBookSeriesFilesRequestObject,
) (RenameBookSeriesFilesResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return RenameBookSeriesFiles403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	preview := req.Params.Preview != nil && *req.Params.Preview
	plan, err := s.books.RenameSeries(ctx, req.Id, preview)
	switch {
	case err == nil:
	case errors.Is(err, book.ErrSeriesNotFound):
		return RenameBookSeriesFiles404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	default:
		return RenameBookSeriesFiles500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return RenameBookSeriesFiles200JSONResponse{
		SeriesId:   req.Id,
		Operations: toRenameOperations(plan.Operations),
	}, nil
}
