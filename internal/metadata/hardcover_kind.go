package metadata

import (
	"regexp"
	"strings"
)

var comicGenreRe = regexp.MustCompile(`(?i)comics|graphic novel|manga|bande dess`)

var mangaGenreRe = regexp.MustCompile(`(?i)manga`)

// ClassifyBookKind guesses a book's kind. Hardcover has no such field, so it
// is a heuristic over genre and tag text plus the original language; a wrong
// guess is corrected with PATCH. Anything that does not look like a comic is a
// novel, and the kind never blocks an add.
func ClassifyBookKind(genres []string, originalLanguage string) string {
	comic, manga := false, false
	for _, g := range genres {
		if comicGenreRe.MatchString(g) {
			comic = true
		}
		if mangaGenreRe.MatchString(g) {
			manga = true
		}
	}
	if !comic {
		return BookKindNovel
	}
	switch lang := strings.ToLower(originalLanguage); {
	case manga || lang == "ja":
		return BookKindManga
	case lang == "fr" || lang == "nl":
		return BookKindBD
	}
	return BookKindComic
}

// roleByContribution maps Hardcover's free-text contribution, lower-cased. A
// null contribution counts as author. Editor, foreword, letterer and the like
// are ignored.
var roleByContribution = map[string]string{
	"":               RoleAuthor,
	"author":         RoleAuthor,
	"writer":         RoleWriter,
	"script":         RoleWriter,
	"scripter":       RoleWriter,
	"illustrator":    RoleArtist,
	"artist":         RoleArtist,
	"penciller":      RoleArtist,
	"penciler":       RoleArtist,
	"inker":          RoleArtist,
	"colorist":       RoleColorist,
	"colourist":      RoleColorist,
	"cover artist":   RoleCover,
	"cover designer": RoleCover,
	"translator":     RoleTranslator,
	"narrator":       RoleNarrator,
	"reader":         RoleNarrator,
}

// RoleForContribution reports the role a Hardcover contribution maps to and
// false for the ones that are ignored.
func RoleForContribution(contribution string) (string, bool) {
	role, ok := roleByContribution[strings.ToLower(strings.TrimSpace(contribution))]
	return role, ok
}

// IsMakerRole reports whether a role belongs on a book or series rather than
// on an edition.
func IsMakerRole(role string) bool {
	switch role {
	case RoleAuthor, RoleWriter, RoleArtist, RoleColorist, RoleCover:
		return true
	}
	return false
}
