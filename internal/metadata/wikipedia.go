package metadata

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/internal/buildinfo"
	"github.com/datahearth/streamline/internal/otelx"
)

const (
	wikidataAPIURL = "https://www.wikidata.org/w/api.php"

	// maxWikiResponse bounds one Wikidata or Wikipedia answer; a summary is a
	// few kilobytes.
	maxWikiResponse = 1 << 20
)

// Wikipedia is the OverviewProvider implementation: the artist's Wikidata
// item names its article in every language, and the REST summary of that
// article is the overview. No key. Wikipedia text is CC BY-SA, which is why
// every overview carries the page it came from.
type Wikipedia struct {
	client *http.Client
}

func NewWikipedia() *Wikipedia {
	c := *otelx.HTTPClient
	return &Wikipedia{client: &c}
}

func (w *Wikipedia) get(
	ctx context.Context,
	span trace.Span,
	u string,
	out any,
) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false, otelx.RecordSpanError(span, err)
	}
	version := buildinfo.Version
	if version == "" {
		version = "dev"
	}
	req.Header.Set("User-Agent",
		"streamline/"+version+" (https://github.com/datahearth/streamline)")
	req.Header.Set("Accept", "application/json")
	resp, err := w.client.Do(req)
	if err != nil {
		return false, otelx.RecordSpanError(span, err)
	}
	defer resp.Body.Close()
	recordProviderStatus(ctx, resp.StatusCode)
	switch resp.StatusCode {
	case http.StatusOK:
		return true, otelx.RecordSpanError(
			span, otelx.DecodeJSON(resp.Body, maxWikiResponse, out))
	case http.StatusNotFound:
		return false, nil
	}
	return false, otelx.RecordSpanError(span,
		fmt.Errorf("wikipedia: %s returned %d", u, resp.StatusCode))
}

func (w *Wikipedia) Overviews(
	ctx context.Context,
	wikidataID string,
	langs []string,
) (map[string]Overview, error) {
	ctx, span := tracer.Start(ctx, "metadata.wikipedia.overviews",
		trace.WithAttributes(attribute.String("wikidata.id", wikidataID)))
	defer span.End()

	out := map[string]Overview{}
	if wikidataID == "" || len(langs) == 0 {
		return out, nil
	}
	sites := make([]string, len(langs))
	for i, l := range langs {
		sites[i] = l + "wiki"
	}
	var entities struct {
		Entities map[string]struct {
			Sitelinks map[string]struct {
				Title string `json:"title"`
			} `json:"sitelinks"`
		} `json:"entities"`
	}
	q := url.Values{
		"action":     {"wbgetentities"},
		"ids":        {wikidataID},
		"props":      {"sitelinks"},
		"sitefilter": {strings.Join(sites, "|")},
		"format":     {"json"},
	}
	if _, err := w.get(
		ctx,
		span,
		wikidataAPIURL+"?"+q.Encode(),
		&entities,
	); err != nil {
		return nil, err
	}
	links := entities.Entities[wikidataID].Sitelinks

	for _, lang := range langs {
		link, ok := links[lang+"wiki"]
		if !ok || link.Title == "" {
			continue
		}
		var summary struct {
			Type        string `json:"type"`
			Extract     string `json:"extract"`
			ContentURLs struct {
				Desktop struct {
					Page string `json:"page"`
				} `json:"desktop"`
			} `json:"content_urls"`
		}
		title := url.PathEscape(strings.ReplaceAll(link.Title, " ", "_"))
		found, err := w.get(
			ctx,
			span,
			"https://"+lang+".wikipedia.org/api/rest_v1/page/summary/"+title,
			&summary,
		)
		if err != nil {
			return nil, err
		}
		if !found || summary.Type != "standard" ||
			strings.TrimSpace(summary.Extract) == "" {
			continue
		}
		source := summary.ContentURLs.Desktop.Page
		if source == "" {
			source = "https://" + lang + ".wikipedia.org/wiki/" + title
		}
		out[lang] = Overview{
			Text:      strings.TrimSpace(summary.Extract),
			SourceURL: source,
		}
	}
	return out, nil
}
