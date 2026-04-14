package otelx_test

import (
	"context"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/trace"

	"github.com/datahearth/streamline/internal/otelx"
)

var _ = Describe("HTTPClient", Label("unit", "otelx"), func() {
	It("propagates trace headers from caller context", func() {
		prev := otel.GetTextMapPropagator()
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{}, propagation.Baggage{},
		))
		DeferCleanup(func() { otel.SetTextMapPropagator(prev) })

		tp := trace.NewTracerProvider()
		tracer := tp.Tracer("test")
		ctx, span := tracer.Start(context.Background(), "client.op")
		defer span.End()

		var gotTraceparent string
		ts := httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotTraceparent = r.Header.Get("Traceparent")
				w.WriteHeader(http.StatusOK)
			}),
		)
		DeferCleanup(ts.Close)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL, nil)
		Expect(err).NotTo(HaveOccurred())
		resp, err := otelx.HTTPClient.Do(req)
		Expect(err).NotTo(HaveOccurred())
		resp.Body.Close()

		Expect(gotTraceparent).NotTo(BeEmpty())
	})
})
