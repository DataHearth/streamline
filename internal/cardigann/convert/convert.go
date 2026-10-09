// Package convert turns an upstream Cardigann definition (Prowlarr/Indexers,
// itself synced from Jackett) into the form streamline embeds and runs.
//
// Upstream definitions are written for a C# engine: .NET regexes, .NET date
// layouts, and a template dialect that is Go's text/template in syntax but
// not in string-literal quoting. What is only spelled differently is
// rewritten here, once. A regex RE2 cannot express with .NET's meaning is
// tagged for regexp2 instead of rewritten; a date layout becomes every Go
// layout the .NET one accepts. What neither covers fails the definition with
// a reason, which the sync records instead of shipping a tracker that
// half-works. The few .NET behaviours left to the engine are listed in
// docs/agents/cardigann.md.
//
// The same Convert runs in two places: the build-time sync that produces the
// embedded snapshot, and the runtime definition update. A definition
// therefore reaches the engine through exactly one set of rules either way.
package convert

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"

	"github.com/datahearth/streamline/internal/cardigann"
)

// Version is the converter's output version, recorded in the catalog. Bump
// it whenever a change here alters what Convert produces for the same input,
// so a snapshot converted by an older converter is recognisably stale.
const Version = 4

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
	// JSON has no spelling for either, so one such value would fail the
	// whole snapshot's encoding rather than this definition.
	if math.IsInf(d.RequestDelay, 0) || math.IsNaN(d.RequestDelay) {
		return nil, fmt.Errorf(
			"requestDelay %v is not a finite number",
			d.RequestDelay,
		)
	}
	// Both upstream engines default a login block without a method to form.
	if d.Login != nil && d.Login.Method == "" {
		d.Login.Method = "form"
	}
	c := converter{}
	if err := c.walk(reflect.ValueOf(d).Elem()); err != nil {
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

// walk rewrites every filter and template string in the definition in place.
func (c *converter) walk(v reflect.Value) error {
	return visit(v, c.filter, func(s reflect.Value) error {
		out, err := c.template(s.String())
		if err != nil {
			return err
		}
		s.SetString(out)
		return nil
	})
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
		pat, rep, engine, err := translatePair(f.Args[0], f.Args[1])
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
		// Args becomes every Go layout the .NET one accepts, tried in order.
		layouts, err := translateDateLayout(f.Args[0])
		if err != nil {
			return err
		}
		f.Args = layouts
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
