package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/artist"
	"github.com/datahearth/streamline/ent/artistmember"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/musiccredit"
	"github.com/datahearth/streamline/ent/predicate"
	"github.com/datahearth/streamline/ent/track"
	"github.com/datahearth/streamline/internal/utils/numeric"
)

const (
	// hydratingCreditsWindow is how long after an album's tracks land its
	// missing credits still count as the artist hydrating. A heavy release
	// call that failed is retried by the metadata sweep, possibly a day later;
	// the SPA must not poll for that long.
	hydratingCreditsWindow = time.Hour

	// MaxCreditsTracks is the largest release the credits sweep retries: the
	// heavy relationship expansion of a box set outgrows the response cap.
	MaxCreditsTracks = 80
)

// PersonSeed is a credited person: a featured artist or a writer of a track.
type PersonSeed struct {
	Name string
	MBID string
}

type TrackSeed struct {
	MBID      string
	Title     string
	Disc      uint8
	Position  uint16
	Duration  uint32
	Bonus     bool
	Featuring []PersonSeed
	Writers   []PersonSeed
}

// AlbumCreditSeed is one production credit of an album; Role is one of the
// musiccredit role values.
type AlbumCreditSeed struct {
	PersonSeed
	Role string
}

type PerformerSeed struct {
	PersonSeed
	Instruments []string
	Guest       bool
}

// AlbumSeed is a release group as the artist's discography lists it. The
// tracks arrive later, through SetAlbumHydration.
type AlbumSeed struct {
	MBID        string
	Title       string
	Type        string
	ReleaseDate *time.Time
	Monitored   bool
}

type MemberSeed struct {
	Name        string
	MBID        string
	Instruments []string
	FromYear    uint16
	ToYear      uint16
}

type CreateArtistParams struct {
	MBID           string
	Name           string
	SortName       string
	Monitor        artist.Monitor
	Type           string
	Origin         string
	Since          uint16
	Genre          string
	DeezerID       uint32
	WikidataID     string
	Path           string
	QualityProfile string
	Members        []MemberSeed
	Albums         []AlbumSeed
}

// RefreshArtistParams carries provider-sourced fields. Albums already stored
// keep their monitored flag and status; only albums new to the artist are
// created, with the seed's Monitored.
type RefreshArtistParams struct {
	Name        string
	SortName    string
	Type        string
	Origin      string
	Since       uint16
	Genre       string
	DeezerID    uint32
	WikidataID  string
	Members     []MemberSeed
	Albums      []AlbumSeed
	RefreshedAt time.Time
}

// ArtistDetailsParams are the overview fields the Wikipedia step fills; only
// non-empty ones are written, so a locale with no article keeps what it had.
type ArtistDetailsParams struct {
	Overview         string
	OverviewSource   string
	OverviewFR       string
	OverviewSourceFR string
	DeezerID         uint32
}

// HydrationParams is everything one album's release call yields. Credits and
// Performers are written only when CreditsComplete: a light fallback has no
// relationships and must not erase what an earlier heavy call stored.
type HydrationParams struct {
	ReleaseMBID     string
	Barcode         string
	Label           string
	CatalogNumber   string
	Country         string
	Media           []string
	Studio          string
	Tracks          []TrackSeed
	Credits         []AlbumCreditSeed
	Performers      []PerformerSeed
	CreditsComplete bool
}

func joinCSV(s []string) string { return strings.Join(s, ",") }

func createAlbum(
	ctx context.Context,
	c *ent.Client,
	artistID uint32,
	a AlbumSeed,
) (uint32, error) {
	b := c.Album.Create().
		SetMbid(a.MBID).
		SetTitle(a.Title).
		SetMonitored(a.Monitored).
		SetArtistID(artistID).
		SetNillableReleaseDate(a.ReleaseDate)
	if a.Type != "" {
		b = b.SetType(album.Type(a.Type))
	}
	row, err := b.Save(ctx)
	if err != nil {
		return 0, err
	}
	return row.ID, nil
}

func replaceMembers(
	ctx context.Context,
	c *ent.Client,
	artistID uint32,
	members []MemberSeed,
) error {
	if _, err := c.ArtistMember.Delete().
		Where(artistmember.HasArtistWith(artist.IDEQ(artistID))).
		Exec(ctx); err != nil {
		return err
	}
	if len(members) == 0 {
		return nil
	}
	creates := make([]*ent.ArtistMemberCreate, len(members))
	for i, m := range members {
		creates[i] = c.ArtistMember.Create().
			SetName(m.Name).
			SetMbid(m.MBID).
			SetInstruments(joinCSV(m.Instruments)).
			SetFromYear(m.FromYear).
			SetToYear(m.ToYear).
			SetOrdinal(numeric.SaturateU16(i)).
			SetArtistID(artistID)
	}
	return c.ArtistMember.CreateBulk(creates...).Exec(ctx)
}

