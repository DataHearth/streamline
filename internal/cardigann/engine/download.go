package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/datahearth/streamline/internal/cardigann"
)

// ErrNoDownload is a download block whose selectors found no link.
var ErrNoDownload = errors.New("no download link found")

// Download is how to fetch a release: a URL — a magnet, or an http(s) link
// fetched with Method and Header — the definition's download block resolved
// it to.
type Download struct {
	URL    string
	Method string
	Header http.Header
}

// Owns reports whether link points at this indexer's site, the test for
// whether its download block applies to it.
func (e *Engine) Owns(link string) bool {
	u, err := url.Parse(link)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, e.site.Host)
}

// Resolve turns a release's download link into what to fetch, following
// the definition's download block (DownloadRequest): a "before" request,
// then an info hash read off the page or the first selector whose link
// holds a torrent. A link no block touches comes back as itself, with the
// headers the definition sends.
func (e *Engine) Resolve(ctx context.Context, link string) (Download, error) {
	if strings.HasPrefix(link, "magnet:") {
		return Download{URL: link}, nil
	}
	u, err := url.Parse(link)
	if err != nil {
		return Download{}, err
	}
	v := e.baseVars()
	v.downloadURI = uriVars(u)

	block := e.def.Search.Headers
	dl := e.def.Download
	if dl != nil && dl.Headers != nil {
		block = dl.Headers
	}
	headers, err := e.headers(block, v)
	if err != nil {
		return Download{}, err
	}
	out := Download{URL: u.String(), Method: http.MethodGet, Header: http.Header{}}
	for k, val := range headers {
		out.Header.Set(k, val)
	}
	if dl == nil {
		return out, nil
	}
	if strings.EqualFold(dl.Method, "post") {
		out.Method = http.MethodPost
	}

	page := request{
		method:   http.MethodGet,
		url:      u.String(),
		headers:  headers,
		redirect: true,
	}
	var before *response
	if b := dl.Before; b != nil {
		path := b.Path
		if b.PathSelector != nil {
			resp, err := e.do(ctx, page)
			if err != nil {
				return Download{}, err
			}
			if path, err = e.matchSelector(resp, *b.PathSelector, v); err != nil {
				return Download{}, err
			}
		}
		if before, err = e.before(
			ctx,
			*b,
			path,
			v,
			headers,
			u.String(),
		); err != nil {
			return Download{}, err
		}
	}

	switch {
	case dl.InfoHash != nil:
		resp := before
		if !dl.InfoHash.UseBeforeResponse || resp == nil {
			if resp, err = e.do(ctx, page); err != nil {
				return Download{}, err
			}
		}
		if dl.InfoHash.Hash == nil || dl.InfoHash.Title == nil {
			return Download{}, fmt.Errorf(
				"%w: infohash block incomplete",
				ErrNoDownload,
			)
		}
		hash, err := e.matchSelector(resp, *dl.InfoHash.Hash, v)
		if err != nil {
			return Download{}, err
		}
		title, err := e.matchSelector(resp, *dl.InfoHash.Title, v)
		if err != nil {
			return Download{}, err
		}
		out.URL = magnetLink(hash, title)
		return out, nil
	case len(dl.Selectors) > 0:
		for _, s := range dl.Selectors {
			// No v11 download selector sets usebeforeresponse, so each reads
			// the release page afresh, as upstream's do without it.
			resp, err := e.do(ctx, page)
			if err != nil {
				return Download{}, err
			}
			href, err := e.matchSelector(resp, cardigann.SelectorField{
				Selector: s.Selector, Attribute: s.Attribute, Filters: s.Filters,
			}, v)
			if err != nil {
				logDebug(ctx, e.def, "cardigann download selector failed",
					"selector", s.Selector, "error", err)
				continue
			}
			target, err := e.resolve(href, u)
			if err != nil {
				continue
			}
			if target.Scheme != "magnet" && e.testLinkTorrent() &&
				!e.isTorrent(ctx, target.String(), headers) {
				logDebug(ctx, e.def, "cardigann download selector is no torrent",
					"selector", s.Selector)
				continue
			}
			out.URL = target.String()
			return out, nil
		}
		return Download{}, fmt.Errorf("%w for %s", ErrNoDownload, link)
	}
	return out, nil
}

