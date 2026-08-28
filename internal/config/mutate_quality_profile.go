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

// QualityProfilePatch carries optional field updates. A nil AllowedCodecs
// leaves the list untouched; a non-nil-but-empty slice clears it back to
// "any codec".
type QualityProfilePatch struct {
	PreferredResolution *string
	MinResolution       *string
	UpgradeAllowed      *bool
	AllowedCodecs       *[]string
	Formats             *[]QualityProfileFormatScore
	MinScore            *int
	UpgradeUntilScore   *int
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
		if p.AllowedCodecs != nil {
			e.AllowedCodecs = *p.AllowedCodecs
		}
		if p.Formats != nil {
			e.Formats = *p.Formats
		}
		if p.MinScore != nil {
			e.MinScore = *p.MinScore
		}
		if p.UpgradeUntilScore != nil {
			e.UpgradeUntilScore = *p.UpgradeUntilScore
		}
		c.QualityProfiles[idx] = e
		slog.InfoContext(ctx, "quality profile updated", "name", name)
		return nil
	})
}

// SetDefaultQualityProfile points quality_default_profile at name. It is also
// the only way to free the current default for deletion — DeleteQualityProfile
// refuses the profile the key names.
func SetDefaultQualityProfile(ctx context.Context, name string) error {
	return Update(ctx, func(c *Config) error {
		for _, x := range c.QualityProfiles {
			if x.Name == name {
				c.QualityDefaultProfile = name
				slog.InfoContext(ctx, "default quality profile set", "name", name)
				return nil
			}
		}
		return ErrQualityProfileNotFound
	})
}

func DeleteQualityProfile(ctx context.Context, name string) error {
	return Update(ctx, func(c *Config) error {
		// Also what keeps the list from being emptied while
		// quality_default_profile still names a profile: checkInvariants only
		// checks that name against a non-empty list, so a config in that state
		// validates, saves, loads — and ResolveQualityProfile then matches
		// nothing at all. Relaxing this into "delete it and clear the default"
		// needs that check to cover the empty list first.
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
