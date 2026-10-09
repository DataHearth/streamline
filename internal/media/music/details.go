package music

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/observability"
	"github.com/datahearth/streamline/internal/otelx"
)

// emptyDeezerPicture is the path Deezer builds for an artist with no photo: an
// empty hash segment.
const emptyDeezerPicture = "/images/artist//"

// fillDetailsInBackground runs the overview and photo steps off the caller's
// goroutine and never reports failure: neither may fail an add or a refresh.
func (s *Service) fillDetailsInBackground(
	ctx context.Context,
	artistID uint32,
	force bool,
) {
	bg := trace.ContextWithSpanContext(
		context.WithoutCancel(ctx),
		trace.SpanContext{},
	)
	go func() {
		defer observability.RecoverPanic(bg, "music.artist_details", nil)
		s.fillDetails(bg, artistID, force)
	}()
}

func (s *Service) hasPhoto(artistID uint32) bool {
	st, err := os.Stat(s.posters.Path(artistKind, artistID))
	return err == nil && st.Size() > 0
}

// fillDetails fetches the artist's Wikipedia overview in every locale, once
// ever: a later refresh (force) fetches again only when the English overview
// is still empty. It also resolves the artist photo when none is cached.
func (s *Service) fillDetails(ctx context.Context, artistID uint32, force bool) {
	ctx, span := tracer.Start(ctx, "music.fill_artist_details",
		trace.WithAttributes(attribute.Int64("artist.id", int64(artistID))))
	defer span.End()

	a, err := s.db.FindArtistRow(ctx, artistID)
	if err != nil {
		if !ent.IsNotFound(err) {
			otelx.RecordSpanError(span, err)
			slog.WarnContext(ctx, "artist details: lookup failed",
				"artist.id", artistID, "error", err)
		}
		return
	}

	var params db.ArtistDetailsParams
	if s.overviews != nil && a.WikidataID != "" &&
		(a.DetailsFetchedAt == nil || (force && a.Overview == "")) {
		got, err := s.overviews.Overviews(ctx, a.WikidataID, overviewLangs)
		if err != nil {
			otelx.RecordSpanError(span, err)
			slog.WarnContext(ctx, "artist overview not fetched",
				"artist.id", artistID, "error", err)
		}
		if o, ok := got["en"]; ok {
			params.Overview, params.OverviewSource = o.Text, o.SourceURL
		}
		if o, ok := got["fr"]; ok {
			params.OverviewFR, params.OverviewSourceFR = o.Text, o.SourceURL
		}
	}

	if s.photos != nil && !s.hasPhoto(artistID) {
		if url := s.photoURL(ctx, a); url != "" {
			if err := s.posters.Fetch(ctx, artistKind, artistID, url); err != nil {
				otelx.RecordSpanError(span, err)
				slog.WarnContext(ctx, "artist photo not fetched",
					"artist.id", artistID, "error", err)
			}
		}
	}

	if err := s.db.SetArtistDetails(ctx, artistID, params, time.Now()); err != nil {
		otelx.RecordSpanError(span, err)
		slog.WarnContext(ctx, "artist details not stored",
			"artist.id", artistID, "error", err)
	}
}

// photoURL finds the artist's picture: through MusicBrainz's own Deezer link
// when it has one, which cannot confuse namesakes, otherwise by name, accepted
// only when exactly one hit folds equal to the artist's name. Two equal hits,
// or none, mean no photo; the first hit never wins.
func (s *Service) photoURL(ctx context.Context, a *ent.Artist) string {
	if a.DeezerID != 0 {
		hit, err := s.photos.ArtistByID(ctx, a.DeezerID)
		if err != nil {
			slog.WarnContext(ctx, "deezer artist lookup failed",
				"artist.id", a.ID, "error", err)
			return ""
		}
		if hit != nil {
			return usablePicture(hit.PictureURL)
		}
		return ""
	}
	hits, err := s.photos.SearchArtists(ctx, a.Name)
	if err != nil {
		slog.WarnContext(ctx, "deezer artist search failed",
			"artist.id", a.ID, "error", err)
		return ""
	}
	var match *metadata.DeezerArtist
	for i := range hits {
		if !library.TitleMatchesStrict(hits[i].Name, a.Name) {
			continue
		}
		if match != nil {
			return ""
		}
		match = &hits[i]
	}
	if match == nil {
		return ""
	}
	return usablePicture(match.PictureURL)
}

func usablePicture(u string) string {
	if u == "" || strings.Contains(u, emptyDeezerPicture) {
		return ""
	}
	return u
}
