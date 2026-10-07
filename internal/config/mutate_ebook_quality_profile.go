package config

import (
	"context"
	"errors"
	"log/slog"
)

var (
	ErrEbookQualityProfileExists = errors.New(
		"ebook quality profile name already exists",
	)
	ErrEbookQualityProfileNotFound = errors.New(
		"ebook quality profile not found",
	)
	ErrEbookQualityProfileInUseAsDefault = errors.New(
		"ebook quality profile is the configured default",
	)
)

// EbookQualityProfilePatch carries optional field updates; a nil field is
// left alone.
type EbookQualityProfilePatch struct {
	Formats        *[]string
	Cutoff         *string
	UpgradeAllowed *bool
}

func AddEbookQualityProfile(ctx context.Context, e EbookQualityProfileEntry) error {
	return Update(ctx, func(c *Config) error {
		for _, x := range c.EbookQualityProfiles {
			if x.Name == e.Name {
				return ErrEbookQualityProfileExists
			}
		}
		c.EbookQualityProfiles = append(c.EbookQualityProfiles, e)
		slog.InfoContext(ctx, "ebook quality profile added", "name", e.Name)
		return nil
	})
}

func UpdateEbookQualityProfile(
	ctx context.Context,
	name string,
	p EbookQualityProfilePatch,
) error {
	return Update(ctx, func(c *Config) error {
		idx := -1
		for i, x := range c.EbookQualityProfiles {
			if x.Name == name {
				idx = i
				break
			}
		}
		if idx < 0 {
			return ErrEbookQualityProfileNotFound
		}
		e := c.EbookQualityProfiles[idx]
		if p.Formats != nil {
			e.Formats = *p.Formats
		}
		if p.Cutoff != nil {
			e.Cutoff = *p.Cutoff
		}
		if p.UpgradeAllowed != nil {
			e.UpgradeAllowed = *p.UpgradeAllowed
		}
		c.EbookQualityProfiles[idx] = e
		slog.InfoContext(ctx, "ebook quality profile updated", "name", name)
		return nil
	})
}

func DeleteEbookQualityProfile(ctx context.Context, name string) error {
	return Update(ctx, func(c *Config) error {
		if name == c.EbookQualityDefaultProfile {
			return ErrEbookQualityProfileInUseAsDefault
		}
		found := false
		next := make([]EbookQualityProfileEntry, 0, len(c.EbookQualityProfiles))
		for _, x := range c.EbookQualityProfiles {
			if x.Name == name {
				found = true
				continue
			}
			next = append(next, x)
		}
		if !found {
			return ErrEbookQualityProfileNotFound
		}
		c.EbookQualityProfiles = next
		slog.InfoContext(ctx, "ebook quality profile deleted", "name", name)
		return nil
	})
}
