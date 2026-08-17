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
	approle "github.com/datahearth/streamline/internal/role"
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

// BootstrapSeedAdmin ensures an admin exists on a fresh install, from
// auth.seed_admin. An unset email resolves to defaultAdminEmail; that account
// gets a generated password, printed once to stdout, when neither password nor
// password_file was configured. No-op if any user already exists — including
// on auth.seed_admin itself, which BootstrapSeedAdmin never rewrites.
//
// A generated password is never written to the config file and never fed to
// slog: the config file outlives the install (and every backup of it), and slog
// records fan out to stderr *and* the OTLP logs pipeline.
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

	email := strings.ToLower(seed.Email)
	if email == "" {
		email = defaultAdminEmail
	}
	// The default admin is the out-of-the-box account, so it must always end up
	// with a usable password, whether it got that email from an empty config or
	// from an explicit declaration.
	//
	// Except when a password_file was configured and read back empty: the
	// secret has not materialised yet (empty mount, whitespace-only file).
	// Minting a random password there would create the admin and flip
	// IsFirstUser, so the file the operator meant to use could never take
	// effect on any later boot. Fall through to the skip-with-warning instead.
	generated := email == defaultAdminEmail && pw == "" && seed.PasswordFile == ""
	if generated {
		pw, err = GeneratePassword()
		if err != nil {
			return fmt.Errorf("generate default admin password: %w", err)
		}
	}

	if pw == "" {
		slog.WarnContext(ctx,
			"seed admin has no usable password; skipping",
			"email", email,
			"password_file", seed.PasswordFile,
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
		Role:         approle.Seed(user.RoleAdmin),
		AuthMethod:   user.AuthMethodLocal,
	}); err != nil {
		return fmt.Errorf("create seed admin: %w", err)
	}

	if generated {
		printGeneratedCredentials(email, pw)
	}
	slog.InfoContext(ctx, "seed admin created", "email", email)
	return nil
}

// printGeneratedCredentials writes the generated default-admin password to
// stdout instead of the log. slog records reach stderr *and* the OTLP logs
// exporter, which would park a working admin credential in whatever backend
// collects them; stdout is what an operator reads once from the container's
// first-run output.
//
// Email and password share one line, and that line carries the words "default
// admin": the Helm NOTES tell operators to recover the credential with
// `kubectl logs … | grep -i "default admin"`, which a multi-line layout would
// filter down to nothing.
func printGeneratedCredentials(email, password string) {
	fmt.Printf(`
================= streamline: admin account =================
 No password was configured for the default admin, so one was
 generated. It is SHOWN ONCE, here: it is not written to the
 config file and not sent to the log pipeline. Anything that
 scrapes this container's stdout captured it too.

 default admin credentials — email: %s   password: %s

 Copy it now, then change it from Settings after logging in.
=============================================================

`, email, password)
}

// GeneratePassword returns a URL-safe random password (~22 chars from 16
// crypto/rand bytes). Shared by the seed-admin bootstrap and the CLI's
// `user set-password --generate`, so both mint credentials with the same
// entropy and alphabet.
func GeneratePassword() (string, error) {
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
		Role:         approle.SelfRegistered(user.Role(defaultRole)),
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
