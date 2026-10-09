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
	"github.com/datahearth/streamline/ent/artist"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/posters"
)

var tracer = otel.Tracer("github.com/datahearth/streamline/internal/media/music")

const (
	artistKind = "artists"

	defaultPageLimit = 20

	detailsTTL        = 10 * time.Minute
	detailsCacheLimit = 32
)

var (
	ErrArtistExists   = errors.New("artist already exists")
	ErrArtistNotFound = errors.New("artist not found")
	ErrAlbumNotFound  = errors.New("album not found")
	ErrTrackNotFound  = errors.New("track not found")

	ErrNoQualityProfile = errors.New("no music quality profile configured")
	ErrUnknownProfile   = errors.New("unknown music quality profile")
	ErrInvalidMonitor   = errors.New("unknown monitor policy")

	// ErrOutsideRoot is a stored file path outside the music library root:
	// nothing is deleted.
	ErrOutsideRoot = library.ErrOutsideRoot
)

// Manager is the surface the REST handlers use.
type Manager interface {
	Add(ctx context.Context, p AddParams) (*ent.Artist, error)
	List(ctx context.Context, p db.ListArtistsParams) (ArtistPage, error)
	Counts(ctx context.Context, p db.ListArtistsParams) (db.ArtistCounts, error)
	Detail(ctx context.Context, id uint32, lang string) (*ArtistView, error)
	AlbumDetail(ctx context.Context, id uint32) (*AlbumView, error)
	SetArtistMonitor(ctx context.Context, id uint32, monitor string) error
	SetArtistQualityProfile(ctx context.Context, id uint32, profile string) error
	SetAlbumMonitored(ctx context.Context, id uint32, m bool) error
	Delete(ctx context.Context, id uint32, deleteFiles bool) error
	RefreshOne(ctx context.Context, id uint32) (*ent.Artist, error)

	SearchArtists(ctx context.Context, query string) ([]LookupHit, error)
	LookupArtist(ctx context.Context, mbid, lang string) (*LookupDetail, error)

	SearchAlbumReleases(ctx context.Context, albumID uint32) ([]AlbumRelease, error)
	BrowseArtistReleases(
		ctx context.Context,
		artistID uint32,
	) ([]AlbumRelease, error)
	GrabAlbumRelease(
		ctx context.Context,
		albumID uint32,
		result indexer.SearchResult,
	) error
	GrabAlbum(
		ctx context.Context,
		albumID uint32,
		result indexer.SearchResult,
		replace bool,
	) error
	GrabArtistRelease(
		ctx context.Context,
		artistID uint32,
		result indexer.SearchResult,
		replace bool,
	) error
	SearchAlbumNow(ctx context.Context, albumID uint32) error
	SearchArtistNow(ctx context.Context, artistID uint32) error
	SearchTrackNow(ctx context.Context, trackID uint32) error
	DeleteTrackFile(ctx context.Context, trackID uint32) error
}

var _ Manager = (*Service)(nil)

type Service struct {
	db        db.Store
	metadata  metadata.MusicProvider
	posters   posters.Manager
	covers    metadata.CoverProvider
	indexer   indexer.Manager
	download  download.Downloader
	overviews metadata.OverviewProvider
	photos    metadata.ArtistPhotoProvider

	details *memo[*metadata.ArtistDetails]
	lookups *memo[*lookupBody]
	hydrate hydrator
}

// NewService wires the music service. overviews and photos may be nil, which
// turns the Wikipedia and Deezer steps of an add off.
func NewService(
	store db.Store,
	meta metadata.MusicProvider,
	p posters.Manager,
	covers metadata.CoverProvider,
	idx indexer.Manager,
	dl download.Downloader,
	overviews metadata.OverviewProvider,
	photos metadata.ArtistPhotoProvider,
) *Service {
	return &Service{
		db: store, metadata: meta, posters: p, covers: covers,
		indexer: idx, download: dl, overviews: overviews, photos: photos,
		details: newMemo[*metadata.ArtistDetails](detailsTTL, detailsCacheLimit),
		lookups: newMemo[*lookupBody](detailsTTL, detailsCacheLimit),
	}
}

// Adder is the slice of the service the request flow and the adoption path
// add artists through.
type Adder interface {
	Add(ctx context.Context, p AddParams) (*ent.Artist, error)
	Get(ctx context.Context, id uint32) (*ent.Artist, error)
}

