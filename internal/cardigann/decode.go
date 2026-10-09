package cardigann

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"sync"

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
	// yaml.Unmarshal reads the first document and ignores the rest, so a
	// second one — parseable or not — would ride along unseen.
	var root yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(src))
	if err := dec.Decode(&root); err != nil {
		return nil, err
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("want one YAML document")
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) != 1 {
		return nil, fmt.Errorf("want one YAML document")
	}
	if err := sanitize(root.Content[0]); err != nil {
		return nil, err
	}
	doc := root.Content[0]
	if err := checkKnown(doc, reflect.TypeFor[Definition]()); err != nil {
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

// sanitize refuses anchors, aliases, explicit tags and duplicate keys, and
// restores \/ in the scalars protectSlashes reached but the YAML parser did
// not unescape. Upstream writes no tags, and each one changes decoding behind
// the strict check's back: !!null on a mapping still fills the struct while
// skipping the unknown-key walk, !!binary base64-decodes a string.
func sanitize(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode || n.Anchor != "" {
		return fmt.Errorf(
			"line %d: YAML anchors and aliases are not accepted",
			n.Line,
		)
	}
	if n.Style&yaml.TaggedStyle != 0 {
		return fmt.Errorf(
			"line %d: explicit YAML tag %s is not accepted",
			n.Line,
			n.Tag,
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

var strListType = reflect.TypeFor[StrList]()

// unknownKey is checkKnown's error. Its path is collected on the way back up
// the recursion, so a definition that passes — every upstream one — never
// pays for building path strings it would not print.
type unknownKey struct {
	line int
	key  string
	path []string // innermost segment first
}

func (e *unknownKey) Error() string {
	var b strings.Builder
	for _, seg := range slices.Backward(e.path) {
		b.WriteString(seg)
	}
	at := strings.TrimPrefix(b.String(), ".")
	if at == "" {
		at = "top level"
	}
	return fmt.Sprintf("line %d: unknown key %q at %s", e.line, e.key, at)
}

func within(err error, seg func() string) error {
	if u, ok := errors.AsType[*unknownKey](err); ok {
		u.path = append(u.path, seg())
	}
	return err
}

func checkKnown(n *yaml.Node, t reflect.Type) error {
	if n.Kind == yaml.ScalarNode && n.Tag == "!!null" {
		return nil
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == strListType {
		return nil
	}
	if vt, ok := reflect.TypeAssert[valueTyper](reflect.Zero(t)); ok {
		if n.Kind != yaml.MappingNode {
			return nil // the unmarshaler reports the shape error
		}
		for i := 0; i+1 < len(n.Content); i += 2 {
			if err := checkKnown(n.Content[i+1], vt.valueType()); err != nil {
				key := n.Content[i].Value
				return within(err, func() string { return "." + key })
			}
		}
		return nil
	}
	switch t.Kind() {
	case reflect.Struct:
		if n.Kind != yaml.MappingNode {
			return nil
		}
		fields := yamlFields(t)
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i].Value
			ft, ok := fields[key]
			if !ok {
				return &unknownKey{line: n.Content[i].Line, key: key}
			}
			if err := checkKnown(n.Content[i+1], ft); err != nil {
				return within(err, func() string { return "." + key })
			}
		}
	case reflect.Slice:
		if n.Kind != yaml.SequenceNode {
			return nil
		}
		for i, c := range n.Content {
			if err := checkKnown(c, t.Elem()); err != nil {
				return within(err, func() string { return fmt.Sprintf("[%d]", i) })
			}
		}
	default:
	}
	return nil
}

// fieldCache holds each struct type's YAML key → field type table, built once
// per type instead of re-parsing tags on every key of every file.
var fieldCache sync.Map // reflect.Type → map[string]reflect.Type

func yamlFields(t reflect.Type) map[string]reflect.Type {
	if m, ok := fieldCache.Load(t); ok {
		fields, _ := m.(map[string]reflect.Type)
		return fields
	}
	m := make(map[string]reflect.Type, t.NumField())
	for f := range t.Fields() {
		tag, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if tag != "" && tag != "-" {
			m[tag] = f.Type
		}
	}
	fieldCache.Store(t, m)
	return m
}
