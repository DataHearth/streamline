package config

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
)

var (
	ErrMediaServerExists   = errors.New("media server name already exists")
	ErrMediaServerNotFound = errors.New("media server not found")
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
// the existing token.
type MediaServerPatch struct {
	ServerType     *string
	Host           *string
	APIKey         *string
	Enabled        *bool
	LibrarySection *string
}

func AddMediaServer(ctx context.Context, e MediaServerEntry) error {
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
