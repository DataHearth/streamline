package music

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/observability"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/posters"
	"github.com/datahearth/streamline/internal/utils/numeric"
)

var tracer = otel.Tracer("github.com/datahearth/streamline/internal/media/music")

var (
	ErrArtistExists   = errors.New("artist already exists")
	ErrArtistNotFound = errors.New("artist not found")
	ErrAlbumNotFound  = errors.New("album not found")

	ErrNoQualityProfile = errors.New("no music quality profile configured")
)

// Manager is the surface the REST handlers use.
type Manager interface {
	Add(ctx context.Context, p AddParams) (*ent.Artist, error)
	List(ctx context.Context, page, limit uint16) ([]*ent.Artist, uint32, error)
	Get(ctx context.Context, id uint32) (*ent.Artist, error)
	GetAlbum(ctx context.Context, id uint32) (*ent.Album, error)
	SetArtistMonitored(ctx context.Context, id uint32, m bool) error
	SetArtistQualityProfile(ctx context.Context, id uint32, profile string) error
	SetAlbumMonitored(ctx context.Context, id uint32, m bool) error
	Delete(ctx context.Context, id uint32, deleteFiles bool) error
	RefreshOne(ctx context.Context, id uint32) (*ent.Artist, error)
	SearchAlbumReleases(ctx context.Context, albumID uint32) ([]AlbumRelease, error)
	GrabAlbumRelease(
		ctx context.Context,
		albumID uint32,
		result indexer.SearchResult,
	) error
}

var _ Manager = (*Service)(nil)

type Service struct {
	db       db.Store
	metadata metadata.MusicProvider
	posters  posters.Manager
	indexer  indexer.Manager
	download download.Downloader
}

func NewService(
	store db.Store,
	meta metadata.MusicProvider,
	p posters.Manager,
	idx indexer.Manager,
	dl download.Downloader,
) *Service {
	return &Service{
		db: store, metadata: meta, posters: p, indexer: idx, download: dl,
	}
}

// Adder is the slice of the service the request flow approves through.
type Adder interface {
	Add(ctx context.Context, p AddParams) (*ent.Artist, error)
	Get(ctx context.Context, id uint32) (*ent.Artist, error)
	SetAlbumMonitored(ctx context.Context, id uint32, m bool) error
}

var _ Adder = (*Service)(nil)

type AddParams struct {
	MBID           string
	Monitored      bool
	QualityProfile string
}

// Add fetches the artist and its discography, creates the Artist, Album and
// Track rows in one transaction, then fetches album cover art in the
// background. MusicBrainz is limited to one request per second, so a large
// discography makes this slow by design.
func (s *Service) Add(ctx context.Context, p AddParams) (*ent.Artist, error) {
	ctx, span := tracer.Start(ctx, "music.add",
		trace.WithAttributes(attribute.String("mbid", p.MBID)))
	defer span.End()

	existing, err := s.db.FindArtistByMBID(ctx, p.MBID)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	if existing != nil {
		return nil, otelx.RecordSpanError(
			span,
			fmt.Errorf("%w: mbid %s", ErrArtistExists, p.MBID),
		)
	}

	details, err := s.metadata.GetArtist(ctx, p.MBID)
	if err != nil {
		return nil, otelx.RecordSpanError(span, fmt.Errorf("get artist: %w", err))
	}
	albums, err := s.albumSeeds(ctx, details.ReleaseGroups)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}

	artist, err := s.db.CreateArtist(ctx, db.CreateArtistParams{
		MBID:      details.MBID,
		Name:      details.Name,
		SortName:  details.SortName,
		Overview:  details.Overview,
		Monitored: p.Monitored,
		Path: filepath.Join(
			config.Get().Library.MusicPath,
			library.SanitizePath(details.Name),
		),
		QualityProfile: p.QualityProfile,
		Albums:         albums,
	})
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	span.SetAttributes(attribute.Int("artist.id", int(artist.ID)))

	s.fetchPosters(ctx, artist.Edges.Albums)
	slog.InfoContext(ctx, "artist added",
		"artist.id", artist.ID, "mbid", artist.Mbid,
		"albums", len(artist.Edges.Albums))
	return artist, nil
}

