package config

import (
	"context"
	"errors"
	"log/slog"
)

var (
	ErrQualityProfileExists = errors.New(
		"quality profile name already exists",
	)
	ErrQualityProfileNotFound       = errors.New("quality profile not found")
	ErrQualityProfileInUseAsDefault = errors.New(
		"quality profile is the configured default",
	)
)

// QualityProfilePatch carries optional field updates.
type QualityProfilePatch struct {
	PreferredResolution *string
	MinResolution       *string
	UpgradeAllowed      *bool
}

func AddQualityProfile(ctx context.Context, e QualityProfileEntry) error {
	return Update(ctx, func(c *Config) error {
		for _, x := range c.QualityProfiles {
			if x.Name == e.Name {
				return ErrQualityProfileExists
			}
		}
		c.QualityProfiles = append(c.QualityProfiles, e)
		slog.InfoContext(ctx, "quality profile added", "name", e.Name)
		return nil
	})
}

func UpdateQualityProfile(
	ctx context.Context,
	name string,
	p QualityProfilePatch,
) error {
	return Update(ctx, func(c *Config) error {
		idx := -1
		for i, x := range c.QualityProfiles {
			if x.Name == name {
				idx = i
				break
			}
		}
		if idx < 0 {
			return ErrQualityProfileNotFound
		}
		e := c.QualityProfiles[idx]
		if p.PreferredResolution != nil {
			e.PreferredResolution = *p.PreferredResolution
		}
		if p.MinResolution != nil {
			e.MinResolution = *p.MinResolution
		}
		if p.UpgradeAllowed != nil {
			e.UpgradeAllowed = *p.UpgradeAllowed
		}
		c.QualityProfiles[idx] = e
		slog.InfoContext(ctx, "quality profile updated", "name", name)
		return nil
	})
}

func DeleteQualityProfile(ctx context.Context, name string) error {
	return Update(ctx, func(c *Config) error {
		if name == c.QualityDefaultProfile {
			return ErrQualityProfileInUseAsDefault
		}
		found := false
		next := make([]QualityProfileEntry, 0, len(c.QualityProfiles))
		for _, x := range c.QualityProfiles {
			if x.Name == name {
				found = true
				continue
			}
			next = append(next, x)
		}
		if !found {
			return ErrQualityProfileNotFound
		}
		c.QualityProfiles = next
		slog.InfoContext(ctx, "quality profile deleted", "name", name)
		return nil
	})
}
