package music

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/media/searchwindow"
	"github.com/datahearth/streamline/internal/observability"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/utils/numeric"
)

// AlbumRelease is one indexer result judged against the artist's quality
// profile. A release the profile rejects is kept, with Score -1 and the
// reason, so the manual search can show it set aside.
type AlbumRelease struct {
	Result indexer.SearchResult
	Parsed library.ParsedMusicRelease
	Score  int
	// Reason says why Score is -1; empty otherwise.
	Reason string
}

func (r AlbumRelease) Rejected() bool { return r.Score < 0 }

// judge scores every result, dropping what the scope never lists and keeping
// the rest, best first with the rejected ones after.
func judge(
	results []indexer.SearchResult,
	profile config.MusicQualityProfileEntry,
	scope library.MusicScope,
	keep func(library.ParsedMusicRelease) bool,
) []AlbumRelease {
	releases := make([]AlbumRelease, 0, len(results))
	for _, r := range results {
		parsed := library.ParseMusicRelease(r.Title)
		if !keep(parsed) {
			continue
		}
		score, reason := library.JudgeMusicRelease(parsed, profile, scope)
		releases = append(releases, AlbumRelease{
			Result: r, Parsed: parsed, Score: score, Reason: reason,
		})
	}
	slices.SortStableFunc(releases, func(x, y AlbumRelease) int {
		switch {
		case !x.Rejected() && y.Rejected():
			return -1
		case x.Rejected() && !y.Rejected():
			return 1
		case x.Score != y.Score:
			return y.Score - x.Score
		}
		return int(y.Result.Seeders) - int(x.Result.Seeders)
	})
	return releases
}

// firstAccepted is the best release the profile accepts, nil when none is.
func firstAccepted(releases []AlbumRelease) *AlbumRelease {
	for i := range releases {
		if !releases[i].Rejected() {
			return &releases[i]
		}
	}
	return nil
}

// SearchAlbumReleases queries the indexers for the album and judges every
// result against the artist's profile. A discography never appears: it is
// rejected by scope, not listed.
func (s *Service) SearchAlbumReleases(
	ctx context.Context,
	albumID uint32,
) ([]AlbumRelease, error) {
	ctx, span := tracer.Start(ctx, "music.search_album",
		trace.WithAttributes(attribute.Int("album.id", int(albumID))))
	defer span.End()

	a, err := s.db.FindAlbumByID(ctx, albumID)
	if err != nil {
		return nil, otelx.RecordSpanError(span, albumNotFound(err))
	}
	artist := a.Edges.Artist
	if artist == nil {
		return nil, otelx.RecordSpanError(
			span, fmt.Errorf("album %d has no artist", albumID))
	}
	profile, ok := config.ResolveMusicQualityProfile(artist.QualityProfile)
	if !ok {
		return nil, otelx.RecordSpanError(span, ErrNoQualityProfile)
	}

	var year uint16
	if a.ReleaseDate != nil {
		year = numeric.SaturateU16(a.ReleaseDate.Year())
	}
	results, err := s.indexer.SearchAlbum(ctx, artist.Name, a.Title, year)
	if err != nil {
		return nil, otelx.RecordSpanError(span, fmt.Errorf("search album: %w", err))
	}
	releases := judge(results, profile, library.MusicScopeAlbum,
		func(p library.ParsedMusicRelease) bool { return !p.Discography })
	span.SetAttributes(attribute.Int("releases", len(releases)))
	return releases, nil
}

