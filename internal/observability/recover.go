package observability

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"

	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

// RecoverPanic is the deferred guard for a goroutine that is not an HTTP
// handler. Recoverer covers the request path; everything started with `go` is
// on its own, and an unrecovered panic there takes the whole process down —
// scheduler, downloads, transcodes and all — over one bad release name in one
// background job.
//
// onPanic decides what happens next, because that is the caller's call: a
// scheduled job can be dropped and retried on its next tick, while a panic in
// a boot goroutine means the process can no longer do its job and should shut
// down cleanly rather than limp. Pass nil to simply absorb the panic.
//
// Deferred directly, not wrapped in a closure:
//
//	defer observability.RecoverPanic(ctx, "scheduled job", nil)
func RecoverPanic(ctx context.Context, goroutine string, onPanic func()) {
	rvr := recover()
	if rvr == nil {
		return
	}

	err, ok := rvr.(error)
	if !ok {
		err = fmt.Errorf("%v", rvr)
	}

	span := trace.SpanFromContext(ctx)
	span.RecordError(err, trace.WithStackTrace(true))
	span.SetStatus(codes.Error, "panic recovered")

	//nolint:sloglint // LogAttrs takes slog.Attr by API design
	slog.LogAttrs(ctx, LevelCritical,
		"panic recovered in background goroutine",
		slog.String("goroutine", goroutine),
		slog.String(string(semconv.ExceptionMessageKey), err.Error()),
		slog.String(string(semconv.ExceptionStacktraceKey), string(debug.Stack())),
	)

	if onPanic != nil {
		onPanic()
	}
}
