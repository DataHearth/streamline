package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"golang.org/x/crypto/bcrypt"
)

// IsFirstUser reports whether the user table is empty.
func (s *auth) IsFirstUser(ctx context.Context) (bool, error) {
	count, err := s.db.CountUsers(ctx)
	if err != nil {
		return false, err
	}
	return count == 0, nil
}

// defaultAdminEmail is used when no auth.seed_admin.email is configured, so a
// fresh install always boots with a usable admin account.
const defaultAdminEmail = "admin@streamline.local"

// BootstrapSeedAdmin ensures an admin exists on a fresh install. When
// auth.seed_admin.email is set it creates that admin from the configured
// password/password_file. When no email is configured it mints a default admin
// (defaultAdminEmail), generating a password if none was supplied and writing
// the resulting credentials back to the config file so the operator can
// retrieve them. No-op if any user already exists.
func (s *auth) BootstrapSeedAdmin(ctx context.Context) error {
	seed := config.Get().Auth.SeedAdmin

	first, err := s.IsFirstUser(ctx)
	if err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if !first {
		return nil
	}

	pw := seed.Password
	if seed.PasswordFile != "" {
		b, err := os.ReadFile(seed.PasswordFile)
		if err != nil {
			return fmt.Errorf("read seed password file: %w", err)
		}
		pw = strings.TrimSpace(string(b))
	}

	// No configured email: fall back to a default admin so the instance is
	// usable out of the box, generating a password when none was provided.
	defaulted := seed.Email == ""
	email := seed.Email
	if defaulted {
		email = defaultAdminEmail
		if pw == "" {
			pw, err = generatePassword()
			if err != nil {
				return fmt.Errorf("generate default admin password: %w", err)
			}
		}
	}
	email = strings.ToLower(email)

	if pw == "" {
		slog.WarnContext(ctx,
			"seed admin email set without password/password_file; skipping",
			"email", email,
		)
		return nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash seed password: %w", err)
	}
	if _, err := s.db.CreateUser(ctx, db.CreateUserParams{
		Email:        email,
		PasswordHash: string(hash),
		Role:         user.RoleAdmin,
		AuthMethod:   user.AuthMethodLocal,
	}); err != nil {
		return fmt.Errorf("create seed admin: %w", err)
	}

	if !defaulted {
		slog.InfoContext(ctx, "seed admin created", "email", email)
		return nil
	}

	// Persist the default admin's credentials so the operator can read them
	// from the config file. If the config can't be written (no backing file,
	// read-only), surface the password in the log instead so the freshly
	// created admin isn't unreachable.
	if err := config.Update(ctx, func(c *config.Config) error {
		c.Auth.SeedAdmin.Email = email
		c.Auth.SeedAdmin.Password = pw
		return nil
	}); err != nil {
		slog.WarnContext(
			ctx,
			"default admin created but credentials could not be saved to config; record this password now",
			"email",
			email,
			"password",
			pw,
			"error",
			err,
		)
		return nil
	}
	slog.WarnContext(
		ctx,
		"default admin created; credentials saved to auth.seed_admin in the config file — change this password",
		"email",
		email,
	)
	return nil
}

// generatePassword returns a URL-safe random password (~22 chars from 16
// crypto/rand bytes) for a generated default admin.
func generatePassword() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// RegisterOpen creates a user for open registration mode.
// Caller must verify mode == "open".
func (s *auth) RegisterOpen(
	ctx context.Context,
	email, password, displayName, defaultRole string,
	meta SessionMeta,
) (*ent.User, string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, "", fmt.Errorf("hash password: %w", err)
	}
	u, err := s.db.CreateUser(ctx, db.CreateUserParams{
		Email:        strings.ToLower(email),
		DisplayName:  displayName,
		PasswordHash: string(hash),
		Role:         user.Role(defaultRole),
		AuthMethod:   user.AuthMethodLocal,
	})
	if err != nil {
		return nil, "", fmt.Errorf("create user: %w", err)
	}
	tok, err := s.issueToken(ctx, u, meta)
	if err != nil {
		return nil, "", err
	}
	return u, tok, nil
}
