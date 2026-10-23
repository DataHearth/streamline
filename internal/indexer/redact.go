package indexer

import (
	"net/url"

	"github.com/datahearth/streamline/internal/otelx"
)

// redactURL keeps scheme, host and path, dropping everything that can carry a
// credential. Torznab has no header auth — the protocol mandates the api key
// as an `apikey` query parameter — so an indexer request URL is a secret.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<indexer url>"
	}
	return otelx.RedactURL(u)
}
