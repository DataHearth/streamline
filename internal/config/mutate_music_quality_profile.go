package config

import (
	"context"
	"errors"
	"log/slog"
	"slices"
)

var (
	ErrMusicQualityProfileExists = errors.New(
		"music quality profile name already exists",
	)
	ErrMusicQualityProfileNotFound = errors.New(
		"music quality profile not found",
	)
	ErrMusicQualityProfileInUseAsDefault = errors.New(
		"music quality profile is the configured default",
	)
)

// MusicQualityProfilePatch carries optional field updates; a nil field is
// left alone.
type MusicQualityProfilePatch struct {
	Tiers          *[]string
	Preferred      *string
	UpgradeAllowed *bool
}

// AddMusicQualityProfile appends e with its tiers in canonical order. The
// first profile added while no default resolves becomes the default in the
// same write, so a stock install can add an artist as soon as one exists.
func AddMusicQualityProfile(ctx context.Context, e MusicQualityProfileEntry) error {
	e.Tiers = canonical(e.Tiers, MusicTiers)
	return Update(ctx, func(c *Config) error {
		for _, x := range c.MusicQualityProfiles {
			if x.Name == e.Name {
				return ErrMusicQualityProfileExists
			}
		}
		c.MusicQualityProfiles = append(c.MusicQualityProfiles, e)
		if _, ok := findMusicProfile(
			c.MusicQualityProfiles, c.MusicQualityDefaultProfile,
		); !ok {
			c.MusicQualityDefaultProfile = e.Name
		}
		slog.InfoContext(ctx, "music quality profile added", "name", e.Name)
		return nil
	})
}

func UpdateMusicQualityProfile(
	ctx context.Context,
	name string,
	p MusicQualityProfilePatch,
) error {
	return Update(ctx, func(c *Config) error {
		idx := slices.IndexFunc(
			c.MusicQualityProfiles,
			func(x MusicQualityProfileEntry) bool { return x.Name == name },
		)
		if idx < 0 {
			return ErrMusicQualityProfileNotFound
		}
		e := c.MusicQualityProfiles[idx]
		if p.Tiers != nil {
			e.Tiers = canonical(*p.Tiers, MusicTiers)
		}
		if p.Preferred != nil {
			e.Preferred = *p.Preferred
		}
		if p.UpgradeAllowed != nil {
			e.UpgradeAllowed = *p.UpgradeAllowed
		}
		c.MusicQualityProfiles[idx] = e
		slog.InfoContext(ctx, "music quality profile updated", "name", name)
		return nil
	})
}

// SetDefaultMusicQualityProfile points the music default at name. It is also
// the only way to free the current default for deletion.
func SetDefaultMusicQualityProfile(ctx context.Context, name string) error {
	return Update(ctx, func(c *Config) error {
		if _, ok := findMusicProfile(c.MusicQualityProfiles, name); !ok {
			return ErrMusicQualityProfileNotFound
		}
		c.MusicQualityDefaultProfile = name
		slog.InfoContext(ctx, "default music quality profile set", "name", name)
		return nil
	})
}

func DeleteMusicQualityProfile(ctx context.Context, name string) error {
	return Update(ctx, func(c *Config) error {
		if name == c.MusicQualityDefaultProfile {
			return ErrMusicQualityProfileInUseAsDefault
		}
		found := false
		next := make([]MusicQualityProfileEntry, 0, len(c.MusicQualityProfiles))
		for _, x := range c.MusicQualityProfiles {
			if x.Name == name {
				found = true
				continue
			}
			next = append(next, x)
		}
		if !found {
			return ErrMusicQualityProfileNotFound
		}
		c.MusicQualityProfiles = next
		slog.InfoContext(ctx, "music quality profile deleted", "name", name)
		return nil
	})
}
