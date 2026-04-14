// Package otelx holds low-level OpenTelemetry helpers shared across the
// project. It must stay a leaf package — it has no dependencies on other
// internal packages — so every feature package (auth, download, indexer, ...)
// can import it without creating cycles through internal/observability.
package otelx

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// HTTPClient is a shared *http.Client with OTel instrumentation wired into the
// transport. Every outbound request becomes a child span of the caller's
// context with HTTP semconv attributes, and metrics are recorded via the
// global meter provider. Use for every outbound HTTP call (external APIs,
// indexers, media servers, download clients) so traces, latencies, and
// errors are visible in the backend.
var HTTPClient = &http.Client{
	Transport: otelhttp.NewTransport(http.DefaultTransport),
}
