package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/datahearth/streamline/internal/cardigann"
)

// optionalFields are optional whatever the definition says (Prowlarr's
// OptionalFields); the converter marks them, the list stays for the
// "|optional" modifier's sake and for definitions loaded from elsewhere.
var optionalFields = []string{
	"imdb", "imdbid", "tmdbid", "rageid", "tvdbid", "tvmazeid", "traktid",
	"doubanid", "poster", "banner", "description", "genre",
}

// errField is a non-optional field a row could not produce.
var errField = errors.New("field")

type row struct {
	html *goquery.Selection
	json any
	// parent is the row a "multiple" JSON row was expanded from, which a
	// "..field" selector reads.
	parent any
}

func (e *Engine) parse(
	ctx context.Context,
	resp *response,
	path cardigann.SearchPath,
	v *vars,
) ([]Release, error) {
	if resp.status >= 300 && resp.status < 400 && resp.location != "" {
		return nil, fmt.Errorf("%w: redirected to %s", ErrStatus, resp.location)
	}
	if resp.status != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrStatus, resp.status)
	}
	if path.Response != nil && path.Response.Type == "json" {
		return e.parseJSON(ctx, resp, path, v)
	}
	return e.parseHTML(ctx, resp, path, v)
}

func (e *Engine) parseJSON(
	ctx context.Context,
	resp *response,
	path cardigann.SearchPath,
	v *vars,
) ([]Release, error) {
	if msg := path.Response.NoResultsMessage; msg != nil {
		if (*msg != "" && strings.Contains(resp.body, *msg)) ||
			(strings.TrimSpace(*msg) == "" && strings.TrimSpace(resp.body) == "") {
			return nil, nil
		}
	}
	doc, err := decodeJSON([]byte(resp.body))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrParse, err)
	}
	rows := e.def.Search.Rows
	if rows.Count != nil {
		if n, err := e.jsonSelector(*rows.Count, doc, v, true); err == nil {
			if c, err := strconv.Atoi(n); err == nil && c < 1 {
				return nil, nil
			}
		}
	}
	sel, err := render(rows.Selector, v, plainText)
	if err != nil {
		return nil, err
	}
	items, ok := jsonRows(doc, sel)
	if !ok {
		if rows.MissingAttributeEqualsNoResults {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: rows selector %q", ErrParse, sel)
	}

	var out []Release
	for _, item := range items {
		obj := item
		if rows.Attribute != "" {
			t, err := selectToken(item, rows.Attribute)
			if err != nil || t == nil {
				if rows.MissingAttributeEqualsNoResults {
					continue
				}
				return nil, fmt.Errorf(
					"%w: row attribute %q",
					ErrParse,
					rows.Attribute,
				)
			}
			obj = t
		}
		expanded := []any{obj}
		if rows.Multiple {
			switch t := obj.(type) {
			case []any:
				expanded = t
			case map[string]any:
				expanded = expanded[:0]
				for _, k := range sortedKeys(t) {
					expanded = append(expanded, t[k])
				}
			}
		}
		for _, one := range expanded {
			rv := v.clone()
			r, err := e.parseRow(row{json: one, parent: item}, rv, resp.url, true)
			if err != nil {
				return nil, err
			}
			out = append(out, r)
		}
	}
	logDebug(ctx, e.def, "cardigann json parsed", "rows", len(out))
	return out, nil
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// jsonRows is JsonParseRowsSelector: the array at the selector's path, kept
// to the items its :has/:not/:contains filters accept.
func jsonRows(doc any, sel string) ([]any, bool) {
	path, rest, _ := strings.Cut(sel, ":")
	tok, err := selectToken(doc, path)
	if err != nil {
		return nil, false
	}
	arr, ok := tok.([]any)
	if !ok {
		return nil, false
	}
	if rest == "" {
		return arr, true
	}
	var out []any
	for _, it := range arr {
		if _, ok, err := fieldSelector(it, ":"+rest); err == nil && ok {
			out = append(out, it)
		}
	}
	return out, true
}

func (e *Engine) parseHTML(
	ctx context.Context,
	resp *response,
	path cardigann.SearchPath,
	v *vars,
) ([]Release, error) {
	body := resp.body
	if pre := e.def.Search.PreprocessingFilters; len(pre) > 0 {
		var err error
		if body, err = e.applyFilters(body, pre, v); err != nil {
			return nil, fmt.Errorf("preprocessing: %w", err)
		}
	}
	xml := path.Response != nil && path.Response.Type == "xml"
	doc, err := parseDocument(body, xml)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrParse, err)
	}
	rows := e.def.Search.Rows
	sel, err := render(rows.Selector, v, plainText)
	if err != nil {
		return nil, err
	}
	found := querySelectorAll(doc.Selection, sel)
	nodes := found.Nodes
	if after := rows.After; after > 0 {
		for i := 0; i < len(nodes); i++ {
			for j := 1; j <= after && i+j < len(nodes); j++ {
				for c := nodes[i+j].FirstChild; c != nil; {
					next := c.NextSibling
					nodes[i+j].RemoveChild(c)
					nodes[i].AppendChild(c)
					c = next
				}
			}
			end := min(i+1+after, len(nodes))
			nodes = slices.Delete(nodes, i+1, end)
		}
	}

	var out []Release
	for _, n := range nodes {
		sel := doc.FindNodes(n)
		rv := v.clone()
		r, err := e.parseRow(row{html: sel}, rv, resp.url, false)
		if err != nil {
			logDebug(ctx, e.def, "cardigann row skipped", "error", err)
			continue
		}
		if r.Published.IsZero() && rows.DateHeaders != nil {
			if d, ok := e.dateHeader(sel, *rows.DateHeaders, rv); ok {
				r.Published = d
			} else if !rows.DateHeaders.Optional {
				logDebug(ctx, e.def, "cardigann row skipped: no date header")
				continue
			}
		}
		out = append(out, r)
	}
	logDebug(ctx, e.def, "cardigann html parsed", "rows", len(out))
	return out, nil
}

