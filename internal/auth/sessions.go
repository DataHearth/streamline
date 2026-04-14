package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

// Session-related sentinel errors. Middleware translates these to 401/302.
var (
	// ErrSessionRevoked is returned when a session row has a non-nil revoked_at.
	ErrSessionRevoked = errors.New("session revoked")

	// ErrSessionExpired is returned when a session row's expires_at is in the past.
	// Middleware usually catches this via JWT expiration first; this covers the
	// edge case of a clock-skewed token or a session revoked-by-expiry.
	ErrSessionExpired = errors.New("session expired")

	// ErrSessionNotFound is returned when no row matches the jti.
	ErrSessionNotFound = errors.New("session not found")
)

// SessionMeta carries per-request metadata recorded on session creation.
// Callers (HTTP handlers) populate this from the incoming request; service
// methods store the values on the Session row for the user-facing session
// list and for audit.
type SessionMeta struct {
	// IP is the originating client IP. Handlers should use the proxy-aware
	// client IP (resolved by the chi ClientIPFrom* middleware), not RemoteAddr.
	IP string

	// UserAgent is the raw User-Agent header, truncated to 512 chars at the
	// storage layer (schema MaxLen). Self-reported, so trustworthy only as
	// a UX signal — never for authorization.
	UserAgent string
}

// maxUserAgentLen mirrors the schema MaxLen so we truncate cleanly in code
// rather than letting the DB driver reject the insert.
const maxUserAgentLen = 512

// CreateSession inserts a session row that corresponds to a freshly issued
// JWT. Caller must ensure jti is unique (generated via crypto/rand). The row
// must exist before the cookie/token is returned to the client, otherwise the
// first authenticated request racing the insert will 401.
func (s *auth) CreateSession(
	ctx context.Context,
	userID uint32,
	jti string,
	ttl time.Duration,
	meta SessionMeta,
) (*ent.Session, error) {
	ctx, span := tracer.Start(ctx, "auth.create_session",
		trace.WithAttributes(semconv.UserID(fmt.Sprint(userID))),
	)
	defer span.End()

	ua := meta.UserAgent
	if len(ua) > maxUserAgentLen {
		ua = ua[:maxUserAgentLen]
	}

	row, err := s.db.CreateSession(ctx, db.CreateSessionParams{
		JTI:       jti,
		UserID:    userID,
		IP:        meta.IP,
		UserAgent: ua,
		ExpiresAt: time.Now().Add(ttl),
	})
	if err != nil {
		return nil, otelx.RecordSpanError(
			span,
			fmt.Errorf("create session: %w", err),
		)
	}
	return row, nil
}

// ValidateSession returns nil if the session exists and is neither revoked
// nor expired. Middleware calls this on every authenticated request (except
// API-key paths).
func (s *auth) ValidateSession(ctx context.Context, jti string) error {
	ctx, span := tracer.Start(ctx, "auth.validate_session")
	defer span.End()

	row, err := s.db.FindSessionByJTI(ctx, jti)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrSessionNotFound
		}
		return otelx.RecordSpanError(span, fmt.Errorf("query session: %w", err))
	}
	if row.RevokedAt != nil {
		return ErrSessionRevoked
	}
	if row.ExpiresAt.Before(time.Now()) {
		return ErrSessionExpired
	}
	return nil
}

// TouchSession updates the last_seen_at timestamp. Best-effort: middleware
// calls this fire-and-forget so a DB error must not break the request.
func (s *auth) TouchSession(
	ctx context.Context,
	jti string,
	when time.Time,
) error {
	if err := s.db.TouchSession(ctx, jti, when); err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	return nil
}

// TouchSessionAsync spawns a tracked goroutine that updates last_seen_at in
// the background. Registered on s.bg so Shutdown can wait for in-flight
// touches before the caller closes the DB.
func (s *auth) TouchSessionAsync(jti string) {
	s.bg.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.TouchSession(ctx, jti, time.Now()); err != nil {
			slog.WarnContext(ctx, "touch session failed",
				"session.id_hash", JTILogValue(jti),
				"error", err,
			)
		}
	})
}

