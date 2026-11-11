package config

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	ErrExtensionExists   = errors.New("extension name already exists")
	ErrExtensionNotFound = errors.New("extension not found")
)

var (
	extensionName   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
	extensionSHA256 = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
)

func validateExtensionName(e ExtensionEntry) error {
	if !extensionName.MatchString(e.Name) {
		return fmt.Errorf(
			"extension name %q: want 1-32 lowercase letters, digits or dashes, not starting with a dash",
			e.Name,
		)
	}
	return nil
}

func validateExtension(e ExtensionEntry) error {
	if err := validateExtensionName(e); err != nil {
		return err
	}
	if e.SHA256 != "" && !extensionSHA256.MatchString(e.SHA256) {
		return fmt.Errorf("extension %q: sha256 must be 64 hex digits", e.Name)
	}
	switch {
	case filepath.IsAbs(e.Source):
	case strings.HasPrefix(e.Source, "https://"):
		u, err := url.Parse(e.Source)
		if err != nil || u.Host == "" {
			return fmt.Errorf(
				"extension %q: source %q is not a valid URL",
				e.Name,
				e.Source,
			)
		}
		if e.SHA256 == "" {
			return fmt.Errorf("extension %q: an https source needs sha256", e.Name)
		}
	case strings.HasPrefix(e.Source, "http://"):
		return fmt.Errorf("extension %q: source must be https, not http", e.Name)
	default:
		return fmt.Errorf(
			"extension %q: source must be an https URL or an absolute path, got %q",
			e.Name, e.Source,
		)
	}
	return nil
}

// checkExtensions holds a loaded entry to its name alone, which names a data
// directory. A bad source or checksum is the extension manager's to report as
// a failed extension, so one mistyped entry cannot keep Streamline from
// booting.
func (c *Config) checkExtensions() []error {
	var errs []error
	for _, e := range c.Extensions {
		if err := validateExtensionName(e); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

func AddExtension(ctx context.Context, e ExtensionEntry) error {
	if err := validateExtension(e); err != nil {
		return err
	}
	return Update(ctx, func(c *Config) error {
		for _, x := range c.Extensions {
			if x.Name == e.Name {
				return ErrExtensionExists
			}
		}
		c.Extensions = append(c.Extensions, e)
		slog.InfoContext(ctx, "extension added", "name", e.Name)
		return nil
	})
}

// UpdateExtension hands fn a copy of the named entry and stores what it
// leaves behind; the name cannot change.
func UpdateExtension(
	ctx context.Context,
	name string,
	fn func(*ExtensionEntry) error,
) error {
	return Update(ctx, func(c *Config) error {
		for i, x := range c.Extensions {
			if x.Name != name {
				continue
			}
			e := x
			if err := fn(&e); err != nil {
				return err
			}
			e.Name = name
			if err := validateExtension(e); err != nil {
				return err
			}
			c.Extensions[i] = e
			slog.InfoContext(ctx, "extension updated", "name", name)
			return nil
		}
		return ErrExtensionNotFound
	})
}

func DeleteExtension(ctx context.Context, name string) error {
	return Update(ctx, func(c *Config) error {
		next := make([]ExtensionEntry, 0, len(c.Extensions))
		found := false
		for _, x := range c.Extensions {
			if x.Name == name {
				found = true
				continue
			}
			next = append(next, x)
		}
		if !found {
			return ErrExtensionNotFound
		}
		c.Extensions = next
		slog.InfoContext(ctx, "extension deleted", "name", name)
		return nil
	})
}
