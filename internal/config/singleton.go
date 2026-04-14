package config

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
)

var (
	mu      sync.RWMutex
	current *Config
	cfgPath string
)

var (
	// ErrNoPath means the config was loaded from a reader (not a file path) so
	// Update has no file to write back to.
	ErrNoPath = errors.New("config has no backing file path")

	// ErrReadOnly means the config is declared read_only, so Update refuses
	// every write-back. Declarative/GitOps deploys mount config read-only and
	// change it through git, not the UI; runtime-generated state
	// (plex_client_id, session secret) is surfaced to the operator instead of
	// persisted here.
	ErrReadOnly = errors.New("config is read-only")
)

// Get returns a pointer to the current singleton config. Callers must treat
// the returned value as read-only; use Update to mutate.
func Get() *Config {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// store sets the singleton config (package-internal).
func store(c *Config, p string) {
	mu.Lock()
	defer mu.Unlock()
	current = c
	cfgPath = p
}

// ResetForTest clears the singleton. Tests only.
func ResetForTest() {
	mu.Lock()
	defer mu.Unlock()
	current = nil
	cfgPath = ""
}

// Update deep-clones the current config, runs fn, validates the result,
// writes it atomically to the backing file, then swaps the singleton.
// Returns ErrNoPath if no file path was captured at Load time.
// Update holds mu.Lock across the full clone/validate/write/swap sequence.
// This blocks concurrent Get readers for the duration of disk I/O; acceptable
// because Update is expected to fire only on admin-triggered settings changes.
func Update(ctx context.Context, fn func(*Config) error) error {
	mu.Lock()
	defer mu.Unlock()

	if current == nil {
		slog.ErrorContext(ctx, "config update called before load")
		return errors.New("config not loaded")
	}
	if current.ReadOnly {
		return ErrReadOnly
	}
	if cfgPath == "" {
		return ErrNoPath
	}

	cloned, err := cloneLocked()
	if err != nil {
		slog.ErrorContext(ctx, "config clone failed", "error", err)
		return fmt.Errorf("clone: %w", err)
	}
	if err := fn(cloned); err != nil {
		return err
	}
	if err := cloned.Validate(); err != nil {
		slog.ErrorContext(ctx, "config validation failed", "error", err)
		return fmt.Errorf("validate: %w", err)
	}
	if err := writeYAMLAtomic(cfgPath, cloned); err != nil {
		slog.ErrorContext(
			ctx,
			"failed to save config",
			"path",
			cfgPath,
			"error",
			err,
		)
		return fmt.Errorf("write: %w", err)
	}
	current = cloned
	slog.InfoContext(ctx, "config saved to disk", "path", cfgPath)
	return nil
}

func cloneLocked() (*Config, error) {
	k := koanf.New(".")
	if err := k.Load(structs.Provider(*current, "koanf"), nil); err != nil {
		return nil, err
	}
	var out Config
	if err := k.Unmarshal("", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// writeYAMLAtomic marshals c to YAML and persists it to dest. It stages the
// bytes in a temp file under the OS temp dir, then renames it into place —
// atomic when dest shares the temp dir's filesystem. When it does not (EXDEV),
// or dest is a single-file bind mount whose parent dir is read-only so rename
// cannot replace it (EBUSY), it overwrites dest in place instead. Staging in
// the OS temp dir rather than beside dest is what lets a read-only config
// directory (e.g. a Docker single-file bind mount) still be updated.
// Does not fsync — config writes are admin-driven and infrequent; a torn write
// on power loss is acceptable.
func writeYAMLAtomic(dest string, c *Config) error {
	k := koanf.New(".")
	if err := k.Load(structs.Provider(*c, "koanf"), nil); err != nil {
		return err
	}
	data, err := k.Marshal(yaml.Parser())
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp("", "streamline-config-*.yaml")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		return errors.Join(err, tmp.Close(), os.Remove(tmpName))
	}
	if err := tmp.Close(); err != nil {
		return errors.Join(err, os.Remove(tmpName))
	}

	if err := os.Rename(tmpName, dest); err != nil {
		// Different filesystem (EXDEV) or single-file bind mount (EBUSY):
		// the rename cannot land, so overwrite dest in place.
		return errors.Join(os.WriteFile(dest, data, 0o600), os.Remove(tmpName))
	}
	return nil
}