var _ Adder = (*Service)(nil)

type AddParams struct {
	MBID string
	// Monitor is a monitor policy: all, future, manual or none. Empty means all.
	Monitor        string
	QualityProfile string
}

func parseMonitor(m string) (artist.Monitor, error) {
	if m == "" {
		return artist.MonitorAll, nil
	}
	mon := artist.Monitor(m)
	if err := artist.MonitorValidator(mon); err != nil {
		return "", fmt.Errorf("%w: %q", ErrInvalidMonitor, m)
	}
	return mon, nil
}

// monitoredFor says whether a release group new to an artist is monitored
// under the policy: all monitors everything, future the undated and the not
// yet released, manual and none nothing.
func monitoredFor(policy artist.Monitor, date *time.Time, now time.Time) bool {
	switch policy {
	case artist.MonitorAll:
		return true
	case artist.MonitorFuture:
		return date == nil || date.After(now)
	}
	return false
}

func albumSeeds(
	groups []metadata.ReleaseGroupInfo,
	policy artist.Monitor,
	now time.Time,
) []db.AlbumSeed {
	seeds := make([]db.AlbumSeed, len(groups))
	for i, rg := range groups {
		seeds[i] = db.AlbumSeed{
			MBID:        rg.MBID,
			Title:       rg.Title,
			Type:        string(rg.Type),
			ReleaseDate: rg.ReleaseDate,
			Monitored:   monitoredFor(policy, rg.ReleaseDate, now),
		}
	}
	return seeds
}

func memberSeeds(members []metadata.ArtistMemberInfo) []db.MemberSeed {
	seeds := make([]db.MemberSeed, len(members))
	for i, m := range members {
		seeds[i] = db.MemberSeed{
			Name:        m.Name,
			MBID:        m.MBID,
			Instruments: m.Instruments,
			FromYear:    m.FromYear,
			ToYear:      m.ToYear,
		}
	}
	return seeds
}

// artistDetails fetches an artist and its release groups through a ten-minute
// cache shared by the lookup and the add, so an artist looked up and then
// added costs one set of MusicBrainz requests.
func (s *Service) artistDetails(
	ctx context.Context,
	mbid string,
) (*metadata.ArtistDetails, error) {
	return s.details.do(mbid, func() (*metadata.ArtistDetails, error) {
		return s.metadata.GetArtist(ctx, mbid)
	})
}

// Add creates the artist and one stub album per release group in a single
// transaction and returns. Tracks, credits, covers, the overview and the photo
// arrive in the background, so a prolific artist adds in seconds and one
// upstream failure cannot fail the add. A request costs 1 + ceil(n/100)
// MusicBrainz calls, none on a cache hit.
func (s *Service) Add(ctx context.Context, p AddParams) (*ent.Artist, error) {
	ctx, span := tracer.Start(ctx, "music.add",
		trace.WithAttributes(attribute.String("mbid", p.MBID)))
	defer span.End()

	policy, err := parseMonitor(p.Monitor)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	if p.QualityProfile != "" {
		if _, ok := config.LookupMusicQualityProfile(p.QualityProfile); !ok {
			return nil, otelx.RecordSpanError(span,
				fmt.Errorf("%w: %q", ErrUnknownProfile, p.QualityProfile))
		}
	}
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

	details, err := s.artistDetails(ctx, p.MBID)
	if err != nil {
		return nil, otelx.RecordSpanError(span, fmt.Errorf("get artist: %w", err))
	}
	row, err := s.db.CreateArtist(ctx, db.CreateArtistParams{
		MBID:       details.MBID,
		Name:       details.Name,
		SortName:   details.SortName,
		Monitor:    policy,
		Type:       details.Type,
		Origin:     details.Origin,
		Since:      details.Since,
		Genre:      details.Genre,
		DeezerID:   details.DeezerID,
		WikidataID: details.WikidataID,
		Path: filepath.Join(
			config.Get().Library.MusicPath,
			library.SanitizePath(details.Name),
		),
		QualityProfile: p.QualityProfile,
		Members:        memberSeeds(details.Members),
		Albums:         albumSeeds(details.ReleaseGroups, policy, time.Now()),
	})
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	span.SetAttributes(attribute.Int("artist.id", int(row.ID)))

	s.afterAdd(ctx, row, false)
	slog.InfoContext(ctx, "artist added",
		"artist.id", row.ID, "mbid", row.Mbid,
		"albums", len(row.Edges.Albums), "monitor", policy)
	return row, nil
}

