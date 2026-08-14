package restapi

// Ceilings mirror the "maximum" declared on each endpoint's limit param in
// api/openapi.yaml. No OpenAPI request-validation middleware is mounted, so
// they are documentation only until a handler applies clampLimit with them.
const (
	usersMaxLimit    = 100
	activityMaxLimit = 200
	importMaxLimit   = 100
	moviesMaxLimit   = 100
	seriesMaxLimit   = 100
	requestsMaxLimit = 100
)

func clampLimit[T ~int | ~uint16 | ~uint32](limit, max T) T {
	if limit > max {
		return max
	}
	return limit
}
