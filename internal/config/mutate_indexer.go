package config

import (
	"context"
	"errors"
	"log/slog"
	"maps"

	"github.com/datahearth/streamline/internal/cardigann/definitions"
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
	Private  *bool
	// Cardigann edits a built-in indexer's block; nil leaves it alone.
	Cardigann *CardigannPatch
}

// CardigannPatch edits a built-in indexer's definition block. Settings
// replaces the settings — but a secret one sent empty or left out keeps its
// stored value, since the API never returns one to send back — unless the
// definition changes, which starts the settings over. Proxy "" clears it.
type CardigannPatch struct {
	Definition *string
	Site       *string
	Settings   map[string]string
	Proxy      *string
}

// apply merges p into the block it edits, under the definitions in force
// in dir.
func (p CardigannPatch) apply(
	dir string,
	old *CardigannIndexer,
) (*CardigannIndexer, error) {
	next := &CardigannIndexer{}
	if old != nil {
		*next = *old
		next.Settings = maps.Clone(old.Settings)
	}
	if p.Definition != nil && *p.Definition != next.Definition {
		next.Definition, next.Settings, next.Site = *p.Definition, nil, ""
	}
	if p.Site != nil {
		next.Site = *p.Site
	}
	if p.Proxy != nil {
		next.Proxy = *p.Proxy
	}
	if p.Settings == nil {
		return next, nil
	}
	def, err := definitions.Load(dir, next.Definition)
	if err != nil {
		return nil, err
	}
	merged := maps.Clone(p.Settings)
	for _, s := range def.Settings {
		if !s.Secret || merged[s.Name] != "" {
			continue
		}
		if v, ok := next.Settings[s.Name]; ok {
			merged[s.Name] = v
		} else {
			delete(merged, s.Name)
		}
	}
	next.Settings = merged
	return next, nil
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
			if e.Protocol != "cardigann" {
				e.Cardigann = nil
			}
		}
		if p.Priority != nil {
			e.Priority = *p.Priority
		}
		if p.Enabled != nil {
			e.Enabled = *p.Enabled
		}
		if p.Private != nil {
			e.Private = *p.Private
		}
		if p.Cardigann != nil {
			next, err := p.Cardigann.apply(c.CardigannDir(), e.Cardigann)
			if err != nil {
				return err
			}
			e.Cardigann = next
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