// BrowseArtistReleases queries the indexers for the artist's name and keeps
// only the discography packs whose artist part is this artist, judged against
// the artist's profile. One request per enabled indexer, no MusicBrainz call.
func (s *Service) BrowseArtistReleases(
	ctx context.Context,
	artistID uint32,
) ([]AlbumRelease, error) {
	ctx, span := tracer.Start(ctx, "music.browse_artist",
		trace.WithAttributes(attribute.Int("artist.id", int(artistID))))
	defer span.End()

	a, err := s.db.FindArtistRow(ctx, artistID)
	if err != nil {
		return nil, otelx.RecordSpanError(span, notFound(err))
	}
	profile, ok := config.ResolveMusicQualityProfile(a.QualityProfile)
	if !ok {
		return nil, otelx.RecordSpanError(span, ErrNoQualityProfile)
	}
	results, err := s.indexer.SearchArtist(ctx, a.Name)
	if err != nil {
		return nil, otelx.RecordSpanError(span, fmt.Errorf("search artist: %w", err))
	}
	byArtist := make([]indexer.SearchResult, 0, len(results))
	for _, r := range results {
		creator, _, ok := library.SplitCreatorTitle(r.Title)
		if ok && library.TitleMatchesStrict(creator, a.Name) {
			byArtist = append(byArtist, r)
		}
	}
	releases := judge(byArtist, profile, library.MusicScopeArtist,
		func(p library.ParsedMusicRelease) bool { return p.Discography })
	span.SetAttributes(attribute.Int("releases", len(releases)))
	return releases, nil
}

// GrabArtistRelease grabs a discography pack for the artist. The download
// manager links the record to every album of the artist that is monitored,
// released, hydrated and wanted or paused, and marks them downloading.
func (s *Service) GrabArtistRelease(
	ctx context.Context,
	artistID uint32,
	result indexer.SearchResult,
	replace bool,
) error {
	ctx, span := tracer.Start(ctx, "music.grab_artist_pack",
		trace.WithAttributes(
			attribute.Int("artist.id", int(artistID)),
			attribute.String("release.title", result.Title),
			attribute.Bool("replace_existing", replace),
		))
	defer span.End()

	a, err := s.db.FindArtistRow(ctx, artistID)
	if err != nil {
		return otelx.RecordSpanError(span, notFound(err))
	}
	if _, ok := config.ResolveMusicQualityProfile(a.QualityProfile); !ok {
		return otelx.RecordSpanError(span, ErrNoQualityProfile)
	}
	rec, err := s.download.GrabArtistPack(ctx, result, artistID)
	if err != nil {
		return otelx.RecordSpanError(span, fmt.Errorf("grab artist pack: %w", err))
	}
	if replace {
		if err := s.db.SetDownloadRecordReplaceMode(
			ctx, rec.ID, downloadrecord.ReplaceModeAll,
		); err != nil {
			slog.WarnContext(ctx, "grab artist pack: set replace mode failed",
				"download_record.id", rec.ID, "error", err)
		}
	}
	slog.InfoContext(ctx, "discography pack grabbed",
		"artist.id", artistID, "release", result.Title, "indexer", result.Indexer)
	return nil
}

// searchAlbum searches the indexers for one album and grabs the best release
// the profile accepts. It reports whether it grabbed. last_search_at is
// stamped whenever the indexers answered, so the cooldown advances on an empty
// result too; a failed search stamps nothing and counts no strike.
func (s *Service) searchAlbum(ctx context.Context, a *ent.Album) bool {
	ctx, span := tracer.Start(ctx, "music.search_album_pass",
		trace.WithAttributes(attribute.Int("album.id", int(a.ID))))
	defer span.End()

	releases, err := s.SearchAlbumReleases(ctx, a.ID)
	if err != nil {
		otelx.RecordSpanError(span, err)
		slog.WarnContext(ctx, "music search: search failed",
			"album.id", a.ID, "error", err)
		return false
	}
	if err := s.db.SetAlbumLastSearchAt(ctx, a.ID, time.Now()); err != nil {
		otelx.RecordSpanError(span, err)
		slog.WarnContext(ctx, "music search: stamp last_search_at failed",
			"album.id", a.ID, "error", err)
	}
	best := firstAccepted(releases)
	if best == nil {
		return false
	}

	if err := s.GrabAlbumRelease(ctx, a.ID, best.Result); err != nil {
		otelx.RecordSpanError(span, err)
		slog.WarnContext(ctx, "music search: grab failed",
			"album.id", a.ID, "error", err)
		if !searchwindow.TransportFailure(err) {
			if e := s.db.IncrementAlbumGrabFailures(ctx, a.ID); e != nil {
				slog.WarnContext(ctx, "music search: bump grab_failures failed",
					"album.id", a.ID, "error", e)
			}
		}
		return false
	}
	if err := s.db.ResetAlbumGrabFailures(ctx, a.ID); err != nil {
		slog.WarnContext(ctx, "music search: reset grab_failures failed",
			"album.id", a.ID, "error", err)
	}
	return true
}

