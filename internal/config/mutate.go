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
	// Lockout is itself optional-partial: a nil Lockout leaves the whole
	// section untouched, and a non-nil one with a single field set leaves the
	// others alone.
	Lockout *LockoutPatch
}

// LockoutPatch carries optional field updates to the auth.lockout section.
// Nil fields are left untouched.
type LockoutPatch struct {
	Threshold *uint8
	Window    *string
	Duration  *string
}

// LibraryPatch carries optional field updates to the library section. Nil
// fields are left untouched.
type LibraryPatch struct {
	MonitorSpecials *bool
	// The three roots are pathmigrate's to write. It rewrites every stored
	// path first and repoints the root afterwards; a caller that sets one of
	// these on its own leaves the library pointing at the old prefix.
	MoviePath            *string
	SeriesPath           *string
	DownloadPath         *string
	MovieNaming          *string
	SeriesNaming         *string
	ImportMode           *string
	KeepTorrentSeeding   *bool
	NoMatchCooldown      *string
	MaxGrabFailures      *uint8
	ImportMaxAttempts    *uint8
	DriftGraceTicks      *uint8
	AllowedDownloadRoots *[]string
	// Probe is itself optional-partial: a nil Probe leaves the whole section
	// untouched, and a non-nil Probe with only one field set leaves the
	// other alone.
	Probe *ProbePatch
}

// DownloadPatch carries optional field updates to the download section. Nil
// fields are left untouched.
type DownloadPatch struct {
	SelectiveFiles *bool
	SelectionGrace *string
}

// MetadataPatch carries optional field updates to the metadata section. A
// blank api key preserves the stored one — the UI never shows the current
// value, so blank means "unchanged."
type MetadataPatch struct {
	TMDBAPIKey *string
	TVDBAPIKey *string
	Language   *string
	TMDBRegion *string
}

// ProbePatch carries optional field updates to the library.probe section.
// Nil fields are left untouched.
type ProbePatch struct {
	AlwaysAsk        *bool
	MinDurationRatio *float64
}

// FFmpegPatch carries optional field updates to the ffmpeg section. Nil
// fields are left untouched.
type FFmpegPatch struct {
	Enabled *bool
	Path    *string
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

// checkDuration rejects a patched Go duration string before it reaches the
// config, naming the key it came from. Nothing else would: Validate requires
// these fields to be non-empty and never parses one, so a typo written here
// lands on disk and surfaces as a job silently reverting to its fallback —
// or, for the ones with no fallback, not at all.
func checkDuration(key string, v *string) error {
	if v == nil {
		return nil
	}
	if _, err := time.ParseDuration(*v); err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	return nil
}

// UpdateAuth validates the patch, merges it into the auth section, and
// persists via Update. Returns the resulting AuthConfig on success so callers
// can echo the new state back to the client without a second Get.
func UpdateAuth(ctx context.Context, patch AuthPatch) (AuthConfig, error) {
	var lockout LockoutPatch
	if patch.Lockout != nil {
		lockout = *patch.Lockout
	}
	if err := errors.Join(
		checkDuration("session_ttl", patch.SessionTTL),
		checkDuration("lockout.window", lockout.Window),
		checkDuration("lockout.duration", lockout.Duration),
	); err != nil {
		return AuthConfig{}, err
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
		if lockout.Threshold != nil {
			c.Auth.Lockout.Threshold = *lockout.Threshold
		}
		if lockout.Window != nil {
			c.Auth.Lockout.Window = *lockout.Window
		}
		if lockout.Duration != nil {
			c.Auth.Lockout.Duration = *lockout.Duration
		}
		out = c.Auth
		return nil
	})
	if err != nil {
		return AuthConfig{}, err
	}
	return out, nil
}

