package restapi

import (
	"errors"

	"github.com/datahearth/streamline/internal/media/book"
	"github.com/datahearth/streamline/internal/metadata"
)

const (
	codeHardcoverNotConfigured = "hardcover_not_configured"
	codeHardcoverKeyRejected   = "hardcover_key_rejected"
)

var (
	errBookProviderMissing = errors.New(
		"hardcover is not configured: set metadata.hardcover_api_key",
	)
	errBookProviderRejected = errors.New(
		"hardcover rejected the configured api key: check metadata.hardcover_api_key",
	)
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

// errHardcoverUnavailable is the 503 for every operation that needs Hardcover.
// The code tells a missing key from one Hardcover refused, which the SPA words
// differently; both are the operator's to fix and neither is a rate limit.
func errHardcoverUnavailable(err error) ServiceUnavailableJSONResponse {
	if errors.Is(err, metadata.ErrHardcoverUnauthorized) {
		code := codeHardcoverKeyRejected
		return ServiceUnavailableJSONResponse{
			Message: errBookProviderRejected.Error(),
			Code:    &code,
		}
	}
	code := codeHardcoverNotConfigured
	return ServiceUnavailableJSONResponse{
		Message: errBookProviderMissing.Error(),
		Code:    &code,
	}
}
