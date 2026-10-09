package cardigann

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"go.yaml.in/yaml/v3"
)

// Str is a scalar read as its literal text. Upstream writes the same key as a
// string in one definition and an int or bool in the next (a select's default
// of 1, a category id of "1"), and every consumer treats it as text anyway.
type Str string

func (s *Str) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode {
		return fmt.Errorf("line %d: want a scalar, got %s", n.Line, kindName(n))
	}
	if n.Tag == "!!null" {
		*s = ""
		return nil
	}
	*s = Str(n.Value)
	return nil
}

// StrList accepts a bare scalar or a list of scalars, normalised to a list.
type StrList []string

func (l *StrList) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		var s Str
		if err := s.UnmarshalYAML(n); err != nil {
			return err
		}
		*l = StrList{string(s)}
		return nil
	case yaml.SequenceNode:
		out := make(StrList, 0, len(n.Content))
		for _, c := range n.Content {
			var s Str
			if err := s.UnmarshalYAML(c); err != nil {
				return err
			}
			out = append(out, string(s))
		}
		*l = out
		return nil
	default:
		return fmt.Errorf(
			"line %d: want a scalar or a list, got %s", n.Line, kindName(n),
		)
	}
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

func (m OrderedMap[V]) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, e := range m {
		if i > 0 {
			b.WriteByte(',')
		}
		if err := writeJSON(&b, e.Key); err != nil {
			return nil, err
		}
		b.WriteByte(':')
		if err := writeJSON(&b, e.Value); err != nil {
			return nil, err
		}
	}
	b.WriteByte('}')
	return b.Bytes(), nil
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

// writeJSON encodes without HTML escaping. A definition is mostly CSS
// selectors and HTML snippets; escaped, "td > a" lands on disk as
// "td \u003e a", and an Encoder's own SetEscapeHTML(false) cannot undo what an
// inner json.Marshal already escaped.
func writeJSON(b *bytes.Buffer, v any) error {
	enc := json.NewEncoder(b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return err
	}
	b.Truncate(b.Len() - 1) // Encode's trailing newline
	return nil
}

func (m *OrderedMap[V]) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		*m = nil
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return errors.New("ordered map: want a JSON object")
	}
	var out OrderedMap[V]
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := tok.(string)
		if !ok {
			return errors.New("ordered map: want a string key")
		}
		var v V
		if err := dec.Decode(&v); err != nil {
			return err
		}
		out = append(out, Entry[V]{Key: key, Value: v})
	}
	*m = out
	return nil
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
