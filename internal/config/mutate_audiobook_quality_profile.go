package config

import (
	"context"
	"errors"
	"log/slog"
)

var (
	ErrAudiobookQualityProfileExists = errors.New(
		"audiobook quality profile name already exists",
	)
	ErrAudiobookQualityProfileNotFound = errors.New(
		"audiobook quality profile not found",
	)
	ErrAudiobookQualityProfileInUseAsDefault = errors.New(
		"audiobook quality profile is the configured default",
	)
)

// AudiobookQualityProfilePatch carries optional field updates; a nil field is
// left alone.
type AudiobookQualityProfilePatch struct {
	Formats        *[]string
	Cutoff         *string
	UpgradeAllowed *bool
}

func AddAudiobookQualityProfile(
	ctx context.Context,
	e AudiobookQualityProfileEntry,
) error {
	return Update(ctx, func(c *Config) error {
		for _, x := range c.AudiobookQualityProfiles {
			if x.Name == e.Name {
				return ErrAudiobookQualityProfileExists
			}
		}
		c.AudiobookQualityProfiles = append(c.AudiobookQualityProfiles, e)
		slog.InfoContext(ctx, "audiobook quality profile added", "name", e.Name)
		return nil
	})
}

func UpdateAudiobookQualityProfile(
	ctx context.Context,
	name string,
	p AudiobookQualityProfilePatch,
) error {
	return Update(ctx, func(c *Config) error {
		idx := -1
		for i, x := range c.AudiobookQualityProfiles {
			if x.Name == name {
				idx = i
				break
			}
		}
		if idx < 0 {
			return ErrAudiobookQualityProfileNotFound
		}
		e := c.AudiobookQualityProfiles[idx]
		if p.Formats != nil {
			e.Formats = *p.Formats
		}
		if p.Cutoff != nil {
			e.Cutoff = *p.Cutoff
		}
		if p.UpgradeAllowed != nil {
			e.UpgradeAllowed = *p.UpgradeAllowed
		}
		c.AudiobookQualityProfiles[idx] = e
		slog.InfoContext(ctx, "audiobook quality profile updated", "name", name)
		return nil
	})
}

func DeleteAudiobookQualityProfile(ctx context.Context, name string) error {
	return Update(ctx, func(c *Config) error {
		if name == c.AudiobookQualityDefaultProfile {
			return ErrAudiobookQualityProfileInUseAsDefault
		}
		found := false
		next := make(
			[]AudiobookQualityProfileEntry,
			0,
			len(c.AudiobookQualityProfiles),
		)
		for _, x := range c.AudiobookQualityProfiles {
			if x.Name == name {
				found = true
				continue
			}
			next = append(next, x)
		}
		if !found {
			return ErrAudiobookQualityProfileNotFound
		}
		c.AudiobookQualityProfiles = next
		slog.InfoContext(ctx, "audiobook quality profile deleted", "name", name)
		return nil
	})
}
