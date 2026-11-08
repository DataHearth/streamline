package convert

import (
	"fmt"
	"strings"
)

// dotnetDateTokens maps a run of one .NET custom format letter onto the Go
// reference-time spellings that parse what .NET's ParseExact accepts for it.
// A run with no entry (a lone y, t or z, the fractional-second and era
// letters) has no Go equivalent and fails the layout rather than parsing
// dates subtly wrong.
//
// Padding is not a formatting detail when parsing: Go's "02", "01", "03",
// "04" and "05" demand two digits, exactly like .NET's dd, MM, hh, mm and ss,
// while "2", "1", "3", "4" and "5" take one or two, like d, M, h, m and s. Go
// has no padded 24-hour form, so HH shares H's "15" and is the one token
// laxer than .NET.
//
// Where .NET accepts two shapes Go spells differently, the token has several
// spellings and the layout becomes one Go layout per combination: zzz and K
// take the offset with or without its colon (+08:00, +0800), and tt matches
// AM/PM case-insensitively, which Go only does as "PM" or "pm".
var dotnetDateTokens = map[string][]string{
	"d": {"2"}, "dd": {"02"}, "ddd": {"Mon"}, "dddd": {"Monday"},
	"M": {"1"}, "MM": {"01"}, "MMM": {"Jan"}, "MMMM": {"January"},
	"yy": {"06"}, "yyy": {"2006"}, "yyyy": {"2006"},
	"H": {"15"}, "HH": {"15"},
	"h": {"3"}, "hh": {"03"},
	"m": {"4"}, "mm": {"04"},
	"s": {"5"}, "ss": {"05"},
	"tt":  {"PM", "pm"},
	"zz":  {"-07"},
	"zzz": {"-07:00", "-0700"},
	"K":   {"Z07:00", "Z0700"},
}

const dotnetDateLetters = "dfFgGhHKmMstyz"

// goLayoutTokens are the substrings Go's time package treats as layout
// elements. A literal from the .NET layout containing one would be parsed as
// a field, and a Go layout has no escape for it — so such a literal fails the
// layout instead.
var goLayoutTokens = []string{
	"Jan", "Mon", "MST", "PM", "pm", "Z07", "-07", "_2", "002",
	"0", "1", "2", "3", "4", "5", "6", "7", "8", "9",
}

// translateDateLayout turns a .NET custom date format (what dateparse and
// timeparse filters carry) into the Go layouts that together accept what it
// does, most common spelling first.
func translateDateLayout(layout string) ([]string, error) {
	out := []string{""}
	appendAll := func(parts ...string) {
		next := make([]string, 0, len(out)*len(parts))
		for _, o := range out {
			for _, p := range parts {
				next = append(next, o+p)
			}
		}
		out = next
	}
	var lit strings.Builder
	flushLit := func() error {
		s := lit.String()
		lit.Reset()
		for _, tok := range goLayoutTokens {
			if strings.Contains(s, tok) {
				return fmt.Errorf(
					"date layout %q: literal %q reads as Go layout element %q",
					layout, s, tok,
				)
			}
		}
		appendAll(s)
		return nil
	}
	for i := 0; i < len(layout); {
		c := layout[i]
		switch {
		case c == '\'' || c == '"':
			end := strings.IndexByte(layout[i+1:], c)
			if end < 0 {
				return nil, fmt.Errorf("date layout %q: unclosed quote", layout)
			}
			lit.WriteString(layout[i+1 : i+1+end])
			i += end + 2
		case c == '\\' && i+1 < len(layout):
			lit.WriteByte(layout[i+1])
			i += 2
		case c == '%' && i+1 < len(layout):
			// %d is .NET's way to write a one-letter custom format alone.
			i++
		case strings.IndexByte(dotnetDateLetters, c) >= 0:
			j := i
			for j < len(layout) && layout[j] == c {
				j++
			}
			toks, ok := dotnetDateTokens[layout[i:j]]
			if !ok {
				return nil, fmt.Errorf(
					"date layout %q: %q has no Go equivalent", layout, layout[i:j],
				)
			}
			if err := flushLit(); err != nil {
				return nil, err
			}
			appendAll(toks...)
			i = j
		default:
			lit.WriteByte(c)
			i++
		}
	}
	if err := flushLit(); err != nil {
		return nil, err
	}
	return out, nil
}
