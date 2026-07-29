package rss

import (
	"context"
	"log/slog"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/library"
)

// QualityConfig drives release filtering. It is resolved per item at search
// time from the movie's or show's quality_profile, so a run picks up profile
// edits and per-item overrides without a restart.
type QualityConfig struct {
	PreferredResolution string
	MinResolution       string
	UpgradeAllowed      bool
}

// qualityFor resolves a quality_profile name into a filter, falling back to
// the configured default when the name is empty or unknown. With no profiles
// configured at all it returns the zero value, which rejects every release —
// grabbing at an unknown quality bar is worse than grabbing nothing.
func qualityFor(ctx context.Context, name string) QualityConfig {
	p, ok := config.ResolveQualityProfile(name)
	if !ok {
		slog.WarnContext(ctx,
			"no quality profile configured, rejecting every release",
			"quality_profile", name,
		)
		return QualityConfig{}
	}
	return QualityConfig{
		PreferredResolution: p.PreferredResolution,
		MinResolution:       p.MinResolution,
		UpgradeAllowed:      p.UpgradeAllowed,
	}
}

// Accepts reports whether a release title meets the quality bar.
// Unparseable titles are rejected (conservative).
func (q QualityConfig) Accepts(releaseTitle string) bool {
	parsed := library.Parse(releaseTitle)
	if parsed.Resolution == "" {
		return false
	}

	got := resolutionRank(parsed.Resolution)
	minR := resolutionRank(q.MinResolution)
	pref := resolutionRank(q.PreferredResolution)

	// Unknown ranks as 0 — rejects vs any valid min (>=1) and vs any valid pref when upgrade disabled.
	if got == 0 || got < minR {
		return false
	}
	if !q.UpgradeAllowed && got != pref {
		return false
	}
	return true
}

// resolutionRank maps a resolution string to a sortable uint8.
// 0 = unknown, 1 = 720p, 2 = 1080p, 3 = 2160p/4K.
func resolutionRank(r string) uint8 {
	switch r {
	case "720p":
		return 1
	case "1080p":
		return 2
	case "2160p", "4K":
		return 3
	default:
		return 0
	}
}
