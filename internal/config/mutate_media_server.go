package config

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	ErrMediaServerExists   = errors.New("media server name already exists")
	ErrMediaServerNotFound = errors.New("media server not found")

	ErrLibrarySectionNotPlex = errors.New(
		"library_section and library_section_tv are only valid for Plex servers",
	)
)

// generatePlexClientID returns a 32-char hex string used as the
// X-Plex-Client-Identifier header on Plex auth requests.
func generatePlexClientID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func hasPlexServer(c *Config) bool {
	for _, s := range c.MediaServer.Servers {
		if s.ServerType == "plex" {
			return true
		}
	}
	return false
}

// ensurePlexClientID assigns a fresh Plex client identifier when c has a Plex
// server configured but no id yet. No-op otherwise. Runs inside an Update
// closure so the id persists in the same atomic write as the change that added
// the server.
func ensurePlexClientID(c *Config) error {
	if c.MediaServer.PlexClientID != "" || !hasPlexServer(c) {
		return nil
	}
	id, err := generatePlexClientID()
	if err != nil {
		return err
	}
	c.MediaServer.PlexClientID = id
	return nil
}

// EnsurePlexClientID generates and persists a Plex client identifier when a
// Plex server is already configured but none exists yet (e.g. on boot from a
// config that lists a Plex server). No-op — and no disk write — when no Plex
// server is present or an id already exists.
func EnsurePlexClientID(ctx context.Context) error {
	cfg := Get()
	if cfg.MediaServer.PlexClientID != "" || !hasPlexServer(cfg) {
		return nil
	}
	return Update(ctx, ensurePlexClientID)
}

// MediaServerPatch carries optional field updates. A blank APIKey preserves
// the existing token, unless the host moves to another destination: then the
// token must be entered again (ErrSecretRequired).
type MediaServerPatch struct {
	ServerType       *string
	Host             *string
	APIKey           *string
	Enabled          *bool
	LibrarySection   *string
	LibrarySectionTV *string
	// PathMappings replaces the whole list when non-nil; a pointer to an empty
	// slice clears it.
	PathMappings *[]MediaServerPathMapping
}

// validateMediaServer enforces cross-field invariants on the resulting entry.
// Update calls it after merging the patch so it sees the resulting
// server_type, not the one the patch happened to carry.
//
// A blank library_section is normalized away instead of rejected: a JSON null
// decodes to "field unchanged", so an empty string is the only way for a
// client to clear the section — which is what a Plex → Jellyfin/Emby type
// switch needs to stay valid.
func validateMediaServer(e *MediaServerEntry) error {
	if e.LibrarySection != nil && *e.LibrarySection == "" {
		e.LibrarySection = nil
	}
	if e.LibrarySectionTV != nil && *e.LibrarySectionTV == "" {
		e.LibrarySectionTV = nil
	}
	if (e.LibrarySection != nil || e.LibrarySectionTV != nil) &&
		e.ServerType != "plex" {
		return ErrLibrarySectionNotPlex
	}
	return checkMediaServerPathMappings(e.PathMappings)
}

// windowsAbsPath matches a drive-rooted (D:\Movies, D:/Movies) or UNC
// (\\nas\movies) path: a media server on Windows reports its folders that way,
// and filepath.IsAbs on this side would refuse them.
var windowsAbsPath = regexp.MustCompile(`^([A-Za-z]:[\\/]|\\\\[^\\])`)

// checkMediaServerPathMappings rejects a mapping list before it is stored.
// from is matched against paths this process builds, so it must be absolute
// here; to only travels to the server, so a Windows path is accepted for it.
func checkMediaServerPathMappings(v []MediaServerPathMapping) error {
	for i, m := range v {
		if strings.TrimSpace(m.From) == "" {
			return fmt.Errorf("path_mappings[%d].from: must not be empty", i)
		}
		if !filepath.IsAbs(m.From) {
			return fmt.Errorf(
				"path_mappings[%d].from: must be an absolute path, got %q",
				i, m.From,
			)
		}
		if strings.TrimSpace(m.To) == "" {
			return fmt.Errorf("path_mappings[%d].to: must not be empty", i)
		}
		if !strings.HasPrefix(m.To, "/") && !windowsAbsPath.MatchString(m.To) {
			return fmt.Errorf(
				"path_mappings[%d].to: must be an absolute path, got %q",
				i, m.To,
			)
		}
	}
	return nil
}

func AddMediaServer(ctx context.Context, e MediaServerEntry) error {
	if err := validateMediaServer(&e); err != nil {
		return err
	}
	return Update(ctx, func(c *Config) error {
		for _, x := range c.MediaServer.Servers {
			if x.Name == e.Name {
				return ErrMediaServerExists
			}
		}
		c.MediaServer.Servers = append(c.MediaServer.Servers, e)
		if err := ensurePlexClientID(c); err != nil {
			return err
		}
		slog.InfoContext(ctx, "media server added", "name", e.Name)
		return nil
	})
}

func UpdateMediaServer(ctx context.Context, name string, p MediaServerPatch) error {
	return Update(ctx, func(c *Config) error {
		idx := -1
		for i, x := range c.MediaServer.Servers {
			if x.Name == name {
				idx = i
				break
			}
		}
		if idx < 0 {
			return ErrMediaServerNotFound
		}
		e := c.MediaServer.Servers[idx]
		if e.APIKeyFile != "" && p.APIKey != nil && *p.APIKey != "" {
			return ErrSecretFileManaged
		}
		if p.ServerType != nil {
			e.ServerType = *p.ServerType
		}
		if p.Host != nil {
			e.Host = *p.Host
		}
		if p.APIKey != nil && *p.APIKey != "" {
			e.APIKey = *p.APIKey
		}
		if p.Enabled != nil {
			e.Enabled = *p.Enabled
		}
		if p.LibrarySection != nil {
			e.LibrarySection = p.LibrarySection
		}
		if p.LibrarySectionTV != nil {
			e.LibrarySectionTV = p.LibrarySectionTV
		}
		if p.PathMappings != nil {
			e.PathMappings = *p.PathMappings
		}
		if urlDestination(
			e.Host,
		) != urlDestination(
			c.MediaServer.Servers[idx].Host,
		) {
			if err := guardMovedSecret(
				&e.APIKey,
				e.APIKeyFile,
				p.APIKey,
				true,
			); err != nil {
				return err
			}
		}
		if err := validateMediaServer(&e); err != nil {
			return err
		}
		c.MediaServer.Servers[idx] = e
		if err := ensurePlexClientID(c); err != nil {
			return err
		}
		slog.InfoContext(ctx, "media server updated", "name", name)
		return nil
	})
}

func DeleteMediaServer(ctx context.Context, name string) error {
	return Update(ctx, func(c *Config) error {
		found := false
		next := make([]MediaServerEntry, 0, len(c.MediaServer.Servers))
		for _, x := range c.MediaServer.Servers {
			if x.Name == name {
				found = true
				continue
			}
			next = append(next, x)
		}
		if !found {
			return ErrMediaServerNotFound
		}
		c.MediaServer.Servers = next
		slog.InfoContext(ctx, "media server deleted", "name", name)
		return nil
	})
}
