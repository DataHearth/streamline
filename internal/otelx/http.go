// Package otelx holds low-level OpenTelemetry helpers shared across the
// project. It must stay a leaf package — it has no dependencies on other
// internal packages — so every feature package (auth, download, indexer, ...)
// can import it without creating cycles through internal/observability.
package otelx

import (
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// HTTPClient is a shared *http.Client with OTel instrumentation wired into the
// transport. Every outbound request becomes a child span of the caller's
// context with HTTP semconv attributes, and metrics are recorded via the
// global meter provider. Use for every outbound HTTP call (external APIs,
// indexers, media servers, download clients) so traces, latencies, and
// errors are visible in the backend.
// Timeout is a backstop, not a tuning knob: every caller here does
// request/response API calls, and a provider that accepts the connection but
// never answers (blocked egress, dead VPN sidecar) otherwise hangs the
// handler — and any mutex it holds — for the process lifetime.
var HTTPClient = &http.Client{
	Transport: otelhttp.NewTransport(http.DefaultTransport),
	Timeout:   30 * time.Second,
}