func (db *DB) CreateArtist(
	ctx context.Context,
	p CreateArtistParams,
) (*ent.Artist, error) {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	b := tx.Artist.Create().
		SetMbid(p.MBID).
		SetName(p.Name).
		SetSortName(p.SortName).
		SetOrigin(p.Origin).
		SetSince(p.Since).
		SetGenre(p.Genre).
		SetDeezerID(p.DeezerID).
		SetWikidataID(p.WikidataID).
		SetPath(p.Path).
		SetQualityProfile(p.QualityProfile)
	if p.Monitor != "" {
		b = b.SetMonitor(p.Monitor)
	}
	if p.Type != "" {
		b = b.SetType(artist.Type(p.Type))
	}
	row, err := b.Save(ctx)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := replaceMembers(ctx, tx.Client(), row.ID, p.Members); err != nil {
		tx.Rollback()
		return nil, err
	}
	for _, a := range p.Albums {
		if _, err := createAlbum(ctx, tx.Client(), row.ID, a); err != nil {
			tx.Rollback()
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return db.FindArtistByID(ctx, row.ID)
}

// withAlbumTree eager-loads an album's tracks in disc order with their files
// and credits, and the album's own credits.
func withAlbumTree(q *ent.AlbumQuery) {
	q.WithCredits(func(cq *ent.MusicCreditQuery) {
		cq.Order(ent.Asc(musiccredit.FieldOrdinal))
	}).WithTracks(func(tq *ent.TrackQuery) {
		tq.Order(ent.Asc(track.FieldDisc), ent.Asc(track.FieldPosition)).
			WithMediaFiles().
			WithCredits(func(cq *ent.MusicCreditQuery) {
				cq.Order(ent.Asc(musiccredit.FieldOrdinal))
			})
	})
}

func (db *DB) FindArtistByID(ctx context.Context, id uint32) (*ent.Artist, error) {
	return db.client.Artist.Query().
		Where(artist.IDEQ(id)).
		WithMembers(func(mq *ent.ArtistMemberQuery) {
			mq.Order(ent.Asc(artistmember.FieldOrdinal))
		}).
		WithAlbums(func(q *ent.AlbumQuery) {
			q.Order(ent.Desc(album.FieldReleaseDate), ent.Asc(album.FieldTitle))
			withAlbumTree(q)
		}).
		Only(ctx)
}

func (db *DB) FindAlbumByID(ctx context.Context, id uint32) (*ent.Album, error) {
	q := db.client.Album.Query().Where(album.IDEQ(id)).WithArtist()
	withAlbumTree(q)
	return q.Only(ctx)
}

func (db *DB) FindTrackByID(ctx context.Context, id uint32) (*ent.Track, error) {
	return db.client.Track.Query().
		Where(track.IDEQ(id)).
		WithMediaFiles().
		WithAlbum(func(q *ent.AlbumQuery) { q.WithArtist() }).
		Only(ctx)
}

func (db *DB) FindArtistByMBID(
	ctx context.Context,
	mbid string,
) (*ent.Artist, error) {
	row, err := db.client.Artist.Query().Where(artist.MbidEQ(mbid)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	return row, err
}

// ArtistIDsByMBID maps each of the mbids that is a library artist to its id.
func (db *DB) ArtistIDsByMBID(
	ctx context.Context,
	mbids []string,
) (map[string]uint32, error) {
	out := make(map[string]uint32, len(mbids))
	if len(mbids) == 0 {
		return out, nil
	}
	rows, err := db.client.Artist.Query().
		Where(artist.MbidIn(mbids...)).
		Select(artist.FieldID, artist.FieldMbid).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("artist ids by mbid: %w", err)
	}
	for _, r := range rows {
		out[r.Mbid] = r.ID
	}
	return out, nil
}

func (db *DB) AlbumIDsByMBID(
	ctx context.Context,
	mbids []string,
) (map[string]uint32, error) {
	out := make(map[string]uint32, len(mbids))
	if len(mbids) == 0 {
		return out, nil
	}
	rows, err := db.client.Album.Query().
		Where(album.MbidIn(mbids...)).
		Select(album.FieldID, album.FieldMbid).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("album ids by mbid: %w", err)
	}
	for _, r := range rows {
		out[r.Mbid] = r.ID
	}
	return out, nil
}

// ListArtistsParams filters, sorts and pages the artist list. Status is
// "wanted", "downloading", "available" or ""; Monitored is "monitored",
// "unmonitored" or ""; Sort is "recent" or "name".
type ListArtistsParams struct {
	Status    string
	Monitored string
	Query     string
	Sort      string
	Order     string
	Offset    uint32
	Limit     uint16
	// Now anchors the upcoming split, so the filter and the counts agree.
	Now time.Time
}

// ArtistCounts is the faceted tally behind the artist list's toolbar: each
// facet is counted with the other facet's filter applied and its own left
// out, each with its own "all" row, and the query narrows every one.
type ArtistCounts struct {
	Total          uint32
	StatusTotal    uint32
	Wanted         uint32
	Downloading    uint32
	Available      uint32
	MonitoredTotal uint32
	Monitored      uint32
	Unmonitored    uint32
	Albums         uint32
}

// releasedAlbum is an album that is not upcoming: undated, or dated today or
// earlier. An undated album is never upcoming.
func releasedAlbum(now time.Time) predicate.Album {
	return album.Or(album.ReleaseDateIsNil(), album.ReleaseDateLTE(now))
}

// wantedAlbum is a monitored album the library is missing and that is out.
func wantedAlbum(now time.Time) predicate.Album {
	return album.And(
		album.Monitored(true),
		album.StatusEQ(album.StatusWanted),
		releasedAlbum(now),
	)
}

func downloadingAlbum() predicate.Album {
	return album.And(
		album.Monitored(true),
		album.StatusIn(album.StatusDownloading, album.StatusPaused),
	)
}

// artistStatus is the rollup over the monitored, non-upcoming albums:
// downloading if any is downloading or paused, else wanted if any is wanted,
// else available.
func artistStatus(status string, now time.Time) predicate.Artist {
	downloading := artist.HasAlbumsWith(downloadingAlbum())
	wanted := artist.HasAlbumsWith(wantedAlbum(now))
	switch status {
	case "downloading":
		return downloading
	case "wanted":
		return artist.And(artist.Not(downloading), wanted)
	case "available":
		return artist.And(artist.Not(downloading), artist.Not(wanted))
	}
	return nil
}

func artistFilters(p ListArtistsParams, skip facet) []predicate.Artist {
	var out []predicate.Artist
	if p.Status != "" && skip != facetStatus {
		if pr := artistStatus(p.Status, p.Now); pr != nil {
			out = append(out, pr)
		}
	}
	switch {
	case skip == facetMonitored:
	case p.Monitored == "monitored":
		out = append(out, artist.MonitorNEQ(artist.MonitorNone))
	case p.Monitored == "unmonitored":
		out = append(out, artist.MonitorEQ(artist.MonitorNone))
	}
	if p.Query != "" {
		out = append(out, artist.Or(
			func(s *entsql.Selector) {
				s.Where(entsql.Or(
					foldContains(s, artist.FieldName, p.Query),
					foldContains(s, artist.FieldSortName, p.Query),
					foldContains(s, artist.FieldGenre, p.Query),
				))
			},
			artist.HasAlbumsWith(func(s *entsql.Selector) {
				s.Where(foldContains(s, album.FieldTitle, p.Query))
			}),
		))
	}
	return out
}

// CountArtistsFiltered counts the artists the list's filters match, which is
// the list's total.
func (db *DB) CountArtistsFiltered(
	ctx context.Context,
	p ListArtistsParams,
) (int, error) {
	return db.client.Artist.Query().
		Where(artistFilters(p, facetAll)...).
		Count(ctx)
}

// ListArtists returns one page of the filtered, sorted list with each artist's
// albums loaded as tiles: no tracks, no credits.
func (db *DB) ListArtists(
	ctx context.Context,
	p ListArtistsParams,
) ([]*ent.Artist, error) {
	desc := p.Order == "desc"
	var order []artist.OrderOption
	switch p.Sort {
	case "name":
		if desc {
			order = append(order,
				artist.BySortName(entsql.OrderDesc()),
				artist.ByName(entsql.OrderDesc()))
		} else {
			order = append(order, artist.BySortName(), artist.ByName())
		}
	default:
		if p.Order == "asc" {
			order = append(order, artist.ByCreateTime())
		} else {
			order = append(order, artist.ByCreateTime(entsql.OrderDesc()))
		}
	}
	order = append(order, artist.ByID())
	return db.client.Artist.Query().
		Where(artistFilters(p, facetAll)...).
		Order(order...).
		Offset(int(p.Offset)).
		Limit(int(p.Limit)).
		WithAlbums().
		All(ctx)
}

func (db *DB) ArtistCounts(
	ctx context.Context,
	p ListArtistsParams,
) (ArtistCounts, error) {
	count := func(skip facet, extra ...predicate.Artist) (uint32, error) {
		n, err := db.client.Artist.Query().
			Where(append(artistFilters(p, skip), extra...)...).
			Count(ctx)
		return numeric.SaturateU32(n), err
	}
	var (
		c   ArtistCounts
		err error
	)
	steps := []struct {
		dst   *uint32
		skip  facet
		extra []predicate.Artist
	}{
		{&c.StatusTotal, facetStatus, nil},
		{&c.Wanted, facetStatus, []predicate.Artist{artistStatus("wanted", p.Now)}},
		{
			&c.Downloading, facetStatus,
			[]predicate.Artist{artistStatus("downloading", p.Now)},
		},
		{
			&c.Available, facetStatus,
			[]predicate.Artist{artistStatus("available", p.Now)},
		},
		{&c.MonitoredTotal, facetMonitored, nil},
		{
			&c.Monitored, facetMonitored,
			[]predicate.Artist{artist.MonitorNEQ(artist.MonitorNone)},
		},
		{
			&c.Unmonitored, facetMonitored,
			[]predicate.Artist{artist.MonitorEQ(artist.MonitorNone)},
		},
	}
	for _, s := range steps {
		if *s.dst, err = count(s.skip, s.extra...); err != nil {
			return ArtistCounts{}, fmt.Errorf("count artists: %w", err)
		}
	}
	total, err := db.client.Artist.Query().Count(ctx)
	if err != nil {
		return ArtistCounts{}, fmt.Errorf("count artists: %w", err)
	}
	albums, err := db.client.Album.Query().Count(ctx)
	if err != nil {
		return ArtistCounts{}, fmt.Errorf("count albums: %w", err)
	}
	c.Total, c.Albums = numeric.SaturateU32(total), numeric.SaturateU32(albums)
	return c, nil
}

// AlbumRollup is what an album tile reports about its tracks without loading
// them.
type AlbumRollup struct {
	TrackCount uint32
	TracksHave uint32
	Size       int64
	Duration   uint32
}

// AlbumRollups tallies tracks, tracks holding a file, file bytes and runtime
// per album for the given artists in two grouped queries, so a page of the
// artist list costs two queries however many albums it shows.
func (db *DB) AlbumRollups(
	ctx context.Context,
	artistIDs []uint32,
) (map[uint32]AlbumRollup, error) {
	out := map[uint32]AlbumRollup{}
	if len(artistIDs) == 0 {
		return out, nil
	}
	var trackRows []struct {
		AlbumID  uint32 `sql:"album_id"`
		N        uint32 `sql:"n"`
		Duration int64  `sql:"duration"`
	}
	err := db.client.Track.Query().
		Where(track.HasAlbumWith(album.HasArtistWith(artist.IDIn(artistIDs...)))).
		Modify(func(s *entsql.Selector) {
			s.Select(
				entsql.As(s.C(track.AlbumColumn), "album_id"),
				entsql.As("COUNT(*)", "n"),
				entsql.As(
					"COALESCE(SUM("+s.C(track.FieldDuration)+"), 0)",
					"duration",
				),
			).GroupBy(s.C(track.AlbumColumn))
		}).
		Scan(ctx, &trackRows)
	if err != nil {
		return nil, fmt.Errorf("track rollup: %w", err)
	}
	for _, r := range trackRows {
		out[r.AlbumID] = AlbumRollup{
			TrackCount: r.N,
			Duration:   numeric.SaturateU32(int(r.Duration)),
		}
	}

	var fileRows []struct {
		AlbumID uint32 `sql:"album_id"`
		Have    uint32 `sql:"have"`
		Size    int64  `sql:"size"`
	}
	err = db.client.Track.Query().
		Where(
			track.HasMediaFiles(),
			track.HasAlbumWith(album.HasArtistWith(artist.IDIn(artistIDs...))),
		).
		Modify(func(s *entsql.Selector) {
			mf := entsql.Dialect(s.Dialect()).
				Table(mediafile.Table).
				As("rollup_file")
			sizes := entsql.Dialect(s.Dialect()).
				Select(
					entsql.As(mf.C(mediafile.TrackColumn), "track_id"),
					entsql.As("SUM("+mf.C(mediafile.FieldSize)+")", "size"),
				).
				From(mf).
				GroupBy(mf.C(mediafile.TrackColumn)).
				As("rollup_sizes")
			s.Join(sizes).On(s.C(track.FieldID), sizes.C("track_id"))
			s.Select(
				entsql.As(s.C(track.AlbumColumn), "album_id"),
				entsql.As("COUNT(*)", "have"),
				entsql.As("COALESCE(SUM("+sizes.C("size")+"), 0)", "size"),
			).GroupBy(s.C(track.AlbumColumn))
		}).
		Scan(ctx, &fileRows)
	if err != nil {
		return nil, fmt.Errorf("file rollup: %w", err)
	}
	for _, r := range fileRows {
		a := out[r.AlbumID]
		a.TracksHave, a.Size = r.Have, r.Size
		out[r.AlbumID] = a
	}
	return out, nil
}

// albumHydrating matches an album still being filled in: tracks not written
// yet, or written but the credits missing and not too long ago and the
// release small enough for the credits sweep to retry.
func albumHydrating(now time.Time) predicate.Album {
	return album.Or(
		album.MetadataFetchedAtIsNil(),
		album.And(
			album.CreditsFetchedAtIsNil(),
			album.MetadataFetchedAtGT(now.Add(-hydratingCreditsWindow)),
			album.HasTracks(),
			smallRelease(),
		),
	)
}

func smallRelease() predicate.Album {
	return func(s *entsql.Selector) {
		s.Where(entsql.ExprP(
			"(SELECT COUNT(*) FROM "+track.Table+
				" WHERE "+track.Table+"."+track.AlbumColumn+" = "+s.C(album.FieldID)+
				") <= ?",
			MaxCreditsTracks,
		))
	}
}

// HydratingArtists reports, for each of the artists, whether any album is
// still being filled in. It drives the SPA's polling.
func (db *DB) HydratingArtists(
	ctx context.Context,
	artistIDs []uint32,
	now time.Time,
) (map[uint32]bool, error) {
	out := map[uint32]bool{}
	if len(artistIDs) == 0 {
		return out, nil
	}
	ids, err := db.client.Artist.Query().
		Where(
			artist.IDIn(artistIDs...),
			artist.HasAlbumsWith(albumHydrating(now)),
		).
		IDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("hydrating artists: %w", err)
	}
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

// RefreshArtist rewrites the artist's provider-sourced fields and members and
// reconciles its release groups: stored albums update title, type and date
// only, new ones are created. It returns the ids of the created albums.
func (db *DB) RefreshArtist(
	ctx context.Context,
	id uint32,
	p RefreshArtistParams,
) ([]uint32, error) {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	c := tx.Client()
	u := c.Artist.UpdateOneID(id).
		SetName(p.Name).
		SetSortName(p.SortName).
		SetOrigin(p.Origin).
		SetSince(p.Since).
		SetGenre(p.Genre).
		SetWikidataID(p.WikidataID).
		SetLastRefreshedAt(p.RefreshedAt)
	if p.DeezerID != 0 {
		u = u.SetDeezerID(p.DeezerID)
	}
	if p.Type != "" {
		u = u.SetType(artist.Type(p.Type))
	} else {
		u = u.ClearType()
	}
	if err := u.Exec(ctx); err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := replaceMembers(ctx, c, id, p.Members); err != nil {
		tx.Rollback()
		return nil, err
	}
	existing, err := c.Album.Query().
		Where(album.HasArtistWith(artist.IDEQ(id))).
		All(ctx)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	byMBID := make(map[string]*ent.Album, len(existing))
	for _, a := range existing {
		byMBID[a.Mbid] = a
	}
	var created []uint32
	for _, a := range p.Albums {
		if cur := byMBID[a.MBID]; cur != nil {
			u := c.Album.UpdateOne(cur).
				SetTitle(a.Title).
				SetNillableReleaseDate(a.ReleaseDate)
			if a.Type != "" {
				u = u.SetType(album.Type(a.Type))
			}
			if err := u.Exec(ctx); err != nil {
				tx.Rollback()
				return nil, err
			}
			continue
		}
		newID, err := createAlbum(ctx, c, id, a)
		if err != nil {
			tx.Rollback()
			return nil, err
		}
		created = append(created, newID)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return created, nil
}

// SetArtistDetails writes the overview fields that are non-empty and stamps
// details_fetched_at.
func (db *DB) SetArtistDetails(
	ctx context.Context,
	id uint32,
	p ArtistDetailsParams,
	at time.Time,
) error {
	u := db.client.Artist.UpdateOneID(id).SetDetailsFetchedAt(at)
	if p.Overview != "" {
		u = u.SetOverview(p.Overview).SetOverviewSource(p.OverviewSource)
	}
	if p.OverviewFR != "" {
		u = u.SetOverviewFr(p.OverviewFR).SetOverviewSourceFr(p.OverviewSourceFR)
	}
	if p.DeezerID != 0 {
		u = u.SetDeezerID(p.DeezerID)
	}
	return u.Exec(ctx)
}

// SetArtistMonitor stores the policy and applies it to the artist's existing
// albums in one transaction: all and none set every album, future monitors the
// undated and the not yet released, manual leaves the flags alone. It never
// touches album status.
func (db *DB) SetArtistMonitor(
	ctx context.Context,
	id uint32,
	monitor artist.Monitor,
	now time.Time,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return err
	}
	if err := tx.Artist.UpdateOneID(id).SetMonitor(monitor).Exec(ctx); err != nil {
		tx.Rollback()
		return err
	}
	owned := album.HasArtistWith(artist.IDEQ(id))
	switch monitor {
	case artist.MonitorAll:
		err = tx.Album.Update().Where(owned).SetMonitored(true).Exec(ctx)
	case artist.MonitorNone:
		err = tx.Album.Update().Where(owned).SetMonitored(false).Exec(ctx)
	case artist.MonitorFuture:
		err = tx.Album.Update().
			Where(owned, album.Or(album.ReleaseDateIsNil(), album.ReleaseDateGT(now))).
			SetMonitored(true).Exec(ctx)
		if err == nil {
			err = tx.Album.Update().
				Where(owned, album.ReleaseDateNotNil(), album.ReleaseDateLTE(now)).
				SetMonitored(false).Exec(ctx)
		}
	}
	if err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (db *DB) SetArtistQualityProfile(
	ctx context.Context,
	id uint32,
	profile string,
) error {
	return db.client.Artist.UpdateOneID(id).SetQualityProfile(profile).Exec(ctx)
}

func (db *DB) SetAlbumMonitored(
	ctx context.Context,
	id uint32,
	monitored bool,
) error {
	return db.client.Album.UpdateOneID(id).SetMonitored(monitored).Exec(ctx)
}

// SetAlbumStatus moves the album to `to` only while it is in one of the from
// statuses, and reports whether it did, so a concurrent change is never
// overwritten.
func (db *DB) SetAlbumStatus(
	ctx context.Context,
	id uint32,
	from []album.Status,
	to album.Status,
) (bool, error) {
	n, err := db.client.Album.Update().
		Where(album.IDEQ(id), album.StatusIn(from...)).
		SetStatus(to).
		Save(ctx)
	return n > 0, err
}

func (db *DB) DeleteArtist(ctx context.Context, id uint32) error {
	return db.client.Artist.DeleteOneID(id).Exec(ctx)
}

// SetAlbumHydration stores one album's release call in a transaction: the
// album-level facts, its tracks (updated in place by disc and position, so a
// track already holding a file keeps it), the featuring credits of every
// track and, when CreditsComplete, the album's credits and performers, the
// tracks' writers and the studio. It stamps metadata_fetched_at, and
// credits_fetched_at when the credits landed; it never touches status or
// monitored.
func (db *DB) SetAlbumHydration(
	ctx context.Context,
	albumID uint32,
	p HydrationParams,
	at time.Time,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return err
	}
	c := tx.Client()
	u := c.Album.UpdateOneID(albumID).
		SetLabel(p.Label).
		SetCatalogNumber(p.CatalogNumber).
		SetCountry(p.Country).
		SetMedia(joinCSV(p.Media)).
		SetMetadataFetchedAt(at)
	if p.ReleaseMBID != "" {
		u = u.SetReleaseMbid(p.ReleaseMBID)
	}
	if p.Barcode != "" {
		u = u.SetBarcode(p.Barcode)
	}
	if p.CreditsComplete {
		u = u.SetStudio(p.Studio).SetCreditsFetchedAt(at)
	}
	if err := u.Exec(ctx); err != nil {
		tx.Rollback()
		return err
	}

	existing, err := c.Track.Query().
		Where(track.HasAlbumWith(album.IDEQ(albumID))).
		All(ctx)
	if err != nil {
		tx.Rollback()
		return err
	}
	type slot struct {
		disc uint8
		pos  uint16
	}
	byslot := make(map[slot]*ent.Track, len(existing))
	for _, t := range existing {
		byslot[slot{t.Disc, t.Position}] = t
	}
	for _, t := range p.Tracks {
		var id uint32
		if cur := byslot[slot{t.Disc, t.Position}]; cur != nil {
			id = cur.ID
			if err := c.Track.UpdateOneID(id).
				SetMbid(t.MBID).
				SetTitle(t.Title).
				SetDuration(t.Duration).
				SetBonus(t.Bonus).
				Exec(ctx); err != nil {
				tx.Rollback()
				return err
			}
		} else {
			row, err := c.Track.Create().
				SetMbid(t.MBID).
				SetTitle(t.Title).
				SetDisc(t.Disc).
				SetPosition(t.Position).
				SetDuration(t.Duration).
				SetBonus(t.Bonus).
				SetAlbumID(albumID).
				Save(ctx)
			if err != nil {
				tx.Rollback()
				return err
			}
			id = row.ID
		}
		kinds := []musiccredit.Kind{musiccredit.KindFeaturing}
		if p.CreditsComplete {
			kinds = append(kinds, musiccredit.KindWriter)
		}
		if _, err := c.MusicCredit.Delete().Where(
			musiccredit.HasTrackWith(track.IDEQ(id)),
			musiccredit.KindIn(kinds...),
		).Exec(ctx); err != nil {
			tx.Rollback()
			return err
		}
		var creates []*ent.MusicCreditCreate
		for i, f := range t.Featuring {
			creates = append(creates, c.MusicCredit.Create().
				SetKind(musiccredit.KindFeaturing).
				SetName(f.Name).SetMbid(f.MBID).
				SetOrdinal(numeric.SaturateU16(i)).SetTrackID(id))
		}
		if p.CreditsComplete {
			for i, w := range t.Writers {
				creates = append(creates, c.MusicCredit.Create().
					SetKind(musiccredit.KindWriter).
					SetName(w.Name).SetMbid(w.MBID).
					SetOrdinal(numeric.SaturateU16(i)).SetTrackID(id))
			}
		}
		if len(creates) > 0 {
			if err := c.MusicCredit.CreateBulk(creates...).Exec(ctx); err != nil {
				tx.Rollback()
				return err
			}
		}
	}

	if p.CreditsComplete {
		if _, err := c.MusicCredit.Delete().Where(
			musiccredit.HasAlbumWith(album.IDEQ(albumID)),
		).Exec(ctx); err != nil {
			tx.Rollback()
			return err
		}
		var creates []*ent.MusicCreditCreate
		for i, cr := range p.Credits {
			creates = append(creates, c.MusicCredit.Create().
				SetKind(musiccredit.KindCredit).
				SetRole(musiccredit.Role(cr.Role)).
				SetName(cr.Name).SetMbid(cr.MBID).
				SetOrdinal(numeric.SaturateU16(i)).SetAlbumID(albumID))
		}
		for i, pf := range p.Performers {
			creates = append(creates, c.MusicCredit.Create().
				SetKind(musiccredit.KindPerformer).
				SetName(pf.Name).SetMbid(pf.MBID).
				SetInstruments(joinCSV(pf.Instruments)).
				SetGuest(pf.Guest).
				SetOrdinal(numeric.SaturateU16(i)).SetAlbumID(albumID))
		}
		if len(creates) > 0 {
			if err := c.MusicCredit.CreateBulk(creates...).Exec(ctx); err != nil {
				tx.Rollback()
				return err
			}
		}
	}
	return tx.Commit()
}

// ListAlbumsAwaitingHydration returns up to limit albums whose tracks have not
// been fetched, monitored and newest first, with the artist loaded.
func (db *DB) ListAlbumsAwaitingHydration(
	ctx context.Context,
	limit int,
) ([]*ent.Album, error) {
	return db.client.Album.Query().
		Where(album.MetadataFetchedAtIsNil()).
		WithArtist().
		Order(
			ent.Desc(album.FieldMonitored),
			ent.Desc(album.FieldReleaseDate),
			ent.Asc(album.FieldID),
		).
		Limit(limit).
		All(ctx)
}

// ListAlbumsAwaitingCredits returns up to limit hydrated albums with tracks
// whose heavy release call never succeeded, small enough to retry.
func (db *DB) ListAlbumsAwaitingCredits(
	ctx context.Context,
	limit int,
) ([]*ent.Album, error) {
	return db.client.Album.Query().
		Where(
			album.MetadataFetchedAtNotNil(),
			album.CreditsFetchedAtIsNil(),
			album.HasTracks(),
			smallRelease(),
		).
		WithArtist().
		Order(ent.Asc(album.FieldMetadataFetchedAt), ent.Asc(album.FieldID)).
		Limit(limit).
		All(ctx)
}

// ListAlbumsTrackless returns up to limit albums that were hydrated before
// staleBefore and still have no tracks, released before releasedBefore or
// undated: MusicBrainz may have gained a release since.
func (db *DB) ListAlbumsTrackless(
	ctx context.Context,
	staleBefore, releasedBefore time.Time,
	limit int,
) ([]*ent.Album, error) {
	return db.client.Album.Query().
		Where(
			album.MetadataFetchedAtLT(staleBefore),
			album.Not(album.HasTracks()),
			album.Or(
				album.ReleaseDateIsNil(),
				album.ReleaseDateLT(releasedBefore),
			),
		).
		WithArtist().
		Order(ent.Asc(album.FieldMetadataFetchedAt), ent.Asc(album.FieldID)).
		Limit(limit).
		All(ctx)
}

func (db *DB) ListUpcomingAlbums(
	ctx context.Context,
	from, to time.Time,
) ([]*ent.Album, error) {
	return db.client.Album.Query().
		Where(
			album.Monitored(true),
			album.ReleaseDateGTE(from),
			album.ReleaseDateLT(to),
		).
		WithArtist().
		Order(ent.Asc(album.FieldReleaseDate)).
		All(ctx)
}

// ListEligibleAlbumsForSync returns wanted, monitored, released and hydrated
// albums under the failure cap whose cooldown has expired or never started,
// least recently searched first (never-searched rows lead, SQLite sorts NULL
// first). Albums with an in-flight download record are excluded so a stale
// status cannot trigger a second grab. An unreleased album is not missing, and
// an unhydrated one has no tracks to import into.
func (db *DB) ListEligibleAlbumsForSync(
	ctx context.Context,
	maxGrabFailures uint8,
	notSearchedSince time.Time,
) ([]*ent.Album, error) {
	return db.client.Album.Query().
		Where(
			album.StatusEQ(album.StatusWanted),
			album.Monitored(true),
			releasedAlbum(time.Now()),
			album.MetadataFetchedAtNotNil(),
			album.GrabFailuresLT(maxGrabFailures),
			album.Or(
				album.LastSearchAtIsNil(),
				album.LastSearchAtLT(notSearchedSince),
			),
			album.Not(album.HasDownloadRecordsWith(liveRecord())),
			album.Not(album.HasPackRecordsWith(liveRecord())),
		).
		WithArtist().
		Order(ent.Asc(album.FieldLastSearchAt), ent.Asc(album.FieldID)).
		All(ctx)
}

func liveRecord() predicate.DownloadRecord {
	return downloadrecord.StatusIn(inFlightRecordStatuses...)
}

// ListArtistAlbumsForSearch returns the artist's albums a search-now pass may
// work: monitored, released, hydrated and wanted. The cooldown and the failure
// cap are the caller's to waive.
func (db *DB) ListArtistAlbumsForSearch(
	ctx context.Context,
	artistID uint32,
) ([]*ent.Album, error) {
	return db.client.Album.Query().
		Where(
			album.HasArtistWith(artist.IDEQ(artistID)),
			wantedAlbum(time.Now()),
			album.MetadataFetchedAtNotNil(),
		).
		WithArtist().
		Order(ent.Desc(album.FieldReleaseDate), ent.Asc(album.FieldID)).
		All(ctx)
}

// ListPackAlbums returns the artist's albums a discography pack may cover:
// monitored, released, hydrated, and wanted or paused.
func (db *DB) ListPackAlbums(
	ctx context.Context,
	artistID uint32,
) ([]*ent.Album, error) {
	return db.client.Album.Query().
		Where(
			album.HasArtistWith(artist.IDEQ(artistID)),
			album.Monitored(true),
			releasedAlbum(time.Now()),
			album.MetadataFetchedAtNotNil(),
			album.StatusIn(album.StatusWanted, album.StatusPaused),
		).
		Order(ent.Asc(album.FieldID)).
		All(ctx)
}

func (db *DB) SetAlbumLastSearchAt(
	ctx context.Context,
	id uint32,
	when time.Time,
) error {
	return db.client.Album.UpdateOneID(id).SetLastSearchAt(when).Exec(ctx)
}

func (db *DB) IncrementAlbumGrabFailures(ctx context.Context, id uint32) error {
	return db.client.Album.UpdateOneID(id).AddGrabFailures(1).Exec(ctx)
}

func (db *DB) ResetAlbumGrabFailures(ctx context.Context, id uint32) error {
	return db.client.Album.UpdateOneID(id).SetGrabFailures(0).Exec(ctx)
}

// ListArtistsStaleSince returns at most limit artists never refreshed or last
// refreshed before cutoff, oldest first. Keyed on last_refreshed_at, not
// update_time, which moves on every write.
func (db *DB) ListArtistsStaleSince(
	ctx context.Context,
	cutoff time.Time,
	limit int,
) ([]*ent.Artist, error) {
	return db.client.Artist.Query().
		Where(artist.Or(
			artist.LastRefreshedAtIsNil(),
			artist.LastRefreshedAtLT(cutoff),
		)).
		Order(ent.Asc(artist.FieldLastRefreshedAt), ent.Asc(artist.FieldID)).
		Limit(limit).
		All(ctx)
}

// ListWantedAlbums returns the monitored, released, hydrated and wanted albums
// under the grab-failure cap that no live download record already covers,
// with their artist loaded. The cooldown is not applied: the feed scanner
// already holds the release.
func (db *DB) ListWantedAlbums(
	ctx context.Context,
	maxGrabFailures uint8,
) ([]*ent.Album, error) {
	return db.client.Album.Query().
		Where(
			album.Monitored(true),
			album.StatusEQ(album.StatusWanted),
			releasedAlbum(time.Now()),
			album.MetadataFetchedAtNotNil(),
			album.GrabFailuresLT(maxGrabFailures),
			album.Not(album.HasDownloadRecordsWith(liveRecord())),
			album.Not(album.HasPackRecordsWith(liveRecord())),
		).
		WithArtist().
		All(ctx)
}

// ArtistMemberMBIDs returns the MusicBrainz ids of the artist's members that
// carry one.
func (db *DB) ArtistMemberMBIDs(
	ctx context.Context,
	artistID uint32,
) ([]string, error) {
	return db.client.ArtistMember.Query().
		Where(
			artistmember.HasArtistWith(artist.IDEQ(artistID)),
			artistmember.MbidNEQ(""),
		).
		Select(artistmember.FieldMbid).
		Strings(ctx)
}

// AlbumHasLiveRecord reports whether a download for the album is in flight,
// whether filed under the album or as part of a discography pack.
func (db *DB) AlbumHasLiveRecord(ctx context.Context, albumID uint32) (bool, error) {
	return db.client.Album.Query().
		Where(
			album.IDEQ(albumID),
			album.Or(
				album.HasDownloadRecordsWith(liveRecord()),
				album.HasPackRecordsWith(liveRecord()),
			),
		).
		Exist(ctx)
}

// FindArtistRow returns the artist alone: no albums, members or credits.
func (db *DB) FindArtistRow(ctx context.Context, id uint32) (*ent.Artist, error) {
	return db.client.Artist.Get(ctx, id)
}
