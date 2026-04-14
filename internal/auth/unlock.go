package auth

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

// UnlockMode tags the operator surface that triggered an unlock event.
// Surfaces in the auth.unlocked slog event so audit logs can distinguish
// admin-modal clicks from CLI invocations.
type UnlockMode string

const (
	UnlockModeAdmin UnlockMode = "admin"
	UnlockModeCLI   UnlockMode = "cli"
)

// Unlock clears every lockout field on the user row matching email. Used by
// the CLI subcommand. Returns ErrUserNotFound when the email has no matching
// user.
func (s *auth) Unlock(
	ctx context.Context,
	email string,
	mode UnlockMode,
) error {
	ctx, span := tracer.Start(ctx, "auth.unlock",
		trace.WithAttributes(
			semconv.UserEmail(email),
			attribute.String("auth.unlock.mode", string(mode)),
		),
	)
	defer span.End()

	u, err := s.db.FindUserByEmail(ctx, email)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrUserNotFound
		}
		return otelx.RecordSpanError(span, fmt.Errorf("lookup user: %w", err))
	}
	return s.unlockByID(ctx, u.ID, mode)
}

// AdminUnlock clears lockout fields by user id, for the admin REST handler.
func (s *auth) AdminUnlock(ctx context.Context, id uint32) error {
	ctx, span := tracer.Start(ctx, "auth.unlock_admin",
		trace.WithAttributes(
			semconv.UserID(fmt.Sprint(id)),
			attribute.String("auth.unlock.mode", string(UnlockModeAdmin)),
		),
	)
	defer span.End()
	return s.unlockByID(ctx, id, UnlockModeAdmin)
}

func (s *auth) unlockByID(
	ctx context.Context,
	id uint32,
	mode UnlockMode,
) error {
	zero := uint8(0)
	if _, err := s.db.UpdateUser(ctx, id, db.UpdateUserParams{
		FailedLoginCount:       &zero,
		ClearLastFailedLoginAt: true,
		ClearLockedUntil:       true,
	}); err != nil {
		if ent.IsNotFound(err) {
			return ErrUserNotFound
		}
		return fmt.Errorf("clear lockout: %w", err)
	}
	slog.InfoContext(ctx, "auth.unlocked",
		"user.id", id, "mode", string(mode))
	return nil
}
