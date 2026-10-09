package convert

import (
	"fmt"
	"strings"
)

// dotnetDateTokens maps a run of one .NET custom format letter onto Go's
// reference-time spelling. A run with no entry (a lone y, t or z, the
// fractional-second and era letters) has no Go equivalent and fails the
// layout rather than parsing dates subtly wrong.
//
// Go's parser reads "15", "3", "4" and "5" as one or two digits, so H, h, m
// and s (.NET's unpadded forms) share their padded form's spelling for
// parsing; only formatting would tell them apart, and definitions only parse.
var dotnetDateTokens = map[string]string{
	"d": "2", "dd": "02", "ddd": "Mon", "dddd": "Monday",
	"M": "1", "MM": "01", "MMM": "Jan", "MMMM": "January",
	"yy": "06", "yyy": "2006", "yyyy": "2006",
	"H": "15", "HH": "15",
	"h": "3", "hh": "03",
	"m": "4", "mm": "04",
	"s": "5", "ss": "05",
	"tt": "PM",
	"zz": "-07", "zzz": "-07:00",
	"K": "Z07:00",
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
// timeparse filters carry) into a Go time layout.
func translateDateLayout(layout string) (string, error) {
	var b strings.Builder
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
		b.WriteString(s)
		return nil
	}
	for i := 0; i < len(layout); {
		c := layout[i]
		switch {
		case c == '\'' || c == '"':
			end := strings.IndexByte(layout[i+1:], c)
			if end < 0 {
				return "", fmt.Errorf("date layout %q: unclosed quote", layout)
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
			tok, ok := dotnetDateTokens[layout[i:j]]
			if !ok {
				return "", fmt.Errorf(
					"date layout %q: %q has no Go equivalent", layout, layout[i:j],
				)
			}
			if err := flushLit(); err != nil {
				return "", err
			}
			b.WriteString(tok)
			i = j
		default:
			lit.WriteByte(c)
			i++
		}
	}
	if err := flushLit(); err != nil {
		return "", err
	}
	return b.String(), nil
}
