package bulkimport

import (
	"sort"

	"github.com/datahearth/streamline/internal/library"
)

// Match-score weights. Only their order matters: an exact title beats a year
// agreement, which beats a prefix, which beats the provider's own relevance.
const (
	scoreTitleExact = 4
	scoreYearAgrees = 2
	scoreTitlePrefx = 1
)

// matchScore rates one provider result against the name parsed off disk. alts
// are alternate names for the same work (TVDB aliases/translations); pass nil
// where the provider has none.
func matchScore(
	wantTitle string,
	wantYear uint16,
	gotTitle string,
	gotAlts []string,
	gotYear uint16,
) int {
	var score int
	switch {
	case library.TitleMatchesAny(wantTitle, gotTitle, gotAlts):
		score += scoreTitleExact
	case library.TitlePrefixMatches(wantTitle, gotTitle):
		score += scoreTitlePrefx
	}
	if wantYear != 0 && gotYear == wantYear {
		score += scoreYearAgrees
	}
	return score
}

// rankByScore sorts hits by descending score, keeping the provider's order
// among equals. It exists because both classifiers truncate to a handful of
// candidates for review, and truncating a provider's own ranking first throws
// away the entry the scoring is meant to find — TVDB returns "Dororo" fifteenth
// for the query "Dororo".
func rankByScore[T any](hits []T, score func(T) int) []T {
	ranked := make([]T, len(hits))
	copy(ranked, hits)
	scores := make(map[int]int, len(ranked))
	for i, h := range ranked {
		scores[i] = score(h)
	}
	idx := make([]int, len(ranked))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		return scores[idx[a]] > scores[idx[b]]
	})
	out := make([]T, 0, len(ranked))
	for _, i := range idx {
		out = append(out, ranked[i])
	}
	return out
}