func (s *Service) albumSeeds(
	ctx context.Context,
	groups []metadata.ReleaseGroupInfo,
) ([]db.AlbumSeed, error) {
	seeds := make([]db.AlbumSeed, 0, len(groups))
	for _, rg := range groups {
		d, err := s.metadata.GetReleaseGroup(ctx, rg.MBID)
		if err != nil {
			return nil, fmt.Errorf("get release group %s: %w", rg.MBID, err)
		}
		tracks := make([]db.TrackSeed, len(d.Tracks))
		for i, t := range d.Tracks {
			tracks[i] = db.TrackSeed{
				MBID:     t.MBID,
				Title:    t.Title,
				Disc:     t.Disc,
				Position: t.Position,
				Duration: t.Duration,
			}
		}
		seeds = append(seeds, db.AlbumSeed{
			MBID:        rg.MBID,
			ReleaseMBID: d.ReleaseMBID,
			Barcode:     d.Barcode,
			Title:       rg.Title,
			Type:        string(rg.Type),
			ReleaseDate: rg.ReleaseDate,
			Tracks:      tracks,
		})
	}
	return seeds, nil
}

// fetchPosters is best-effort and runs after the commit: a Cover Art Archive
// 404 is routine and must not fail the add.
func (s *Service) fetchPosters(ctx context.Context, albums []*ent.Album) {
	if len(albums) == 0 {
		return
	}
	bg := context.WithoutCancel(ctx)
	go func() {
		defer observability.RecoverPanic(bg, "music.fetch_posters", nil)
		for _, a := range albums {
			src := metadata.CoverArtURL(a.Mbid)
			if src == "" {
				continue
			}
			if err := s.posters.Fetch(bg, "albums", a.ID, src); err != nil {
				slog.WarnContext(bg, "album cover fetch failed",
					"album.id", a.ID, "error", err)
			}
		}
	}()
}

func (s *Service) List(
	ctx context.Context,
	page, limit uint16,
) ([]*ent.Artist, uint32, error) {
	ctx, span := tracer.Start(ctx, "music.list")
	defer span.End()
	if page == 0 {
		page = 1
	}
	if limit == 0 {
		limit = 20
	}
	total, err := s.db.CountArtists(ctx)
	if err != nil {
		return nil, 0, otelx.RecordSpanError(span, err)
	}
	rows, err := s.db.ListArtists(ctx, uint32(page-1)*uint32(limit), uint32(limit))
	if err != nil {
		return nil, 0, otelx.RecordSpanError(span, err)
	}
	return rows, numeric.SaturateU32(total), nil
}

func (s *Service) Get(ctx context.Context, id uint32) (*ent.Artist, error) {
	ctx, span := tracer.Start(ctx, "music.get",
		trace.WithAttributes(attribute.Int("artist.id", int(id))))
	defer span.End()
	artist, err := s.db.FindArtistByID(ctx, id)
	if err != nil {
		return nil, otelx.RecordSpanError(span, notFound(err))
	}
	return artist, nil
}

func (s *Service) GetAlbum(ctx context.Context, id uint32) (*ent.Album, error) {
	ctx, span := tracer.Start(ctx, "music.get_album",
		trace.WithAttributes(attribute.Int("album.id", int(id))))
	defer span.End()
	album, err := s.db.FindAlbumByID(ctx, id)
	if ent.IsNotFound(err) {
		err = ErrAlbumNotFound
	}
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	return album, nil
}