// dateHeader walks back from a row through its earlier siblings, then its
// parent's, for the first one the date-header selector reads.
func (e *Engine) dateHeader(
	sel *goquery.Selection,
	block cardigann.Selector,
	v *vars,
) (dt time.Time, ok bool) {
	cur := sel.Prev()
	if cur.Length() == 0 {
		cur = sel.Parent().Prev()
	}
	for cur.Length() > 0 {
		if value, found, err := e.htmlSelector(block, cur, v, true); err == nil &&
			found {
			if t, err := fromUnknown(value, e.now()); err == nil {
				return t, true
			}
			return time.Time{}, false
		}
		prev := cur.Prev()
		if prev.Length() == 0 {
			prev = cur.Parent().Prev()
		}
		cur = prev
	}
	return time.Time{}, false
}

// parseRow reads every field of one row, in declaration order, into a
// release; .Result carries each field's normalised value to the fields after
// it. On a JSON row a failing required field fails the whole response, as
// Prowlarr's does; on an HTML row it only skips that field.
func (e *Engine) parseRow(
	r row,
	v *vars,
	base *url.URL,
	isJSON bool,
) (Release, error) {
	var rel Release
	for _, f := range e.def.Search.Fields {
		name, _, _ := strings.Cut(f.Key, "|")
		v.result[name] = ""
	}
	for _, f := range e.def.Search.Fields {
		parts := strings.Split(f.Key, "|")
		name, mods := parts[0], parts[1:]
		optional := f.Value.Optional || slices.Contains(optionalFields, f.Key) ||
			slices.Contains(mods, "optional")

		var (
			value string
			found bool
			err   error
		)
		if isJSON {
			obj := r.json
			if strings.HasPrefix(f.Value.Selector, "..") {
				obj = r.parent
			}
			value, found, err = e.jsonField(f.Value, obj, v, !optional)
		} else {
			value, found, err = e.htmlSelector(f.Value, r.html, v, !optional)
		}
		if err == nil && optional && strings.TrimSpace(value) == "" {
			def := ""
			if f.Value.Default != nil {
				def, err = render(string(*f.Value.Default), v, plainText)
			}
			if err == nil && strings.TrimSpace(def) == "" {
				v.result[name] = ""
				continue
			}
			value, found = def, true
		}
		if err == nil && !found && !optional {
			err = fmt.Errorf("%w %s: no value", errField, f.Key)
		}
		if err == nil {
			value, err = e.field(name, mods, value, &rel, base)
		}
		if err != nil {
			// A failed required field leaves what an earlier field of the
			// same name wrote ("title" before a failing "title|append").
			if optional {
				v.result[name] = ""
				continue
			}
			if isJSON {
				return Release{}, fmt.Errorf("field %s: %w", f.Key, err)
			}
			continue
		}
		v.result[name] = value
	}
	if rel.Magnet == "" && rel.InfoHash != "" && e.def.Type != "private" {
		rel.Magnet = magnetLink(rel.InfoHash, rel.Title)
	}
	return rel, nil
}

