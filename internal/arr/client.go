package arr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/internal/otelx"
)

var tracer = otel.Tracer("github.com/datahearth/streamline/internal/arr")

type factory struct{}

func NewFactory() Factory { return factory{} }

func (factory) Client(app App, baseURL, apiKey string) (Library, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return nil, ErrInvalidURL
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("%w: got scheme %q", ErrInvalidURL, u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("%w: no host", ErrInvalidURL)
	}
	u.RawQuery, u.Fragment = "", ""
	return &client{app: app, base: u.String(), key: apiKey}, nil
}

type client struct {
	app  App
	base string
	key  string
}

// get decodes one v3 route. route is a constant template, never interpolated
// with a secret: it lands on the span as an attribute.
func get[T any](ctx context.Context, c *client, route, query string) (T, error) {
	var zero T
	ctx, span := tracer.Start(ctx, "arr.request", trace.WithAttributes(
		attribute.String("arr.app", string(c.app)),
		attribute.String("arr.route", route),
	))
	defer span.End()

	u := c.base + "/api/v3" + route
	if query != "" {
		u += "?" + query
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return zero, otelx.RecordSpanError(span, err)
	}
	req.Header.Set("X-Api-Key", c.key)
	req.Header.Set("Accept", "application/json")

	resp, err := otelx.HTTPClient.Do(req)
	if err != nil {
		return zero, otelx.RecordSpanError(
			span, fmt.Errorf("%w: %s", ErrUnreachable, transportReason(err)),
		)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized,
		resp.StatusCode == http.StatusForbidden:
		return zero, otelx.RecordSpanError(span, ErrUnauthorized)
	case resp.StatusCode >= 300:
		return zero, otelx.RecordSpanError(span, fmt.Errorf(
			"%w: %s answered %s", ErrUnreachable, route, resp.Status,
		))
	}

	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return zero, otelx.RecordSpanError(
			span, fmt.Errorf("decode %s: %w", route, err),
		)
	}
	return out, nil
}

// transportReason drops the request URL net/http wraps every transport error
// in. The base URL is the operator's own, but the error text travels into a
// scan's failure_reason and the SPA verbatim, so only the cause is kept.
func transportReason(err error) string {
	if uerr, ok := errors.AsType[*url.Error](err); ok {
		return uerr.Err.Error()
	}
	return err.Error()
}

func (c *client) Status(ctx context.Context) (Status, error) {
	return get[Status](ctx, c, "/system/status", "")
}

func (c *client) RootFolders(ctx context.Context) ([]RootFolder, error) {
	return get[[]RootFolder](ctx, c, "/rootfolder", "")
}

func (c *client) QualityProfiles(ctx context.Context) ([]QualityProfile, error) {
	return get[[]QualityProfile](ctx, c, "/qualityprofile", "")
}

func (c *client) Indexers(ctx context.Context) ([]Provider, error) {
	return get[[]Provider](ctx, c, "/indexer", "")
}

func (c *client) DownloadClients(ctx context.Context) ([]Provider, error) {
	return get[[]Provider](ctx, c, "/downloadclient", "")
}

// TestConnection also settles which application answered: pointing a Radarr
// migration at a Sonarr reaches every shared route and returns plausible
// results, so the app name is the only thing that catches it.
func (c *client) TestConnection(ctx context.Context) error {
	st, err := c.Status(ctx)
	if err != nil {
		return err
	}
	if !strings.EqualFold(st.AppName, string(c.app)) {
		return fmt.Errorf("%w: it reports itself as %s", ErrWrongApp, st.AppName)
	}
	return nil
}
