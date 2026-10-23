package otelx_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/internal/otelx"
)

// recordSpans drives fn under a tracer provider whose spans are captured, and
// returns everything that ended — the client span otelhttp creates included,
// which is the only way to see the attributes it writes. Swapping
// HTTPClient.Transport instead would discard the instrumentation under test.
func recordSpans(fn func(ctx context.Context)) []trace.ReadOnlySpan {
	GinkgoHelper()

	rec := tracetest.NewSpanRecorder()
	tp := trace.NewTracerProvider(trace.WithSpanProcessor(rec))
	DeferCleanup(func() {
		Expect(tp.Shutdown(context.Background())).To(Succeed())
	})

	ctx, span := tp.Tracer("test").Start(context.Background(), "caller.op")
	fn(ctx)
	span.End()

	return rec.Ended()
}

func clientSpan(spans []trace.ReadOnlySpan) trace.ReadOnlySpan {
	GinkgoHelper()

	for _, s := range spans {
		if s.SpanKind() == oteltrace.SpanKindClient {
			return s
		}
	}
	Fail("no client span was recorded")
	return nil
}

func attrValue(span trace.ReadOnlySpan, key string) string {
	GinkgoHelper()

	for _, kv := range span.Attributes() {
		if string(kv.Key) == key {
			return kv.Value.String()
		}
	}
	Fail("span carries no " + key + " attribute")
	return ""
}

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

	It("bounds requests to a provider that never answers", func() {
		Expect(otelx.HTTPClient.Timeout).To(BeNumerically(">", 0))
	})

	Describe("query-string credentials", func() {
		const key = "super-secret-key"

		expectNoKey := func(spans []trace.ReadOnlySpan) {
			GinkgoHelper()

			Expect(spans).NotTo(BeEmpty())
			for _, s := range spans {
				for _, kv := range s.Attributes() {
					Expect(kv.Value.String()).NotTo(ContainSubstring(key))
				}
				Expect(s.Status().Description).NotTo(ContainSubstring(key))
				for _, ev := range s.Events() {
					for _, kv := range ev.Attributes {
						Expect(kv.Value.String()).NotTo(ContainSubstring(key))
					}
				}
			}
		}

		It("keeps them off the client span of a successful request", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusOK)
				}),
			)
			DeferCleanup(ts.Close)

			var gotQuery string
			spans := recordSpans(func(ctx context.Context) {
				req, err := http.NewRequestWithContext(
					ctx,
					http.MethodGet,
					ts.URL+"/api?apikey="+key+"&t=caps",
					nil,
				)
				Expect(err).NotTo(HaveOccurred())

				resp, err := otelx.HTTPClient.Do(req)
				Expect(err).NotTo(HaveOccurred())
				gotQuery = resp.Request.URL.RawQuery
				Expect(resp.Body.Close()).To(Succeed())
			})

			expectNoKey(spans)
			Expect(attrValue(clientSpan(spans), "url.full")).
				To(Equal(ts.URL + "/api"))
			Expect(gotQuery).To(Equal("apikey=" + key + "&t=caps"))
		})

		It("keeps them off the client span when the request fails", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
			)
			target := ts.URL + "/api?apikey=" + key + "&t=caps"
			ts.Close()

			spans := recordSpans(func(ctx context.Context) {
				req, err := http.NewRequestWithContext(
					ctx, http.MethodGet, target, nil,
				)
				Expect(err).NotTo(HaveOccurred())

				_, err = otelx.HTTPClient.Do(
					req,
				)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(key))
			})

			expectNoKey(spans)
		})
	})
})

var _ = Describe("HTTPClient body cap", Label("unit", "otelx"), func() {
	// A body served gzip-encoded is inflated by the transport before anyone
	// reads it, so the cap has to count inflated bytes: MaxResponseBody+1 zero
	// bytes are a few dozen KiB on the wire.
	serveGzipped := func(size int) *httptest.Server {
		GinkgoHelper()
		var wire bytes.Buffer
		zw := gzip.NewWriter(&wire)
		_, err := zw.Write(make([]byte, size))
		Expect(err).NotTo(HaveOccurred())
		Expect(zw.Close()).To(Succeed())
		srv := httptest.NewServer(http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				defer GinkgoRecover()
				w.Header().Set("Content-Encoding", "gzip")
				_, err := w.Write(wire.Bytes())
				Expect(err).NotTo(HaveOccurred())
			}))
		DeferCleanup(srv.Close)
		return srv
	}

	read := func(url string) (int, error) {
		GinkgoHelper()
		resp, err := otelx.HTTPClient.Get(url)
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		n, err := io.Copy(io.Discard, resp.Body)
		return int(n), err
	}

	It("refuses an inflated body past the limit", func() {
		_, err := read(serveGzipped(otelx.MaxResponseBody + 1).URL)
		Expect(err).To(MatchError(otelx.ErrResponseTooLarge))
	})

	It("delivers a body exactly at the limit", func() {
		n, err := read(serveGzipped(otelx.MaxResponseBody).URL)
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(Equal(otelx.MaxResponseBody))
	})
})

var _ = Describe("DecodeJSON", Label("unit", "otelx"), func() {
	It("decodes a value within the limit", func() {
		var v map[string]int
		Expect(otelx.DecodeJSON(strings.NewReader(`{"a":1}`), 64, &v)).To(Succeed())
		Expect(v).To(HaveKeyWithValue("a", 1))
	})

	It("refuses a value past the limit", func() {
		var v []int
		body := "[" + strings.Repeat("1,", 100) + "1]"
		Expect(otelx.DecodeJSON(strings.NewReader(body), 64, &v)).
			To(MatchError(otelx.ErrResponseTooLarge))
	})

	It("reports a malformed value as itself, not as too large", func() {
		var v map[string]int
		err := otelx.DecodeJSON(strings.NewReader(`{"a":`), 64, &v)
		Expect(err).To(HaveOccurred())
		Expect(err).NotTo(MatchError(otelx.ErrResponseTooLarge))
	})
})
