package restapi

// Ceilings mirror the "maximum" declared on each endpoint's limit param in
// api/openapi.yaml, and the zero rejections mirror their "minimum: 1". No
// OpenAPI request-validation middleware is mounted, so the spec's bounds are
// enforced here: handlers clamp values above the ceiling and answer an
// explicit zero with a 400.
const (
	usersMaxLimit    = 100
	activityMaxLimit = 200
	importMaxLimit   = 100
	moviesMaxLimit   = 100
	seriesMaxLimit   = 100
	requestsMaxLimit = 100
)

const (
	msgZeroPage  = "page must be >= 1"
	msgZeroLimit = "limit must be >= 1"
)

func clampLimit[T ~int | ~uint16 | ~uint32](limit, max T) T {
	if limit > max {
		return max
	}
	return limit
}

// positiveOr returns def when v is absent and *v otherwise. ok is false for
// an explicit zero — the caller's cue to enforce the spec's minimum: 1 with
// a 400 rather than silently serving a different page size than it echoes.
func positiveOr[T ~uint16 | ~uint32](v *T, def T) (T, bool) {
	if v == nil {
		return def, true
	}
	if *v == 0 {
		return 0, false
	}
	return *v, true
}
