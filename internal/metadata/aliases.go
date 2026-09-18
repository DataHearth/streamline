package metadata

import (
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

var aliasNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// minAliasChars is the shortest folded alias worth storing.
//
// preferTitleMatches does not stamp what it keeps as a title mismatch, so an
// automatic grabber acts on whatever an alias matched. A language rendering a
// film's title as one short common word ("Up", "It") would match most of a
// tracker's catalogue and hand the grabber the wrong film.
const minAliasChars = 4

// foldAlias reduces a title to the characters library.TitlePrefixMatches
// actually compares on: lowercase ASCII alphanumerics, accents decomposed away.
//
// NFD is what makes "Amélie" and "Amelie" the same alias, and what keeps "Léon"
// four characters long instead of three — decomposing splits the accented rune
// into a plain letter plus a combining mark, and the mark drops out with the
// rest of the punctuation. Deliberately not a call into library.normalizeTitle:
// that package pulls in ent, and nothing else in this one needs a database.
// Exact parity with the matcher is not required here — a near-duplicate that
// survives costs one string in a JSON column and one string comparison.
func foldAlias(s string) string {
	return aliasNonAlnum.ReplaceAllString(strings.ToLower(norm.NFD.String(s)), "")
}

// collectAliases returns the candidates naming the same work under a different
// name, in first-seen order. Candidates folding to a primary title, to each
// other, or to fewer than minAliasChars characters are dropped — as are those
// with no ASCII alphanumerics at all, which fold to the empty string and which
// the matcher could therefore never match against anything.
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
		if len(k) < minAliasChars {
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
