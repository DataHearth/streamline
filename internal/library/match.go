package library

import (
	"regexp"
	"strings"

	"github.com/datahearth/streamline/ent"
)

var (
	titleNonAlnum    = regexp.MustCompile(`[^a-z0-9]+`)
	titleArticleHead = regexp.MustCompile(`^(the|a|an)`)
)

// NormalizeTitle lowercases, strips non-alphanumerics, and strips a leading
// article ("the"/"a"/"an") for tolerant title comparison (e.g. "The Batman" vs
// "the.batman" vs "Batman").
func NormalizeTitle(s string) string {
	s = strings.ToLower(s)
	s = titleNonAlnum.ReplaceAllString(s, "")
	s = titleArticleHead.ReplaceAllString(s, "")
	return s
}

// TitleMatches reports whether two titles are equal after normalization.
func TitleMatches(a, b string) bool { return NormalizeTitle(a) == NormalizeTitle(b) }

// MatchEpisode resolves a parsed release to an episode within the show's
// seasons. Anime packs match on absolute number; everything else on
// season+episode number. Returns nil when nothing matches.
func MatchEpisode(
	parsed ParseResult,
	seasons []*ent.Season,
	anime bool,
) *ent.Episode {
	for _, se := range seasons {
		for _, e := range se.Edges.Episodes {
			if anime && parsed.AbsoluteNumber > 0 {
				if e.AbsoluteNumber == parsed.AbsoluteNumber {
					return e
				}
				continue
			}
			if se.Number == parsed.Season && e.Number == parsed.Episode {
				return e
			}
		}
	}
	return nil
}