func (e *Engine) testLinkTorrent() bool {
	return e.def.TestLinkTorrent == nil || *e.def.TestLinkTorrent
}

// isTorrent fetches a candidate link and checks the body is bencoded, which
// is how upstream moves past a selector that found an HTML page instead.
func (e *Engine) isTorrent(
	ctx context.Context,
	link string,
	headers map[string]string,
) bool {
	resp, err := e.do(ctx, request{
		method: http.MethodGet, url: link, headers: headers, redirect: true,
	})
	if err != nil || resp.status != http.StatusOK {
		return false
	}
	return resp.body == "" || resp.body[0] == 'd'
}

// before runs a download block's "before" request (HandleRequest).
func (e *Engine) before(
	ctx context.Context,
	b cardigann.Before,
	path string,
	v *vars,
	headers map[string]string,
	referer string,
) (*response, error) {
	rendered, err := render(path, v, plainText)
	if err != nil {
		return nil, err
	}
	target, err := e.resolve(rendered, nil)
	if err != nil {
		return nil, err
	}
	method := http.MethodGet
	if strings.EqualFold(b.Method, "post") {
		method = http.MethodPost
	}
	var pairs [][2]string
	for _, in := range b.Inputs {
		val, err := render(string(in.Value), v, plainText)
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, [2]string{in.Key, val})
	}
	r := request{
		method: method, url: target.String(), headers: headers,
		referer: referer, redirect: true,
	}
	if len(pairs) > 0 {
		sep := "&"
		if method == http.MethodGet && b.QuerySeparator != "" {
			sep = b.QuerySeparator
		}
		qs, err := e.queryString(pairs, sep)
		if err != nil {
			return nil, err
		}
		if method == http.MethodPost {
			r.form = qs
		} else {
			if !strings.Contains(r.url, "?") {
				r.url += "?"
			}
			r.url += qs
		}
	}
	return e.do(ctx, r)
}

// matchSelector reads one value off a response page (MatchSelector).
func (e *Engine) matchSelector(
	resp *response,
	s cardigann.SelectorField,
	v *vars,
) (string, error) {
	sel, err := render(s.Selector, v, plainText)
	if err != nil {
		return "", err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(resp.body))
	if err != nil {
		return "", err
	}
	found := querySelectorAll(doc.Selection, sel).First()
	if found.Length() == 0 {
		return "", fmt.Errorf("%w: selector %q matched nothing", ErrNoDownload, sel)
	}
	value := found.Text()
	if s.Attribute != "" {
		attr, ok := found.Attr(s.Attribute)
		if !ok {
			return "", fmt.Errorf(
				"%w: attribute %q not set",
				ErrNoDownload,
				s.Attribute,
			)
		}
		value = attr
	}
	return e.applyFilters(value, s.Filters, v)
}

// uriVars is AddTemplateVariablesFromUri under .DownloadUri.
func uriVars(u *url.URL) map[string]any {
	q := map[string]string{}
	for k, vals := range u.Query() {
		if len(vals) > 0 {
			q[k] = vals[0]
		}
	}
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	pathAndQuery := u.EscapedPath()
	if u.RawQuery != "" {
		pathAndQuery += "?" + u.RawQuery
	}
	return map[string]any{
		"AbsoluteUri":  u.String(),
		"AbsolutePath": u.EscapedPath(),
		"Scheme":       u.Scheme,
		"Host":         u.Hostname(),
		"Port":         port,
		"PathAndQuery": pathAndQuery,
		"Query":        q,
	}
}
