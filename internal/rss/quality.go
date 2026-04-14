package rss

import (
	"time"

	"github.com/datahearth/streamline/internal/library"
)

// QualityConfig drives result filtering in MissingSearcher.
// Built from config.QualityConfig (string fields) or from a per-movie
// DB QualityProfile.
type QualityConfig struct {
	PreferredResolution string
	MinResolution       string
	UpgradeAllowed      bool
	NoMatchCooldown     time.Duration
	MaxGrabFailures     uint8
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
