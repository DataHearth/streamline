package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

// RotateJWTSecret generates a new 32-byte HMAC secret, persists it via
// config.Update (atomic YAML write-back), atomically swaps the in-memory
// secret, truncates the sessions table (signals "everyone out"), and
// re-issues a fresh token for the calling admin so they stay signed in.
//
// Returns the new bearer token. Web callers wrap it in a session cookie;
// API callers return it in the response body.
func (s *auth) RotateJWTSecret(
	ctx context.Context,
	callerID uint32,
) (string, error) {
	ctx, span := tracer.Start(ctx, "auth.rotate_jwt",
		trace.WithAttributes(
			semconv.UserID(fmt.Sprint(callerID)),
			attribute.String("auth.method", "local"),
		),
	)
	defer span.End()

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", otelx.RecordSpanError(span, fmt.Errorf("read random: %w", err))
	}
	encoded := base64.StdEncoding.EncodeToString(raw)

	// Persist first. ErrNoPath (dev/tests with no backing file) is a warn,
	// not a hard failure — the new secret still rotates in memory.
	if err := config.Update(ctx, func(c *config.Config) error {
		c.Auth.SessionSecret = encoded
		return nil
	}); err != nil {
		if !errors.Is(err, config.ErrNoPath) {
			return "", otelx.RecordSpanError(
				span,
				fmt.Errorf("persist secret: %w", err),
			)
		}
		slog.WarnContext(ctx, "auth.jwt_rotate_no_backing_file",
			"caller.id", callerID, "error", err)
	}

	newSecret := []byte(encoded)
	s.jwtSecret.Store(&newSecret)

	// Truncate sessions — old tokens are already invalid (signed with old
	// secret). Failure here is benign; cleanup eventually reaps dead rows.
	if err := s.db.TruncateSessions(ctx); err != nil {
		slog.WarnContext(ctx, "auth.jwt_rotate_truncate_failed",
			"user.id", callerID, "error", err)
	}

	caller, err := s.db.FindUserByID(ctx, callerID)
	if err != nil {
		return "", otelx.RecordSpanError(span, fmt.Errorf("reload caller: %w", err))
	}
	tok, err := s.issueToken(ctx, caller, SessionMeta{})
	if err != nil {
		return "", otelx.RecordSpanError(span, err)
	}

	slog.InfoContext(ctx, "auth.jwt_rotated",
		"caller.id", callerID, "caller.email", caller.Email)
	return tok, nil
}
