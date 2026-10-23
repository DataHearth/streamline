package music

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel/attribute"

	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/otelx"
)

// ReleaseHit is one MusicBrainz release-group search hit with its local state.
type ReleaseHit struct {
	metadata.ReleaseGroupSearchResult
	AlreadyAdded bool
}

// SearchReleaseGroups searches MusicBrainz release groups by free text, one
// request remembered for ten minutes, and flags the hits whose album the
// library already holds with one query.
func (s *Service) SearchReleaseGroups(
	ctx context.Context,
	query string,
) ([]ReleaseHit, error) {
	ctx, span := tracer.Start(ctx, "music.search_release_groups")
	defer span.End()

	results, err := s.releaseSearches.do(
		strings.ToLower(strings.TrimSpace(query)),
		func() ([]metadata.ReleaseGroupSearchResult, error) {
			return s.metadata.SearchReleaseGroupsFreeText(ctx, query)
		},
	)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	mbids := make([]string, len(results))
	for i, r := range results {
		mbids[i] = r.MBID
	}
	held, err := s.db.AlbumIDsByMBID(ctx, mbids)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	hits := make([]ReleaseHit, len(results))
	for i, r := range results {
		_, in := held[r.MBID]
		hits[i] = ReleaseHit{ReleaseGroupSearchResult: r, AlreadyAdded: in}
	}
	span.SetAttributes(attribute.Int("hits", len(hits)))
	return hits, nil
}
