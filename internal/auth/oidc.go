package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/oauth2"

	"github.com/datahearth/streamline/ent"
	entuser "github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/otelx"
)

// OIDC sentinel errors translated to user-facing messages by the webui.
var (
	ErrOIDCEmailUnverified = errors.New("oidc_email_unverified")
	ErrOIDCRegDisabled     = errors.New("oidc_registration_disabled")
	ErrOIDCNoInvite        = errors.New("oidc_no_invite")
)

// OIDCProvider bundles a verifier + OAuth2 config for one named provider.
type OIDCProvider struct {
	Name     string
	Verifier *oidc.IDTokenVerifier
	OAuth2   *oauth2.Config
}

// OIDCManager is the consumer-facing surface for resolving configured OIDC
// providers at HTTP request time and warming the provider cache at startup.
type OIDCManager interface {
	Init(ctx context.Context, redirectBase string)
	Get(name string) (*OIDCProvider, bool)
}

// oidcManager holds initialized OIDC providers keyed by name.
type oidcManager struct {
	mu        sync.RWMutex
	providers map[string]*OIDCProvider
}

func NewOIDCManager() OIDCManager {
	return &oidcManager{providers: map[string]*OIDCProvider{}}
}

// Init discovers each configured provider (from the config singleton's
// auth.oidc list) and caches its verifier + oauth2 config. Logs and skips
// providers whose discovery fails. redirectBase is the public base URL, e.g.
// "https://streamline.example.com".
func (m *oidcManager) Init(ctx context.Context, redirectBase string) {
	providers := config.Get().Auth.OIDC
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range providers {
		prv, err := oidc.NewProvider(ctx, p.Issuer)
		if err != nil {
			slog.ErrorContext(
				ctx,
				"OIDC provider discovery failed; provider unavailable",
				"provider",
				p.Name,
				"issuer",
				p.Issuer,
				"error",
				err,
			)
			continue
		}
		m.providers[p.Name] = &OIDCProvider{
			Name:     p.Name,
			Verifier: prv.Verifier(&oidc.Config{ClientID: p.ClientID}),
			OAuth2: &oauth2.Config{
				ClientID:     p.ClientID,
				ClientSecret: config.SecretValue(p.ClientSecret, p.ClientSecretFile),
				Endpoint:     prv.Endpoint(),
				Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
				RedirectURL:  redirectBase + "/auth/oidc/" + p.Name + "/callback",
			},
		}
	}
}

// Register is a test helper for injecting a pre-built provider.
func (m *oidcManager) Register(p *OIDCProvider) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.providers[p.Name] = p
}

func (m *oidcManager) Get(name string) (*OIDCProvider, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.providers[name]
	return p, ok
}

