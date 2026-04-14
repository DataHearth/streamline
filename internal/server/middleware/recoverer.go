package middleware

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/datahearth/streamline/internal/observability"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

// Recoverer catches panics in downstream handlers, logs them via the process
// default slog logger (which fans out to stderr + the OTel logs bridge), marks
// the current span as errored, and returns a 500 to the client.
//
// Replaces chi's middleware.Recoverer so panic reports flow through the same
// observability pipeline as every other log line: context-aware (so trace_id
// is auto-attached by otelslog) and span-linked.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rvr := recover()
			if rvr == nil {
				return
			}

			err, ok := rvr.(error)
			if !ok {
				err = fmt.Errorf("%v", rvr)
			}
			// http.ErrAbortHandler is chi's sentinel for intentional aborts.
			// Re-panic so net/http closes the connection without logging.
			if ok && errors.Is(err, http.ErrAbortHandler) {
				panic(rvr)
			}

			ctx := r.Context()
			stack := debug.Stack()

			span := trace.SpanFromContext(ctx)
			span.RecordError(err, trace.WithStackTrace(true))
			span.SetStatus(codes.Error, "panic recovered")

			slog.LogAttrs(ctx, observability.LevelCritical,
				"panic recovered in HTTP handler",
				slog.String(string(semconv.HTTPRequestMethodKey), r.Method),
				slog.String(string(semconv.URLPathKey), r.URL.Path),
				slog.String(string(semconv.ExceptionMessageKey), err.Error()),
				slog.String(string(semconv.ExceptionStacktraceKey), string(stack)),
			)

			// If the response was already written, we can't rewrite the status.
			// Best we can do is close the connection.
			if errors.Is(err, http.ErrHandlerTimeout) {
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
		}()

		next.ServeHTTP(w, r)
	})
}
