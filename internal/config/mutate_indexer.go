package config

import (
	"context"
	"errors"
	"log/slog"
	"maps"

	"github.com/datahearth/streamline/internal/cardigann/definitions"
	"github.com/datahearth/streamline/internal/cardigann/engine"
)

var (
	ErrIndexerExists   = errors.New("indexer name already exists")
	ErrIndexerNotFound = errors.New("indexer not found")
)

// IndexerPatch carries optional field updates. A blank APIKey preserves the
// existing key, unless host, port or TLS move it to another destination: then
// the key must be entered again (ErrSecretRequired).
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
// definition changes, which starts the settings over, or the edit moves
// where the secrets are sent: another of the definition's links, or a
// proxy they will now pass through. Then every stored secret must be sent
// again (ErrSecretRequired), as a moved host asks for a Torznab key. Proxy
// "" clears it.
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
	if p.Settings == nil && len(next.Settings) == 0 {
		return next, nil
	}
	def, err := definitions.Load(dir, next.Definition)
	if err != nil {
		return nil, err
	}
	moved := false
	if old != nil && next.Definition == old.Definition {
		from, err := engine.Site(def, old.Site)
		if err != nil {
			return nil, err
		}
		to, err := engine.Site(def, next.Site)
		if err != nil {
			return nil, err
		}
		moved = from.String() != to.String() || next.Proxy != old.Proxy
	}
	// Non-secret settings come from the patch when it carries any, else
	// stay; a secret only ever comes from the patch or the store.
	merged := maps.Clone(next.Settings)
	if p.Settings != nil {
		merged = maps.Clone(p.Settings)
	}
	for _, s := range def.Settings {
		if !s.Secret {
			continue
		}
		if p.Settings[s.Name] != "" {
			merged[s.Name] = p.Settings[s.Name]
			continue
		}
		stored := next.Settings[s.Name]
		switch {
		case stored == "":
			delete(merged, s.Name)
		case moved:
			return nil, ErrSecretRequired
		default:
			merged[s.Name] = stored
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
		if e.Protocol == "cardigann" {
			if err := CheckCardigann(c.CardigannDir(), e); err != nil {
				return err
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
		prev := c.Indexers[idx]
		if hostDestination(e.Host, e.Port, e.UseSSL) !=
			hostDestination(prev.Host, prev.Port, prev.UseSSL) {
			if err := guardMovedSecret(
				&e.APIKey,
				e.APIKeyFile,
				p.APIKey,
				true,
			); err != nil {
				return err
			}
		}
		if p.Cardigann != nil {
			next, err := p.Cardigann.apply(c.CardigannDir(), e.Cardigann)
			if err != nil {
				return err
			}
			e.Cardigann = next
		}
		if e.Protocol == "cardigann" {
			if err := CheckCardigann(c.CardigannDir(), e); err != nil {
				return err
			}
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
