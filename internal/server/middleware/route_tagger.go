package middleware

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

// RouteTagger rewrites the outer otelhttp span name to the chi route pattern
// once routing has resolved. Without this, span names contain concrete paths
// ("/api/v1/movies/42") which blow up cardinality in trace/metric backends.
// After this middleware the span is named "<METHOD> <pattern>"
// ("GET /api/v1/movies/{id}") and carries http.route as a semantic attribute.
//
// Must run *after* chi has at least entered its matching phase. chi populates
// RoutePattern() progressively, so the final value is only stable once the
// inner handler has returned. We therefore rewrite post-hoc; the enclosing
// otelhttp span is still open (span.End is called by the outer wrapper after
// our chain returns) so SetName/SetAttributes take effect.
func RouteTagger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)

		rctx := chi.RouteContext(r.Context())
		if rctx == nil {
			return
		}
		pattern := rctx.RoutePattern()
		if pattern == "" {
			return
		}

		span := trace.SpanFromContext(r.Context())
		if !span.IsRecording() {
			return
		}
		span.SetName(r.Method + " " + pattern)
		span.SetAttributes(semconv.HTTPRoute(pattern))
	})
}