// LoginOIDC applies the linking + onboarding policy and returns a JWT for the
// resulting user. Flow:
//  1. Existing identity (provider, subject) → log that user in.
//  2. Reject if email is unverified by the provider.
//  3. Existing user by email → link identity, promote auth_method local → both, log in.
//  4. New user → respect registration_mode (disabled rejects; invite needs a
//     matching invite; open uses oidc_default_role).
func (s *auth) LoginOIDC(
	ctx context.Context,
	provider, subject, email, displayName string,
	emailVerified bool,
	claims map[string]any,
	meta SessionMeta,
) (*ent.User, string, error) {
	ctx, span := tracer.Start(ctx, "auth.login_oidc",
		trace.WithAttributes(
			attribute.String("oidc.provider", provider),
			semconv.UserEmail(email),
			attribute.Bool("email_verified", emailVerified),
			attribute.String("auth.method", "oidc"),
		),
	)
	defer span.End()

	outcome := "error"
	defer func() {
		oidcLogins.Add(ctx, 1, metric.WithAttributes(
			attribute.String("provider", provider),
			attribute.String("outcome", outcome),
		))
	}()

	cfg := config.Get()
	mappedRole, roleMatched := oidcRoleFromClaims(cfg, provider, claims)

	// 1. existing identity
	id, err := s.db.FindOIDCIdentity(ctx, provider, subject)
	if err != nil && !ent.IsNotFound(err) {
		return nil, "", otelx.RecordSpanError(
			span,
			fmt.Errorf("query oidc identity: %w", err),
		)
	}
	if err == nil {
		span.SetAttributes(attribute.String("oidc.outcome", "existing_identity"))
		u := id.Edges.Owner
		if changed, syncErr := s.syncOIDCProfile(
			ctx,
			u,
			email,
			displayName,
			emailVerified,
		); syncErr != nil {
			slog.WarnContext(ctx, "auth.oidc_profile_sync_failed",
				"user.id", u.ID, "error", syncErr)
		} else if changed {
			span.SetAttributes(attribute.Bool("auth.oidc.profile_changed", true))
			if reloaded, rerr := s.db.FindUserByID(ctx, u.ID); rerr == nil {
				u = reloaded
			}
		}
		u = s.syncOIDCRole(ctx, u, mappedRole, roleMatched)
		tok, err := s.issueToken(ctx, u, meta)
		if err != nil {
			return u, tok, otelx.RecordSpanError(span, err)
		}
		outcome = "success"
		slog.InfoContext(ctx, "oidc login: existing identity",
			"oidc.provider", provider, "oidc.subject", subject,
			"user.id", u.ID, "user.email", u.Email)
		return u, tok, nil
	}

	if !emailVerified {
		outcome = "email_unverified"
		return nil, "", otelx.RecordSpanError(span, ErrOIDCEmailUnverified)
	}

	// 3. auto-link by email
	existing, err := s.db.FindUserByEmail(ctx, strings.ToLower(email))
	if err != nil && !ent.IsNotFound(err) {
		return nil, "", otelx.RecordSpanError(
			span,
			fmt.Errorf("query user by email: %w", err),
		)
	}
	if err == nil {
		span.SetAttributes(attribute.String("oidc.outcome", "linked_existing"))
		if _, err := s.db.CreateOIDCIdentity(ctx, db.CreateOIDCIdentityParams{
			Provider: provider,
			Subject:  subject,
			Email:    strings.ToLower(email),
			OwnerID:  existing.ID,
		}); err != nil {
			return nil, "", otelx.RecordSpanError(
				span,
				fmt.Errorf("link oidc identity: %w", err),
			)
		}
		if existing.AuthMethod == entuser.AuthMethodLocal {
			method := entuser.AuthMethodBoth
			updated, err := s.db.UpdateUser(
				ctx,
				existing.ID,
				db.UpdateUserParams{AuthMethod: &method},
			)
			if err != nil {
				return nil, "", otelx.RecordSpanError(
					span,
					fmt.Errorf("update auth_method: %w", err),
				)
			}
			existing = updated
		}
		existing = s.syncOIDCRole(ctx, existing, mappedRole, roleMatched)
		tok, err := s.issueToken(ctx, existing, meta)
		if err != nil {
			return existing, tok, otelx.RecordSpanError(span, err)
		}
		outcome = "linked_existing"
		slog.InfoContext(ctx, "oidc login: linked existing user by email",
			"oidc.provider", provider, "oidc.subject", subject,
			"user.id", existing.ID, "user.email", existing.Email)
		return existing, tok, nil
	}

	// 4. new user — apply onboarding policy
	role := cfg.Auth.OIDCDefaultRole
	switch cfg.Auth.RegistrationMode {
	case "disabled":
		outcome = "reg_disabled"
		return nil, "", otelx.RecordSpanError(span, ErrOIDCRegDisabled)
	case "invite":
		inv, err := s.db.FindUnusedInviteForEmail(
			ctx,
			strings.ToLower(email),
			time.Now(),
		)
		if err != nil {
			outcome = "no_invite"
			return nil, "", otelx.RecordSpanError(span, ErrOIDCNoInvite)
		}
		role = inv.Role.String()
		if _, err := s.db.MarkInviteUsed(
			ctx,
			inv.ID,
			time.Now(),
		); err != nil {
			return nil, "", otelx.RecordSpanError(
				span,
				fmt.Errorf("mark invite used: %w", err),
			)
		}
	}
	if roleMatched {
		role = mappedRole
	}
	span.SetAttributes(
		attribute.String("oidc.outcome", "new_user"),
		semconv.UserRoles(role),
	)

	u, err := s.db.CreateUser(ctx, db.CreateUserParams{
		Email:       strings.ToLower(email),
		DisplayName: displayName,
		Role:        entuser.Role(role),
		AuthMethod:  entuser.AuthMethodOidc,
	})
	if err != nil {
		return nil, "", otelx.RecordSpanError(
			span,
			fmt.Errorf("create user: %w", err),
		)
	}
	if _, err := s.db.CreateOIDCIdentity(ctx, db.CreateOIDCIdentityParams{
		Provider: provider,
		Subject:  subject,
		Email:    strings.ToLower(email),
		OwnerID:  u.ID,
	}); err != nil {
		return nil, "", otelx.RecordSpanError(
			span,
			fmt.Errorf("create identity: %w", err),
		)
	}
	tok, err := s.issueToken(ctx, u, meta)
	if err != nil {
		return u, tok, otelx.RecordSpanError(span, err)
	}
	outcome = "new_user"
	slog.InfoContext(ctx, "oidc login: new user provisioned",
		"oidc.provider", provider, "oidc.subject", subject,
		"user.id", u.ID, "user.email", u.Email, "role", role)
	return u, tok, nil
}

