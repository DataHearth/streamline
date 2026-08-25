package config

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/datahearth/streamline/internal/quality"
)

var (
	ErrCustomFormatExists   = errors.New("custom format name already exists")
	ErrCustomFormatNotFound = errors.New("custom format not found")
	ErrCustomFormatBuiltin  = errors.New(
		"name collides with a built-in format",
	)
	ErrCustomFormatInUse = errors.New(
		"custom format is scored by a quality profile",
	)
)

func AddCustomFormat(ctx context.Context, e CustomFormatEntry) error {
	if quality.IsBuiltinName(e.Name) {
		return ErrCustomFormatBuiltin
	}
	if _, err := e.ToFormat(); err != nil {
		return err
	}
	return Update(ctx, func(c *Config) error {
		for _, x := range c.CustomFormats {
			if x.Name == e.Name {
				return ErrCustomFormatExists
			}
		}
		c.CustomFormats = append(c.CustomFormats, e)
		slog.InfoContext(ctx, "custom format added", "name", e.Name)
		return nil
	})
}

// UpdateCustomFormat is full-replace (PUT semantics): e replaces the entry
// named name wholesale. e.Name must equal name — this endpoint doesn't
// rename, matching every other config resource, where a rename simply has
// no patch field to carry it.
func UpdateCustomFormat(
	ctx context.Context,
	name string,
	e CustomFormatEntry,
) error {
	if quality.IsBuiltinName(name) {
		return ErrCustomFormatBuiltin
	}
	if e.Name != name {
		return fmt.Errorf(
			"custom format entry name %q does not match %q", e.Name, name,
		)
	}
	if _, err := e.ToFormat(); err != nil {
		return err
	}
	return Update(ctx, func(c *Config) error {
		idx := -1
		for i, x := range c.CustomFormats {
			if x.Name == name {
				idx = i
				break
			}
		}
		if idx < 0 {
			return ErrCustomFormatNotFound
		}
		c.CustomFormats[idx] = e
		slog.InfoContext(ctx, "custom format updated", "name", name)
		return nil
	})
}

func DeleteCustomFormat(ctx context.Context, name string) error {
	if quality.IsBuiltinName(name) {
		return ErrCustomFormatBuiltin
	}
	return Update(ctx, func(c *Config) error {
		for _, p := range c.QualityProfiles {
			for _, fs := range p.Formats {
				if fs.Name == name {
					return ErrCustomFormatInUse
				}
			}
		}
		found := false
		next := make([]CustomFormatEntry, 0, len(c.CustomFormats))
		for _, x := range c.CustomFormats {
			if x.Name == name {
				found = true
				continue
			}
			next = append(next, x)
		}
		if !found {
			return ErrCustomFormatNotFound
		}
		c.CustomFormats = next
		slog.InfoContext(ctx, "custom format deleted", "name", name)
		return nil
	})
}
