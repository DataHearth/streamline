package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/datahearth/streamline/internal/observability"
)

// slowBootStep is the step duration logged at info rather than debug.
const slowBootStep = time.Second

// bootTimer logs how long each NewFromConfig step takes. mark closes the
// step that ran since the previous mark.
type bootTimer struct {
	start       time.Time
	last        time.Time
	slowest     string
	slowestTook time.Duration
}

func newBootTimer() *bootTimer {
	now := time.Now()
	return &bootTimer{start: now, last: now}
}

func (b *bootTimer) mark(ctx context.Context, step string) {
	now := time.Now()
	took := now.Sub(b.last)
	b.last = now
	if took > b.slowestTook {
		b.slowest, b.slowestTook = step, took
	}
	level := slog.LevelDebug
	if took >= slowBootStep {
		level = slog.LevelInfo
	}
	slog.Log(ctx, level, "boot step finished",
		"step", step, "duration_ms", took.Milliseconds())
}

// bootStep is a database-heavy boot step that runs in the background.
type bootStep struct {
	name string
	run  func(context.Context)
}

// runInBackground runs the steps one after another off the boot path, so a
// cold page cache neither holds back the listener nor takes every read
// connection.
func runInBackground(ctx context.Context, fatal func(error), steps ...bootStep) {
	go func() {
		defer observability.RecoverPanic(ctx, "background boot steps", func() {
			fatal(errors.New("a background boot step panicked"))
		})
		for _, step := range steps {
			if ctx.Err() != nil {
				return
			}
			start := time.Now()
			step.run(ctx)
			slog.InfoContext(ctx, "background boot step finished",
				"step", step.name, "duration_ms", time.Since(start).Milliseconds())
		}
	}()
}

// afterEngineReady starts a background worker once the torrent engine can
// answer for its sessions, since workers read an unanswered lookup as a failure.
func afterEngineReady(
	ctx context.Context,
	what string,
	ready <-chan struct{},
	fatal func(error),
	start func(context.Context),
) {
	go func() {
		defer observability.RecoverPanic(ctx, what, func() {
			fatal(fmt.Errorf("%s panicked", what))
		})
		select {
		case <-ready:
			start(ctx)
		case <-ctx.Done():
		}
	}()
}

// done logs the total and the slowest step.
func (b *bootTimer) done(ctx context.Context) {
	slog.InfoContext(ctx, "application wired",
		"duration_ms", time.Since(b.start).Milliseconds(),
		"slowest_step", b.slowest,
		"slowest_ms", b.slowestTook.Milliseconds(),
	)
}

// closedChan is the readiness of an install with no torrent engine to wait for.
func closedChan() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
