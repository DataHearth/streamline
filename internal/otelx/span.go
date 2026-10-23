package otelx

import (
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Must panics when err is non-nil and returns v otherwise. Use for OTel
// instrument creation at package init(), where a failure is a programmer
// error (invalid instrument name/unit) that should abort startup:
//
//	counter = otelx.Must(meter.Int64Counter("streamline.foo", ...))
func Must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// RecordSpanError marks span as failed (records err + sets Error status) and
// returns err unchanged so callers can use it inline at a return site:
//
//	if err != nil {
//	    return nil, otelx.RecordSpanError(span, fmt.Errorf("..."))
//	}
//
// A nil err leaves the span untouched. Marking a span failed with no error is
// meaningless, and the alternative — dereferencing it for SetStatus — turns a
// missing `if err != nil` into a panic, which the caller's handler reports as a
// 500 nowhere near the mistake.
func RecordSpanError(span trace.Span, err error) error {
	if err == nil {
		return nil
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	return err
}
