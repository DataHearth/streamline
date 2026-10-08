package restapi

import (
	"context"
	"errors"
	"slices"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/media/book"
	"github.com/datahearth/streamline/internal/metadata"
)

const (
	slotKindEbook     = "ebook"
	slotKindAudiobook = "audiobook"

	invalidSlotKindMsg = "kind must be ebook or audiobook"
)

var (
	errBookProviderMissing = errors.New(
		"hardcover is not configured: set metadata.hardcover_api_key",
	)

	validMonitorPolicies = []string{"all", "future", "none"}
	validWantKinds       = []string{"ebook", "audiobook", "both"}
)

// providerUnavailable reports whether err means Hardcover cannot be reached
// with the configured credentials, which is the operator's to fix and not a
// fault in the request.
func providerUnavailable(err error) bool {
	return errors.Is(err, book.ErrNotConfigured) ||
		errors.Is(err, metadata.ErrHardcoverKeyMissing) ||
		errors.Is(err, metadata.ErrHardcoverUnauthorized)
}

func (s *Server) SearchBookAuthors(
	ctx context.Context,
	request SearchBookAuthorsRequestObject,
) (SearchBookAuthorsResponseObject, error) {
	if s.metadataBook == nil {
		return SearchBookAuthors503JSONResponse{
			ServiceUnavailableJSONResponse: errServiceUnavailable(
				errBookProviderMissing.Error(),
			),
		}, nil
	}
	results, err := s.metadataBook.SearchAuthors(ctx, request.Params.Query)
	switch {
	case errors.Is(err, metadata.ErrRateLimited):
		return SearchBookAuthors429JSONResponse{
			RateLimitedJSONResponse: errRateLimited(err),
		}, nil
	case providerUnavailable(err):
		return SearchBookAuthors503JSONResponse{
			ServiceUnavailableJSONResponse: errServiceUnavailable(err.Error()),
		}, nil
	case err != nil:
		return SearchBookAuthors500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	items := make([]BookAuthorSearchResult, 0, len(results))
	for _, r := range results {
		existing, err := s.store.FindAuthorByHardcoverID(ctx, r.HardcoverID)
		if err != nil {
			return SearchBookAuthors500JSONResponse{
				InternalErrorJSONResponse: errInternal(ctx, err),
			}, nil
		}
		items = append(items, BookAuthorSearchResult{
			HardcoverId:  r.HardcoverID,
			Name:         r.Name,
			BooksCount:   r.BooksCount,
			ImageUrl:     optString(r.ImageURL),
			AlreadyAdded: existing != nil,
		})
	}
	return SearchBookAuthors200JSONResponse{Items: items}, nil
}

func (s *Server) ListBookAuthors(
	ctx context.Context,
	request ListBookAuthorsRequestObject,
) (ListBookAuthorsResponseObject, error) {
	page, ok := positiveOr(request.Params.Page, uint16(1))
	if !ok {
		return ListBookAuthors400JSONResponse{
			BadRequestJSONResponse: errBadRequest(msgZeroPage),
		}, nil
	}
	limit, ok := limitOr(request.Params.Limit, 20, bookMaxLimit)
	if !ok {
		return ListBookAuthors400JSONResponse{
			BadRequestJSONResponse: errBadRequest(limitRangeMsg(bookMaxLimit)),
		}, nil
	}
	rows, total, err := s.books.List(ctx, page, limit)
	if err != nil {
		return ListBookAuthors500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	items := make([]BookAuthor, len(rows))
	for i, r := range rows {
		items[i] = bookAuthorToAPI(r)
		items[i].Books = nil
	}
	return ListBookAuthors200JSONResponse{
		Items: items,
		Total: total,
		Page:  uint32(page),
		Limit: limit,
	}, nil
}

func (s *Server) AddBookAuthor(
	ctx context.Context,
	request AddBookAuthorRequestObject,
) (AddBookAuthorResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return AddBookAuthor403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	body := request.Body
	p := book.AddParams{HardcoverID: body.HardcoverId, Monitored: true}
	if body.Monitored != nil {
		p.Monitored = *body.Monitored
	}
	if body.MonitorPolicy != nil {
		p.MonitorPolicy = string(*body.MonitorPolicy)
	}
	if body.WantKinds != nil {
		p.WantKinds = string(*body.WantKinds)
	}
	if body.EbookQualityProfile != nil {
		p.EbookQualityProfile = *body.EbookQualityProfile
	}
	if body.AudiobookQualityProfile != nil {
		p.AudiobookQualityProfile = *body.AudiobookQualityProfile
	}
	if msg := validateAuthorFields(
		p.MonitorPolicy,
		p.WantKinds,
		p.EbookQualityProfile,
		p.AudiobookQualityProfile,
	); msg != "" {
		return AddBookAuthor422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(msg),
		}, nil
	}
	a, err := s.books.Add(ctx, p)
	switch {
	case errors.Is(err, book.ErrAuthorExists):
		return AddBookAuthor409JSONResponse{
			ConflictJSONResponse: errConflict(err.Error()),
		}, nil
	case errors.Is(err, metadata.ErrRateLimited):
		return AddBookAuthor429JSONResponse{
			RateLimitedJSONResponse: errRateLimited(err),
		}, nil
	case providerUnavailable(err):
		return AddBookAuthor503JSONResponse{
			ServiceUnavailableJSONResponse: errServiceUnavailable(err.Error()),
		}, nil
	case err != nil:
		return AddBookAuthor500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return AddBookAuthor201JSONResponse{
		BookAuthorCreatedJSONResponse: BookAuthorCreatedJSONResponse(
			bookAuthorToAPI(a),
		),
	}, nil
}

func (s *Server) GetBookAuthor(
	ctx context.Context,
	request GetBookAuthorRequestObject,
) (GetBookAuthorResponseObject, error) {
	a, err := s.books.Get(ctx, request.Id)
	switch {
	case errors.Is(err, book.ErrAuthorNotFound):
		return GetBookAuthor404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case err != nil:
		return GetBookAuthor500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return GetBookAuthor200JSONResponse{
		BookAuthorDetailJSONResponse: BookAuthorDetailJSONResponse(
			bookAuthorToAPI(a),
		),
	}, nil
}

func (s *Server) PatchBookAuthor(
	ctx context.Context,
	request PatchBookAuthorRequestObject,
) (PatchBookAuthorResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return PatchBookAuthor403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	body := request.Body
	p := book.UpdateAuthorParams{
		Monitored:               body.Monitored,
		EbookQualityProfile:     body.EbookQualityProfile,
		AudiobookQualityProfile: body.AudiobookQualityProfile,
	}
	if body.MonitorPolicy != nil {
		v := string(*body.MonitorPolicy)
		p.MonitorPolicy = &v
	}
	if body.WantKinds != nil {
		v := string(*body.WantKinds)
		p.WantKinds = &v
	}
	if msg := validateAuthorFields(
		deref(p.MonitorPolicy), deref(p.WantKinds),
		deref(p.EbookQualityProfile), deref(p.AudiobookQualityProfile),
	); msg != "" {
		return PatchBookAuthor422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(msg),
		}, nil
	}
	if err := s.books.UpdateAuthor(ctx, request.Id, p); err != nil {
		return patchBookAuthorErr(ctx, err), nil
	}
	a, err := s.books.Get(ctx, request.Id)
	if err != nil {
		return patchBookAuthorErr(ctx, err), nil
	}
	return PatchBookAuthor200JSONResponse{
		BookAuthorDetailJSONResponse: BookAuthorDetailJSONResponse(
			bookAuthorToAPI(a),
		),
	}, nil
}

func patchBookAuthorErr(
	ctx context.Context,
	err error,
) PatchBookAuthorResponseObject {
	if errors.Is(err, book.ErrAuthorNotFound) {
		return PatchBookAuthor404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}
	}
	return PatchBookAuthor500JSONResponse{
		InternalErrorJSONResponse: errInternal(ctx, err),
	}
}

func (s *Server) DeleteBookAuthor(
	ctx context.Context,
	request DeleteBookAuthorRequestObject,
) (DeleteBookAuthorResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return DeleteBookAuthor403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	deleteFiles := request.Params.DeleteFiles != nil && *request.Params.DeleteFiles
	err := s.books.Delete(ctx, request.Id, deleteFiles)
	switch {
	case errors.Is(err, book.ErrAuthorNotFound):
		return DeleteBookAuthor404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case err != nil:
		return DeleteBookAuthor500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return DeleteBookAuthor204Response{}, nil
}

func (s *Server) RefreshBookAuthor(
	ctx context.Context,
	request RefreshBookAuthorRequestObject,
) (RefreshBookAuthorResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return RefreshBookAuthor403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	a, err := s.books.RefreshOne(ctx, request.Id)
	switch {
	case errors.Is(err, book.ErrAuthorNotFound):
		return RefreshBookAuthor404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case errors.Is(err, metadata.ErrRateLimited):
		return RefreshBookAuthor429JSONResponse{
			RateLimitedJSONResponse: errRateLimited(err),
		}, nil
	case providerUnavailable(err):
		return RefreshBookAuthor503JSONResponse{
			ServiceUnavailableJSONResponse: errServiceUnavailable(err.Error()),
		}, nil
	case err != nil:
		return RefreshBookAuthor500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return RefreshBookAuthor200JSONResponse{
		BookAuthorDetailJSONResponse: BookAuthorDetailJSONResponse(
			bookAuthorToAPI(a),
		),
	}, nil
}

func (s *Server) GetBook(
	ctx context.Context,
	request GetBookRequestObject,
) (GetBookResponseObject, error) {
	b, err := s.books.GetBook(ctx, request.Id)
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
	return GetBook200JSONResponse{
		BookDetailJSONResponse: BookDetailJSONResponse(bookToAPI(b)),
	}, nil
}

func (s *Server) PatchBook(
	ctx context.Context,
	request PatchBookRequestObject,
) (PatchBookResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return PatchBook403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	slots := []struct {
		kind      string
		monitored *bool
	}{
		{slotKindEbook, request.Body.EbookMonitored},
		{slotKindAudiobook, request.Body.AudiobookMonitored},
	}
	for _, slot := range slots {
		if slot.monitored == nil {
			continue
		}
		err := s.books.SetBookSlot(ctx, request.Id, slot.kind, *slot.monitored)
		if err != nil {
			return patchBookErr(ctx, err), nil
		}
	}
	b, err := s.books.GetBook(ctx, request.Id)
	if err != nil {
		return patchBookErr(ctx, err), nil
	}
	return PatchBook200JSONResponse{
		BookDetailJSONResponse: BookDetailJSONResponse(bookToAPI(b)),
	}, nil
}

func patchBookErr(ctx context.Context, err error) PatchBookResponseObject {
	if errors.Is(err, book.ErrBookNotFound) {
		return PatchBook404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}
	}
	return PatchBook500JSONResponse{
		InternalErrorJSONResponse: errInternal(ctx, err),
	}
}

// validateAuthorFields returns the 422 message for the first value that is
// set and not accepted; an empty string means unset or valid. Nothing
// validates a request body against the spec, so an unchecked enum would reach
// the database as an ent error and surface as a 500.
func validateAuthorFields(policy, wantKinds, ebook, audiobook string) string {
	switch {
	case policy != "" && !slices.Contains(validMonitorPolicies, policy):
		return "monitor_policy must be one of all, future, none"
	case wantKinds != "" && !slices.Contains(validWantKinds, wantKinds):
		return "want_kinds must be one of ebook, audiobook, both"
	case ebook != "" && !ebookProfileExists(ebook):
		return "unknown ebook quality profile"
	case audiobook != "" && !audiobookProfileExists(audiobook):
		return "unknown audiobook quality profile"
	}
	return ""
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// The Resolve functions fall back to the default profile, so they cannot tell
// a typo from a real name.
func ebookProfileExists(name string) bool {
	c := config.Get()
	if c == nil {
		return false
	}
	return slices.ContainsFunc(
		c.EbookQualityProfiles,
		func(p config.EbookQualityProfileEntry) bool { return p.Name == name },
	)
}

func audiobookProfileExists(name string) bool {
	c := config.Get()
	if c == nil {
		return false
	}
	return slices.ContainsFunc(
		c.AudiobookQualityProfiles,
		func(p config.AudiobookQualityProfileEntry) bool { return p.Name == name },
	)
}

func (s *Server) SearchBookReleases(
	ctx context.Context,
	request SearchBookReleasesRequestObject,
) (SearchBookReleasesResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return SearchBookReleases403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	if !request.Params.Kind.Valid() {
		return SearchBookReleases400JSONResponse{
			BadRequestJSONResponse: errBadRequest(invalidSlotKindMsg),
		}, nil
	}
	releases, err := s.books.SearchBookReleases(
		ctx, request.Id, string(request.Params.Kind),
	)
	switch {
	case errors.Is(err, book.ErrBookNotFound):
		return SearchBookReleases404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case errors.Is(err, book.ErrInvalidSlotKind):
		return SearchBookReleases400JSONResponse{
			BadRequestJSONResponse: errBadRequest(err.Error()),
		}, nil
	case errors.Is(err, book.ErrNoQualityProfile):
		return SearchBookReleases422JSONResponse{
			UnprocessableEntityJSONResponse: errNoQualityProfile(err.Error()),
		}, nil
	case err != nil:
		return SearchBookReleases500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	private := indexerPrivacy()
	items := make([]BookRelease, 0, len(releases))
	for _, r := range releases {
		item := toSearchResult(r.SearchResult)
		score := r.Score
		item.Score = &score
		rejected := r.Rejected
		item.Rejected = &rejected
		if r.Reason != "" {
			reason := r.Reason
			item.RejectReason = &reason
		}
		if p, ok := private[r.ConfiguredIndexer]; ok {
			item.IndexerPrivate = &p
		}
		items = append(items, BookRelease{Format: r.Format, Release: item})
	}
	out := BookReleaseListJSONResponse{Items: items}
	return SearchBookReleases200JSONResponse{
		BookReleaseListJSONResponse: out,
	}, nil
}

func (s *Server) GrabBookRelease(
	ctx context.Context,
	request GrabBookReleaseRequestObject,
) (GrabBookReleaseResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return GrabBookRelease403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	if !request.Params.Kind.Valid() {
		return GrabBookRelease400JSONResponse{
			BadRequestJSONResponse: errBadRequest(invalidSlotKindMsg),
		}, nil
	}
	sr, err := toIndexerResult(request.Body)
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
	err = s.books.GrabBookRelease(ctx, request.Id, book.GrabParams{
		Kind:   string(request.Params.Kind),
		Result: sr,
	})
	switch {
	case errors.Is(err, book.ErrBookNotFound):
		return GrabBookRelease404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case errors.Is(err, book.ErrInvalidSlotKind):
		return GrabBookRelease400JSONResponse{
			BadRequestJSONResponse: errBadRequest(err.Error()),
		}, nil
	case errors.Is(err, book.ErrNoQualityProfile):
		return GrabBookRelease422JSONResponse{
			UnprocessableEntityJSONResponse: errNoQualityProfile(err.Error()),
		}, nil
	case errors.Is(err, download.ErrUntrustedSource),
		errors.Is(err, download.ErrClientFull),
		errors.Is(err, download.ErrUnsafeTorrentName):
		return GrabBookRelease422JSONResponse{
			UnprocessableEntityJSONResponse: errGrabRejected(err.Error()),
		}, nil
	case err != nil:
		return GrabBookRelease500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return GrabBookRelease202Response{}, nil
}
