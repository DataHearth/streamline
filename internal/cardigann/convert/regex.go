package convert

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/dlclark/regexp2/v2"

	"github.com/datahearth/streamline/internal/cardigann"
)

// dotnetBlocks maps the .NET Unicode block names upstream patterns use onto
// Go's script tables. A block is a code-point range and a script is a set of
// assigned characters, so the Go side is a slight superset (Cyrillic covers
// the supplement blocks too); every use upstream is "strip or keep this
// alphabet", where the superset is what was meant.
var dotnetBlocks = map[string]string{
	"IsCyrillic":             "Cyrillic",
	"IsCJKUnifiedIdeographs": "Han",
	"IsArabic":               "Arabic",
	"IsGreek":                "Greek",
	"IsHebrew":               "Hebrew",
	"IsThai":                 "Thai",
	"IsHiragana":             "Hiragana",
	"IsKatakana":             "Katakana",
	"IsHangulSyllables":      "Hangul",
}

// translateRegex rewrites a .NET pattern for Go's regexp where the difference
// is only spelling, and reports which engine the result needs.
//
// Spelling differences, rewritten: .NET block names (\p{IsCyrillic}), \uXXXX
// escapes, and a backslash before a non-ASCII character (.NET reads it as the
// literal; Go rejects it). Semantic differences — lookarounds and
// backreferences — have no RE2 equivalent, so those patterns keep their
// original text and are tagged RegexEngineNET. A pattern neither engine
// compiles fails the definition.
func translateRegex(pat string) (string, string, error) {
	rewritten := rewriteRegex(pat)
	if _, err := regexp.Compile(rewritten); err == nil {
		return rewritten, "", nil
	}
	if _, err := regexp2.Compile(pat, regexp2.None); err != nil {
		return "", "", fmt.Errorf("regex %q: %w", pat, err)
	}
	return pat, cardigann.RegexEngineNET, nil
}

func rewriteRegex(pat string) string {
	var b strings.Builder
	for i := 0; i < len(pat); {
		if pat[i] != '\\' || i+1 >= len(pat) {
			b.WriteByte(pat[i])
			i++
			continue
		}
		next := pat[i+1]
		switch {
		case (next == 'p' || next == 'P') && strings.HasPrefix(pat[i+2:], "{"):
			end := strings.IndexByte(pat[i+2:], '}')
			if end < 0 {
				b.WriteString(pat[i:])
				return b.String()
			}
			name := pat[i+3 : i+2+end]
			if goName, ok := dotnetBlocks[name]; ok {
				name = goName
			}
			fmt.Fprintf(&b, `\%c{%s}`, next, name)
			i += 3 + end
		case next == 'u' && i+6 <= len(pat) && isHex(pat[i+2:i+6]):
			fmt.Fprintf(&b, `\x{%s}`, pat[i+2:i+6])
			i += 6
		case next >= utf8.RuneSelf:
			// .NET: an escaped non-word character is itself. Go only grants
			// that to ASCII punctuation, so drop the backslash and keep the
			// rune — re-quoted, in case it is special.
			r, size := utf8.DecodeRuneInString(pat[i+1:])
			b.WriteString(regexp.QuoteMeta(string(r)))
			i += 1 + size
		default:
			b.WriteString(pat[i : i+2])
			i += 2
		}
	}
	return b.String()
}

func isHex(s string) bool {
	for i := range len(s) {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// translateReplacement rewrites a .NET substitution string for the engine
// its pattern was assigned. regexp2 speaks .NET's syntax natively, so only an
// RE2 pattern needs it changed — and there it matters: Go reads $1a as the
// group named "1a", not group 1 then "a", so every numbered reference is
// braced. $& and $0 are both the whole match.
func translateReplacement(rep, engine string) (string, error) {
	if engine == cardigann.RegexEngineNET {
		return rep, nil
	}
	var b strings.Builder
	for i := 0; i < len(rep); i++ {
		if rep[i] != '$' || i+1 >= len(rep) {
			b.WriteByte(rep[i])
			continue
		}
		next := rep[i+1]
		switch {
		case next == '$':
			b.WriteString("$$")
			i++
		case next == '&':
			b.WriteString("${0}")
			i++
		case next >= '0' && next <= '9':
			j := i + 1
			for j < len(rep) && rep[j] >= '0' && rep[j] <= '9' {
				j++
			}
			fmt.Fprintf(&b, "${%s}", rep[i+1:j])
			i = j - 1
		case next == '{':
			end := strings.IndexByte(rep[i:], '}')
			if end < 0 {
				return "", fmt.Errorf("replacement %q: unclosed ${", rep)
			}
			b.WriteString(rep[i : i+end+1])
			i += end
		case next == '`' || next == '\'' || next == '+' || next == '_':
			return "", fmt.Errorf(
				"replacement %q: .NET substitution $%c has no Go equivalent",
				rep, next,
			)
		default:
			// .NET writes a lone $ literally; so does Go, but only by
			// accident of a failed parse — say it explicitly.
			b.WriteString("$$")
		}
	}
	return b.String(), nil
}