func (s *Service) SetArtistQualityProfile(
	ctx context.Context,
	id uint32,
	profile string,
) error {
	ctx, span := tracer.Start(ctx, "music.set_artist_quality_profile",
		trace.WithAttributes(
			attribute.Int("artist.id", int(id)),
			attribute.String("quality_profile", profile),
		))
	defer span.End()
	return otelx.RecordSpanError(
		span,
		notFound(s.db.SetArtistQualityProfile(ctx, id, profile)),
	)
}

func (s *Service) SetArtistMonitored(ctx context.Context, id uint32, m bool) error {
	ctx, span := tracer.Start(ctx, "music.set_artist_monitored",
		trace.WithAttributes(
			attribute.Int("artist.id", int(id)),
			attribute.Bool("monitored", m),
		))
	defer span.End()
	return otelx.RecordSpanError(span, notFound(s.db.SetArtistMonitored(ctx, id, m)))
}

func (s *Service) SetAlbumMonitored(ctx context.Context, id uint32, m bool) error {
	ctx, span := tracer.Start(ctx, "music.set_album_monitored",
		trace.WithAttributes(
			attribute.Int("album.id", int(id)),
			attribute.Bool("monitored", m),
		))
	defer span.End()
	err := s.db.SetAlbumMonitored(ctx, id, m)
	if ent.IsNotFound(err) {
		err = ErrAlbumNotFound
	}
	return otelx.RecordSpanError(span, err)
}

// Delete removes the artist; albums, tracks and media-file rows cascade in the
// schema. Files on disk are removed only when deleteFiles is set.
func (s *Service) Delete(ctx context.Context, id uint32, deleteFiles bool) error {
	ctx, span := tracer.Start(ctx, "music.delete",
		trace.WithAttributes(
			attribute.Int("artist.id", int(id)),
			attribute.Bool("delete_files", deleteFiles),
		))
	defer span.End()

	artist, err := s.db.FindArtistByID(ctx, id)
	if err != nil {
		return otelx.RecordSpanError(span, notFound(err))
	}
	if deleteFiles {
		root := config.Get().Library.MusicPath
		for _, a := range artist.Edges.Albums {
			for _, t := range a.Edges.Tracks {
				for _, f := range t.Edges.MediaFiles {
					if err := library.RemoveMediaFile(
						ctx,
						f.Path,
						root,
					); err != nil {
						slog.ErrorContext(
							ctx,
							"music file was not deleted from disk",
							"artist.id",
							id,
							"path",
							f.Path,
							"error",
							err,
						)
					}
				}
			}
		}
	}
	if err := s.db.DeleteArtist(ctx, id); err != nil {
		return otelx.RecordSpanError(span, notFound(err))
	}
	for _, a := range artist.Edges.Albums {
		if err := s.posters.Remove("albums", a.ID); err != nil {
			slog.WarnContext(ctx, "album poster was not removed",
				"album.id", a.ID, "error", err)
		}
	}
	slog.InfoContext(ctx, "artist deleted", "artist.id", id)
	return nil
}

