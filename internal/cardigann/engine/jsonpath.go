package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dlclark/regexp2/v2"
)

// JSON values are decoded with UseNumber, so a number keeps the text it was
// written with: map[string]any, []any, string, json.Number, bool or nil.

func decodeJSON(body []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

var errNoToken = errors.New("no such JSON token")

// selectToken evaluates the subset of Newtonsoft's SelectToken paths the
// definitions use: an optional "$" root, dotted names, [n] indexes and
// ['quoted names'].
func selectToken(root any, path string) (any, error) {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "$")
	cur := root
	for path != "" {
		var seg string
		switch {
		case strings.HasPrefix(path, "."):
			path = path[1:]
			continue
		case strings.HasPrefix(path, "['"):
			end := strings.Index(path, "']")
			if end < 0 {
				return nil, fmt.Errorf("unterminated %q", path)
			}
			seg, path = path[2:end], path[end+2:]
			m, ok := cur.(map[string]any)
			if !ok {
				return nil, errNoToken
			}
			if cur, ok = m[seg]; !ok {
				return nil, errNoToken
			}
			continue
		case strings.HasPrefix(path, "["):
			end := strings.IndexByte(path, ']')
			if end < 0 {
				return nil, fmt.Errorf("unterminated %q", path)
			}
			n, err := strconv.Atoi(strings.TrimSpace(path[1:end]))
			if err != nil {
				return nil, fmt.Errorf("index %q: %w", path[1:end], err)
			}
			path = path[end+1:]
			a, ok := cur.([]any)
			if !ok || n < 0 || n >= len(a) {
				return nil, errNoToken
			}
			cur = a[n]
			continue
		}
		end := strings.IndexAny(path, ".[")
		if end < 0 {
			end = len(path)
		}
		seg, path = path[:end], path[end:]
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, errNoToken
		}
		if cur, ok = m[seg]; !ok {
			return nil, errNoToken
		}
	}
	return cur, nil
}

// jsonFilterRe is Prowlarr's _jsonSelectorRegex, which needs a lookahead —
// hence regexp2.
var jsonFilterRe = regexp2.MustCompile(
	`\:(?<filter>.+?)\((?<key>.+?)\)(?=:|\z)`, regexp2.None,
)

// fieldSelector is JsonParseFieldSelector: the path before the first ':' when
// obj has it and every :has/:not/:contains filter after it holds.
func fieldSelector(obj any, sel string) (string, bool, error) {
	path, _, _ := strings.Cut(sel, ":")
	target := obj
	if strings.TrimSpace(path) != "" {
		t, err := selectToken(obj, path)
		if err != nil {
			return "", false, nil
		}
		target = t
	}
	m, err := jsonFilterRe.FindStringMatch(sel)
	for ; m != nil && err == nil; m, err = jsonFilterRe.FindNextMatch(m) {
		filter := m.GroupByName("filter").String()
		key := m.GroupByName("key").String()
		switch filter {
		case "has", "not":
			found, ferr := jsonHas(target, key)
			if ferr != nil {
				return "", false, ferr
			}
			if found != (filter == "has") {
				return "", false, nil
			}
		case "contains":
			if !strings.Contains(tokenString(target), key) {
				return "", false, nil
			}
		}
	}
	if err != nil {
		return "", false, err
	}
	return path, true, nil
}

func jsonHas(target any, key string) (bool, error) {
	if inner, err := jsonFilterRe.MatchString(key); err != nil {
		return false, err
	} else if inner {
		_, ok, err := fieldSelector(target, key)
		return ok, err
	}
	_, err := selectToken(target, key)
	return err == nil, nil
}

var (
	isoDateRe = regexp.MustCompile(
		`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})?$`,
	)
	newtonsoftDate = "01/02/2006 15:04:05"
)

// tokenString is a token's string form as Newtonsoft gives it — what a
// definition's case keys and filters were written against. An ISO timestamp
// is a DateTime there, so it reads back as MM/dd/yyyy HH:mm:ss: a "Z" one
// stays in UTC and one with an offset moves to local time
// (DateTimeZoneHandling.RoundtripKind). Booleans are True/False; an array
// joins its items with ',', an object is its indented JSON.
func tokenString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		if isoDateRe.MatchString(t) {
			if d, err := time.Parse(time.RFC3339Nano, t); err == nil {
				if !strings.HasSuffix(t, "Z") {
					d = d.Local()
				}
				return d.Format(newtonsoftDate)
			}
			if d, err := time.Parse("2006-01-02T15:04:05.999999999", t); err == nil {
				return d.Format(newtonsoftDate)
			}
		}
		return t
	case json.Number:
		s := t.String()
		if !strings.ContainsAny(s, ".eE") {
			return s
		}
		f, err := t.Float64()
		if err != nil {
			return s
		}
		return strconv.FormatFloat(f, 'f', -1, 64)
	case bool:
		if t {
			return "True"
		}
		return "False"
	case []any:
		parts := make([]string, len(t))
		for i, e := range t {
			parts[i] = tokenString(e)
		}
		return strings.Join(parts, ",")
	default:
		b, err := json.MarshalIndent(t, "", "  ")
		if err != nil {
			return ""
		}
		return string(b)
	}
}
