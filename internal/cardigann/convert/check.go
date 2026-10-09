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
func Check(d *cardigann.Definition) error {
	return check(reflect.ValueOf(d), "")
}

func check(v reflect.Value, path string) error {
	at := func(err error) error {
		return fmt.Errorf("%s: %w", strings.TrimPrefix(path, "."), err)
	}
	if v.Type() == filterType {
		f, _ := reflect.TypeAssert[cardigann.Filter](v)
		if err := checkFilter(f); err != nil {
			return at(err)
		}
		return nil
	}
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			return check(v.Elem(), path)
		}
	case reflect.Struct:
		t := v.Type()
		for i := range t.NumField() {
			if !t.Field(i).IsExported() {
				continue
			}
			if err := check(v.Field(i), path+"."+t.Field(i).Name); err != nil {
				return err
			}
		}
	case reflect.Slice:
		for i := range v.Len() {
			if err := check(v.Index(i), fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case reflect.String:
		if err := checkTemplate(v.String()); err != nil {
			return at(err)
		}
	default:
	}
	return nil
}

func checkFilter(f cardigann.Filter) error {
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
		if err := checkTemplate(a); err != nil {
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

func checkTemplate(s string) error {
	if !strings.Contains(s, "{{") {
		return nil
	}
	t, err := template.New("").Funcs(templateStubs).Parse(s)
	if err != nil {
		return fmt.Errorf("template %q: %w", s, err)
	}
	return checkNode(t.Root)
}

// checkNode compiles the pattern of every re_replace call in a parsed
// template, on the engine its function name selects.
func checkNode(n parse.Node) error {
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
		if err := checkCall(n); err != nil {
			return err
		}
		kids = append(kids, n.Args...)
	default:
	}
	for _, k := range kids {
		if k == nil || reflect.ValueOf(k).IsNil() {
			continue
		}
		if err := checkNode(k); err != nil {
			return err
		}
	}
	return nil
}

func checkCall(c *parse.CommandNode) error {
	if len(c.Args) < 3 {
		return nil
	}
	fn, ok := c.Args[0].(*parse.IdentifierNode)
	if !ok {
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
