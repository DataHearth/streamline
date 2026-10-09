// Package pick holds the pure rules that choose a book's slot editions and
// derive its display strings from them. It imports nothing from the project,
// so both the database layer and the service can share one answer.
package pick

import (
	"cmp"
	"slices"
	"strings"
	"time"
)

// Edition is the part of a stored edition the rules read.
type Edition struct {
	ID         uint32
	Language   string
	Title      string
	Publisher  string
	Format     string
	Original   bool
	Popularity uint32
}

// Slot chooses the edition of format a slot should point at: the one in the
// preferred language, else the original-language one, else the most popular.
// ok is false when the book has no edition of that format.
func Slot(editions []Edition, format, preferredLanguage string) (Edition, bool) {
	of := ofFormat(editions, format)
	if len(of) == 0 {
		return Edition{}, false
	}
	if e, ok := best(
		of,
		func(e Edition) bool { return e.Language == preferredLanguage },
	); ok {
		return e, true
	}
	if e, ok := best(of, func(e Edition) bool { return e.Original }); ok {
		return e, true
	}
	return best(of, func(Edition) bool { return true })
}

// SlotIn is Slot restricted to one (language, publisher). ok is false when the
// book has no such edition.
func SlotIn(
	editions []Edition,
	format, language, publisher string,
) (Edition, bool) {
	return best(ofFormat(editions, format), func(e Edition) bool {
		return e.Language == language && e.Publisher == publisher
	})
}

// Titles derives the display title and the original title. The title is the
// best edition's in the preferred language (ebook first, then audiobook), else
// the original edition's, else fallback. The original title is the original
// edition's title when it differs from the display title, else empty. Neither
// is tied to a slot's edition.
func Titles(
	editions []Edition,
	preferredLanguage, fallback string,
) (title, original string) {
	origin, _ := titleOf(editions, func(e Edition) bool { return e.Original })
	preferred, ok := titleOf(editions, func(e Edition) bool {
		return e.Language == preferredLanguage
	})
	switch {
	case ok:
		title = preferred
	case origin != "":
		title = origin
	default:
		title = fallback
	}
	if origin != "" && origin != title {
		original = origin
	}
	return title, original
}

// titleOf is the title of the most popular matching edition, ebook before
// audiobook.
func titleOf(editions []Edition, keep func(Edition) bool) (string, bool) {
	for _, format := range []string{"ebook", "audiobook"} {
		if e, ok := best(ofFormat(editions, format), keep); ok {
			return e.Title, true
		}
	}
	return "", false
}

// OfFormat lists the editions that fill one format.
func OfFormat(
	editions []Edition,
	format string,
) []Edition {
	return ofFormat(editions, format)
}

func ofFormat(editions []Edition, format string) []Edition {
	out := make([]Edition, 0, len(editions))
	for _, e := range editions {
		if e.Format == format {
			out = append(out, e)
		}
	}
	return out
}

func best(editions []Edition, keep func(Edition) bool) (Edition, bool) {
	var (
		top   Edition
		found bool
	)
	for _, e := range editions {
		if !keep(e) {
			continue
		}
		if !found || e.Popularity > top.Popularity ||
			(e.Popularity == top.Popularity && e.ID < top.ID) {
			top, found = e, true
		}
	}
	return top, found
}

// Credit is a maker with the role it holds.
type Credit struct {
	Name string
	Role string
}

const (
	maxDisplayedAuthors = 3
	etAl                = " et al."
)

// DisplayAuthor renders the makers of a title: the authors, else the writers,
// else the artists, joined with " & " and cut after three with " et al.".
func DisplayAuthor(credits []Credit) string {
	for _, role := range []string{"author", "writer", "artist"} {
		var names []string
		for _, c := range credits {
			if c.Role == role && !slices.Contains(names, c.Name) {
				names = append(names, c.Name)
			}
		}
		if len(names) == 0 {
			continue
		}
		if len(names) > maxDisplayedAuthors {
			return strings.Join(names[:maxDisplayedAuthors], " & ") + etAl
		}
		return strings.Join(names, " & ")
	}
	return ""
}

// ByPopularity orders editions most popular first, lower id breaking a tie.
func ByPopularity(a, b Edition) int {
	return cmp.Or(cmp.Compare(b.Popularity, a.Popularity), cmp.Compare(a.ID, b.ID))
}

// MonitorsVolume says whether a series policy monitors a volume's ebook slot:
// all monitors every volume, future the ones releasing after since (a volume
// with no known date is not one), none no volume.
func MonitorsVolume(policy string, release *time.Time, since time.Time) bool {
	switch policy {
	case "all":
		return true
	case "future":
		return release != nil && release.After(since)
	}
	return false
}
