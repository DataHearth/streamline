package restapi

import (
	"context"
	"errors"
	"log/slog"
)

// internalErrorMessage is the only 500 body the API hands back. The real error
// carries ent/SQLite fragments, filesystem paths and internal hostnames, so it
// goes to the log — where the request id ties it back to this call — and never
// to the client.
const internalErrorMessage = "internal error"

var (
	// errNotAdmin is the canonical "caller is not an admin" sentinel returned by
	// requireAdmin. Handlers wrap notAdminResp in their per-route 403 envelope.
	errNotAdmin = errors.New("admin role required")
	// notAdminResp is the shared 403 payload returned when requireAdmin rejects.
	notAdminResp = ForbiddenJSONResponse{Message: errNotAdmin.Error()}

	// errRequestOnly is returned by requireNotRequestOnly when a request_only user
	// attempts a direct library add; they may only submit a request.
	errRequestOnly = errors.New(
		"request-only role cannot add directly; submit a request instead",
	)
	// requestOnlyResp is the shared 403 payload returned when requireNotRequestOnly rejects.
	requestOnlyResp = ForbiddenJSONResponse{Message: errRequestOnly.Error()}

	errSearchNotConfigured   = errors.New("search not configured")
	errTVSearchNotConfigured = errors.New("tv search not configured")
	errRenamerNotConfigured  = errors.New("renamer not configured")
	errPlayOnNotConfigured   = errors.New("play-on resolver not configured")
)

func errBadRequest(msg string) BadRequestJSONResponse {
	return BadRequestJSONResponse{Message: msg}
}

func errInternal(ctx context.Context, err error) InternalErrorJSONResponse {
	slog.ErrorContext(ctx, "api request failed", "error", err)
	return InternalErrorJSONResponse{Message: internalErrorMessage}
}

func errNotFound(msg string) NotFoundJSONResponse {
	return NotFoundJSONResponse{Message: msg}
}

func errConflict(msg string) ConflictJSONResponse {
	return ConflictJSONResponse{Message: msg}
}

func errUnprocessable(msg string) UnprocessableEntityJSONResponse {
	return UnprocessableEntityJSONResponse{Message: msg}
}

// codeConnectionFailed marks a 422 whose message is an upstream connection
// diagnostic ("plex: unexpected status 401") rather than input validation.
// Without it the SPA renders every 422 as "some of those values weren't
// accepted", which points at the credentials when the credentials are fine.
const codeConnectionFailed = "connection_failed"

// errConnectionFailed is errUnprocessable for a connection test. The message is
// composed by our own client code, not a raw driver string, so it is safe to
// show verbatim — and it is the only place the upstream status appears.
func errConnectionFailed(msg string) UnprocessableEntityJSONResponse {
	code := codeConnectionFailed
	return UnprocessableEntityJSONResponse{Message: msg, Code: &code}
}

// codeInvalidCondition marks a 422 whose message names the custom-format
// condition that failed to compile. The SPA has no client-side regex gate — it
// renders this message verbatim, which is the only place the offending
// condition's index and the regexp package's diagnostic appear.
const codeInvalidCondition = "invalid_condition"

// errInvalidCondition is errUnprocessable for a condition-compile failure. The
// message comes from quality.NewFormat — a condition index plus a regexp/enum
// diagnostic, all of it derived from the caller's own request body.
func errInvalidCondition(msg string) UnprocessableEntityJSONResponse {
	code := codeInvalidCondition
	return UnprocessableEntityJSONResponse{Message: msg, Code: &code}
}

// codeGrabRejected marks a 422 raised because the grab itself was refused —
// an untrusted download host, or a release whose files match no wanted
// episode. Nothing about the request body is wrong, so the generic 422 ("some
// of those values weren't accepted") sends the operator looking at the release
// they picked instead of at the reason, which the message already names.
const codeGrabRejected = "grab_rejected"

// errGrabRejected is errUnprocessable for a refused grab. The message is a
// download-package sentinel plus the release title the caller submitted, so it
// is safe to show verbatim and is the only place the refusal's reason appears.
func errGrabRejected(msg string) UnprocessableEntityJSONResponse {
	code := codeGrabRejected
	return UnprocessableEntityJSONResponse{Message: msg, Code: &code}
}
