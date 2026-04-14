package otelx_test

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/datahearth/streamline/internal/otelx"
)

var _ = Describe("Must", Label("unit", "otelx"), func() {
	It("returns value when err is nil", func() {
		Expect(otelx.Must("ok", nil)).To(Equal("ok"))
	})

	It("panics when err is non-nil", func() {
		Expect(func() { otelx.Must("x", errors.New("boom")) }).To(Panic())
	})
})

var _ = Describe("RecordSpanError", Label("unit", "otelx"), func() {
	It("records error event, sets Error status, returns err", func() {
		recorder := tracetest.NewSpanRecorder()
		tp := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))
		tracer := tp.Tracer("test")
		_, span := tracer.Start(GinkgoT().Context(), "test.op")

		want := errors.New("boom")
		got := otelx.RecordSpanError(span, want)
		span.End()

		Expect(got).To(Equal(want))
		spans := recorder.Ended()
		Expect(spans).To(HaveLen(1))
		Expect(spans[0].Status().Code).To(Equal(codes.Error))
		Expect(spans[0].Status().Description).To(Equal("boom"))
		Expect(spans[0].Events()).NotTo(BeEmpty())
	})
})
