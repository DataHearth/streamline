package metadata

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

var (
	aliasNonAlnum    = regexp.MustCompile(`[^a-z0-9]+`)
	aliasLatinLetter = regexp.MustCompile(`[a-z]`)
	aliasAccent      = regexp.MustCompile(`([\p{Latin}\p{Greek}\p{Cyrillic}])\p{M}+`)
	aliasNonLetter   = regexp.MustCompile(`[^\p{L}\p{N}\p{M}]+`)
)

// minAliasChars is the shortest folded alias worth storing.
//
// preferTitleMatches does not stamp what it keeps as a title mismatch, so an
// automatic grabber acts on whatever an alias matched. A language rendering a
// film's title as one short common word ("Up", "It") would match most of a
// tracker's catalogue and hand the grabber the wrong film.
const minAliasChars = 4

// foldAlias reduces a title to the characters library.TitlePrefixMatches
// compares on: a title holding a Latin letter to lowercase ASCII
// alphanumerics, accents decomposed away, and one holding none to the letters
// and digits of its own script.
//
// NFD is what makes "Amélie" and "Amelie" the same alias, and what keeps "Léon"
// four characters long instead of three: decomposing splits the accented rune
// into a plain letter plus a combining mark, and the mark drops out with the
// rest of the punctuation. Deliberately not a call into library.normalizeTitle:
// that package pulls in ent, and nothing else in this one needs a database.
// Exact parity with the matcher is not required here: a near-duplicate that
// survives costs one string in a JSON column and one string comparison.
func foldAlias(s string) string {
	s = strings.ToLower(norm.NFD.String(s))
	if aliasLatinLetter.MatchString(s) {
		return aliasNonAlnum.ReplaceAllString(s, "")
	}
	return aliasNonLetter.ReplaceAllString(aliasAccent.ReplaceAllString(s, "$1"), "")
}

// collectAliases returns the candidates naming the same work under a different
// name, in first-seen order. Candidates folding to a primary title, to each
// other, or to fewer than minAliasChars characters are dropped.
func collectAliases(primary, candidates []string) []string {
	seen := make(map[string]struct{}, len(primary)+len(candidates))
	for _, p := range primary {
		if k := foldAlias(p); k != "" {
			seen[k] = struct{}{}
		}
	}
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		c = strings.TrimSpace(c)
		k := foldAlias(c)
		if utf8.RuneCountInString(k) < minAliasChars {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, c)
	}
	return out
}