// UpdateLibrary merges the patch into the library section and persists it.
// Returns the resulting LibraryConfig so callers can echo the new state back.
func UpdateLibrary(ctx context.Context, patch LibraryPatch) (LibraryConfig, error) {
	if err := checkDuration(
		"no_match_cooldown", patch.NoMatchCooldown,
	); err != nil {
		return LibraryConfig{}, err
	}

	var out LibraryConfig
	err := Update(ctx, func(c *Config) error {
		if patch.MonitorSpecials != nil {
			c.Library.MonitorSpecials = *patch.MonitorSpecials
		}
		if patch.MoviePath != nil {
			c.Library.MoviePath = *patch.MoviePath
		}
		if patch.SeriesPath != nil {
			c.Library.SeriesPath = *patch.SeriesPath
		}
		if patch.DownloadPath != nil {
			c.Library.DownloadPath = *patch.DownloadPath
		}
		if patch.MovieNaming != nil {
			c.Library.MovieNaming = *patch.MovieNaming
		}
		if patch.SeriesNaming != nil {
			c.Library.SeriesNaming = *patch.SeriesNaming
		}
		if patch.ImportMode != nil {
			c.Library.ImportMode = *patch.ImportMode
		}
		if patch.KeepTorrentSeeding != nil {
			c.Library.KeepTorrentSeeding = *patch.KeepTorrentSeeding
		}
		if patch.NoMatchCooldown != nil {
			c.Library.NoMatchCooldown = *patch.NoMatchCooldown
		}
		if patch.MaxGrabFailures != nil {
			c.Library.MaxGrabFailures = *patch.MaxGrabFailures
		}
		if patch.ImportMaxAttempts != nil {
			c.Library.ImportMaxAttempts = *patch.ImportMaxAttempts
		}
		if patch.DriftGraceTicks != nil {
			c.Library.DriftGraceTicks = *patch.DriftGraceTicks
		}
		if patch.AllowedDownloadRoots != nil {
			c.Library.AllowedDownloadRoots = *patch.AllowedDownloadRoots
		}
		if patch.Probe != nil {
			if patch.Probe.AlwaysAsk != nil {
				c.Library.Probe.AlwaysAsk = *patch.Probe.AlwaysAsk
			}
			if patch.Probe.MinDurationRatio != nil {
				c.Library.Probe.MinDurationRatio = *patch.Probe.MinDurationRatio
			}
		}
		out = c.Library
		return nil
	})
	if err != nil {
		return LibraryConfig{}, err
	}
	return out, nil
}

// UpdateFFmpeg merges the patch into the ffmpeg section and persists it.
// Returns the resulting FFmpegConfig so callers can echo the new state back.
//
// Path takes effect only on the next process start: the Prober handed to the
// importer, hygiene backfill job, and restapi Server is constructed once at
// boot from the old path, so a changed path here doesn't move what those
// already resolved.
func UpdateFFmpeg(ctx context.Context, patch FFmpegPatch) (FFmpegConfig, error) {
	var out FFmpegConfig
	err := Update(ctx, func(c *Config) error {
		if patch.Enabled != nil {
			c.FFmpeg.Enabled = *patch.Enabled
		}
		if patch.Path != nil {
			c.FFmpeg.Path = *patch.Path
		}
		out = c.FFmpeg
		return nil
	})
	if err != nil {
		return FFmpegConfig{}, err
	}
	return out, nil
}

// UpdateDownload merges the patch into the download section and persists it.
// Returns the resulting DownloadConfig so callers can echo the new state back.
func UpdateDownload(
	ctx context.Context,
	patch DownloadPatch,
) (DownloadConfig, error) {
	if err := checkDuration(
		"selection_grace", patch.SelectionGrace,
	); err != nil {
		return DownloadConfig{}, err
	}

	var out DownloadConfig
	err := Update(ctx, func(c *Config) error {
		if patch.SelectiveFiles != nil {
			c.Download.SelectiveFiles = *patch.SelectiveFiles
		}
		if patch.SelectionGrace != nil {
			c.Download.SelectionGrace = *patch.SelectionGrace
		}
		out = c.Download
		return nil
	})
	if err != nil {
		return DownloadConfig{}, err
	}
	return out, nil
}

// UpdateMetadata merges the patch into the metadata section and persists it.
// A blank api key preserves the stored one, and setting one inline while the
// matching *_file path is configured returns ErrSecretFileManaged — the file
// is the source of truth, and the loader would read straight past an inline
// value anyway (see SecretValue).
//
// Nothing here applies before a restart: metadata.NewTMDB and metadata.NewTVDB
// read these four keys once, when wire.go constructs the clients.
func UpdateMetadata(
	ctx context.Context,
	patch MetadataPatch,
) (MetadataConfig, error) {
	var out MetadataConfig
	err := Update(ctx, func(c *Config) error {
		set := func(dst *string, file string, v *string) error {
			if v == nil || strings.TrimSpace(*v) == "" {
				return nil
			}
			if file != "" {
				return ErrSecretFileManaged
			}
			*dst = *v
			return nil
		}
		if err := set(
			&c.Metadata.TMDBAPIKey, c.Metadata.TMDBAPIKeyFile, patch.TMDBAPIKey,
		); err != nil {
			return err
		}
		if err := set(
			&c.Metadata.TVDBAPIKey, c.Metadata.TVDBAPIKeyFile, patch.TVDBAPIKey,
		); err != nil {
			return err
		}
		if patch.Language != nil {
			c.Metadata.Language = *patch.Language
		}
		if patch.TMDBRegion != nil {
			c.Metadata.TMDBRegion = *patch.TMDBRegion
		}
		out = c.Metadata
		return nil
	})
	if err != nil {
		return MetadataConfig{}, err
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
