package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/invite"
	entuser "github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/otelx"
	approle "github.com/datahearth/streamline/internal/role"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInviteInvalid = errors.New("invite invalid or expired")

	// ErrRegistrationDisabled rejects minting an invite nobody could redeem:
	// both the local register route and the OIDC new-user path refuse outright
	// under registration_mode=disabled, invite or not.
	ErrRegistrationDisabled = errors.New("registration is disabled")
)

func hashInviteToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// CreateInvite generates a random token, stores its hash, and returns the raw
// token (shown once). ttl may be negative for immediate expiry (testing).
func (s *auth) CreateInvite(
	ctx context.Context,
	createdByID uint32,
	email, role string,
	ttl time.Duration,
) (string, *ent.Invite, error) {
	ctx, span := tracer.Start(ctx, "auth.create_invite", trace.WithAttributes(
		semconv.UserRoles(role),
		attribute.Int64("invite.created_by", int64(createdByID)),
	))
	defer span.End()

	if config.Get().Auth.RegistrationMode == "disabled" {
		return "", nil, otelx.RecordSpanError(span, ErrRegistrationDisabled)
	}
	raw, err := generateToken(32)
	if err != nil {
		return "", nil, otelx.RecordSpanError(
			span, fmt.Errorf("generate invite token: %w", err),
		)
	}
	inv, err := s.db.CreateInvite(ctx, db.CreateInviteParams{
		TokenHash:   hashInviteToken(raw),
		Email:       strings.ToLower(email),
		Role:        invite.Role(role),
		ExpiresAt:   time.Now().Add(ttl),
		CreatedByID: createdByID,
	})
	if err != nil {
		return "", nil, fmt.Errorf("store invite: %w", err)
	}
	return raw, inv, nil
}

// validateInvite performs all invite validity checks but does not mutate the
// row. It is a fail-fast pre-check only — single-use enforcement lives in the
// guarded UPDATE behind db.ConsumeInvite.
func (s *auth) validateInvite(
	ctx context.Context,
	rawToken, submittedEmail string,
) (*ent.Invite, error) {
	inv, err := s.db.FindInviteByTokenHash(ctx, hashInviteToken(rawToken))
	if err != nil {
		return nil, ErrInviteInvalid
	}
	if inv.UsedAt != nil {
		return nil, ErrInviteInvalid
	}
	if inv.ExpiresAt.Before(time.Now()) {
		return nil, ErrInviteInvalid
	}
	if inv.Email != "" && !strings.EqualFold(inv.Email, submittedEmail) {
		return nil, ErrInviteInvalid
	}
	return inv, nil
}

// LookupInviteForPrefill returns the invite matching rawToken if it is valid
// (unused + unexpired). Used by the webui register page to pre-fill the email
// field when invite.Email is set — does NOT mark the invite used and does NOT
// check the email field (prefill only shows data, grants nothing).
func (s *auth) LookupInviteForPrefill(
	ctx context.Context,
	rawToken string,
) (*ent.Invite, error) {
	inv, err := s.db.FindInviteByTokenHash(ctx, hashInviteToken(rawToken))
	if err != nil {
		return nil, ErrInviteInvalid
	}
	if inv.UsedAt != nil || inv.ExpiresAt.Before(time.Now()) {
		return nil, ErrInviteInvalid
	}
	return inv, nil
}

func (s *auth) ListInvites(ctx context.Context) ([]*ent.Invite, error) {
	return s.db.ListInvites(ctx)
}

// RevokeInvite marks an invite unusable. A not-found error is the caller's to
// turn into a 404; anything else is ours and is logged here, because the
// handler collapses every error into the same "no such invite" answer and an
// admin whose revocation is silently failing would be told the invite never
// existed.
func (s *auth) RevokeInvite(ctx context.Context, id uint32) error {
	ctx, span := tracer.Start(ctx, "auth.revoke_invite", trace.WithAttributes(
		attribute.Int64("invite.id", int64(id)),
	))
	defer span.End()

	err := s.db.RevokeInvite(ctx, id, time.Now())
	if err != nil && !ent.IsNotFound(err) {
		slog.ErrorContext(ctx, "could not revoke an invite",
			"invite.id", id, "error", err)
		return otelx.RecordSpanError(span, err)
	}
	return err
}

// RegisterWithInvite consumes the invite and creates the user atomically.
// On any failure the invite is NOT marked used and the user is NOT created.
func (s *auth) RegisterWithInvite(
	ctx context.Context,
	rawToken, email, password, displayName string,
	meta SessionMeta,
) (*ent.User, string, error) {
	ctx, span := tracer.Start(ctx, "auth.register_with_invite",
		trace.WithAttributes(
			semconv.UserEmail(email),
			attribute.String("auth.method", "invite"),
		),
	)
	defer span.End()

	outcome := "success"
	defer func() {
		registrations.Add(ctx, 1, metric.WithAttributes(
			attribute.String("auth.method", "invite"),
			attribute.String("outcome", outcome),
		))
	}()

	// Validate invite first (read-only) so we fail fast without starting a tx.
	inv, err := s.validateInvite(ctx, rawToken, email)
	if err != nil {
		outcome = "invalid_invite"
		return nil, "", otelx.RecordSpanError(span, err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		outcome = "error"
		return nil, "", otelx.RecordSpanError(
			span, fmt.Errorf("hash password: %w", err),
		)
	}

	tx, err := s.db.Tx(ctx)
	if err != nil {
		outcome = "error"
		return nil, "", otelx.RecordSpanError(
			span, fmt.Errorf("begin tx: %w", err),
		)
	}

	u, err := tx.CreateUser(ctx, db.CreateUserParams{
		Email:        strings.ToLower(email),
		DisplayName:  displayName,
		PasswordHash: string(hash),
		Role:         approle.Invited(entuser.Role(inv.Role.String())),
		AuthMethod:   entuser.AuthMethodLocal,
	})
	if err != nil {
		tx.Rollback()
		outcome = "error"
		return nil, "", otelx.RecordSpanError(
			span, fmt.Errorf("create user: %w", err),
		)
	}

	if err := tx.ConsumeInvite(ctx, inv.ID, u.ID, time.Now()); err != nil {
		tx.Rollback()
		if errors.Is(err, db.ErrInviteUsed) {
			outcome = "invalid_invite"
			return nil, "", otelx.RecordSpanError(span, ErrInviteInvalid)
		}
		outcome = "error"
		return nil, "", otelx.RecordSpanError(
			span, fmt.Errorf("consume invite: %w", err),
		)
	}

	if err := tx.Commit(); err != nil {
		outcome = "error"
		return nil, "", otelx.RecordSpanError(
			span, fmt.Errorf("commit tx: %w", err),
		)
	}

	tok, err := s.issueToken(ctx, u, meta)
	if err != nil {
		outcome = "error"
		return nil, "", otelx.RecordSpanError(span, err)
	}
	slog.InfoContext(ctx, "user registered",
		"user.id", u.ID, "auth.method", "invite", "user.roles", string(u.Role))
	return u, tok, nil
}
