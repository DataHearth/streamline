// Package convert turns an upstream Cardigann definition (Prowlarr/Indexers,
// itself synced from Jackett) into the form streamline embeds and runs.
//
// Upstream definitions are written for a C# engine: .NET regexes, .NET date
// layouts, and a template dialect that is Go's text/template in syntax but
// not in string-literal quoting. Everything that is merely spelled
// differently is rewritten here, once, so the engine never has to know which
// dialect a value came from; everything that cannot be rewritten fails the
// definition with a reason, which the sync records instead of shipping a
// tracker that half-works.
//
// The same Convert runs in two places: the build-time sync that produces the
// embedded snapshot, and the runtime definition update. A definition
// therefore reaches the engine through exactly one set of rules either way.
package convert

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/datahearth/streamline/internal/cardigann"
)

// Version is the converter's output version, recorded in the catalog. Bump
// it whenever a change here alters what Convert produces for the same input,
// so a snapshot converted by an older converter is recognisably stale.
const Version = 1

// Modified is the change notice each converted definition carries in its
// source block (the GPL asks a modified file to say it was changed).
const Modified = "converted by streamline's internal/cardigann/convert: " +
	"YAML to JSON; .NET regexes, substitutions, date layouts and template " +
	"string literals rewritten for Go"

// knownFilters is the schema's filter vocabulary. An unknown name is a filter
// the engine has no implementation for, so it fails the definition here.
var knownFilters = []string{
	"querystring", "timeparse", "dateparse", "regexp", "re_replace", "split",
	"replace", "trim", "prepend", "append", "tolower", "toupper", "urldecode",
	"urlencode", "htmldecode", "htmlencode", "timeago", "reltime", "fuzzytime",
	"validfilename", "diacritics", "jsonjoinarray", "hexdump", "strdump",
	"validate", "andmatch",
}

// secretHints flag a text-typed setting as a secret by name. Upstream types
// cookies, API keys, passkeys, PINs and 2FA codes as plain "text", so the
// type alone would leave every one of them in clear. Over-flagging costs a
// masked input; under-flagging leaks a credential into the config audit.
var secretHints = []string{"pass", "key", "cookie", "token", "2fa", "secret"}

// Convert decodes and rewrites one definition. path is the file's location
// in the upstream repository, recorded in the definition's source block.
func Convert(path string, src []byte) (*cardigann.Definition, error) {
	d, err := cardigann.Decode(src)
	if err != nil {
		return nil, err
	}
	if d.ID == "" {
		return nil, fmt.Errorf("definition has no id")
	}
	if len(d.Links) == 0 {
		return nil, fmt.Errorf("definition has no links")
	}
	c := converter{}
	if err := c.walk(reflect.ValueOf(d).Elem(), ""); err != nil {
		return nil, err
	}
	for i := range d.Settings {
		d.Settings[i].Secret = isSecret(d.Settings[i])
	}
	sum := sha256.Sum256(src)
	d.Source = &cardigann.Source{
		Path:     path,
		SHA256:   hex.EncodeToString(sum[:]),
		Modified: Modified,
	}
	d.RegexNET = c.usesNET
	return d, nil
}

func isSecret(s cardigann.Setting) bool {
	if s.Type == "password" {
		return true
	}
	if s.Type != "text" {
		return false
	}
	name := strings.ToLower(s.Name)
	if name == "pin" {
		return true
	}
	for _, h := range secretHints {
		if strings.Contains(name, h) {
			return true
		}
	}
	return false
}

type converter struct {
	usesNET bool
}

var filterType = reflect.TypeFor[cardigann.Filter]()

// walk visits every string in the definition. Filters are handled whole,
// since which of their arguments is a regex, a layout or a template depends
// on the filter; every other string is a template candidate.
func (c *converter) walk(v reflect.Value, path string) error {
	if v.Type() == filterType {
		f, _ := reflect.TypeAssert[*cardigann.Filter](v.Addr())
		if err := c.filter(f); err != nil {
			return fmt.Errorf("%s: %w", strings.TrimPrefix(path, "."), err)
		}
		return nil
	}
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			return c.walk(v.Elem(), path)
		}
	case reflect.Struct:
		t := v.Type()
		for i := range t.NumField() {
			if !t.Field(i).IsExported() {
				continue
			}
			if err := c.walk(v.Field(i), path+"."+t.Field(i).Name); err != nil {
				return err
			}
		}
	case reflect.Slice:
		for i := range v.Len() {
			if err := c.walk(
				v.Index(i),
				fmt.Sprintf("%s[%d]", path, i),
			); err != nil {
				return err
			}
		}
	case reflect.String:
		out, err := c.template(v.String())
		if err != nil {
			return fmt.Errorf("%s: %w", strings.TrimPrefix(path, "."), err)
		}
		v.SetString(out)
	default:
	}
	return nil
}

func (c *converter) template(s string) (string, error) {
	out, usesNET, err := translateTemplate(s)
	if err != nil {
		return "", err
	}
	c.usesNET = c.usesNET || usesNET
	return out, nil
}

func (c *converter) filter(f *cardigann.Filter) error {
	if !slices.Contains(knownFilters, f.Name) {
		return fmt.Errorf("unknown filter %q", f.Name)
	}
	switch f.Name {
	case "regexp":
		if len(f.Args) < 1 {
			return fmt.Errorf("regexp filter without a pattern")
		}
		pat, engine, err := translateRegex(f.Args[0])
		if err != nil {
			return err
		}
		f.Args[0], f.Engine = pat, engine
	case "re_replace":
		if len(f.Args) < 2 {
			return fmt.Errorf("re_replace filter needs a pattern and a replacement")
		}
		pat, engine, err := translateRegex(f.Args[0])
		if err != nil {
			return err
		}
		rep, err := translateReplacement(f.Args[1], engine)
		if err != nil {
			return err
		}
		// The replacement is itself templated upstream (it may read
		// .Config), the pattern never is.
		if rep, err = c.template(rep); err != nil {
			return err
		}
		f.Args[0], f.Args[1], f.Engine = pat, rep, engine
	case "dateparse", "timeparse":
		// No layout means "guess the format", which the engine does itself.
		if len(f.Args) == 0 || f.Args[0] == "" {
			return nil
		}
		layout, err := translateDateLayout(f.Args[0])
		if err != nil {
			return err
		}
		f.Args[0] = layout
	default:
		for i, a := range f.Args {
			out, err := c.template(a)
			if err != nil {
				return err
			}
			f.Args[i] = out
		}
	}
	if f.Engine == cardigann.RegexEngineNET {
		c.usesNET = true
	}
	return nil
}
