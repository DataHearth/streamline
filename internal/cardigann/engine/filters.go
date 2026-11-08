package engine

import (
	"encoding/base64"
	"fmt"
	"html"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/datahearth/streamline/internal/cardigann"
)

// validateDelims split a validate filter's word list and the value it checks,
// as Prowlarr's does.
const validateDelims = ", /)(.;[]\"|:"

func arg(f cardigann.Filter, i int) string {
	if i < len(f.Args) {
		return f.Args[i]
	}
	return ""
}

// applyFilters runs a value through a filter chain in order (ApplyFilters).
// A failing dateparse keeps its input, as upstream's does; any other failure
// is the field's.
func (e *Engine) applyFilters(
	value string,
	filters []cardigann.Filter,
	v *vars,
) (string, error) {
	for _, f := range filters {
		out, err := e.applyFilter(value, f, v)
		if err != nil {
			return "", fmt.Errorf("filter %s: %w", f.Name, err)
		}
		value = out
	}
	return value, nil
}

func (e *Engine) applyFilter(
	value string,
	f cardigann.Filter,
	v *vars,
) (string, error) {
	switch f.Name {
	case "querystring":
		_, rawQuery, ok := strings.Cut(value, "?")
		if !ok {
			return "", fmt.Errorf("%q has no query string", value)
		}
		rawQuery, _, _ = strings.Cut(rawQuery, "#")
		q, err := url.ParseQuery(rawQuery)
		if err != nil {
			return "", err
		}
		return q.Get(arg(f, 0)), nil
	case "dateparse", "timeparse":
		if len(f.Args) == 0 {
			t, err := fromUnknown(value, e.now())
			if err != nil {
				return value, nil //nolint:nilerr // upstream passes an unparsed date through
			}
			return t.Format(dateLayout), nil
		}
		t, err := cardigann.ParseDate(f.Args, value, e.now())
		if err != nil {
			return value, nil //nolint:nilerr // upstream passes an unparsed date through
		}
		return t.Format(dateLayout), nil
	case "regexp":
		return e.regexpGroup(value, arg(f, 0), f.Engine)
	case "re_replace":
		repl, err := render(arg(f, 1), v, plainText)
		if err != nil {
			return "", err
		}
		return e.regexReplace(value, arg(f, 0), repl, f.Engine)
	case "split":
		sep, _ := utf8.DecodeRuneInString(arg(f, 0))
		pos, err := strconv.Atoi(arg(f, 1))
		if err != nil {
			return "", err
		}
		parts := strings.Split(value, string(sep))
		if pos < 0 {
			pos += len(parts)
		}
		if pos < 0 || pos >= len(parts) {
			return "", fmt.Errorf("no part %s of %q", arg(f, 1), value)
		}
		return parts[pos], nil
	case "replace":
		to, err := render(arg(f, 1), v, plainText)
		if err != nil {
			return "", err
		}
		return strings.ReplaceAll(value, arg(f, 0), to), nil
	case "trim":
		if c, _ := utf8.DecodeRuneInString(arg(f, 0)); len(f.Args) > 0 &&
			arg(f, 0) != "" {
			return strings.Trim(value, string(c)), nil
		}
		return strings.TrimSpace(value), nil
	case "prepend":
		s, err := render(arg(f, 0), v, plainText)
		return s + value, err
	case "append":
		s, err := render(arg(f, 0), v, plainText)
		return value + s, err
	case "tolower":
		return strings.ToLower(value), nil
	case "toupper":
		return strings.ToUpper(value), nil
	case "urldecode":
		return e.urlDecode(value)
	case "urlencode":
		return e.urlEncode(value)
	case "htmldecode":
		return html.UnescapeString(value), nil
	case "htmlencode":
		return htmlEncode(value), nil
	case "timeago", "reltime":
		t, err := fromTimeAgo(value, e.now())
		if err != nil {
			return "", err
		}
		return t.Format(dateLayout), nil
	case "fuzzytime":
		t, err := fromUnknown(value, e.now())
		if err != nil {
			return "", err
		}
		return t.Format(dateLayout), nil
	case "validfilename":
		// Path.GetInvalidFileNameChars on the Linux host Prowlarr runs on.
		return strings.NewReplacer("/", "_", "\x00", "_").Replace(value), nil
	case "diacritics":
		if arg(f, 0) != "replace" {
			return "", fmt.Errorf("unsupported argument %q", arg(f, 0))
		}
		var b strings.Builder
		for _, r := range norm.NFD.String(value) {
			if !unicode.Is(unicode.Mn, r) {
				b.WriteRune(r)
			}
		}
		return norm.NFC.String(b.String()), nil
	case "jsonjoinarray":
		doc, err := decodeJSON([]byte(value))
		if err != nil {
			return "", err
		}
		tok, err := selectToken(doc, arg(f, 0))
		if err != nil {
			return "", err
		}
		items, ok := tok.([]any)
		if !ok {
			return "", fmt.Errorf("%q is not an array", arg(f, 0))
		}
		parts := make([]string, len(items))
		for i, it := range items {
			parts[i] = tokenString(it)
		}
		return strings.Join(parts, arg(f, 1)), nil
	case "base64decode":
		// WebEncoders.Base64UrlDecode: the URL alphabet, padding optional;
		// the standard alphabet reads too, as .NET's decoder accepts it.
		b, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(
			strings.NewReplacer("-", "+", "_", "/").Replace(value), "="))
		if err != nil {
			return "", err
		}
		return string(b), nil
	case "base64encode":
		return base64.RawURLEncoding.EncodeToString([]byte(value)), nil
	case "hexdump", "strdump":
		// Upstream's debugging aids: they log the value and pass it on.
		return value, nil
	case "validate":
		valid := splitWords(strings.ToLower(arg(f, 0)))
		have := splitWords(strings.ToLower(value))
		out := make([]string, 0, len(valid))
		for _, w := range valid {
			if slices.Contains(have, w) && !slices.Contains(out, w) {
				out = append(out, w)
			}
		}
		return strings.Join(out, ", "), nil
	default:
		// convert refuses a filter outside its vocabulary, so this is a
		// definition that did not come through it.
		return "", fmt.Errorf("unknown filter %q", f.Name)
	}
}

