package convert

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/datahearth/streamline/internal/cardigann"
)

var filterType = reflect.TypeFor[cardigann.Filter]()

// visit walks every Filter and every string in a definition, depth first.
// Filters are handed over whole (which of their arguments is a regex, a
// layout or a template depends on the filter) and not descended into; every
// other string goes to onString, settable in place.
func visit(
	v reflect.Value,
	onFilter func(*cardigann.Filter) error,
	onString func(reflect.Value) error,
) error {
	if v.Type() == filterType {
		f, _ := reflect.TypeAssert[*cardigann.Filter](v.Addr())
		return onFilter(f)
	}
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			return visit(v.Elem(), onFilter, onString)
		}
	case reflect.Struct:
		t := v.Type()
		for i := range t.NumField() {
			if !t.Field(i).IsExported() {
				continue
			}
			if err := visit(v.Field(i), onFilter, onString); err != nil {
				return locate(err, "."+t.Field(i).Name)
			}
		}
	case reflect.Slice:
		for i := range v.Len() {
			if err := visit(v.Index(i), onFilter, onString); err != nil {
				return locate(err, fmt.Sprintf("[%d]", i))
			}
		}
	case reflect.String:
		return onString(v)
	default:
	}
	return nil
}

// located prefixes an error with where in the definition it happened. The
// path is collected as the walk unwinds, so a definition that converts
// cleanly builds no path strings at all.
type located struct {
	path []string // innermost segment first
	err  error
}

func (l *located) Error() string {
	var b strings.Builder
	for _, seg := range slices.Backward(l.path) {
		b.WriteString(seg)
	}
	return strings.TrimPrefix(b.String(), ".") + ": " + l.err.Error()
}

func (l *located) Unwrap() error { return l.err }

func locate(err error, seg string) error {
	if l, ok := errors.AsType[*located](err); ok {
		l.path = append(l.path, seg)
		return l
	}
	return &located{path: []string{seg}, err: err}
}
