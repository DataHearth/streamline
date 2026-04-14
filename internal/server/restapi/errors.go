package restapi

import "errors"

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
)

func errBadRequest(msg string) BadRequestJSONResponse {
	return BadRequestJSONResponse{Message: msg}
}

func errInternal(msg string) InternalErrorJSONResponse {
	return InternalErrorJSONResponse{Message: msg}
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
