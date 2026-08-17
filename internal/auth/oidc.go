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
	approle "github.com/datahearth/streamline/internal/role"
)

// OIDC sentinel errors translated to user-facing messages by the webui.
var (
	ErrOIDCEmailUnverified = errors.New("oidc_email_unverified")
	ErrOIDCRegDisabled     = errors.New("oidc_registration_disabled")
	ErrOIDCNoInvite        = errors.New("oidc_no_invite")
	ErrOIDCLinkNotAllowed  = errors.New("oidc_link_not_allowed")
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
//  1. Existing identity (provider, subject) → log that user in, re-syncing the
//     claim-mapped role through approle.Federated.
//  2. Reject if email is unverified by the provider.
//  3. Existing user by email → adopt the account only where the provider's
//     email_linking setting permits it for the role that account holds, then
//     link identity, promote auth_method local → both and log in. The adoption
//     never changes the role. Otherwise ErrOIDCLinkNotAllowed: matching an
//     address is not proof the same human holds both accounts.
//  4. New user → respect registration_mode (disabled rejects; invite needs a
//     matching invite; open falls back to oidc_default_role). User, identity
//     and invite consumption commit as one transaction, so a failed
//     provisioning never burns the invite.
//
// Every role this function can put on an account — claim-mapped, invite-carried
// or oidc_default_role — is decided by approle.Federated and carried as an
// approle.Value, a type only that package can fill in. Nothing here reads the
// account's auth_method to rank it: the shape an adoption leaves behind
// describes how the row was reached, not how far the provider is trusted, and
// three rounds of this fix died to gates that confused the two.
func (s *auth) LoginOIDC(
	ctx context.Context,
	provider, subject, email, displayName string,
	emailVerified bool,
	claims map[string]any,
	meta SessionMeta,
) (*ent.User, string, error) {
	// Normalised once here so every lookup, comparison and write below agrees
	// on one form. Surrounding whitespace and case are what this folds — a
	// padded or shout-cased address that reached the queries raw would miss the
	// step-3 match and fork a second row shadowing the account it should have
	// found.
	//
	// It is deliberately no more than that. NFKC folding would also collapse
	// U+FF41 fullwidth and ligature forms, and stripping a trailing root dot
	// would collapse "a@b.com." — but each of those makes two addresses the IdP
	// considers distinct resolve to one local account, which turns a duplicate
	// row into a match against someone else's account. A duplicate row is
	// confusing; a spurious match is the escalation this whole path exists to
	// prevent. Those forms therefore stay distinct users.
	email = strings.ToLower(strings.TrimSpace(email))

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
	pc, _ := findOIDCProvider(cfg, provider)

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
		u = s.syncOIDCRole(
			ctx,
			u,
			approle.Federated(provider, "", oidcClaimRoles(pc, claims)...),
		)
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

	// 3. adopt an existing account whose email matches — opt-in only
	existing, err := s.db.FindUserByEmail(ctx, email)
	if err != nil && !ent.IsNotFound(err) {
		return nil, "", otelx.RecordSpanError(
			span,
			fmt.Errorf("query user by email: %w", err),
		)
	}
	if err == nil {
		if !emailLinkingAllowed(pc.EmailLinking, existing.Role) {
			span.SetAttributes(
				attribute.String("oidc.outcome", "link_not_allowed"),
			)
			slog.WarnContext(
				ctx,
				"oidc login refused: unlinked identity matched an existing account by email",
				"oidc.provider",
				provider,
				"oidc.subject",
				subject,
				"user.id",
				existing.ID,
				"user.role",
				existing.Role.String(),
				"auth.oidc.email_linking",
				pc.EmailLinking,
			)
			outcome = "link_not_allowed"
			return nil, "", otelx.RecordSpanError(span, ErrOIDCLinkNotAllowed)
		}
		span.SetAttributes(attribute.String("oidc.outcome", "linked_existing"))
		if _, err := s.db.CreateOIDCIdentity(ctx, db.CreateOIDCIdentityParams{
			Provider: provider,
			Subject:  subject,
			Email:    email,
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
		// No role sync here on purpose: the request that establishes a link
		// must not also raise privilege on the account it just adopted. The
		// mapped role applies from the next login, by which point the identity
		// is one the operator can see and revoke.
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
	fallbackRole := cfg.Auth.OIDCDefaultRole
	var inv *ent.Invite
	switch cfg.Auth.RegistrationMode {
	case "disabled":
		outcome = "reg_disabled"
		return nil, "", otelx.RecordSpanError(span, ErrOIDCRegDisabled)
	case "invite":
		found, err := s.db.FindUnusedInviteForEmail(ctx, email, time.Now())
		if err != nil {
			outcome = "no_invite"
			return nil, "", otelx.RecordSpanError(span, ErrOIDCNoInvite)
		}
		inv = found
		fallbackRole = inv.Role.String()
	}
	// The invite-carried role goes through the ceiling like every other: it
	// arrives over a channel the provider controls the far end of, and the
	// documented promise for a provider without allow_admin is that no login
	// through it yields admin, with no exception to read past.
	role := approle.Federated(provider, fallbackRole, oidcClaimRoles(pc, claims)...)
	span.SetAttributes(
		attribute.String("oidc.outcome", "new_user"),
		semconv.UserRoles(role.String()),
	)

	tx, err := s.db.Tx(ctx)
	if err != nil {
		return nil, "", otelx.RecordSpanError(span, fmt.Errorf("begin tx: %w", err))
	}

	u, err := tx.CreateUser(ctx, db.CreateUserParams{
		Email:       email,
		DisplayName: displayName,
		Role:        role,
		AuthMethod:  entuser.AuthMethodOidc,
	})
	if err != nil {
		tx.Rollback()
		return nil, "", otelx.RecordSpanError(
			span,
			fmt.Errorf("create user: %w", err),
		)
	}
	if _, err := tx.CreateOIDCIdentity(ctx, db.CreateOIDCIdentityParams{
		Provider: provider,
		Subject:  subject,
		Email:    email,
		OwnerID:  u.ID,
	}); err != nil {
		tx.Rollback()
		return nil, "", otelx.RecordSpanError(
			span,
			fmt.Errorf("create identity: %w", err),
		)
	}
	if inv != nil {
		if err := tx.ConsumeInvite(ctx, inv.ID, u.ID, time.Now()); err != nil {
			tx.Rollback()
			if errors.Is(err, db.ErrInviteUsed) {
				outcome = "no_invite"
				return nil, "", otelx.RecordSpanError(span, ErrOIDCNoInvite)
			}
			return nil, "", otelx.RecordSpanError(
				span,
				fmt.Errorf("consume invite: %w", err),
			)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, "", otelx.RecordSpanError(span, fmt.Errorf("commit tx: %w", err))
	}
	tok, err := s.issueToken(ctx, u, meta)
	if err != nil {
		return u, tok, otelx.RecordSpanError(span, err)
	}
	outcome = "new_user"
	slog.InfoContext(ctx, "oidc login: new user provisioned",
		"oidc.provider", provider, "oidc.subject", subject,
		"user.id", u.ID, "user.email", u.Email, "role", role.String())
	return u, tok, nil
}

// findOIDCProvider returns the auth.oidc entry named provider. The zero
// OIDCConfig comes back when nothing matches, which every caller treats as
// "no opt-in of any kind" — a provider the operator removed mid-flight must
// not inherit the permissions of the one that replaced it.
func findOIDCProvider(
	cfg *config.Config,
	provider string,
) (config.OIDCConfig, bool) {
	for _, p := range cfg.Auth.OIDC {
		if p.Name == provider {
			return p, true
		}
	}
	return config.OIDCConfig{}, false
}

// emailLinkingAllowed reports whether an identity this provider has never
// presented before may be adopted into the existing account holding the same
// email, given the provider's email_linking setting and the role that account
// holds.
//
// The default (empty / disabled) is a refusal. Admins are excluded from
// non_admin because the seeded admin's address is the one an attacker can
// guess and the account can rewrite auth config, including this setting.
//
// Adoption is all this decides. What role the adopted account may afterwards
// be moved to is approle.Federated's business, so no move of this setting — in
// either direction — can change a role outcome.
func emailLinkingAllowed(mode string, role entuser.Role) bool {
	switch mode {
	case config.OIDCEmailLinkingAll:
		return true
	case config.OIDCEmailLinkingNonAdmin:
		return role != entuser.RoleAdmin
	default:
		return false
	}
}

// oidcClaimRoles returns every Streamline role the provider's role_mapping
// matches in this request's claims, unranked and uncapped — approle.Federated ranks
// them and applies the ceiling. Empty when the provider configures no mapping
// or nothing matches.
func oidcClaimRoles(pc config.OIDCConfig, claims map[string]any) []string {
	if pc.RoleClaim == "" || len(pc.RoleMapping) == 0 {
		return nil
	}
	var roles []string
	for _, v := range claimStrings(claimValue(claims, pc.RoleClaim)) {
		if role, ok := pc.RoleMapping[v]; ok {
			roles = append(roles, role)
		}
	}
	return roles
}

// claimValue resolves path against claims, preferring a claim literally named
// path and otherwise walking it as a dot-separated route through nested
// objects. Keycloak publishes roles at realm_access.roles rather than at the
// top level, and a flat lookup of that name silently matches nothing — leaving
// role mapping, and every ceiling derived from it, permanently inert.
func claimValue(claims map[string]any, path string) any {
	if v, ok := claims[path]; ok {
		return v
	}
	var cur any = claims
	for seg := range strings.SplitSeq(path, ".") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		if cur, ok = obj[seg]; !ok {
			return nil
		}
	}
	return cur
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

// syncOIDCRole updates u's role to the claim-mapped role when one was decided
// and differs from the current one, so IdP group changes propagate on each
// login. A failed update is logged and the original user returned — login still
// succeeds.
//
// Only the already-linked path calls this: adopting an account by email is not
// an occasion to re-rank it. Taking an approle.Value rather than a string is
// what makes the ceiling unskippable — the type has no exported way to hold a
// role approle.Federated did not put there, so there is no value to call this with
// that skipped the ceiling.
//
// The write goes through db.UpdateUserRole, whose guarded UPDATE refuses to
// demote the last admin: a claim change at the IdP may lower a role, but it
// cannot leave the instance with nobody able to administer it.
func (s *auth) syncOIDCRole(
	ctx context.Context,
	u *ent.User,
	mapped approle.Value,
) *ent.User {
	if mapped.Empty() || u.Role.String() == mapped.String() {
		return u
	}
	updated, err := s.db.UpdateUserRole(ctx, u.ID, mapped)
	if err != nil {
		if errors.Is(err, db.ErrLastAdmin) {
			slog.WarnContext(ctx, "auth.oidc_role_sync_refused_last_admin",
				"user.id", u.ID, "user.role", string(u.Role),
				"role", mapped.String())
			return u
		}
		slog.WarnContext(ctx, "auth.oidc_role_sync_failed",
			"user.id", u.ID, "role", mapped.String(), "error", err)
		return u
	}
	slog.InfoContext(ctx, "oidc role synced from claims",
		"user.id", u.ID, "role", mapped.String())
	return updated
}

// syncOIDCProfile reconciles the local user row with fresh claims from the
// IdP on every identity-matched OIDC login. claimEmail arrives already
// normalised from LoginOIDC. The email update is skipped (with a logged
// warning) when the claim collides with another local user; display_name is
// always overwritten when changed; lockout state is cleared so a successful
// federated login auto-unlocks the account.
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

	if emailVerified && claimEmail != "" && claimEmail != u.Email {
		other, err := s.db.FindUserByEmail(ctx, claimEmail)
		if err != nil && !ent.IsNotFound(err) {
			return false, fmt.Errorf("lookup email collision: %w", err)
		}
		if err == nil && other.ID != u.ID {
			slog.WarnContext(ctx, "auth.oidc_email_collision",
				"user.id", u.ID, "claim.email", claimEmail, "other.id", other.ID)
		} else {
			params.Email = &claimEmail
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
