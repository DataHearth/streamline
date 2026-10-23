package config

import (
	"context"
	"errors"
	"log/slog"
	"slices"
)

var (
	ErrBookQualityProfileExists = errors.New(
		"book quality profile name already exists",
	)
	ErrBookQualityProfileNotFound = errors.New(
		"book quality profile not found",
	)
	ErrBookQualityProfileInUseAsDefault = errors.New(
		"book quality profile is the configured default for a book kind",
	)
	ErrBookKindUnknown = errors.New("unknown book kind")
)

// BookQualityProfilePatch carries optional field updates; a nil field is
// left alone. A slot replaces the whole slot.
type BookQualityProfilePatch struct {
	UpgradeAllowed *bool
	Ebook          *EbookSlot
	Audiobook      *AudiobookSlot
}

func canonicalBookSlots(e *BookQualityProfileEntry) {
	e.Ebook.Formats = canonical(e.Ebook.Formats, EbookFormats)
	e.Audiobook.Formats = canonical(e.Audiobook.Formats, AudiobookFormats)
}

// AddBookQualityProfile appends e with its formats in canonical order. When a
// book kind has no resolvable default, the new profile becomes it, so a stock
// install can add a book as soon as one profile exists.
func AddBookQualityProfile(ctx context.Context, e BookQualityProfileEntry) error {
	canonicalBookSlots(&e)
	return Update(ctx, func(c *Config) error {
		for _, x := range c.BookQualityProfiles {
			if x.Name == e.Name {
				return ErrBookQualityProfileExists
			}
		}
		c.BookQualityProfiles = append(c.BookQualityProfiles, e)
		for _, k := range BookKinds {
			if _, ok := findBookProfile(
				c.BookQualityProfiles, c.BookQualityDefaultProfiles.For(k),
			); !ok {
				c.BookQualityDefaultProfiles.set(k, e.Name)
			}
		}
		slog.InfoContext(ctx, "book quality profile added", "name", e.Name)
		return nil
	})
}

func UpdateBookQualityProfile(
	ctx context.Context,
	name string,
	p BookQualityProfilePatch,
) error {
	return Update(ctx, func(c *Config) error {
		idx := slices.IndexFunc(
			c.BookQualityProfiles,
			func(x BookQualityProfileEntry) bool { return x.Name == name },
		)
		if idx < 0 {
			return ErrBookQualityProfileNotFound
		}
		e := c.BookQualityProfiles[idx]
		if p.UpgradeAllowed != nil {
			e.UpgradeAllowed = *p.UpgradeAllowed
		}
		if p.Ebook != nil {
			e.Ebook = *p.Ebook
		}
		if p.Audiobook != nil {
			e.Audiobook = *p.Audiobook
		}
		canonicalBookSlots(&e)
		c.BookQualityProfiles[idx] = e
		slog.InfoContext(ctx, "book quality profile updated", "name", name)
		return nil
	})
}

// SetDefaultBookQualityProfile points the default of kind at name. It is also
// the only way to free a default for deletion.
func SetDefaultBookQualityProfile(ctx context.Context, name, kind string) error {
	if !slices.Contains(BookKinds, kind) {
		return ErrBookKindUnknown
	}
	return Update(ctx, func(c *Config) error {
		if _, ok := findBookProfile(c.BookQualityProfiles, name); !ok {
			return ErrBookQualityProfileNotFound
		}
		c.BookQualityDefaultProfiles.set(kind, name)
		slog.InfoContext(ctx, "default book quality profile set",
			"name", name, "kind", kind)
		return nil
	})
}

func DeleteBookQualityProfile(ctx context.Context, name string) error {
	return Update(ctx, func(c *Config) error {
		if len(c.BookDefaultFor(name)) > 0 {
			return ErrBookQualityProfileInUseAsDefault
		}
		found := false
		next := make([]BookQualityProfileEntry, 0, len(c.BookQualityProfiles))
		for _, x := range c.BookQualityProfiles {
			if x.Name == name {
				found = true
				continue
			}
			next = append(next, x)
		}
		if !found {
			return ErrBookQualityProfileNotFound
		}
		c.BookQualityProfiles = next
		slog.InfoContext(ctx, "book quality profile deleted", "name", name)
		return nil
	})
}
