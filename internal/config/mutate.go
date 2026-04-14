package config

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/datahearth/streamline/internal/otelx"
)

// AuthPatch carries optional field updates to the auth section. Nil fields
// are left untouched so callers only need to populate keys the user actually
// changed.
type AuthPatch struct {
	RegistrationMode *string
	SessionTTL       *string
	OIDCDefaultRole  *string
}

// OIDCProviderPatch carries optional field updates to a single OIDC provider.
// A nil ClientSecret (or empty string) preserves the existing secret — the
// UI never shows the current value, so blank means "unchanged."
type OIDCProviderPatch struct {
	Issuer       *string
	ClientID     *string
	ClientSecret *string
}

// Named errors returned by the mutate layer. Handlers branch on these to
// decide between 404 / 409 / 422 responses.
var (
	ErrOIDCProviderExists   = errors.New("oidc provider name already exists")
	ErrOIDCProviderNotFound = errors.New("oidc provider not found")
	ErrOIDCDiscoveryFailed  = errors.New("oidc discovery failed")

	// ErrSecretFileManaged is returned when a UI/API edit tries to set a secret
	// inline while that field is sourced from a *_file path. The file is the
	// source of truth — change it (or git), not the UI.
	ErrSecretFileManaged = errors.New(
		"secret is file-managed; edit the file, not the UI",
	)
)

// UpdateAuth validates the patch, merges it into the auth section, and
// persists via Update. Returns the resulting AuthConfig on success so callers
// can echo the new state back to the client without a second Get.
func UpdateAuth(ctx context.Context, patch AuthPatch) (AuthConfig, error) {
	if patch.SessionTTL != nil {
		if _, err := time.ParseDuration(*patch.SessionTTL); err != nil {
			return AuthConfig{}, fmt.Errorf("session_ttl: %w", err)
		}
	}

	var out AuthConfig
	err := Update(ctx, func(c *Config) error {
		if patch.RegistrationMode != nil {
			c.Auth.RegistrationMode = *patch.RegistrationMode
		}
		if patch.SessionTTL != nil {
			c.Auth.SessionTTL = *patch.SessionTTL
		}
		if patch.OIDCDefaultRole != nil {
			c.Auth.OIDCDefaultRole = *patch.OIDCDefaultRole
		}
		out = c.Auth
		return nil
	})
	if err != nil {
		return AuthConfig{}, err
	}
	return out, nil
}

// AddOIDCProvider probes the issuer's discovery document, then appends the
// provider to the auth.oidc list. Returns ErrOIDCProviderExists if the name
// is already in use or ErrOIDCDiscoveryFailed if the issuer is unreachable.
//
// Discovery runs against the caller's ctx (with a 5s timeout) so slow remotes
// can't hold the config lock — Update is called only after the probe returns.
func AddOIDCProvider(ctx context.Context, p OIDCConfig) error {
	cur := Get()
	if cur != nil {
		for _, existing := range cur.Auth.OIDC {
			if existing.Name == p.Name {
				slog.WarnContext(
					ctx,
					"oidc provider name already in use",
					"name",
					p.Name,
				)
				return ErrOIDCProviderExists
			}
		}
	}

	if err := discoverOIDC(ctx, p.Issuer); err != nil {
		slog.WarnContext(
			ctx,
			"oidc discovery failed",
			"issuer",
			p.Issuer,
			"error",
			err,
		)
		return fmt.Errorf("%w: %w", ErrOIDCDiscoveryFailed, err)
	}

	return Update(ctx, func(c *Config) error {
		for _, existing := range c.Auth.OIDC {
			if existing.Name == p.Name {
				return ErrOIDCProviderExists
			}
		}
		c.Auth.OIDC = append(c.Auth.OIDC, p)
		return nil
	})
}

// UpdateOIDCProvider merges the patch into the named provider. An empty/nil
// ClientSecret is treated as "keep existing" so the UI doesn't have to
// re-surface the secret just to let the admin change the issuer. When Issuer
// is patched the new URL is probed before the Update is written.
func UpdateOIDCProvider(
	ctx context.Context,
	name string,
	patch OIDCProviderPatch,
) error {
	if patch.Issuer != nil {
		if err := discoverOIDC(ctx, *patch.Issuer); err != nil {
			slog.WarnContext(
				ctx,
				"oidc discovery failed",
				"issuer",
				*patch.Issuer,
				"error",
				err,
			)
			return fmt.Errorf("%w: %w", ErrOIDCDiscoveryFailed, err)
		}
	}

	return Update(ctx, func(c *Config) error {
		idx := -1
		for i, p := range c.Auth.OIDC {
			if p.Name == name {
				idx = i
				break
			}
		}
		if idx < 0 {
			slog.WarnContext(ctx, "oidc provider not found", "name", name)
			return ErrOIDCProviderNotFound
		}
		p := c.Auth.OIDC[idx]
		if p.ClientSecretFile != "" && patch.ClientSecret != nil &&
			strings.TrimSpace(*patch.ClientSecret) != "" {
			return ErrSecretFileManaged
		}
		if patch.Issuer != nil {
			p.Issuer = *patch.Issuer
		}
		if patch.ClientID != nil {
			p.ClientID = *patch.ClientID
		}
		if patch.ClientSecret != nil &&
			strings.TrimSpace(*patch.ClientSecret) != "" {
			p.ClientSecret = *patch.ClientSecret
		}
		c.Auth.OIDC[idx] = p
		return nil
	})
}

// DeleteOIDCProvider removes the named provider from auth.oidc. Returns
// ErrOIDCProviderNotFound if no provider carries that name.
func DeleteOIDCProvider(ctx context.Context, name string) error {
	return Update(ctx, func(c *Config) error {
		found := false
		next := make([]OIDCConfig, 0, len(c.Auth.OIDC))
		for _, p := range c.Auth.OIDC {
			if p.Name == name {
				found = true
				continue
			}
			next = append(next, p)
		}
		if !found {
			slog.WarnContext(ctx, "oidc provider not found", "name", name)
			return ErrOIDCProviderNotFound
		}
		c.Auth.OIDC = next
		return nil
	})
}

// discoverOIDC fetches the issuer's well-known discovery document over the
// OTel-instrumented HTTP client. Returns the underlying error on transport
// failure or a synthesized error on non-200 status.
func discoverOIDC(ctx context.Context, issuer string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	url := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := otelx.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("discovery status %d", resp.StatusCode)
	}
	return nil
}
