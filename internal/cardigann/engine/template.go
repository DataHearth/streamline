package engine

import (
	"fmt"
	"maps"
	"regexp"
	"strings"
	"sync"
	"text/template"
	"text/template/parse"

	"github.com/dlclark/regexp2/v2"

	"github.com/datahearth/streamline/internal/cardigann"
)

// vars is a request's template data. Every value is a string, and absent is
// "" — Prowlarr's null — never Go's "<no value>", so the maps are read with
// missingkey=zero and every .Query key is pre-set (see the engine contract
// in docs/agents/cardigann.md).
type vars struct {
	config      map[string]string
	query       map[string]string
	result      map[string]string
	keywords    string
	categories  []string
	downloadURI map[string]any
	year        string
}

func (v *vars) data() map[string]any {
	d := map[string]any{
		"Config":     v.config,
		"Query":      v.query,
		"Result":     v.result,
		"Keywords":   v.keywords,
		"Categories": v.categories,
		"True":       "True",
		"False":      "",
		"Today":      map[string]string{"Year": v.year},
	}
	if v.downloadURI != nil {
		d["DownloadUri"] = v.downloadURI
	}
	return d
}

// clone copies what a search path or a row writes to, so one path's
// categories or one row's fields never leak into the next.
func (v *vars) clone() *vars {
	c := *v
	c.result = maps.Clone(v.result)
	c.categories = append([]string(nil), v.categories...)
	return &c
}

// templateMode says how a template's output is escaped. A search path and a
// $raw input URL-encode each substituted value but none of the literal text
// around it, which Go's templates have no hook for: the parse tree gets an
// extra urlencode command at the end of every action instead.
type templateMode uint8

const (
	plainText templateMode = iota
	urlEncoded
)

var templateFuncs = template.FuncMap{
	cardigann.TemplateFuncReplace: func(in, pattern, repl string) (string, error) {
		re, err := compileRE2(pattern)
		if err != nil {
			return "", err
		}
		return re.ReplaceAllString(in, repl), nil
	},
	cardigann.TemplateFuncReplaceNET: func(in, pattern, repl string) (string, error) {
		re, err := compileNET(pattern)
		if err != nil {
			return "", err
		}
		return re.Replace(in, repl, -1, -1)
	},
	cardigann.TemplateFuncJoin: func(items []string, sep string) string {
		return strings.Join(items, sep)
	},
	"urlencode": func(s string) string { return webURLEncode([]byte(s)) },
}

type templateKey struct {
	text string
	mode templateMode
}

var templateCache sync.Map // templateKey → *template.Template

func render(text string, v *vars, mode templateMode) (string, error) {
	if !strings.Contains(text, "{{") {
		return text, nil
	}
	key := templateKey{text, mode}
	cached, ok := templateCache.Load(key)
	if !ok {
		t, err := template.New("").
			Option("missingkey=zero").
			Funcs(templateFuncs).
			Parse(text)
		if err != nil {
			return "", fmt.Errorf("template %q: %w", text, err)
		}
		if mode == urlEncoded {
			encodeActions(t.Tree, t.Root)
		}
		cached, _ = templateCache.LoadOrStore(key, t)
	}
	var b strings.Builder
	if err := cached.(*template.Template).Execute(&b, v.data()); err != nil {
		return "", fmt.Errorf("template %q: %w", text, err)
	}
	return b.String(), nil
}

func encodeActions(tree *parse.Tree, n parse.Node) {
	switch n := n.(type) {
	case *parse.ListNode:
		if n == nil {
			return
		}
		for _, c := range n.Nodes {
			encodeActions(tree, c)
		}
	case *parse.ActionNode:
		if len(n.Pipe.Decl) > 0 {
			return
		}
		cmd := &parse.CommandNode{NodeType: parse.NodeCommand, Pos: n.Pos}
		cmd.Args = []parse.Node{
			parse.NewIdentifier("urlencode").SetTree(tree).SetPos(n.Pos),
		}
		n.Pipe.Cmds = append(n.Pipe.Cmds, cmd)
	case *parse.IfNode:
		encodeActions(tree, n.List)
		encodeActions(tree, n.ElseList)
	case *parse.RangeNode:
		encodeActions(tree, n.List)
		encodeActions(tree, n.ElseList)
	case *parse.WithNode:
		encodeActions(tree, n.List)
		encodeActions(tree, n.ElseList)
	}
}

// webURLEncode is .NET's WebUtility.UrlEncode over already-encoded bytes:
// letters, digits and -_.!*() pass, a space becomes '+', everything else
// %XX in upper case.
func webURLEncode(b []byte) string {
	const hex = "0123456789ABCDEF"
	var out strings.Builder
	for _, c := range b {
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9',
			strings.IndexByte("-_.!*()", c) >= 0:
			out.WriteByte(c)
		case c == ' ':
			out.WriteByte('+')
		default:
			out.WriteByte('%')
			out.WriteByte(hex[c>>4])
			out.WriteByte(hex[c&15])
		}
	}
	return out.String()
}

var (
	re2Cache sync.Map // pattern → *regexp.Regexp
	netCache sync.Map // pattern → *regexp2.Regexp
)

func compileRE2(pattern string) (*regexp.Regexp, error) {
	if re, ok := re2Cache.Load(pattern); ok {
		return re.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	re2Cache.Store(pattern, re)
	return re, nil
}

// compileNET compiles a pattern the converter tagged for regexp2, with the
// match timeout the backtracking engine needs against a hostile page.
func compileNET(pattern string) (*regexp2.Regexp, error) {
	if re, ok := netCache.Load(pattern); ok {
		return re.(*regexp2.Regexp), nil
	}
	re, err := regexp2.Compile(pattern, regexp2.None)
	if err != nil {
		return nil, err
	}
	re.MatchTimeout = regexTimeout
	netCache.Store(pattern, re)
	return re, nil
}
