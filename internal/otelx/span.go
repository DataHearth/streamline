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
func RecordSpanError(span trace.Span, err error) error {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	return err
}
