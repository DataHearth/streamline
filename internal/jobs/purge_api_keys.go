package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/scheduler"
)

// APIKeyPurgeGrace is how long an expired API key's row is kept past its
// expiry. The key authenticates nothing from the moment it expires; the row
// lingers so the account page can show it as expired rather than have it
// vanish without a trace.
const APIKeyPurgeGrace = 7 * 24 * time.Hour

// PurgeAPIKeys returns a scheduler JobFunc that deletes API keys that expired
// more than APIKeyPurgeGrace ago. Intended to run hourly.
func PurgeAPIKeys(p auth.APIKeyPurger) scheduler.JobFunc {
	return func(ctx context.Context) error {
		cutoff := time.Now().Add(-APIKeyPurgeGrace)
		n, err := p.PurgeExpiredAPIKeys(ctx, cutoff)
		if err != nil {
			return err
		}
		if n > 0 {
			slog.InfoContext(ctx, "purged expired API keys", "count", n)
		}
		return nil
	}
}
