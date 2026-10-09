package cardigann

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Decode parses one upstream YAML definition, rejecting any key the model
// does not know.
//
// The strictness is the point. The upstream schema forbids extra keys, so an
// unknown one means the schema grew and this package did not — and a field
// dropped on the floor here is a tracker that silently half-works later.
// yaml.v3's KnownFields does not survive a custom unmarshaler (Node.Decode
// starts a fresh, lenient decoder), which every OrderedMap is, so the check
// walks the node tree against the Go types itself.
//
// Anchors and aliases are refused outright, and so are duplicate keys.
// Upstream uses neither (its CI runs yamllint), and both are what a hostile
// file would use: an alias tree is exponential to walk, and a duplicate key
// is read last-wins by the C# engines and first-wins by OrderedMap.Get.
func Decode(src []byte) (*Definition, error) {
	src, err := protectSlashes(src)
	if err != nil {
		return nil, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(src, &root); err != nil {
		return nil, err
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) != 1 {
		return nil, fmt.Errorf("want one YAML document")
	}
	if err := sanitize(root.Content[0]); err != nil {
		return nil, err
	}
	if err := checkKnown(
		root.Content[0], reflect.TypeFor[Definition](), "",
	); err != nil {
		return nil, err
	}
	var d Definition
	if err := root.Content[0].Decode(&d); err != nil {
		return nil, err
	}
	return &d, nil
}

// slashSentinel stands in for \/ while the YAML is parsed. YAML 1.2 (and the
// .NET parser upstream is written against) reads "\/" in a double-quoted
// scalar as "/", for JSON compatibility; yaml.v3 implements 1.1 and rejects it
// as an unknown escape. Everywhere else — plain, single-quoted and block
// scalars — the backslash is literal text and must survive, since \/ in a CSS
// identifier or a replace filter's argument is not /. So each unescaped \/
// becomes an escape yaml.v3 does know for "/", and sanitize puts \/ back in
// every scalar that is not double-quoted, where the escape was never read.
const slashSentinel = `\U0000002F`

func protectSlashes(src []byte) ([]byte, error) {
	if !bytes.Contains(src, []byte(`\/`)) {
		return src, nil
	}
	if bytes.Contains(src, []byte(slashSentinel)) {
		return nil, fmt.Errorf("source spells both \\/ and %s", slashSentinel)
	}
	out := make([]byte, 0, len(src)+64)
	run := 0 // consecutive backslashes just copied
	for _, c := range src {
		if c == '/' && run%2 == 1 {
			out = append(out[:len(out)-1], slashSentinel...)
			run = 0
			continue
		}
		if c == '\\' {
			run++
		} else {
			run = 0
		}
		out = append(out, c)
	}
	return out, nil
}

// sanitize refuses anchors, aliases and duplicate keys, and restores \/ in
// the scalars protectSlashes reached but the YAML parser did not unescape.
func sanitize(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode || n.Anchor != "" {
		return fmt.Errorf(
			"line %d: YAML anchors and aliases are not accepted",
			n.Line,
		)
	}
	if n.Kind == yaml.ScalarNode && n.Style&yaml.DoubleQuotedStyle == 0 {
		n.Value = strings.ReplaceAll(n.Value, slashSentinel, `\/`)
	}
	if n.Kind == yaml.MappingNode {
		seen := make(map[string]bool, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i]
			if seen[k.Value] {
				return fmt.Errorf("line %d: duplicate key %q", k.Line, k.Value)
			}
			seen[k.Value] = true
		}
	}
	for _, c := range n.Content {
		if err := sanitize(c); err != nil {
			return err
		}
	}
	return nil
}

// valueTyper is how checkKnown sees through an OrderedMap to its values.
type valueTyper interface{ valueType() reflect.Type }

func (OrderedMap[V]) valueType() reflect.Type { return reflect.TypeFor[V]() }

var (
	strType     = reflect.TypeFor[Str]()
	strListType = reflect.TypeFor[StrList]()
)

func checkKnown(n *yaml.Node, t reflect.Type, path string) error {
	if n.Tag == "!!null" {
		return nil
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == strType || t == strListType {
		return nil
	}
	if vt, ok := reflect.TypeAssert[valueTyper](reflect.Zero(t)); ok {
		if n.Kind != yaml.MappingNode {
			return nil // the unmarshaler reports the shape error
		}
		for i := 0; i+1 < len(n.Content); i += 2 {
			p := path + "." + n.Content[i].Value
			if err := checkKnown(n.Content[i+1], vt.valueType(), p); err != nil {
				return err
			}
		}
		return nil
	}
	switch t.Kind() {
	case reflect.Struct:
		if n.Kind != yaml.MappingNode {
			return nil
		}
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i].Value
			f, ok := fieldByYAMLName(t, key)
			if !ok {
				return fmt.Errorf(
					"line %d: unknown key %q at %s",
					n.Content[i].Line, key, strings.TrimPrefix(path, "."),
				)
			}
			if err := checkKnown(n.Content[i+1], f.Type, path+"."+key); err != nil {
				return err
			}
		}
	case reflect.Slice:
		if n.Kind != yaml.SequenceNode {
			return nil
		}
		for i, c := range n.Content {
			p := fmt.Sprintf("%s[%d]", path, i)
			if err := checkKnown(c, t.Elem(), p); err != nil {
				return err
			}
		}
	default:
	}
	return nil
}

func fieldByYAMLName(t reflect.Type, name string) (reflect.StructField, bool) {
	for f := range t.Fields() {
		tag, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if tag != "-" && tag == name {
			return f, true
		}
	}
	return reflect.StructField{}, false
}
