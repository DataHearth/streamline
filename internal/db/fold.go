package db

import (
	"database/sql/driver"
	"strings"
	"unicode"

	entsql "entgo.io/ent/dialect/sql"
	"golang.org/x/text/unicode/norm"
	"modernc.org/sqlite"
)

// foldText lowercases, strips diacritics and turns every run of punctuation or
// whitespace into a single space, mirroring the SPA's fold() in
// web/app/lib/text.ts so a title typed without accents or punctuation finds one
// that has them ("detective" → "Détective Conan", "moi quand je me reincarne en
// slime" → "Moi, quand je me réincarne en Slime").
//
// Punctuation becomes a space rather than vanishing: a separator the user typed
// as a space has to line up with one the title spells with a hyphen, so "spider
// man" finds "Spider-Man" while "spiderman" — which is not how either is
// written — does not.
func foldText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		b.WriteRune(' ')
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// SQLite's LIKE folds ASCII case only, so accent folding has to happen inside
// the query. A Go callback per row costs nothing extra here: a leading-wildcard
// LIKE scans the column either way.
func init() {
	sqlite.MustRegisterDeterministicScalarFunction(
		"fold", 1,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			s, _ := args[0].(string)
			return foldText(s), nil
		},
	)
}

// foldContains matches col against q with both sides accent- and case-folded.
// Like ent's own ContainsFold it does not escape LIKE wildcards.
func foldContains(s *entsql.Selector, col, q string) *entsql.Predicate {
	return entsql.ExprP("fold("+s.C(col)+") LIKE ?", "%"+foldText(q)+"%")
}
