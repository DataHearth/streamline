package restapi

import "fmt"

// Ceilings mirror the "maximum" declared on each endpoint's limit param in
// api/openapi.yaml, and the rejections mirror their "minimum: 1". No OpenAPI
// request-validation middleware is mounted, so the spec's bounds are enforced
// here: a present value outside them answers with a 400.
const (
	usersMaxLimit    = 100
	activityMaxLimit = 200
	importMaxLimit   = 100
	moviesMaxLimit   = 100
	seriesMaxLimit   = 100
	requestsMaxLimit = 100
)

const msgZeroPage = "page must be >= 1"

// limitRangeMsg names the endpoint's documented bound in the 400 body. Every
// call site passes one of the untyped ceiling constants above, so a plain int
// takes them all.
func limitRangeMsg(maxLimit int) string {
	return fmt.Sprintf("limit must be between 1 and %d", maxLimit)
}

// positiveOr returns def when v is absent and *v otherwise. ok is false for
// an explicit zero (or a negative on params bound as plain int) — the
// caller's cue to enforce the spec's minimum: 1 with a 400 rather than
// silently serving a different page than it echoes.
func positiveOr[T ~int | ~uint16 | ~uint32](v *T, def T) (T, bool) {
	if v == nil {
		return def, true
	}
	if *v <= 0 {
		return 0, false
	}
	return *v, true
}

// limitOr is positiveOr with the documented ceiling enforced the same way: a
// present limit outside [1, maxLimit] reports ok=false and the caller 400s.
func limitOr[T ~int | ~uint16 | ~uint32](v *T, def, maxLimit T) (T, bool) {
	if v == nil {
		return def, true
	}
	if *v <= 0 || *v > maxLimit {
		return 0, false
	}
	return *v, true
}