// afterAdd starts the background work of an artist whose albums are stubs:
// hydration of every album that has no tracks yet and the overview and photo
// step. It never fails the caller.
func (s *Service) afterAdd(ctx context.Context, a *ent.Artist, force bool) {
	items := make([]hydrateItem, 0, len(a.Edges.Albums))
	for _, al := range a.Edges.Albums {
		if al.MetadataFetchedAt == nil {
			items = append(items, hydrateItem{
				albumID:   al.ID,
				monitored: al.Monitored,
				date:      al.ReleaseDate,
			})
		}
	}
	s.enqueueHydration(a.ID, items...)
	s.fillDetailsInBackground(ctx, a.ID, force)
}

func (s *Service) Get(ctx context.Context, id uint32) (*ent.Artist, error) {
	ctx, span := tracer.Start(ctx, "music.get",
		trace.WithAttributes(attribute.Int("artist.id", int(id))))
	defer span.End()
	row, err := s.db.FindArtistByID(ctx, id)
	if err != nil {
		return nil, otelx.RecordSpanError(span, notFound(err))
	}
	return row, nil
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
	if profile != "" {
		if _, ok := config.LookupMusicQualityProfile(profile); !ok {
			return otelx.RecordSpanError(span,
				fmt.Errorf("%w: %q", ErrUnknownProfile, profile))
		}
	}
	return otelx.RecordSpanError(
		span,
		notFound(s.db.SetArtistQualityProfile(ctx, id, profile)),
	)
}

