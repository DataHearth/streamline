package convert

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/dlclark/regexp2/v2"

	"github.com/datahearth/streamline/internal/cardigann"
)

// dotnetBlocks are the .NET named Unicode blocks, as the code-point ranges
// .NET defines them (RegexCharClass's block table). They are rewritten to the
// range itself for both engines: Go has no block table at all, and regexp2's
// \p{} only knows scripts and categories — and a script is not a block (Go's
// Cyrillic script misses U+0485–0486, which the block covers).
var dotnetBlocks = map[string][2]rune{
	"IsBasicLatin":           {0x0000, 0x007F},
	"IsLatin-1Supplement":    {0x0080, 0x00FF},
	"IsGreek":                {0x0370, 0x03FF},
	"IsGreekandCoptic":       {0x0370, 0x03FF},
	"IsCyrillic":             {0x0400, 0x04FF},
	"IsHebrew":               {0x0590, 0x05FF},
	"IsArabic":               {0x0600, 0x06FF},
	"IsThai":                 {0x0E00, 0x0E7F},
	"IsHiragana":             {0x3040, 0x309F},
	"IsKatakana":             {0x30A0, 0x30FF},
	"IsCJKUnifiedIdeographs": {0x4E00, 0x9FFF},
	"IsHangulSyllables":      {0xAC00, 0xD7AF},
}

// .NET's shorthand classes are Unicode-aware (upstream compiles every pattern
// with RegexOptions.None); Go's are ASCII-only. Spelled out as Unicode
// properties they mean the same thing on both engines: \w is letters,
// non-spacing marks, decimal digits and connector punctuation, \s is
// char.IsWhiteSpace, \d is any decimal digit.
const (
	netWord  = `\p{L}\p{Mn}\p{Nd}\p{Pc}`
	netSpace = `\t-\r\x{85}\p{Z}`
)

// translateRegex rewrites a .NET pattern for Go's regexp where the difference
// is spelling, and reports which engine the result needs.
//
// Spelling, rewritten for RE2: block names, \uXXXX escapes, a backslash before
// a non-ASCII rune (.NET reads it as the rune; Go rejects it), and the
// shorthand classes \d \w \s and their negations, spelled as the Unicode sets
// .NET means by them. What RE2 cannot say goes to regexp2 instead:
// lookarounds, backreferences, \b and \B (Go's word boundary is ASCII-only,
// and a boundary beside a Cyrillic letter never matches), $ (.NET's also
// matches before a final newline), a negated shorthand inside a character
// class, class subtraction. A pattern neither engine compiles fails the
// definition.
func translateRegex(pat string) (string, string, error) {
	if out, ok := rewriteRE2(pat); ok {
		if _, err := regexp.Compile(out); err == nil {
			return out, "", nil
		}
	}
	net := rewriteNET(pat)
	if _, err := regexp2.Compile(net, regexp2.None); err != nil {
		return "", "", fmt.Errorf("regex %q: %w", pat, err)
	}
	return net, cardigann.RegexEngineNET, nil
}

// translatePair translates a re_replace's pattern and replacement together,
// since which engine the pattern lands on decides how the replacement is
// spelled — and a replacement using a .NET-only substitution sends an
// otherwise RE2-safe pattern to regexp2.
func translatePair(pat, rep string) (string, string, string, error) {
	out, engine, err := translateRegex(pat)
	if err != nil {
		return "", "", "", err
	}
	if engine == "" {
		if goRep, ok := expandRE2(rep, regexp.MustCompile(out)); ok {
			return out, goRep, "", nil
		}
		out = rewriteNET(pat)
		if _, err := regexp2.Compile(out, regexp2.None); err != nil {
			return "", "", "", fmt.Errorf("regex %q: %w", pat, err)
		}
	}
	// regexp2 speaks .NET substitution syntax natively.
	return out, rep, cardigann.RegexEngineNET, nil
}

