package rss

import (
	"cmp"
	"context"
	"log/slog"
	"slices"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/quality"
	"github.com/datahearth/streamline/internal/quality/qualityctx"
)

// qualityFor resolves a quality_profile name into a scored profile, falling
// back to the configured default when the name is empty or unknown. It is
// resolved per item at search time, so a run picks up profile edits and
// per-item overrides without a restart. With no profiles configured at all it
// returns the zero value, whose empty resolution band rejects every release —
// grabbing at an unknown quality bar is worse than grabbing nothing.
func qualityFor(ctx context.Context, name string) quality.Profile {
	p, ok := config.ResolveScoredProfile(name)
	if !ok {
		slog.WarnContext(ctx,
			"no quality profile configured, rejecting every release",
			"quality_profile", name,
		)
		return quality.Profile{}
	}
	return p
}

// singleRelease is the episode count for a release carrying one thing: a
// movie, or one episode. Size bounds are multiplied by the count, so this
// leaves them reading exactly as the operator typed them.
const singleRelease = 1

// releaseEpisodes is how many episodes a release makes us download, which is
// what its size bounds are measured in. A single episode counts one; a pack
// counts the real length of every season it names, looked up per show. The
// release states its scope and never its file count, and no indexer reports
// one either — the number can only come from the library.
//
// Zero from the span means the library cannot say: an untracked season, or a
// name that named no season. That degrades to an unscaled bound rather than a
// tighter one invented from a partial count.
func releaseEpisodes(
	title string,
	showID uint32,
	counts map[uint32]map[uint16]int,
) int {
	if n := library.ParseSeasonSpan(title).EpisodeCount(counts[showID]); n > 0 {
		return n
	}
	return singleRelease
}

// showIDs collects the ids of every show in the given batches, deduplicated —
// the wanted and upgrade sets overlap for a show with both missing and
// on-disk episodes.
func showIDs(batches ...[]*ent.TVShow) []uint32 {
	seen := make(map[uint32]struct{})
	out := make([]uint32, 0, len(batches))
	for _, batch := range batches {
		for _, show := range batch {
			if _, dup := seen[show.ID]; dup {
				continue
			}
			seen[show.ID] = struct{}{}
			out = append(out, show.ID)
		}
	}
	return out
}

// evaluateRelease scores one indexer result against p. episodes is how many
// episodes the release carries and scales the profile's size bounds; 1 for a
// movie or a single episode.
func evaluateRelease(
	p quality.Profile,
	r indexer.SearchResult,
	episodes int,
) quality.Result {
	return quality.Evaluate(
		p,
		qualityctx.ContextFromRelease(r.Title, r.Size, r.Seeders, episodes),
	)
}

// rankAccepted drops the results p rejects and returns the rest best-first:
// highest score, ties broken by seeders. Callers walk the whole slice so a
// grab that fails falls through to the next-best release instead of ending
// the attempt.
func rankAccepted(
	p quality.Profile,
	results []indexer.SearchResult,
	episodes int,
) []indexer.SearchResult {
	type scored struct {
		result indexer.SearchResult
		score  int
	}

	accepted := make([]scored, 0, len(results))
	for _, r := range results {
		res := evaluateRelease(p, r, episodes)
		if res.Rejected {
			continue
		}
		accepted = append(accepted, scored{result: r, score: res.Score})
	}
	slices.SortStableFunc(accepted, func(a, b scored) int {
		if c := cmp.Compare(b.score, a.score); c != 0 {
			return c
		}
		return cmp.Compare(b.result.Seeders, a.result.Seeders)
	})

	ranked := make([]indexer.SearchResult, len(accepted))
	for i, s := range accepted {
		ranked[i] = s.result
	}
	return ranked
}
