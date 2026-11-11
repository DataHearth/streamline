package extension

import (
	"strings"

	"modernc.org/sqlite"
)

const (
	coreSchema = "core"
	viewPrefix = "streamline_"
)

// View is one read-only slice of core handed to an extension as the temp view
// streamline_<Name>.
type View struct {
	Name   string   // "books" → temp view streamline_books
	SQL    string   // SELECT … FROM core.<table> …
	Tables []string // core tables it reads; column-less reads are allowed on these only
}

func (v View) viewName() string { return viewPrefix + v.Name }

func newPolicy(views []View) sqlite.AuthorizerFn {
	hostView := make(map[string]bool, len(views))
	baseTable := map[string]bool{}
	for _, v := range views {
		hostView[v.viewName()] = true
		for _, t := range v.Tables {
			baseTable[t] = true
		}
	}

	return func(
		action sqlite.AuthorizerActionCode,
		arg1, arg2, db, via string,
	) sqlite.AuthorizerReturnCode {
		switch action {
		case sqlite.AuthSelect, sqlite.AuthTransaction, sqlite.AuthSavepoint,
			sqlite.AuthRecursive, sqlite.AuthFunction:
			return sqlite.AuthorizerOK
		case sqlite.AuthRead:
			switch db {
			case "main", "":
				return sqlite.AuthorizerOK
			case "temp":
				if hostView[arg1] {
					return sqlite.AuthorizerOK
				}
			case coreSchema:
				if hostView[via] || arg2 == "" && baseTable[arg1] {
					return sqlite.AuthorizerOK
				}
			}
		case sqlite.AuthCreateView, sqlite.AuthCreateTrigger:
			// A view or trigger in main that borrows a host view's name would
			// pass the core-read check above, which keys on that name alone.
			if db == "main" && !strings.HasPrefix(arg1, viewPrefix) {
				return sqlite.AuthorizerOK
			}
		case sqlite.AuthInsert, sqlite.AuthUpdate, sqlite.AuthDelete,
			sqlite.AuthCreateTable, sqlite.AuthDropTable, sqlite.AuthAlterTable,
			sqlite.AuthCreateIndex, sqlite.AuthDropIndex,
			sqlite.AuthDropView, sqlite.AuthDropTrigger:
			if db == "main" {
				return sqlite.AuthorizerOK
			}
		}
		return sqlite.AuthorizerDeny
	}
}
