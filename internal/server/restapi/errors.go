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
