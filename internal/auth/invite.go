package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/invite"
	entuser "github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/db"
	"golang.org/x/crypto/bcrypt"
)

var ErrInviteInvalid = errors.New("invite invalid or expired")

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
	raw, err := generateToken(32)
	if err != nil {
		return "", nil, fmt.Errorf("generate invite token: %w", err)
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
// row. The caller (RegisterWithInvite) performs the marking.
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

func (s *auth) RevokeInvite(ctx context.Context, id uint32) error {
	return s.db.RevokeInvite(ctx, id, time.Now())
}

// RegisterWithInvite consumes the invite and creates the user atomically.
// On any failure the invite is NOT marked used and the user is NOT created.
func (s *auth) RegisterWithInvite(
	ctx context.Context,
	rawToken, email, password, displayName string,
	meta SessionMeta,
) (*ent.User, string, error) {
	// Validate invite first (read-only) so we fail fast without starting a tx.
	inv, err := s.validateInvite(ctx, rawToken, email)
	if err != nil {
		return nil, "", err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, "", fmt.Errorf("hash password: %w", err)
	}

	tx, err := s.db.Tx(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("begin tx: %w", err)
	}

	u, err := tx.CreateUser(ctx, db.CreateUserParams{
		Email:        strings.ToLower(email),
		DisplayName:  displayName,
		PasswordHash: string(hash),
		Role:         entuser.Role(inv.Role.String()),
		AuthMethod:   entuser.AuthMethodLocal,
	})
	if err != nil {
		tx.Rollback()
		return nil, "", fmt.Errorf("create user: %w", err)
	}

	if _, err := tx.MarkInviteUsedWithUser(
		ctx,
		inv.ID,
		u.ID,
		time.Now(),
	); err != nil {
		tx.Rollback()
		return nil, "", fmt.Errorf("mark invite used: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, "", fmt.Errorf("commit tx: %w", err)
	}

	tok, err := s.issueToken(ctx, u, meta)
	if err != nil {
		return nil, "", err
	}
	return u, tok, nil
}