// SetArtistMonitor stores the policy and applies it to the artist's existing
// albums. It never touches album status.
func (s *Service) SetArtistMonitor(
	ctx context.Context,
	id uint32,
	monitor string,
) error {
	ctx, span := tracer.Start(ctx, "music.set_artist_monitor",
		trace.WithAttributes(
			attribute.Int("artist.id", int(id)),
			attribute.String("monitor", monitor),
		))
	defer span.End()
	policy, err := parseMonitor(monitor)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	return otelx.RecordSpanError(
		span,
		notFound(s.db.SetArtistMonitor(ctx, id, policy, time.Now())),
	)
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
// schema. Files on disk are removed only when deleteFiles is set. The cached
// artist photo and album covers go with it.
func (s *Service) Delete(ctx context.Context, id uint32, deleteFiles bool) error {
	ctx, span := tracer.Start(ctx, "music.delete",
		trace.WithAttributes(
			attribute.Int("artist.id", int(id)),
			attribute.Bool("delete_files", deleteFiles),
		))
	defer span.End()

	row, err := s.db.FindArtistByID(ctx, id)
	if err != nil {
		return otelx.RecordSpanError(span, notFound(err))
	}
	if deleteFiles {
		root := config.Get().Library.MusicPath
		for _, a := range row.Edges.Albums {
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
	if err := s.posters.Remove(artistKind, id); err != nil {
		slog.WarnContext(ctx, "artist poster was not removed",
			"artist.id", id, "error", err)
	}
	for _, a := range row.Edges.Albums {
		if err := s.posters.Remove(coverKind, a.ID); err != nil {
			slog.WarnContext(ctx, "album poster was not removed",
				"album.id", a.ID, "error", err)
		}
	}
	slog.InfoContext(ctx, "artist deleted", "artist.id", id)
	return nil
}

const (
	// tracklessStale is how long an album that came back without tracks is
	// left before MusicBrainz is asked again; tracklessHorizon bounds that to
	// albums that are out or near release.
	tracklessStale   = 7 * 24 * time.Hour
	tracklessHorizon = 90 * 24 * time.Hour
	tracklessPerRun  = 5
)

// RefreshOne re-fetches the artist and its discography synchronously
// (1 + ceil(n/100) MusicBrainz calls): the artist's own facts and members are
// rewritten, release groups new to it become albums under its monitor policy
// and are hydrated in the background, as are known albums that still have no
// tracks. The photo is re-resolved when none is cached and the overview when
// it is empty.
func (s *Service) RefreshOne(ctx context.Context, id uint32) (*ent.Artist, error) {
	ctx, span := tracer.Start(ctx, "music.refresh_one",
		trace.WithAttributes(attribute.Int("artist.id", int(id))))
	defer span.End()

	row, err := s.db.FindArtistByID(ctx, id)
	if err != nil {
		return nil, otelx.RecordSpanError(span, notFound(err))
	}
	details, err := s.metadata.GetArtist(ctx, row.Mbid)
	if err != nil {
		return nil, otelx.RecordSpanError(span, fmt.Errorf("get artist: %w", err))
	}
	s.details.put(row.Mbid, details)

	now := time.Now()
	created, err := s.db.RefreshArtist(ctx, id, db.RefreshArtistParams{
		Name:        details.Name,
		SortName:    details.SortName,
		Type:        details.Type,
		Origin:      details.Origin,
		Since:       details.Since,
		Genre:       details.Genre,
		DeezerID:    details.DeezerID,
		WikidataID:  details.WikidataID,
		Members:     memberSeeds(details.Members),
		Albums:      albumSeeds(details.ReleaseGroups, row.Monitor, now),
		RefreshedAt: now,
	})
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}

	updated, err := s.db.FindArtistByID(ctx, id)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	var items []hydrateItem
	retried := 0
	for _, a := range updated.Edges.Albums {
		retry := retried < tracklessPerRun && a.MetadataFetchedAt != nil &&
			len(a.Edges.Tracks) == 0 &&
			now.Sub(*a.MetadataFetchedAt) > tracklessStale &&
			(a.ReleaseDate == nil || a.ReleaseDate.Before(now.Add(tracklessHorizon)))
		if retry {
			retried++
		}
		if !retry && a.MetadataFetchedAt != nil && !slices.Contains(created, a.ID) {
			continue
		}
		items = append(items, hydrateItem{
			albumID: a.ID, monitored: a.Monitored, date: a.ReleaseDate,
		})
	}
	s.enqueueHydration(id, items...)
	s.fillDetailsInBackground(ctx, id, true)

	var uncovered []uint32
	for _, a := range updated.Edges.Albums {
		if !s.hasCover(a.ID) && a.MetadataFetchedAt != nil {
			uncovered = append(uncovered, a.ID)
		}
	}
	s.ResolveCoversInBackground(ctx, uncovered...)
	return updated, nil
}

func notFound(err error) error {
	if ent.IsNotFound(err) {
		return ErrArtistNotFound
	}
	return err
}

func albumNotFound(err error) error {
	if ent.IsNotFound(err) {
		return ErrAlbumNotFound
	}
	return err
}

// GrabAlbumRelease grabs one release as the album's single download record and
// marks the album downloading. A failed grab touches no status, and a failed
// status write is only logged, since the torrent is already added.
func (s *Service) GrabAlbumRelease(
	ctx context.Context,
	albumID uint32,
	result indexer.SearchResult,
) error {
	return s.GrabAlbum(ctx, albumID, result, false)
}

// GrabAlbum is GrabAlbumRelease for a manual grab: replace flags the record
// so the importer, once the new files are placed and verified, removes the old
// file of every track the release matched.
func (s *Service) GrabAlbum(
	ctx context.Context,
	albumID uint32,
	result indexer.SearchResult,
	replace bool,
) error {
	ctx, span := tracer.Start(ctx, "music.grab_album",
		trace.WithAttributes(
			attribute.Int("album.id", int(albumID)),
			attribute.String("release.title", result.Title),
			attribute.Bool("replace_existing", replace),
		))
	defer span.End()

	if _, err := s.db.FindAlbumByID(ctx, albumID); err != nil {
		return otelx.RecordSpanError(span, albumNotFound(err))
	}
	rec, err := s.download.GrabAlbum(ctx, result, albumID)
	if err != nil {
		return otelx.RecordSpanError(span, fmt.Errorf("grab album: %w", err))
	}
	if replace {
		if err := s.db.SetDownloadRecordReplaceMode(
			ctx, rec.ID, downloadrecord.ReplaceModeAll,
		); err != nil {
			slog.WarnContext(ctx, "grab album: set replace mode failed",
				"download_record.id", rec.ID, "error", err)
		}
	}

	_, err = s.db.SetAlbumStatus(
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