// field normalises one field's value into the release and returns what
// .Result holds for it afterwards (ParseFields).
func (e *Engine) field(
	name string,
	mods []string,
	value string,
	rel *Release,
	base *url.URL,
) (string, error) {
	switch name {
	case "download":
		if value == "" {
			rel.Download = ""
			return "", nil
		}
		if strings.HasPrefix(value, "magnet:") {
			rel.Magnet = value
			return value, nil
		}
		u, err := e.resolve(value, base)
		if err != nil {
			return "", err
		}
		rel.Download = u.String()
		return rel.Download, nil
	case "magnet":
		rel.Magnet = value
	case "infohash":
		rel.InfoHash = value
	case "details", "comments":
		u, err := e.resolve(value, base)
		if err != nil {
			return "", err
		}
		if name == "details" || rel.Details == "" {
			rel.Details = u.String()
		}
		return u.String(), nil
	case "title":
		if slices.Contains(mods, "append") {
			rel.Title += value
		} else {
			rel.Title = value
		}
		return rel.Title, nil
	case "category", "categorydesc":
		cats := e.cats.newznab(value, name == "categorydesc")
		if len(cats) > 0 {
			if slices.Contains(mods, "noappend") {
				rel.Category = cats
			} else {
				for _, c := range cats {
					if !slices.Contains(rel.Category, c) {
						rel.Category = append(rel.Category, c)
					}
				}
			}
		}
	case "size":
		n, ok := parseSize(value)
		if !ok {
			return "", fmt.Errorf("size %q", value)
		}
		rel.Size = n
		return strconv.FormatInt(n, 10), nil
	case "seeders", "leechers", "grabs", "files":
		n, ok := coerceInt(value)
		if !ok {
			return "", fmt.Errorf("%s %q", name, value)
		}
		if (name == "seeders" || name == "leechers") && n >= 5_000_000 {
			n = 0
		}
		switch name {
		case "seeders":
			rel.Seeders = n
		case "leechers":
			rel.Leechers = n
		}
		return strconv.FormatInt(n, 10), nil
	case "date":
		t, err := fromUnknown(value, e.now())
		if err != nil {
			return "", err
		}
		rel.Published = t
		return t.Format(dateLayout), nil
	case "imdb", "imdbid":
		rel.IMDbID = leadingInt(value)
		return strconv.FormatInt(rel.IMDbID, 10), nil
	case "tmdbid":
		rel.TMDBID = leadingInt(value)
		return strconv.FormatInt(rel.TMDBID, 10), nil
	case "tvdbid":
		rel.TVDBID = leadingInt(value)
		return strconv.FormatInt(rel.TVDBID, 10), nil
	case "rageid", "tvmazeid", "traktid", "doubanid":
		return strconv.FormatInt(leadingInt(value), 10), nil
	case "genre":
		return strings.Join(splitWords(value), ", "), nil
	case "year":
		n, _ := coerceInt(value)
		return strconv.FormatInt(n, 10), nil
	case "downloadvolumefactor", "uploadvolumefactor", "minimumratio":
		f, ok := coerceFloat(value)
		if !ok {
			return "", fmt.Errorf("%s %q", name, value)
		}
		return strconv.FormatFloat(f, 'f', -1, 64), nil
	case "minimumseedtime":
		n, ok := coerceInt(value)
		if !ok {
			return "", fmt.Errorf("%s %q", name, value)
		}
		return strconv.FormatInt(n, 10), nil
	default:
	}
	return value, nil
}