// rewriteRE2 spells a .NET pattern for RE2. ok is false when the pattern uses
// something RE2 cannot express with .NET's meaning.
func rewriteRE2(pat string) (string, bool) {
	var b strings.Builder
	inClass := false
	for i := 0; i < len(pat); {
		c := pat[i]
		switch {
		case c == '\\' && i+1 < len(pat):
			n, ok := rewriteEscapeRE2(&b, pat[i:], inClass)
			if !ok {
				return "", false
			}
			i += n
			continue
		case !inClass && c == '[':
			inClass = true
			b.WriteByte(c)
			i++
			if i < len(pat) && pat[i] == '^' {
				b.WriteByte('^')
				i++
			}
			if i < len(pat) && pat[i] == ']' { // a leading ] is literal
				b.WriteString(`\]`)
				i++
			}
			continue
		case inClass && c == '-' && i+1 < len(pat) && pat[i+1] == '[':
			return "", false // .NET class subtraction
		case !inClass && c == '$':
			// .NET's $ also matches just before a final newline, which RE2
			// can only say with a lookahead it does not have.
			return "", false
		case inClass && c == '[':
			b.WriteString(`\[`) // literal in .NET; Go would read [: as POSIX
		case inClass && c == ']':
			inClass = false
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
		i++
	}
	return b.String(), true
}

// rewriteEscapeRE2 writes the RE2 spelling of the escape at the start of s
// and returns how many bytes it consumed.
func rewriteEscapeRE2(b *strings.Builder, s string, inClass bool) (int, bool) {
	next := s[1]
	set := func(body string, negated bool) {
		switch {
		case inClass:
			b.WriteString(body)
		case negated:
			b.WriteString("[^" + body + "]")
		default:
			b.WriteString("[" + body + "]")
		}
	}
	switch {
	case next == 'p' || next == 'P':
		name, n, ok := propertyName(s)
		if !ok {
			return 0, false
		}
		r, isBlock := dotnetBlocks[name]
		switch {
		case !isBlock:
			b.WriteString(s[:n])
		case next == 'P' && inClass:
			return 0, false
		default:
			set(fmt.Sprintf(`\x{%04X}-\x{%04X}`, r[0], r[1]), next == 'P')
		}
		return n, true
	case next == 'u' && len(s) >= 6 && isHex(s[2:6]):
		fmt.Fprintf(b, `\x{%s}`, s[2:6])
		return 6, true
	case next == 'd':
		b.WriteString(`\p{Nd}`)
	case next == 'D':
		b.WriteString(`\P{Nd}`)
	case next == 'w':
		set(netWord, false)
	case next == 's':
		set(netSpace, false)
	case next == 'W' || next == 'S':
		if inClass {
			return 0, false // a negated set has no place inside a class
		}
		set(map[byte]string{'W': netWord, 'S': netSpace}[next], true)
	case next == 'b' || next == 'B':
		return 0, false
	case next >= utf8.RuneSelf:
		// .NET: an escaped non-word character is itself. Go grants that only
		// to ASCII punctuation, and no non-ASCII rune is special to RE2.
		_, size := utf8.DecodeRuneInString(s[1:])
		b.WriteString(s[1 : 1+size])
		return 1 + size, true
	default:
		b.WriteString(s[:2])
	}
	return 2, true
}

// rewriteNET only replaces block names, the one .NET feature regexp2 lacks;
// everything else is regexp2's native dialect.
func rewriteNET(pat string) string {
	var b strings.Builder
	inClass := false
	for i := 0; i < len(pat); {
		c := pat[i]
		if c == '\\' && i+1 < len(pat) {
			name, n, ok := propertyName(pat[i:])
			r, isBlock := dotnetBlocks[name]
			if !ok || !isBlock || (pat[i+1] == 'P' && inClass) {
				// Not a block, or a negated block inside a class, which
				// regexp2 then rejects and the definition fails on.
				width := 2
				if ok {
					width = n
				}
				b.WriteString(pat[i : i+width])
				i += width
				continue
			}
			rng := fmt.Sprintf(`\u%04X-\u%04X`, r[0], r[1])
			switch {
			case inClass:
				b.WriteString(rng)
			case pat[i+1] == 'P':
				b.WriteString("[^" + rng + "]")
			default:
				b.WriteString("[" + rng + "]")
			}
			i += n
			continue
		}
		switch {
		case !inClass && c == '[':
			inClass = true
			b.WriteByte(c)
			i++
			if i < len(pat) && pat[i] == '^' {
				b.WriteByte('^')
				i++
			}
			if i < len(pat) && pat[i] == ']' {
				b.WriteByte(']')
				i++
			}
			continue
		case inClass && c == ']':
			inClass = false
		default:
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

// propertyName reads a \p{Name} or \P{Name} at the start of s.
func propertyName(s string) (string, int, bool) {
	if len(s) < 3 || (s[1] != 'p' && s[1] != 'P') || s[2] != '{' {
		return "", 0, false
	}
	end := strings.IndexByte(s, '}')
	if end < 0 {
		return "", 0, false
	}
	return s[3:end], end + 1, true
}

func isHex(s string) bool {
	_, err := strconv.ParseUint(s, 16, 16)
	return err == nil
}

// expandRE2 rewrites a .NET substitution string for Regexp.Expand against the
// compiled RE2 pattern. ok is false when it uses a substitution only .NET has
// ($` $' $+ $_), which sends the pattern to regexp2.
//
// Go reads $1a as the group named "1a", so every numbered reference is
// braced. A reference to a group the pattern does not have is literal text in
// .NET and an empty string in Go, so such references are written out as
// literals here. $& is the whole match.
func expandRE2(rep string, re *regexp.Regexp) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(rep); i++ {
		if rep[i] != '$' || i+1 >= len(rep) {
			if rep[i] == '$' {
				b.WriteString("$$")
			} else {
				b.WriteByte(rep[i])
			}
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
			ref(&b, rep[i+1:j], rep[i:j], re)
			i = j - 1
		case next == '{':
			end := strings.IndexByte(rep[i:], '}')
			if end < 0 {
				b.WriteString("$$")
				continue
			}
			ref(&b, rep[i+2:i+end], rep[i:i+end+1], re)
			i += end
		case next == '`' || next == '\'' || next == '+' || next == '_':
			return "", false
		default:
			b.WriteString("$$")
		}
	}
	return b.String(), true
}

// ref writes a group reference that exists as ${name}, and one that does not
// as the literal text .NET would leave in place.
func ref(b *strings.Builder, name, literal string, re *regexp.Regexp) {
	exists := re.SubexpIndex(name) >= 0
	if n, err := strconv.Atoi(name); err == nil {
		exists = n <= re.NumSubexp()
	}
	if exists {
		b.WriteString("${" + name + "}")
		return
	}
	b.WriteString("$" + literal)
}