// roleRank orders roles by privilege so the highest-privilege claim match wins.
var roleRank = map[string]int{"request_only": 1, "member": 2, "admin": 3}

// oidcRoleFromClaims maps the provider's configured role_claim values to a
// Streamline role via its role_mapping. Returns ("", false) when the provider
// has no role mapping configured or no claim value matches; otherwise the
// highest-privilege matched role.
func oidcRoleFromClaims(
	cfg *config.Config,
	provider string,
	claims map[string]any,
) (string, bool) {
	var pc config.OIDCConfig
	for _, p := range cfg.Auth.OIDC {
		if p.Name == provider {
			pc = p
			break
		}
	}
	if pc.RoleClaim == "" || len(pc.RoleMapping) == 0 {
		return "", false
	}
	best := ""
	for _, v := range claimStrings(claims[pc.RoleClaim]) {
		if role, ok := pc.RoleMapping[v]; ok && roleRank[role] > roleRank[best] {
			best = role
		}
	}
	return best, best != ""
}

// claimStrings coerces a claim value (string, []string, or []any of strings)
// into a string slice; non-string elements are skipped.
func claimStrings(raw any) []string {
	switch v := raw.(type) {
	case string:
		return []string{v}
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// syncOIDCRole updates u's role to the claim-mapped role when mapping matched
// and the role differs, so IdP group changes propagate on each login. A failed
// update is logged and the original user returned — login still succeeds.
func (s *auth) syncOIDCRole(
	ctx context.Context,
	u *ent.User,
	mappedRole string,
	matched bool,
) *ent.User {
	if !matched || u.Role.String() == mappedRole {
		return u
	}
	role := entuser.Role(mappedRole)
	updated, err := s.db.UpdateUser(ctx, u.ID, db.UpdateUserParams{Role: &role})
	if err != nil {
		slog.WarnContext(ctx, "auth.oidc_role_sync_failed",
			"user.id", u.ID, "role", mappedRole, "error", err)
		return u
	}
	slog.InfoContext(ctx, "oidc role synced from claims",
		"user.id", u.ID, "role", mappedRole)
	return updated
}

// syncOIDCProfile reconciles the local user row with fresh claims from the
// IdP on every identity-matched OIDC login. Email update is skipped (with a
// logged warning) when the lowercased claim collides with another local
// user; display_name is always overwritten when changed; lockout state is
// cleared so a successful federated login auto-unlocks the account.
//
// Returns true when any field was actually written.
func (s *auth) syncOIDCProfile(
	ctx context.Context,
	u *ent.User,
	claimEmail, claimDisplayName string,
	emailVerified bool,
) (bool, error) {
	params := db.UpdateUserParams{}
	dirty := false

	newEmail := strings.ToLower(strings.TrimSpace(claimEmail))
	if emailVerified && newEmail != "" && newEmail != u.Email {
		other, err := s.db.FindUserByEmail(ctx, newEmail)
		if err != nil && !ent.IsNotFound(err) {
			return false, fmt.Errorf("lookup email collision: %w", err)
		}
		if err == nil && other.ID != u.ID {
			slog.WarnContext(ctx, "auth.oidc_email_collision",
				"user.id", u.ID, "claim.email", newEmail, "other.id", other.ID)
		} else {
			params.Email = &newEmail
			dirty = true
		}
	}

	if claimDisplayName != "" && claimDisplayName != u.DisplayName {
		params.DisplayName = &claimDisplayName
		dirty = true
	}

	if u.FailedLoginCount != 0 || u.LastFailedLoginAt != nil ||
		u.LockedUntil != nil {
		zero := uint8(0)
		params.FailedLoginCount = &zero
		params.ClearLastFailedLoginAt = true
		params.ClearLockedUntil = true
		dirty = true
	}

	if !dirty {
		return false, nil
	}
	if _, err := s.db.UpdateUser(ctx, u.ID, params); err != nil {
		return false, fmt.Errorf("update user: %w", err)
	}
	return true, nil
}
