package bittorrent

import (
	"context"
	"log/slog"
	"time"
)

const enforceInterval = time.Minute

// enforceSeedLimits ticks once a minute: records completion timestamps and
// stops uploading once the configured ratio or seed-time limit is reached.
func (e *Engine) enforceSeedLimits() {
	tick := time.NewTicker(enforceInterval)
	defer tick.Stop()
	for {
		select {
		case <-e.stop:
			return
		case <-tick.C:
		}
		e.enforceOnce(context.Background())
	}
}

func (e *Engine) enforceOnce(ctx context.Context) {
	for _, t := range e.client.Torrents() {
		if t.Info() == nil || t.BytesMissing() != 0 {
			continue
		}
		hash := t.InfoHash().HexString()
		st := e.getState(hash)
		if st.paused || st.seedStopped {
			continue
		}
		if st.completedAt.IsZero() {
			now := time.Now()
			e.setState(hash, func(s *torrentState) { s.completedAt = now })
			if err := e.store.SetTorrentSessionCompleted(
				ctx,
				hash,
				now,
			); err != nil {
				slog.WarnContext(ctx, "persisting torrent completion failed",
					"info_hash", hash, "error", err)
			}
			st.completedAt = now
		}
		stats := t.Stats()
		r := ratio(stats.BytesWrittenData.Int64(), t.Length())
		if !shouldStopSeeding(
			r, e.seedRatio, st.completedAt, e.seedTime, time.Now(),
		) {
			continue
		}
		t.DisallowDataUpload()
		e.setState(hash, func(s *torrentState) { s.seedStopped = true })
		if err := e.store.SetTorrentSessionSeedStopped(ctx, hash); err != nil {
			slog.WarnContext(ctx, "persisting seed stop failed",
				"info_hash", hash, "error", err)
		}
		slog.InfoContext(ctx, "seed limits reached, stopped seeding",
			"info_hash", hash, "ratio", r)
	}
}

func ratio(uploaded, size int64) float64 {
	if size <= 0 {
		return 0
	}
	return float64(uploaded) / float64(size)
}

// shouldStopSeeding is pure so the limit logic is unit-testable. Zero
// limits mean unlimited; a zero completedAt means completion time is not
// yet known, so the time limit cannot apply.
func shouldStopSeeding(
	currentRatio, maxRatio float64,
	completedAt time.Time,
	maxSeedTime time.Duration,
	now time.Time,
) bool {
	if maxRatio > 0 && currentRatio >= maxRatio {
		return true
	}
	if maxSeedTime > 0 && !completedAt.IsZero() &&
		now.Sub(completedAt) >= maxSeedTime {
		return true
	}
	return false
}
