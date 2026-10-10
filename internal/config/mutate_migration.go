package config

import (
	"context"
	"fmt"
	"log/slog"
)

// AddResources adds quality profiles, indexers and download clients in one
// config write, so a migration copying several of them either lands all of
// them or none: half a copied config reads as a finished one and leaves the
// operator to find which entries are missing.
func AddResources(
	ctx context.Context,
	profiles []QualityProfileEntry,
	indexers []IndexerEntry,
	clients []DownloadClientEntry,
) error {
	return Update(ctx, func(c *Config) error {
		for _, e := range profiles {
			if _, ok := findProfile(c.QualityProfiles, e.Name); ok {
				return fmt.Errorf("%w: %q", ErrQualityProfileExists, e.Name)
			}
			c.QualityProfiles = append(c.QualityProfiles, e)
		}
		for _, e := range indexers {
			for _, x := range c.Indexers {
				if x.Name == e.Name {
					return fmt.Errorf("%w: %q", ErrIndexerExists, e.Name)
				}
			}
			c.Indexers = append(c.Indexers, e)
		}
		for _, e := range clients {
			for _, x := range c.DownloadClients {
				if x.Name == e.Name {
					return fmt.Errorf("%w: %q", ErrDownloadClientExists, e.Name)
				}
			}
			c.DownloadClients = append(c.DownloadClients, e)
		}
		slog.InfoContext(ctx, "migrated resources added",
			"quality_profiles", len(profiles),
			"indexers", len(indexers),
			"download_clients", len(clients))
		return nil
	})
}
