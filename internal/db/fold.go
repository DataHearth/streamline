package db

import (
	"database/sql/driver"
	"strings"
	"unicode"

	entsql "entgo.io/ent/dialect/sql"
	"golang.org/x/text/unicode/norm"
	"modernc.org/sqlite"
)

// foldText lowercases and strips diacritics, mirroring the SPA's fold() in
// web/app/lib/text.ts so a title typed without accents finds one that has
// them ("detective" → "Détective Conan").
func foldText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
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