// htmlSelector is HandleSelector: a field's text, or what its selector finds
// in the row — the row itself first, as AngleSharp's Matches-then-
// QuerySelector does — read through case, attribute or text content, then
// the filters. found is false when a non-required selector finds nothing.
func (e *Engine) htmlSelector(
	s cardigann.Selector,
	dom *goquery.Selection,
	v *vars,
	required bool,
) (string, bool, error) {
	if s.Text != nil {
		t, err := render(string(*s.Text), v, plainText)
		if err != nil {
			return "", false, err
		}
		out, err := e.applyFilters(t, s.Filters, v)
		return out, err == nil, err
	}
	selection := dom
	if s.Selector != "" {
		sel, err := render(s.Selector, v, plainText)
		if err != nil {
			return "", false, err
		}
		selection = matchOrFind(dom, sel)
		if selection.Length() == 0 {
			if required {
				return "", false, fmt.Errorf(
					"%w: selector %q matched nothing",
					errField,
					sel,
				)
			}
			return "", false, nil
		}
	}
	if s.Remove != "" {
		selection.Find(s.Remove).Remove()
	}
	var value string
	switch {
	case len(s.Case) > 0:
		matched := false
		for _, c := range s.Case {
			if selection.Is(c.Key) ||
				querySelectorAll(selection, c.Key).Length() > 0 {
				out, err := render(string(c.Value), v, plainText)
				if err != nil {
					return "", false, err
				}
				value, matched = out, true
				break
			}
		}
		if !matched {
			if required {
				return "", false, fmt.Errorf("%w: no case matched", errField)
			}
			return "", false, nil
		}
	case s.Attribute != "":
		attr, ok := selection.Attr(s.Attribute)
		if !ok {
			if required {
				return "", false, fmt.Errorf(
					"%w: attribute %q not set", errField, s.Attribute,
				)
			}
			return "", false, nil
		}
		value = attr
	default:
		value = selection.Text()
	}
	out, err := e.applyFilters(strings.TrimSpace(value), s.Filters, v)
	return out, err == nil, err
}

// jsonField is HandleJsonSelector for one field.
func (e *Engine) jsonField(
	s cardigann.Selector,
	obj any,
	v *vars,
	required bool,
) (string, bool, error) {
	value, err := e.jsonSelector(s, obj, v, required)
	if errors.Is(err, errNoToken) && !required {
		return "", false, nil
	}
	return value, err == nil, err
}

func (e *Engine) jsonSelector(
	s cardigann.Selector,
	obj any,
	v *vars,
	required bool,
) (string, error) {
	if s.Text != nil {
		t, err := render(string(*s.Text), v, plainText)
		if err != nil {
			return "", err
		}
		return e.applyFilters(t, s.Filters, v)
	}
	var (
		value string
		has   bool
	)
	if s.Selector != "" {
		sel, err := render(strings.TrimLeft(s.Selector, "."), v, plainText)
		if err != nil {
			return "", err
		}
		path, ok, err := fieldSelector(obj, sel)
		if err != nil {
			return "", err
		}
		var tok any
		if ok {
			if tok, err = selectToken(obj, path); err != nil {
				ok = false
			}
		}
		if !ok {
			if required {
				return "", fmt.Errorf("%w: selector %q", errNoToken, sel)
			}
			return "", errNoToken
		}
		if _, isObj := tok.(map[string]any); isObj {
			return "", fmt.Errorf("%w: selector %q is an object", errField, sel)
		}
		value, has = tokenString(tok), tok != nil
	}
	if len(s.Case) > 0 {
		matched := false
		for _, c := range s.Case {
			if (has && value == c.Key) || c.Key == "*" {
				out, err := render(string(c.Value), v, plainText)
				if err != nil {
					return "", err
				}
				value, matched = out, true
				break
			}
		}
		if !matched {
			if required {
				return "", fmt.Errorf("%w: no case matched %q", errField, value)
			}
			return "", errNoToken
		}
	}
	return e.applyFilters(strings.TrimSpace(value), s.Filters, v)
}
