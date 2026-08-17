package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/otelx"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/crypto/bcrypt"
)

// Account-related sentinel errors. Handlers translate these to HTTP codes.
var (
	// ErrPasswordInvalid is returned when the supplied current password does
	// not match the stored hash. Handlers map to 401.
	ErrPasswordInvalid = errors.New("current password invalid")

	// ErrPasswordWeak is returned when a new password fails the strength
	// policy. Handlers map to 422.
	ErrPasswordWeak = errors.New("new password fails policy")

	// ErrAPIKeyNotFound is returned when no key matches the (userID, keyID)
	// tuple — either the key does not exist or it belongs to another user.
	// Handlers must not leak ownership information.
	ErrAPIKeyNotFound = errors.New("api key not found")

	// ErrAPIKeyQuotaExceeded is returned when a user tries to create an API
	// key past maxAPIKeysPerUser. Handlers map to a 4xx.
	ErrAPIKeyQuotaExceeded = errors.New("api key quota exceeded")
)

// maxAPIKeysPerUser bounds how many API keys a single account may hold, so
// the lowest-privilege, roleRequestOnly CreateMyApiKey endpoint cannot grow
// the table without bound.
const maxAPIKeysPerUser = 20

// minPasswordLen mirrors the OpenAPI minLength on ChangePasswordRequest.new_password.
const minPasswordLen = 8

// validatePassword enforces the minimum strength policy. Keep in sync with
// the OpenAPI schema's minLength so request-level validation catches the same
// floor before service-level checks.
func validatePassword(p string) error {
	if len(p) < minPasswordLen {
		return ErrPasswordWeak
	}
	return nil
}

// UpdateProfile writes the user-editable profile fields. Currently limited to
// display_name; extend here when more fields become self-service.
func (s *auth) UpdateProfile(
	ctx context.Context,
	userID uint32,
	displayName string,
) (*ent.User, error) {
	ctx, span := tracer.Start(ctx, "auth.update_profile",
		trace.WithAttributes(semconv.UserID(fmt.Sprint(userID))),
	)
	defer span.End()

	dn := strings.TrimSpace(displayName)
	u, err := s.db.UpdateUser(ctx, userID, db.UpdateUserParams{
		DisplayName: &dn,
	})
	if err != nil {
		return nil, otelx.RecordSpanError(
			span,
			fmt.Errorf("update profile: %w", err),
		)
	}
	return u, nil
}

// ChangePassword verifies the current password, applies the new one, revokes
// every other active session for userID so peer devices are signed out, and
// deletes every API key the user owns. keepJTI is the session the caller wants
// to stay logged in on — pass Claims.JTI from the request context. Key
// revocation is unconditional: a caller who authenticates this request with
// X-API-Key loses that key too, because rotating the password is meant to cut
// every standing credential, not only the interactive ones.
//
// The current password goes through comparePassword, so maxUsableHashCost
// bounds the work here exactly as it does on login. Being authenticated does
// not make that optional: this endpoint admits a session cookie or an API key,
// and the restored backup or migration that brings a costlier hash brings those
// alongside it, so the comparison is reachable — and /api/v1 carries no per-IP
// limiter to slow the repeat, unlike POST /auth/login. Measured before this
// call went through comparePassword, one wrong current_password answered in
// 48.8ms against a cost-10 hash, 746.0ms against cost 14, and not at all
// against $2a$31$ by a 45s client timeout: the core it took stayed at 100% long
// after the client gave up, and four concurrent ones held 398% of a 16-core
// box. Through comparePassword all three answer in 48-49ms.
func (s *auth) ChangePassword(
	ctx context.Context,
	userID uint32,
	current, newPassword, keepJTI string,
) error {
	ctx, span := tracer.Start(ctx, "auth.change_password",
		trace.WithAttributes(semconv.UserID(fmt.Sprint(userID))),
	)
	defer span.End()

	u, err := s.db.FindUserByID(ctx, userID)
	if err != nil {
		return otelx.RecordSpanError(span, fmt.Errorf("load user: %w", err))
	}
	if u.PasswordHash == "" {
		// OIDC-only accounts have no local password to compare against.
		return ErrPasswordInvalid
	}
	if err := comparePassword(ctx, u.PasswordHash, current); err != nil {
		return ErrPasswordInvalid
	}
	if err := validatePassword(newPassword); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return otelx.RecordSpanError(span, fmt.Errorf("hash password: %w", err))
	}
	if err := s.db.UpdateUserPassword(ctx, userID, string(hash)); err != nil {
		return otelx.RecordSpanError(span, fmt.Errorf("update password: %w", err))
	}
	if err := s.RevokeOtherSessions(ctx, userID, keepJTI); err != nil {
		// Best-effort: the password is already updated. Log and carry on so the
		// caller's own session stays usable even if the peer revoke races.
		slog.WarnContext(ctx, "revoke_other_sessions_failed", "error", err)
	}
	revoked, err := s.db.DeleteAPIKeysByUser(ctx, userID)
	if err != nil {
		// Best-effort like the session revoke, but logged at ERROR: a key that
		// survives the rotation is a standing credential the rotation was
		// meant to cut.
		slog.ErrorContext(ctx, "revoke_api_keys_failed",
			"user.id", userID, "error", err)
	}
	slog.InfoContext(ctx, "auth_password_changed",
		"user.id", userID, "api_keys_revoked", revoked)
	return nil
}

// ListAPIKeys returns every API key record owned by userID, newest first.
// Never returns the raw token — only CreateAPIKey does, once.
func (s *auth) ListAPIKeys(
	ctx context.Context,
	userID uint32,
) ([]*ent.ApiKey, error) {
	return s.db.ListAPIKeysByUser(ctx, userID)
}

// RevokeAPIKeyByID deletes an API key owned by userID. Returns
// ErrAPIKeyNotFound if the row does not exist or belongs to another user —
// callers must not leak ownership information.
func (s *auth) RevokeAPIKeyByID(
	ctx context.Context,
	userID, keyID uint32,
) error {
	ctx, span := tracer.Start(ctx, "auth.revoke_api_key",
		trace.WithAttributes(semconv.UserID(fmt.Sprint(userID))),
	)
	defer span.End()

	n, err := s.db.DeleteAPIKeyByID(ctx, userID, keyID)
	if err != nil {
		return otelx.RecordSpanError(span, fmt.Errorf("revoke api key: %w", err))
	}
	if n == 0 {
		return ErrAPIKeyNotFound
	}
	return nil
}
