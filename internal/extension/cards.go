package extension

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// ErrBadCardValues is a settings card write that does not fit the card's
// declared fields.
var ErrBadCardValues = errors.New("the values do not match the card's fields")

func (c AccountCard) checkFields() error {
	if len(c.Fields) == 0 {
		return fmt.Errorf(
			"%w: settings card %q declares no fields",
			ErrBadManifest,
			c.ID,
		)
	}
	seen := map[string]bool{}
	for _, f := range c.Fields {
		switch {
		case f.ID == "" || seen[f.ID]:
			return fmt.Errorf(
				"%w: settings card %q has an empty or repeated field id %q",
				ErrBadManifest, c.ID, f.ID,
			)
		case f.Kind != FieldSwitch && f.Kind != FieldNumber:
			return fmt.Errorf(
				"%w: field %q of card %q has unknown kind %q",
				ErrBadManifest, f.ID, c.ID, f.Kind,
			)
		case f.Kind == FieldNumber && f.Min > f.Max:
			return fmt.Errorf(
				"%w: field %q of card %q has min above max",
				ErrBadManifest, f.ID, c.ID,
			)
		}
		seen[f.ID] = true
	}
	return nil
}

// CheckValues validates a settings card write against the declared fields:
// every key declared, a switch holds a boolean, a number an integer within
// its bounds. A field left out is left as it is.
func (c AccountCard) CheckValues(raw []byte) error {
	var body struct {
		Values map[string]json.RawMessage `json:"values"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || body.Values == nil {
		return fmt.Errorf("%w: expected {\"values\": {...}}", ErrBadCardValues)
	}
	for id, v := range body.Values {
		i := slices.IndexFunc(c.Fields, func(f CardField) bool { return f.ID == id })
		if i < 0 {
			return fmt.Errorf(
				"%w: %q is not a field of this card",
				ErrBadCardValues,
				id,
			)
		}
		if err := c.Fields[i].check(v); err != nil {
			return err
		}
	}
	return nil
}

func (f CardField) check(v json.RawMessage) error {
	switch f.Kind {
	case FieldSwitch:
		var b *bool
		if err := json.Unmarshal(v, &b); err != nil || b == nil {
			return fmt.Errorf("%w: %q must be true or false", ErrBadCardValues, f.ID)
		}
	case FieldNumber:
		dec := json.NewDecoder(bytes.NewReader(v))
		dec.UseNumber()
		var raw any
		n, isNumber := json.Number(""), false
		if err := dec.Decode(&raw); err == nil {
			n, isNumber = raw.(json.Number)
		}
		if !isNumber {
			return fmt.Errorf(
				"%w: %q must be a whole number",
				ErrBadCardValues,
				f.ID,
			)
		}
		i, err := n.Int64()
		if err != nil {
			return fmt.Errorf(
				"%w: %q must be a whole number",
				ErrBadCardValues,
				f.ID,
			)
		}
		if i < f.Min || i > f.Max {
			return fmt.Errorf(
				"%w: %q must be between %d and %d",
				ErrBadCardValues, f.ID, f.Min, f.Max,
			)
		}
	}
	return nil
}
