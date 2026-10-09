package cardigann

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"

	"go.yaml.in/yaml/v3"
)

// Str is a scalar read as its literal text. Upstream writes the same key as a
// string in one definition and an int or bool in the next (a select's default
// of 1, a category id of "1"), and every consumer treats it as text anyway.
// yaml.v3 already decodes any scalar into a string as its literal text (and
// refuses a list or a mapping), so the type only names the intent.
type Str string

// StrList accepts a bare scalar or a list of scalars, normalised to a list.
type StrList []string

func (l *StrList) UnmarshalYAML(n *yaml.Node) error {
	items := []*yaml.Node{n}
	switch n.Kind {
	case yaml.ScalarNode:
	case yaml.SequenceNode:
		items = n.Content
	default:
		return fmt.Errorf(
			"line %d: want a scalar or a list, got %s", n.Line, kindName(n),
		)
	}
	out := make(StrList, len(items))
	for i, c := range items {
		if err := c.Decode(&out[i]); err != nil {
			return err
		}
	}
	*l = out
	return nil
}

// Entry is one key/value pair of an OrderedMap.
type Entry[V any] struct {
	Key   string
	Value V
}

// OrderedMap is a YAML mapping that keeps its declaration order. It encodes to
// a JSON object written in that order and decodes one back the same way, so
// the embedded files read like the upstream YAML they came from.
type OrderedMap[V any] []Entry[V]

// Get returns the value under key, if any.
func (m OrderedMap[V]) Get(key string) (V, bool) {
	for _, e := range m {
		if e.Key == key {
			return e.Value, true
		}
	}
	var zero V
	return zero, false
}

func (m *OrderedMap[V]) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: want a mapping, got %s", n.Line, kindName(n))
	}
	out := make(OrderedMap[V], 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		var v V
		if err := n.Content[i+1].Decode(&v); err != nil {
			return err
		}
		out = append(out, Entry[V]{Key: n.Content[i].Value, Value: v})
	}
	*m = out
	return nil
}

// MarshalJSONTo writes the entries as one JSON object, in order. It is the
// json/v2 interface, which encoding/json honours as well; working on the
// caller's encoder, it inherits the caller's options — no HTML escaping and
// the indent in EncodeJSON — rather than re-encoding each value apart.
func (m OrderedMap[V]) MarshalJSONTo(enc *jsontext.Encoder) error {
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return err
	}
	for _, e := range m {
		if err := enc.WriteToken(jsontext.String(e.Key)); err != nil {
			return err
		}
		if err := jsonv2.MarshalEncode(enc, e.Value); err != nil {
			return err
		}
	}
	return enc.WriteToken(jsontext.EndObject)
}

// UnmarshalJSONFrom reads one JSON object back in document order.
func (m *OrderedMap[V]) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	if dec.PeekKind() == 'n' {
		_, err := dec.ReadToken()
		*m = nil
		return err
	}
	tok, err := dec.ReadToken()
	if err != nil {
		return err
	}
	if tok.Kind() != '{' {
		return fmt.Errorf("ordered map: want a JSON object, got %s", tok.Kind())
	}
	var out OrderedMap[V]
	for dec.PeekKind() == '"' {
		tok, err := dec.ReadToken()
		if err != nil {
			return err
		}
		key := tok.String() // a token is only valid until the next read
		var v V
		if err := jsonv2.UnmarshalDecode(dec, &v); err != nil {
			return err
		}
		out = append(out, Entry[V]{Key: key, Value: v})
	}
	if _, err := dec.ReadToken(); err != nil { // the closing brace
		return err
	}
	*m = out
	return nil
}

// EncodeJSON is the snapshot's on-disk encoding: two-space indent, no HTML
// escaping, trailing newline. The sync writes with it and the embedded-data
// test re-encodes with it, so any field the model fails to round-trip shows
// up as a diff against the committed file.
func EncodeJSON(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func kindName(n *yaml.Node) string {
	switch n.Kind {
	case yaml.DocumentNode:
		return "a document"
	case yaml.SequenceNode:
		return "a list"
	case yaml.MappingNode:
		return "a mapping"
	case yaml.ScalarNode:
		return "a scalar"
	case yaml.AliasNode:
		return "an alias"
	default:
		return "an unknown node"
	}
}