// Shutdown blocks until all background goroutines spawned by the service
// (currently just async session touches) complete, or ctx is cancelled.
// Call after the HTTP server has drained and before closing the DB so
// in-flight writes don't race DB.Close.
func (s *auth) Shutdown(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.bg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RevokeSession sets revoked_at=now on the matching row. Idempotent: a second
// call against an already-revoked row leaves the original revoked_at intact.
// No-ops silently if the session is unknown — logout of a dropped row should
// still succeed from the user's perspective.
func (s *auth) RevokeSession(ctx context.Context, jti string) error {
	ctx, span := tracer.Start(ctx, "auth.revoke_session")
	defer span.End()

	if err := s.db.RevokeSessionByJTI(ctx, jti, time.Now()); err != nil {
		return otelx.RecordSpanError(span, fmt.Errorf("revoke session: %w", err))
	}
	slog.InfoContext(ctx, "session revoked", "session.id_hash", JTILogValue(jti))
	return nil
}

// RevokeSessionByID revokes a session owned by userID. Returns
// ErrSessionNotFound if the row does not exist or belongs to another user —
// callers must not leak ownership information.
func (s *auth) RevokeSessionByID(
	ctx context.Context,
	userID, sessionID uint32,
) error {
	ctx, span := tracer.Start(ctx, "auth.revoke_session_by_id",
		trace.WithAttributes(
			semconv.UserID(fmt.Sprint(userID)),
			attribute.Int64("session.id", int64(sessionID)),
		),
	)
	defer span.End()

	n, err := s.db.RevokeUserSessionByID(ctx, userID, sessionID, time.Now())
	if err != nil {
		return otelx.RecordSpanError(
			span,
			fmt.Errorf("revoke session by id: %w", err),
		)
	}
	if n == 0 {
		// Either the row is already revoked, missing, or owned by another user.
		// Probe once more to distinguish "already revoked" (success) from
		// "not yours / not found" (error).
		exists, err := s.db.UserSessionExists(ctx, userID, sessionID)
		if err != nil {
			return otelx.RecordSpanError(span, fmt.Errorf("check session: %w", err))
		}
		if !exists {
			return ErrSessionNotFound
		}
	}
	return nil
}

// RevokeAllUserSessions revokes every non-revoked session belonging to the
// user. Used by admin-initiated "sign everyone out" flows (Rev 3).
func (s *auth) RevokeAllUserSessions(ctx context.Context, userID uint32) error {
	ctx, span := tracer.Start(ctx, "auth.revoke_all_user_sessions",
		trace.WithAttributes(semconv.UserID(fmt.Sprint(userID))),
	)
	defer span.End()

	if err := s.db.RevokeAllUserSessions(ctx, userID, time.Now()); err != nil {
		return otelx.RecordSpanError(
			span,
			fmt.Errorf("revoke all user sessions: %w", err),
		)
	}
	slog.InfoContext(ctx, "all user sessions revoked", "user.id", userID)
	return nil
}

// RevokeOtherSessions revokes every session belonging to userID except the
// one matching keepJTI. Used on password change so the caller stays logged
// in while peer sessions are invalidated.
func (s *auth) RevokeOtherSessions(
	ctx context.Context,
	userID uint32,
	keepJTI string,
) error {
	ctx, span := tracer.Start(ctx, "auth.revoke_other_sessions",
		trace.WithAttributes(semconv.UserID(fmt.Sprint(userID))),
	)
	defer span.End()

	if err := s.db.RevokeOtherUserSessions(
		ctx,
		userID,
		keepJTI,
		time.Now(),
	); err != nil {
		return otelx.RecordSpanError(
			span,
			fmt.Errorf("revoke other sessions: %w", err),
		)
	}
	return nil
}

// ListUserSessions returns every session row for the user, newest first, so
// the caller can render the Rev 2 "active sessions" UI. Includes revoked and
// expired rows — the UI filters/flags them.
func (s *auth) ListUserSessions(
	ctx context.Context,
	userID uint32,
) ([]*ent.Session, error) {
	return s.db.ListUserSessions(ctx, userID)
}

// SessionPurger is the consumer-facing surface for the periodic
// expired-session sweep. jobs.PurgeSessions accepts it so it can be driven by
// a fake in tests without standing up the full Service.
type SessionPurger interface {
	PurgeExpiredSessions(ctx context.Context, before time.Time) (int, error)
}

// PurgeExpiredSessions deletes rows whose expires_at is before the supplied
// cutoff. Callers pass a cutoff in the past (e.g. now-7d) so recently expired
// rows linger briefly for audit. Returns the number of rows deleted.
func (s *auth) PurgeExpiredSessions(
	ctx context.Context,
	before time.Time,
) (int, error) {
	ctx, span := tracer.Start(ctx, "auth.purge_sessions")
	defer span.End()

	n, err := s.db.PurgeExpiredSessions(ctx, before)
	if err != nil {
		return 0, otelx.RecordSpanError(
			span,
			fmt.Errorf("purge expired sessions: %w", err),
		)
	}
	span.SetAttributes(attribute.Int("sessions.purged", n))
	return n, nil
}
