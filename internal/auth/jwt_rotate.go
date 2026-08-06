package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

// pendingRotationTTL bounds how long a read-only instance holds a generated
// secret waiting for the operator to confirm they copied it. Long enough to
// paste into a GitOps repo, short enough that an abandoned dialog doesn't
// leave a live secret sitting in memory.
const pendingRotationTTL = 5 * time.Minute

var ErrNoPendingRotation = errors.New("no pending rotation to confirm")

type pendingRotation struct {
	secret    string
	expiresAt time.Time
}

// PrepareJWTRotation generates a secret and parks it for the caller without
// touching anything. Read-only instances need this: config.Update refuses the
// write-back, so the operator has to copy the secret into the file they manage
// before the swap happens — a rotation applied first would leave them locked
// out of the value that now signs their sessions.
func (s *auth) PrepareJWTRotation(
	ctx context.Context,
	callerID uint32,
) (string, error) {
	_, span := tracer.Start(ctx, "auth.prepare_jwt_rotation",
		trace.WithAttributes(semconv.UserID(fmt.Sprint(callerID))),
	)
	defer span.End()

	encoded, err := generateJWTSecret()
	if err != nil {
		return "", otelx.RecordSpanError(span, err)
	}
	s.pendingRotations.Store(callerID, pendingRotation{
		secret:    encoded,
		expiresAt: time.Now().Add(pendingRotationTTL),
	})
	return encoded, nil
}

// ConfirmJWTRotation applies the secret handed out by PrepareJWTRotation. The
// candidate is consumed either way, so a stale or expired one can't be
// replayed.
func (s *auth) ConfirmJWTRotation(
	ctx context.Context,
	callerID uint32,
) (string, error) {
	ctx, span := tracer.Start(ctx, "auth.confirm_jwt_rotation",
		trace.WithAttributes(semconv.UserID(fmt.Sprint(callerID))),
	)
	defer span.End()

	v, ok := s.pendingRotations.LoadAndDelete(callerID)
	if !ok {
		return "", otelx.RecordSpanError(span, ErrNoPendingRotation)
	}
	p := v.(pendingRotation)
	if time.Now().After(p.expiresAt) {
		return "", otelx.RecordSpanError(span, ErrNoPendingRotation)
	}
	return s.applyJWTSecret(ctx, span, callerID, p.secret)
}

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

	encoded, err := generateJWTSecret()
	if err != nil {
		return "", otelx.RecordSpanError(span, err)
	}

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

	return s.applyJWTSecret(ctx, span, callerID, encoded)
}

func generateJWTSecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("read random: %w", err)
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

// applyJWTSecret swaps the signing secret in memory, drops every session, and
// re-issues a token for the caller so they survive their own rotation.
func (s *auth) applyJWTSecret(
	ctx context.Context,
	span trace.Span,
	callerID uint32,
	encoded string,
) (string, error) {
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
