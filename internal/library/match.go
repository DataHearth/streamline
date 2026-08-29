package library

import (
	"regexp"
	"strings"

	"github.com/datahearth/streamline/ent"
)

var (
	titleNonAlnum    = regexp.MustCompile(`[^a-z0-9]+`)
	titleArticleHead = regexp.MustCompile(`^(the|a|an)`)
	// TVDB disambiguates same-named shows inside the title string itself
	// ("Foundation (2021)"), so the year has to come off before comparing or
	// the disambiguated entry never equals the folder it belongs to.
	titleYearSuffix = regexp.MustCompile(`\s*\((?:19|20)\d{2}\)\s*$`)
)

// normalizeTitle lowercases, strips non-alphanumerics, and strips a leading
// article ("the"/"a"/"an") for tolerant title comparison (e.g. "The Batman" vs
// "the.batman" vs "Batman").
func normalizeTitle(s string) string {
	s = titleYearSuffix.ReplaceAllString(s, "")
	s = strings.ToLower(s)
	s = titleNonAlnum.ReplaceAllString(s, "")
	s = titleArticleHead.ReplaceAllString(s, "")
	return s
}

// TitleMatches reports whether two titles are equal after normalization.
func TitleMatches(a, b string) bool { return normalizeTitle(a) == normalizeTitle(b) }

// TitleMatchesAny reports whether title equals name or any of alts after
// normalization. alts are alternate names for the same work (TVDB aliases and
// translations), which is the only way a romaji folder name reaches an entry
// TVDB stores under its English title.
func TitleMatchesAny(title, name string, alts []string) bool {
	if TitleMatches(title, name) {
		return true
	}
	for _, a := range alts {
		if TitleMatches(title, a) {
			return true
		}
	}
	return false
}

// TitlePrefixMatches reports whether one normalized title starts with the
// other, both being non-empty. It is a weaker signal than TitleMatches and is
// only used to rank candidates, never to confirm one.
func TitlePrefixMatches(a, b string) bool {
	na, nb := normalizeTitle(a), normalizeTitle(b)
	if na == "" || nb == "" {
		return false
	}
	return strings.HasPrefix(na, nb) || strings.HasPrefix(nb, na)
}

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
	// An anime group may write the absolute number in SxxExx form: TLC and
	// Tsundere-Raws both publish "The.Elusive.Samurai.S02E15" for the episode
	// TVDB numbers S02E03 (absolute 15), listing it on the indexer as S02E03
	// and naming the file inside the torrent S02E15. The parser reads a real
	// season and episode there, so AbsoluteNumber is 0 and the loop above never
	// consults it — a grab whose keep-set matched nothing then fails with
	// ErrNoWantedFiles before any client is contacted, and the episode is never
	// filled no matter how many acceptable releases exist.
	//
	// Only reached once the exact season+episode found nothing, so a season
	// that genuinely holds that episode still wins.
	if anime && parsed.AbsoluteNumber == 0 && parsed.Episode > 0 {
		for _, se := range seasons {
			for _, e := range se.Edges.Episodes {
				if e.AbsoluteNumber == parsed.Episode {
					return se.Number, e
				}
			}
		}
	}
	return 0, nil
}
