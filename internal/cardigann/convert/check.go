package convert

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"text/template"
	"text/template/parse"

	"github.com/dlclark/regexp2/v2"

	"github.com/datahearth/streamline/internal/cardigann"
)

// Check verifies that a converted definition is runnable as it stands: every
// regex — in a filter or inside a template's re_replace call — compiles on the
// engine it is assigned, and every template parses with the engine's
// function set. Convert only produces definitions that pass; Check is for
// what comes back from storage, so the embedded snapshot's test runs it over
// every file and a hand edit cannot slip past.
//
// It also refuses a template that reads a setting or field the definition
// does not declare. Prowlarr renders an unknown variable as an empty string;
// Go renders it as "<no value>" and compares it unequal to everything, so an
// undeclared reference is a definition that runs differently here.
func Check(d *cardigann.Definition) error {
	names := declaredNames(d)
	return visit(reflect.ValueOf(d).Elem(),
		func(f *cardigann.Filter) error { return checkFilter(*f, names) },
		func(s reflect.Value) error { return checkTemplate(s.String(), names) },
	)
}

// declared is what a template may read under .Config and .Result: every
// setting, plus sitelink, which the engine always provides; and every field,
// named without its |modifiers.
type declared struct{ config, result map[string]bool }

func declaredNames(d *cardigann.Definition) declared {
	n := declared{
		config: map[string]bool{"sitelink": true},
		result: map[string]bool{},
	}
	for _, s := range d.Settings {
		n.config[s.Name] = true
	}
	for _, f := range d.Search.Fields {
		name, _, _ := strings.Cut(f.Key, "|")
		n.result[name] = true
	}
	return n
}

func (n declared) has(scope, name string) bool {
	switch scope {
	case "Config":
		return n.config[name]
	case "Result":
		return n.result[name]
	default:
		return true
	}
}

func checkFilter(f cardigann.Filter, names declared) error {
	args := f.Args
	switch f.Name {
	case "regexp", "re_replace":
		if len(args) == 0 {
			return fmt.Errorf("%s filter without a pattern", f.Name)
		}
		if err := compileFor(f.Engine, args[0]); err != nil {
			return err
		}
		args = args[1:] // a re_replace's replacement may be templated
	case "dateparse", "timeparse":
		return nil
	default:
	}
	for _, a := range args {
		if err := checkTemplate(a, names); err != nil {
			return err
		}
	}
	return nil
}

func compileFor(engine, pat string) error {
	var err error
	switch engine {
	case "":
		_, err = regexp.Compile(pat)
	case cardigann.RegexEngineNET:
		_, err = regexp2.Compile(pat, regexp2.None)
	default:
		err = fmt.Errorf("unknown regex engine %q", engine)
	}
	return err
}

func checkTemplate(s string, names declared) error {
	if !strings.Contains(s, "{{") {
		return nil
	}
	t, err := template.New("").Funcs(templateStubs).Parse(s)
	if err != nil {
		return fmt.Errorf("template %q: %w", s, err)
	}
	return checkNode(t.Root, names)
}

// checkNode compiles the pattern of every re_replace call in a parsed
// template, on the engine its function name selects.
func checkNode(n parse.Node, names declared) error {
	var kids []parse.Node
	switch n := n.(type) {
	case *parse.ListNode:
		if n != nil {
			kids = append(kids, n.Nodes...)
		}
	case *parse.ActionNode:
		kids = append(kids, n.Pipe)
	case *parse.IfNode:
		kids = append(kids, n.Pipe, n.List, n.ElseList)
	case *parse.RangeNode:
		kids = append(kids, n.Pipe, n.List, n.ElseList)
	case *parse.WithNode:
		kids = append(kids, n.Pipe, n.List, n.ElseList)
	case *parse.PipeNode:
		if n != nil {
			for _, c := range n.Cmds {
				kids = append(kids, c)
			}
		}
	case *parse.CommandNode:
		if err := checkCall(n, names); err != nil {
			return err
		}
		kids = append(kids, n.Args...)
	case *parse.FieldNode:
		if len(n.Ident) >= 2 && !names.has(n.Ident[0], n.Ident[1]) {
			return fmt.Errorf(
				"template reads undeclared .%s.%s",
				n.Ident[0],
				n.Ident[1],
			)
		}
	default:
	}
	for _, k := range kids {
		if k == nil || reflect.ValueOf(k).IsNil() {
			continue
		}
		if err := checkNode(k, names); err != nil {
			return err
		}
	}
	return nil
}

func checkCall(c *parse.CommandNode, names declared) error {
	if len(c.Args) < 3 {
		return nil
	}
	fn, ok := c.Args[0].(*parse.IdentifierNode)
	if !ok {
		return nil
	}
	// (index .Config "cat-id") is how the converter spells a lookup whose
	// name is not a Go identifier.
	if fn.Ident == "index" {
		scope, okS := c.Args[1].(*parse.FieldNode)
		key, okK := c.Args[2].(*parse.StringNode)
		if okS && okK && len(scope.Ident) == 1 &&
			!names.has(scope.Ident[0], key.Text) {
			return fmt.Errorf(
				"template reads undeclared .%s.%s",
				scope.Ident[0],
				key.Text,
			)
		}
		return nil
	}
	engine := ""
	switch fn.Ident {
	case cardigann.TemplateFuncReplace:
	case cardigann.TemplateFuncReplaceNET:
		engine = cardigann.RegexEngineNET
	default:
		return nil
	}
	pat, ok := c.Args[2].(*parse.StringNode)
	if !ok {
		return fmt.Errorf("%s pattern is not a string literal", fn.Ident)
	}
	return compileFor(engine, pat.Text)
}