// SearchAlbumNow runs one search-and-grab pass for the album in the
// background. It waives the cooldown and the failure cap, and does nothing for
// an album that is not wanted, is upcoming, or already has a download in
// flight. A missing quality profile is logged, not answered.
func (s *Service) SearchAlbumNow(ctx context.Context, albumID uint32) error {
	ctx, span := tracer.Start(ctx, "music.search_album_now",
		trace.WithAttributes(attribute.Int("album.id", int(albumID))))
	defer span.End()

	a, err := s.db.FindAlbumByID(ctx, albumID)
	if err != nil {
		return otelx.RecordSpanError(span, albumNotFound(err))
	}
	if a.Status != album.StatusWanted ||
		albumStatusAt(a, time.Now()) == StatusUpcoming {
		return nil
	}
	live, err := s.db.AlbumHasLiveRecord(ctx, albumID)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	if live {
		return nil
	}
	if _, ok := config.PickDownloadClient(); !ok {
		slog.InfoContext(ctx, "music search-now: no enabled download client",
			"album.id", albumID)
		return nil
	}
	bg := trace.ContextWithSpanContext(
		context.WithoutCancel(ctx),
		trace.SpanContext{},
	)
	go func() {
		defer observability.RecoverPanic(bg, "music.search_album", nil)
		s.searchAlbum(bg, a)
	}()
	return nil
}

// SearchArtistNow runs the album pass over the artist's monitored, released,
// hydrated, wanted albums one after the other in the background.
func (s *Service) SearchArtistNow(ctx context.Context, artistID uint32) error {
	ctx, span := tracer.Start(ctx, "music.search_artist_now",
		trace.WithAttributes(attribute.Int("artist.id", int(artistID))))
	defer span.End()

	if _, err := s.db.FindArtistRow(ctx, artistID); err != nil {
		return otelx.RecordSpanError(span, notFound(err))
	}
	albums, err := s.db.ListArtistAlbumsForSearch(ctx, artistID)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	if len(albums) == 0 {
		return nil
	}
	if _, ok := config.PickDownloadClient(); !ok {
		slog.InfoContext(ctx, "music search-now: no enabled download client",
			"artist.id", artistID)
		return nil
	}
	bg := trace.ContextWithSpanContext(
		context.WithoutCancel(ctx),
		trace.SpanContext{},
	)
	go func() {
		defer observability.RecoverPanic(bg, "music.search_artist", nil)
		bg, span := tracer.Start(bg, "music.search_artist",
			trace.WithAttributes(
				attribute.Int("artist.id", int(artistID)),
				attribute.Int("albums", len(albums)),
			))
		defer span.End()
		grabbed := 0
		for _, a := range albums {
			live, err := s.db.AlbumHasLiveRecord(bg, a.ID)
			if err != nil || live {
				continue
			}
			if s.searchAlbum(bg, a) {
				grabbed++
			}
		}
		span.SetAttributes(attribute.Int("grabbed", grabbed))
	}()
	return nil
}

// SearchTrackNow searches for the album holding the track: indexers answer by
// artist and album, and the importer fills the missing track from the grabbed
// release.
func (s *Service) SearchTrackNow(ctx context.Context, trackID uint32) error {
	t, err := s.db.FindTrackByID(ctx, trackID)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrTrackNotFound
		}
		return err
	}
	return s.SearchAlbumNow(ctx, t.Edges.Album.ID)
}
