package config

import (
	"context"
	"errors"
	"log/slog"
)

var (
	ErrDownloadClientExists   = errors.New("download client name already exists")
	ErrDownloadClientNotFound = errors.New("download client not found")
)

// DownloadClientPatch carries optional field updates. Blank Password/APIKey
// preserve the existing secret, unless host, port or TLS move the client to
// another destination: then the secret it sends must be entered again
// (ErrSecretRequired) and the one it does not send is dropped.
type DownloadClientPatch struct {
	ClientType *string
	Host       *string
	Port       *uint16
	AuthMethod *string
	Username   *string
	Password   *string
	APIKey     *string
	UseSSL     *bool
	Priority   *uint8
	Enabled    *bool

	// builtin-only knobs (client_type "builtin").
	DownloadDir     *string
	ListenPort      *uint16
	MaxUploadKbps   *int
	MaxDownloadKbps *int
	SeedRatio       *float64
	SeedTime        *string
	DisableDHT      *bool
	BindInterface   *string
}

func AddDownloadClient(ctx context.Context, e DownloadClientEntry) error {
	return Update(ctx, func(c *Config) error {
		for _, x := range c.DownloadClients {
			if x.Name == e.Name {
				return ErrDownloadClientExists
			}
		}
		c.DownloadClients = append(c.DownloadClients, e)
		slog.InfoContext(ctx, "download client added", "name", e.Name)
		return nil
	})
}

func UpdateDownloadClient(
	ctx context.Context,
	name string,
	p DownloadClientPatch,
) error {
	return Update(ctx, func(c *Config) error {
		idx := -1
		for i, x := range c.DownloadClients {
			if x.Name == name {
				idx = i
				break
			}
		}
		if idx < 0 {
			return ErrDownloadClientNotFound
		}
		e := c.DownloadClients[idx]
		if e.PasswordFile != "" && p.Password != nil && *p.Password != "" {
			return ErrSecretFileManaged
		}
		if e.APIKeyFile != "" && p.APIKey != nil && *p.APIKey != "" {
			return ErrSecretFileManaged
		}
		if p.ClientType != nil {
			e.ClientType = *p.ClientType
		}
		if p.Host != nil {
			e.Host = *p.Host
		}
		if p.Port != nil {
			e.Port = *p.Port
		}
		if p.AuthMethod != nil {
			e.AuthMethod = *p.AuthMethod
		}
		if p.Username != nil {
			e.Username = *p.Username
		}
		if p.Password != nil && *p.Password != "" {
			e.Password = *p.Password
		}
		if p.APIKey != nil && *p.APIKey != "" {
			e.APIKey = *p.APIKey
		}
		if p.UseSSL != nil {
			e.UseSSL = *p.UseSSL
		}
		if p.Priority != nil {
			e.Priority = *p.Priority
		}
		if p.Enabled != nil {
			e.Enabled = *p.Enabled
		}
		if p.DownloadDir != nil {
			e.DownloadDir = *p.DownloadDir
		}
		if p.ListenPort != nil {
			e.ListenPort = *p.ListenPort
		}
		if p.MaxUploadKbps != nil {
			e.MaxUploadKbps = *p.MaxUploadKbps
		}
		if p.MaxDownloadKbps != nil {
			e.MaxDownloadKbps = *p.MaxDownloadKbps
		}
		if p.SeedRatio != nil {
			e.SeedRatio = *p.SeedRatio
		}
		if p.SeedTime != nil {
			e.SeedTime = *p.SeedTime
		}
		if p.DisableDHT != nil {
			e.DisableDHT = *p.DisableDHT
		}
		if p.BindInterface != nil {
			e.BindInterface = *p.BindInterface
		}
		if downloadClientDestination(
			e,
		) != downloadClientDestination(
			c.DownloadClients[idx],
		) {
			usesKey := e.ClientType == "qbittorrent" && e.AuthMethod == "api_key"
			usesPassword := e.ClientType != "builtin" && !usesKey
			if err := guardMovedSecret(
				&e.Password, e.PasswordFile, p.Password, usesPassword,
			); err != nil {
				return err
			}
			if err := guardMovedSecret(
				&e.APIKey,
				e.APIKeyFile,
				p.APIKey,
				usesKey,
			); err != nil {
				return err
			}
		}
		c.DownloadClients[idx] = e
		slog.InfoContext(ctx, "download client updated", "name", name)
		return nil
	})
}

// downloadClientDestination is where the client's requests go. The builtin
// engine sends none, whatever host an earlier client type left behind.
func downloadClientDestination(e DownloadClientEntry) string {
	if e.ClientType == "builtin" {
		return ""
	}
	return hostDestination(e.Host, e.Port, e.UseSSL)
}

func DeleteDownloadClient(ctx context.Context, name string) error {
	return Update(ctx, func(c *Config) error {
		found := false
		next := make([]DownloadClientEntry, 0, len(c.DownloadClients))
		for _, x := range c.DownloadClients {
			if x.Name == name {
				found = true
				continue
			}
			next = append(next, x)
		}
		if !found {
			return ErrDownloadClientNotFound
		}
		c.DownloadClients = next
		slog.InfoContext(ctx, "download client deleted", "name", name)
		return nil
	})
}