// RefreshOne re-fetches the discography. Release-groups new to the artist
// become albums inheriting its monitored flag; existing albums update metadata
// only, so monitored and status stay as the user left them.
func (s *Service) RefreshOne(ctx context.Context, id uint32) (*ent.Artist, error) {
	ctx, span := tracer.Start(ctx, "music.refresh_one",
		trace.WithAttributes(attribute.Int("artist.id", int(id))))
	defer span.End()

	artist, err := s.db.FindArtistByID(ctx, id)
	if err != nil {
		return nil, otelx.RecordSpanError(span, notFound(err))
	}
	details, err := s.metadata.GetArtist(ctx, artist.Mbid)
	if err != nil {
		return nil, otelx.RecordSpanError(span, fmt.Errorf("get artist: %w", err))
	}

	known := make(map[string]bool, len(artist.Edges.Albums))
	for _, a := range artist.Edges.Albums {
		known[a.Mbid] = true
	}
	var fresh []metadata.ReleaseGroupInfo
	seeds := make([]db.AlbumSeed, 0, len(details.ReleaseGroups))
	for _, rg := range details.ReleaseGroups {
		if known[rg.MBID] {
			seeds = append(seeds, db.AlbumSeed{
				MBID: rg.MBID, Title: rg.Title,
				Type: string(rg.Type), ReleaseDate: rg.ReleaseDate,
			})
			continue
		}
		fresh = append(fresh, rg)
	}
	created, err := s.albumSeeds(ctx, fresh)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	seeds = append(seeds, created...)

	if err := s.db.RefreshArtist(ctx, id, db.RefreshArtistParams{
		Name:        details.Name,
		SortName:    details.SortName,
		Overview:    details.Overview,
		Albums:      seeds,
		RefreshedAt: time.Now(),
	}); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}

	updated, err := s.db.FindArtistByID(ctx, id)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	var added []*ent.Album
	for _, a := range updated.Edges.Albums {
		if !known[a.Mbid] {
			added = append(added, a)
		}
	}
	s.fetchPosters(ctx, added)
	return updated, nil
}

func notFound(err error) error {
	if ent.IsNotFound(err) {
		return ErrArtistNotFound
	}
	return err
}

// AlbumRelease is one indexer result that fits the artist's quality profile.
type AlbumRelease struct {
	Result indexer.SearchResult
	Format string
	Score  int
}

// SearchAlbumReleases queries the indexers for the album and returns the
// results the artist's profile accepts, best first.
func (s *Service) SearchAlbumReleases(
	ctx context.Context,
	albumID uint32,
) ([]AlbumRelease, error) {
	ctx, span := tracer.Start(ctx, "music.search_album",
		trace.WithAttributes(attribute.Int("album.id", int(albumID))))
	defer span.End()

	a, err := s.db.FindAlbumByID(ctx, albumID)
	if ent.IsNotFound(err) {
		err = ErrAlbumNotFound
	}
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
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

	releases := make([]AlbumRelease, 0, len(results))
	for _, r := range results {
		parsed := library.ParseMusicRelease(r.Title)
		score := library.ScoreMusicRelease(parsed, profile)
		if score < 0 {
			continue
		}
		releases = append(releases, AlbumRelease{
			Result: r, Format: parsed.Format, Score: score,
		})
	}
	slices.SortStableFunc(releases, func(x, y AlbumRelease) int {
		if x.Score != y.Score {
			return y.Score - x.Score
		}
		switch {
		case x.Result.Seeders > y.Result.Seeders:
			return -1
		case x.Result.Seeders < y.Result.Seeders:
			return 1
		}
		return 0
	})
	span.SetAttributes(attribute.Int("releases", len(releases)))
	return releases, nil
}

// GrabAlbumRelease grabs one release as the album's single download record and
// marks the album downloading. A failed grab touches no status, and a failed
// status write is only logged, since the torrent is already added.
func (s *Service) GrabAlbumRelease(
	ctx context.Context,
	albumID uint32,
	result indexer.SearchResult,
) error {
	ctx, span := tracer.Start(ctx, "music.grab_album",
		trace.WithAttributes(
			attribute.Int("album.id", int(albumID)),
			attribute.String("release.title", result.Title),
		))
	defer span.End()

	if _, err := s.download.GrabAlbum(ctx, result, albumID); err != nil {
		return otelx.RecordSpanError(span, fmt.Errorf("grab album: %w", err))
	}

	_, err := s.db.SetAlbumStatus(
		ctx, albumID,
		[]album.Status{album.StatusWanted, album.StatusPaused},
		album.StatusDownloading,
	)
	if err != nil {
		slog.WarnContext(ctx, "mark album downloading failed",
			"album.id", albumID, "error", err)
	}
	slog.InfoContext(ctx, "album grabbed",
		"album.id", albumID, "release", result.Title, "indexer", result.Indexer)
	return nil
}
