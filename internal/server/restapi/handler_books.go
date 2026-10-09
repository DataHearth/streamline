package restapi

import (
	"context"
	"errors"
	"slices"

	"github.com/datahearth/streamline/internal/config"
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
