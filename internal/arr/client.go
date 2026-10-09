package arr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/internal/otelx"
)

var tracer = otel.Tracer("github.com/datahearth/streamline/internal/arr")

type factory struct{}

func NewFactory() Factory { return factory{} }

func (factory) Client(app App, baseURL, apiKey string) (Library, error) {
	u, err := ParseBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	return &client{app: app, base: u.String(), key: apiKey}, nil
}

// ParseBaseURL checks an instance URL the way Client does — an http or https
// address with a host — without connecting, so a caller can refuse a bad one
// before anything else runs. The query and fragment are dropped, and the
// path's trailing slash with them: trimmed from the raw string instead, a
// pasted "http://radarr:7878/?apikey=…" kept its "/" and every route went to
// "//api/v3/…".
func ParseBaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, ErrInvalidURL
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("%w: got scheme %q", ErrInvalidURL, u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("%w: no host", ErrInvalidURL)
	}
	u.RawQuery, u.ForceQuery = "", false
	u.Fragment, u.RawFragment = "", ""
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = strings.TrimRight(u.RawPath, "/")
	return u, nil
}

type client struct {
	app  App
	base string
	key  string
}

const (
	// maxSmallResponse bounds every route but the two whole-library ones:
	// status, root folders, profiles, indexers, download clients and one
	// show's episodes are kilobytes — a few MiB for a long daily show.
	maxSmallResponse = 16 << 20
	// maxLibraryResponse and maxLibraryItems bound GET /movie and GET
	// /series, the one unpaged answer an *arr gives for its whole library:
	// several KB per movie once its file, media info and alternate titles are
	// embedded, so a large library outgrows otelx.MaxResponseBody. It is
	// decoded one element at a time, so the item cap is what bounds memory.
	maxLibraryResponse = 1 << 30
	maxLibraryItems    = 250_000
	// libraryTimeout replaces HTTPClient's 30 s for the same two routes:
	// building and streaming the whole list takes minutes on a NAS.
	libraryTimeout = 10 * time.Minute
)

// libraryClient is otelx.HTTPClient with bounds sized for a whole library;
// every other route goes through otelx.HTTPClient itself.
var libraryClient = otelx.NewHTTPClient(libraryTimeout, maxLibraryResponse)

// get decodes one small v3 route. route is a constant template, never
// interpolated with a secret: it lands on the span as an attribute.
func get[T any](ctx context.Context, c *client, route, query string) (T, error) {
	var out T
	err := c.do(ctx, otelx.HTTPClient, route, query, func(body io.Reader) error {
		return otelx.DecodeJSON(body, maxSmallResponse, &out)
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return out, nil
}

// list decodes a whole-library route — a JSON array of every title — one
// element at a time, so it is bounded by its item count rather than by
// holding the whole document in memory at once.
func list[T any](ctx context.Context, c *client, route string) ([]T, error) {
	var out []T
	err := c.do(ctx, libraryClient, route, "", func(body io.Reader) error {
		dec := json.NewDecoder(body)
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := tok.(json.Delim); !ok || d != '[' {
			return errors.New("expected a JSON array")
		}
		for dec.More() {
			if len(out) >= maxLibraryItems {
				return fmt.Errorf("more than %d titles", maxLibraryItems)
			}
			var v T
			if err := dec.Decode(&v); err != nil {
				return err
			}
			out = append(out, v)
		}
		_, err = dec.Token()
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// do issues one GET against a v3 route and hands the body to decode.
func (c *client) do(
	ctx context.Context,
	hc *http.Client,
	route, query string,
	decode func(io.Reader) error,
) error {
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
		return otelx.RecordSpanError(span, err)
	}
	req.Header.Set("X-Api-Key", c.key)
	req.Header.Set("Accept", "application/json")

	resp, err := hc.Do(req)
	if err != nil {
		return otelx.RecordSpanError(
			span, fmt.Errorf("%w: %s", ErrUnreachable, transportReason(err)),
		)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized,
		resp.StatusCode == http.StatusForbidden:
		return otelx.RecordSpanError(span, ErrUnauthorized)
	case resp.StatusCode >= 300:
		return otelx.RecordSpanError(span, fmt.Errorf(
			"%w: %s answered %s", ErrUnreachable, route, resp.Status,
		))
	}

	if err := decode(resp.Body); err != nil {
		return otelx.RecordSpanError(
			span, fmt.Errorf("decode %s: %w", route, err),
		)
	}
	return nil
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