func splitWords(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return strings.ContainsRune(validateDelims, r)
	})
}

// regexpGroup is the regexp filter: the first group of the first match, or
// "" when nothing matches.
func (e *Engine) regexpGroup(value, pattern, engine string) (string, error) {
	if engine == cardigann.RegexEngineNET {
		re, err := compileNET(pattern)
		if err != nil {
			return "", err
		}
		m, err := re.FindStringMatch(value)
		if err != nil || m == nil {
			return "", err
		}
		if g := m.GroupByNumber(1); g != nil {
			return g.String(), nil
		}
		return "", nil
	}
	re, err := compileRE2(pattern)
	if err != nil {
		return "", err
	}
	if m := re.FindStringSubmatch(value); len(m) > 1 {
		return m[1], nil
	}
	return "", nil
}

func (e *Engine) regexReplace(value, pattern, repl, engine string) (string, error) {
	if engine == cardigann.RegexEngineNET {
		re, err := compileNET(pattern)
		if err != nil {
			return "", err
		}
		return re.Replace(value, repl, -1, -1)
	}
	re, err := compileRE2(pattern)
	if err != nil {
		return "", err
	}
	return re.ReplaceAllString(value, repl), nil
}

// htmlEncode is WebUtility.HtmlEncode: the five markup characters, and
// U+00A0–U+00FF as numeric references.
func htmlEncode(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '&':
			b.WriteString("&amp;")
		case r == '"':
			b.WriteString("&quot;")
		case r == '\'':
			b.WriteString("&#39;")
		case r >= 0xA0 && r <= 0xFF:
			b.WriteString("&#" + strconv.Itoa(int(r)) + ";")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
