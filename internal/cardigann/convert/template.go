package convert

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	"github.com/datahearth/streamline/internal/cardigann"
)

// reReplaceCall matches a template's re_replace call the way Jackett does:
// the pattern and replacement are taken verbatim between the quotes, with no
// escape processing. Go's template lexer unquotes string literals like Go
// source, so a "\s+" upstream is a parse error here until it is re-quoted.
var reReplaceCall = regexp.MustCompile(
	`\{\{\s*re_replace\s+(\.\S+)\s+"(.*?)"\s+"(.*?)"\s*\}\}`,
)

// templateAction is one {{ ... }} action; dottedName is a .Config or
// .Result lookup inside one. Upstream names settings and fields freely
// (2facode, cat-id), and Go's template lexer reads .Config.2facode as a
// number and .Config.cat-id as a subtraction. Such lookups become
// (index .Config "2facode"), which is why the engine must hand both to the
// template as maps.
var (
	templateAction = regexp.MustCompile(`\{\{.*?\}\}`)
	dottedName     = regexp.MustCompile(`\.(Config|Result)\.([A-Za-z0-9_-]+)`)
	goIdentifier   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

func indexOddNames(s string) string {
	return templateAction.ReplaceAllStringFunc(s, func(action string) string {
		return dottedName.ReplaceAllStringFunc(action, func(ref string) string {
			m := dottedName.FindStringSubmatch(ref)
			if goIdentifier.MatchString(m[2]) {
				return ref
			}
			return fmt.Sprintf("(index .%s %s)", m[1], strconv.Quote(m[2]))
		})
	})
}

// templateStubs exist only so Parse accepts the calls; the engine supplies
// the implementations under the same names.
var templateStubs = template.FuncMap{
	cardigann.TemplateFuncReplace:    func(string, string, string) string { return "" },
	cardigann.TemplateFuncReplaceNET: func(string, string, string) string { return "" },
	cardigann.TemplateFuncJoin:       func([]string, string) string { return "" },
}

// translateTemplate rewrites a template string for Go's text/template and
// checks it parses. It reports whether a re_replace in it needed regexp2.
func translateTemplate(s string) (string, bool, error) {
	if !strings.Contains(s, "{{") {
		return s, false, nil
	}
	var (
		usesNET bool
		failed  error
	)
	out := reReplaceCall.ReplaceAllStringFunc(s, func(call string) string {
		m := reReplaceCall.FindStringSubmatch(call)
		pat, rep, engine, err := translatePair(m[2], m[3])
		if err != nil {
			failed = err
			return call
		}
		fn := cardigann.TemplateFuncReplace
		if engine == cardigann.RegexEngineNET {
			fn = cardigann.TemplateFuncReplaceNET
			usesNET = true
		}
		return fmt.Sprintf(
			"{{ %s %s %s %s }}", fn, m[1], strconv.Quote(pat), strconv.Quote(rep),
		)
	})
	if failed != nil {
		return "", false, failed
	}
	out = indexOddNames(out)
	if _, err := template.New("").Funcs(templateStubs).Parse(out); err != nil {
		return "", false, fmt.Errorf("template %q: %w", s, err)
	}
	return out, usesNET, nil
}
