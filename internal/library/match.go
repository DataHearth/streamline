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

// normalizeTitle lowercases, strips non-alphanumerics, and strips a leading
// article ("the"/"a"/"an") for tolerant title comparison (e.g. "The Batman" vs
// "the.batman" vs "Batman").
func normalizeTitle(s string) string {
	s = strings.ToLower(s)
	s = titleNonAlnum.ReplaceAllString(s, "")
	s = titleArticleHead.ReplaceAllString(s, "")
	return s
}

// TitleMatches reports whether two titles are equal after normalization.
func TitleMatches(a, b string) bool { return normalizeTitle(a) == normalizeTitle(b) }

// MatchEpisode resolves a parsed release to an episode within the show's
// seasons. Anime packs match on absolute number; everything else on
// season+episode number. Returns nil when nothing matches.
func MatchEpisode(
	parsed ParseResult,
	seasons []*ent.Season,
	anime bool,
) *ent.Episode {
	_, ep := MatchEpisodeInSeason(parsed, seasons, anime)
	return ep
}

// MatchEpisodeInSeason is MatchEpisode plus the number of the season the match
// lives in — episodes don't carry it, and callers rendering a destination path
// need it. Returns (0, nil) when nothing matches.
func MatchEpisodeInSeason(
	parsed ParseResult,
	seasons []*ent.Season,
	anime bool,
) (uint16, *ent.Episode) {
	for _, se := range seasons {
		for _, e := range se.Edges.Episodes {
			if anime && parsed.AbsoluteNumber > 0 {
				if e.AbsoluteNumber == parsed.AbsoluteNumber {
					return se.Number, e
				}
				continue
			}
			if se.Number == parsed.Season && e.Number == parsed.Episode {
				return se.Number, e
			}
		}
	}
	return 0, nil
}
