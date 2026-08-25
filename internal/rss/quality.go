package rss

import (
	"cmp"
	"context"
	"log/slog"
	"slices"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/indexer"
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

// evaluateRelease scores one indexer result against p.
func evaluateRelease(
	p quality.Profile,
	r indexer.SearchResult,
) quality.Result {
	return quality.Evaluate(
		p,
		qualityctx.ContextFromRelease(r.Title, r.Size, r.Seeders),
	)
}

// rankAccepted drops the results p rejects and returns the rest best-first:
// highest score, ties broken by seeders. Callers walk the whole slice so a
// grab that fails falls through to the next-best release instead of ending
// the attempt.
func rankAccepted(
	p quality.Profile,
	results []indexer.SearchResult,
) []indexer.SearchResult {
	type scored struct {
		result indexer.SearchResult
		score  int
	}

	accepted := make([]scored, 0, len(results))
	for _, r := range results {
		res := evaluateRelease(p, r)
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
