package config

import (
	"context"
	"errors"
	"log/slog"
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
	Formats        *[]string
	Cutoff         *string
	UpgradeAllowed *bool
}

func AddMusicQualityProfile(ctx context.Context, e MusicQualityProfileEntry) error {
	return Update(ctx, func(c *Config) error {
		for _, x := range c.MusicQualityProfiles {
			if x.Name == e.Name {
				return ErrMusicQualityProfileExists
			}
		}
		c.MusicQualityProfiles = append(c.MusicQualityProfiles, e)
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
		idx := -1
		for i, x := range c.MusicQualityProfiles {
			if x.Name == name {
				idx = i
				break
			}
		}
		if idx < 0 {
			return ErrMusicQualityProfileNotFound
		}
		e := c.MusicQualityProfiles[idx]
		if p.Formats != nil {
			e.Formats = *p.Formats
		}
		if p.Cutoff != nil {
			e.Cutoff = *p.Cutoff
		}
		if p.UpgradeAllowed != nil {
			e.UpgradeAllowed = *p.UpgradeAllowed
		}
		c.MusicQualityProfiles[idx] = e
		slog.InfoContext(ctx, "music quality profile updated", "name", name)
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
