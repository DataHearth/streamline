// Package jobs holds scheduler job constructors that bridge service
// packages to the scheduler without dragging infrastructure deps into the
// services themselves. Each file exports a constructor that returns a
// scheduler.JobFunc ready to register.
package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/scheduler"
)

// SessionPurgeGrace is how long expired session rows are retained past
// expiry before deletion. Keeps a short audit window for post-incident
// forensics without letting the table grow unbounded.
const SessionPurgeGrace = 7 * 24 * time.Hour

// PurgeSessions returns a scheduler JobFunc that deletes expired session
// rows older than SessionPurgeGrace. Intended to run hourly.
func PurgeSessions(p auth.SessionPurger) scheduler.JobFunc {
	return func(ctx context.Context) error {
		cutoff := time.Now().Add(-SessionPurgeGrace)
		n, err := p.PurgeExpiredSessions(ctx, cutoff)
		if err != nil {
			return err
		}
		if n > 0 {
			slog.InfoContext(ctx, "purged expired sessions", "count", n)
		}
		return nil
	}
}
