// Package searchwindow holds the throttles shared by the backlog searches of
// the music and book verticals. It mirrors the unexported window logic in
// internal/rss/missing.go so those packages need not import rss.
package searchwindow

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/scheduler"
)

// Window holds the library knobs that gate which rows a search pass may
// touch. Read fresh at the start of every pass: config is hot-editable.
type Window struct {
	MaxGrabFailures  uint8
	NotSearchedSince time.Time
}

// Current returns the window for this pass. A manual run waives both
// throttles: a cap no counter reaches and a cutoff every past search predates.
func Current(ctx context.Context) (Window, error) {
	if scheduler.Manual(ctx) {
		return Window{
			MaxGrabFailures:  math.MaxUint8,
			NotSearchedSince: time.Now(),
		}, nil
	}
	c := config.Get()
	cooldown, err := time.ParseDuration(c.Library.NoMatchCooldown)
	if err != nil {
		return Window{}, fmt.Errorf("parse library.no_match_cooldown: %w", err)
	}
	return Window{
		MaxGrabFailures:  c.Library.MaxGrabFailures,
		NotSearchedSince: time.Now().Add(-cooldown),
	}, nil
}

// TransportFailure reports whether err is the indexer or the download client
// being unreachable rather than anything about the release. grab_failures is
// a strike count against a release choice and only a successful grab resets
// it, so counting a timeout would retire an item after a few flaky ticks.
func TransportFailure(err error) bool {
	return errors.Is(err, download.ErrUnreachable) ||
		errors.Is(err, indexer.ErrUnreachable)
}
