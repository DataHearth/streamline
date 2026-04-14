package config

import (
	"context"
	"errors"
	"log/slog"
)

var (
	ErrIndexerExists   = errors.New("indexer name already exists")
	ErrIndexerNotFound = errors.New("indexer not found")
)

// IndexerPatch carries optional field updates. A blank APIKey preserves the
// existing key.
type IndexerPatch struct {
	Host     *string
	Port     *uint16
	Path     *string
	UseSSL   *bool
	APIKey   *string
	Protocol *string
	Priority *uint8
	Enabled  *bool
}

func AddIndexer(ctx context.Context, e IndexerEntry) error {
	return Update(ctx, func(c *Config) error {
		for _, x := range c.Indexers {
			if x.Name == e.Name {
				return ErrIndexerExists
			}
		}
		c.Indexers = append(c.Indexers, e)
		slog.InfoContext(ctx, "indexer added", "name", e.Name)
		return nil
	})
}

func UpdateIndexer(ctx context.Context, name string, p IndexerPatch) error {
	return Update(ctx, func(c *Config) error {
		idx := -1
		for i, x := range c.Indexers {
			if x.Name == name {
				idx = i
				break
			}
		}
		if idx < 0 {
			return ErrIndexerNotFound
		}
		e := c.Indexers[idx]
		if e.APIKeyFile != "" && p.APIKey != nil && *p.APIKey != "" {
			return ErrSecretFileManaged
		}
		if p.Host != nil {
			e.Host = *p.Host
		}
		if p.Port != nil {
			e.Port = *p.Port
		}
		if p.Path != nil {
			e.Path = *p.Path
		}
		if p.UseSSL != nil {
			e.UseSSL = *p.UseSSL
		}
		if p.APIKey != nil && *p.APIKey != "" {
			e.APIKey = *p.APIKey
		}
		if p.Protocol != nil {
			e.Protocol = *p.Protocol
		}
		if p.Priority != nil {
			e.Priority = *p.Priority
		}
		if p.Enabled != nil {
			e.Enabled = *p.Enabled
		}
		c.Indexers[idx] = e
		slog.InfoContext(ctx, "indexer updated", "name", name)
		return nil
	})
}

func DeleteIndexer(ctx context.Context, name string) error {
	return Update(ctx, func(c *Config) error {
		found := false
		next := make([]IndexerEntry, 0, len(c.Indexers))
		for _, x := range c.Indexers {
			if x.Name == name {
				found = true
				continue
			}
			next = append(next, x)
		}
		if !found {
			return ErrIndexerNotFound
		}
		c.Indexers = next
		slog.InfoContext(ctx, "indexer deleted", "name", name)
		return nil
	})
}
