package config

import (
	"context"
	"errors"
	"log/slog"
	"slices"
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
	// Transcode replaces the whole block when set. There is no way to remove
	// one through a patch — nil means "leave it alone", so clearing a policy
	// is a config-file edit.
	Transcode *TranscodePolicy
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
		if p.Transcode != nil {
			e.Transcode = p.Transcode
		}
		c.QualityProfiles[idx] = e
		slog.InfoContext(ctx, "quality profile updated", "name", name)
		return nil
	})
}

// SetDefaultQualityProfile points the default for each of media at name. It is
// also the only way to free a current default for deletion —
// DeleteQualityProfile refuses a profile either default key names.
func SetDefaultQualityProfile(
	ctx context.Context,
	name string,
	media ...Media,
) error {
	return Update(ctx, func(c *Config) error {
		if !slices.ContainsFunc(
			c.QualityProfiles,
			func(x QualityProfileEntry) bool { return x.Name == name },
		) {
			return ErrQualityProfileNotFound
		}
		for _, m := range media {
			if m == MediaSeries {
				c.SeriesQualityDefaultProfile = name
			} else {
				c.MovieQualityDefaultProfile = name
			}
		}
		slog.InfoContext(ctx, "default quality profile set",
			"name", name, "media", media)
		return nil
	})
}

func DeleteQualityProfile(ctx context.Context, name string) error {
	return Update(ctx, func(c *Config) error {
		// Also what keeps the list from being emptied while
		// a default key still names a profile: checkInvariants only
		// checks those names against a non-empty list, so a config in that state
		// validates, saves, loads — and ResolveQualityProfile then matches
		// nothing at all. Relaxing this into "delete it and clear the default"
		// needs that check to cover the empty list first.
		if len(c.DefaultFor(name)) > 0 {
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
