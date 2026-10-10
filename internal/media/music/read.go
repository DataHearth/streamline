package music

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/musiccredit"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/utils/numeric"
)

// progressByAlbum reads the live torrent progress, as a percentage, of every
// album with a record in flight, from the cached queue snapshot: a pack's
// progress is every linked album's. A snapshot that cannot be read leaves the
// albums without a progress rather than failing the read.
func (s *Service) progressByAlbum(ctx context.Context) map[uint32]float64 {
	snap, err := s.download.Queue(ctx)
	if err != nil {
		slog.DebugContext(ctx, "album progress unavailable", "error", err)
		return nil
	}
	out := map[uint32]float64{}
	for _, e := range snap.Items {
		pct := e.Progress * 100
		if e.Status == "importing" {
			pct = 100
		}
		if e.Album != nil {
			out[e.Album.ID] = pct
		}
		for _, id := range e.PackAlbumIDs {
			out[id] = pct
		}
	}
	return out
}

func (s *Service) List(
	ctx context.Context,
	p db.ListArtistsParams,
) (ArtistPage, error) {
	ctx, span := tracer.Start(ctx, "music.list")
	defer span.End()

	if p.Limit == 0 {
		p.Limit = defaultPageLimit
	}
	p.Now = time.Now()
	rows, err := s.db.ListArtists(ctx, p)
	if err != nil {
		return ArtistPage{}, otelx.RecordSpanError(span, err)
	}
	total, err := s.db.CountArtistsFiltered(ctx, p)
	if err != nil {
		return ArtistPage{}, otelx.RecordSpanError(span, err)
	}
	ids := make([]uint32, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	rollups, err := s.db.AlbumRollups(ctx, ids)
	if err != nil {
		return ArtistPage{}, otelx.RecordSpanError(span, err)
	}
	hydrating, err := s.db.HydratingArtists(ctx, ids, p.Now)
	if err != nil {
		return ArtistPage{}, otelx.RecordSpanError(span, err)
	}
	progress := s.progressByAlbum(ctx)

	items := make([]ArtistView, len(rows))
	for i, r := range rows {
		v := ArtistView{
			Artist:     r,
			Status:     artistStatusAt(r.Edges.Albums, p.Now),
			AlbumCount: numeric.SaturateU32(len(r.Edges.Albums)),
			Hydrating:  hydrating[r.ID],
		}
		v.Overview, v.OverviewSource = pickOverview(r, "en")
		albums := slices.Clone(r.Edges.Albums)
		slices.SortStableFunc(albums, byNewest)
		for _, a := range albums {
			t := tile(a, rollups[a.ID], progress, p.Now)
			v.TracksHave += t.TracksHave
			v.Size += t.Size
			v.Albums = append(v.Albums, t)
		}
		items[i] = v
	}
	span.SetAttributes(attribute.Int("artists.count", len(items)))
	return ArtistPage{Items: items, Total: numeric.SaturateU32(total)}, nil
}

func tile(
	a *ent.Album,
	r db.AlbumRollup,
	progress map[uint32]float64,
	now time.Time,
) AlbumView {
	v := AlbumView{
		Album:         a,
		Status:        albumStatusAt(a, now),
		TrackCount:    r.TrackCount,
		TracksHave:    r.TracksHave,
		Size:          r.Size,
		Duration:      r.Duration,
		TracksPending: a.MetadataFetchedAt == nil,
	}
	if a.Status == album.StatusDownloading {
		if pct, ok := progress[a.ID]; ok {
			v.Progress = &pct
		}
	}
	return v
}

func (s *Service) Counts(
	ctx context.Context,
	p db.ListArtistsParams,
) (db.ArtistCounts, error) {
	ctx, span := tracer.Start(ctx, "music.counts")
	defer span.End()
	p.Now = time.Now()
	c, err := s.db.ArtistCounts(ctx, p)
	return c, otelx.RecordSpanError(span, err)
}

// Detail reads one artist with its full tree: members, and every album with
// tracks, credits and personnel. lang picks the overview ("fr", else English).
func (s *Service) Detail(
	ctx context.Context,
	id uint32,
	lang string,
) (*ArtistView, error) {
	ctx, span := tracer.Start(ctx, "music.detail",
		trace.WithAttributes(attribute.Int("artist.id", int(id))))
	defer span.End()

	row, err := s.db.FindArtistByID(ctx, id)
	if err != nil {
		return nil, otelx.RecordSpanError(span, notFound(err))
	}
	now := time.Now()
	hydrating, err := s.db.HydratingArtists(ctx, []uint32{id}, now)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	progress := s.progressByAlbum(ctx)

	albums := slices.Clone(row.Edges.Albums)
	slices.SortStableFunc(albums, byNewest)
	people, err := s.libraryPeople(ctx, row, albums)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}

	v := &ArtistView{
		Artist:     row,
		Status:     artistStatusAt(albums, now),
		AlbumCount: numeric.SaturateU32(len(albums)),
		Hydrating:  hydrating[id],
	}
	v.Overview, v.OverviewSource = pickOverview(row, lang)
	for _, a := range albums {
		av := fullAlbum(a, progress, people, now)
		v.TracksHave += av.TracksHave
		v.Size += av.Size
		v.Albums = append(v.Albums, av)
	}
	for _, m := range row.Edges.Members {
		mv := MemberView{
			PersonView:  people.person(m.Name, m.Mbid),
			Instruments: splitList(m.Instruments),
			From:        m.FromYear,
			To:          m.ToYear,
		}
		if mv.From == 0 {
			mv.From = row.Since
		}
		v.Members = append(v.Members, mv)
	}
	return v, nil
}

// AlbumDetail reads one album with its tracks, credits and personnel.
func (s *Service) AlbumDetail(ctx context.Context, id uint32) (*AlbumView, error) {
	ctx, span := tracer.Start(ctx, "music.album_detail",
		trace.WithAttributes(attribute.Int("album.id", int(id))))
	defer span.End()

	a, err := s.db.FindAlbumByID(ctx, id)
	if err != nil {
		return nil, otelx.RecordSpanError(span, albumNotFound(err))
	}
	people, err := s.libraryPeople(ctx, nil, []*ent.Album{a})
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	v := fullAlbum(a, s.progressByAlbum(ctx), people, time.Now())
	return &v, nil
}

// peopleIndex resolves a MusicBrainz artist id to the library artist it
// belongs to.
type peopleIndex map[string]uint32

func (p peopleIndex) person(name, mbid string) PersonView {
	return PersonView{Name: name, MBID: mbid, ArtistID: p[mbid]}
}

// libraryPeople resolves every credited MusicBrainz id of the response in one
// query.
func (s *Service) libraryPeople(
	ctx context.Context,
	a *ent.Artist,
	albums []*ent.Album,
) (peopleIndex, error) {
	seen := map[string]struct{}{}
	add := func(mbid string) {
		if mbid != "" {
			seen[mbid] = struct{}{}
		}
	}
	if a != nil {
		for _, m := range a.Edges.Members {
			add(m.Mbid)
		}
	}
	for _, al := range albums {
		for _, c := range al.Edges.Credits {
			add(c.Mbid)
		}
		for _, t := range al.Edges.Tracks {
			for _, c := range t.Edges.Credits {
				add(c.Mbid)
			}
		}
	}
	mbids := make([]string, 0, len(seen))
	for m := range seen {
		mbids = append(mbids, m)
	}
	ids, err := s.db.ArtistIDsByMBID(ctx, mbids)
	return peopleIndex(ids), err
}

func fullAlbum(
	a *ent.Album,
	progress map[uint32]float64,
	people peopleIndex,
	now time.Time,
) AlbumView {
	v := AlbumView{
		Album:         a,
		Status:        albumStatusAt(a, now),
		TrackCount:    numeric.SaturateU32(len(a.Edges.Tracks)),
		TracksPending: a.MetadataFetchedAt == nil,
	}
	if a.Status == album.StatusDownloading {
		if pct, ok := progress[a.ID]; ok {
			v.Progress = &pct
		}
	}
	for _, t := range a.Edges.Tracks {
		tv := TrackView{Track: t, HasFile: len(t.Edges.MediaFiles) > 0}
		if tv.HasFile {
			v.TracksHave++
		}
		for _, f := range t.Edges.MediaFiles {
			v.Size += f.Size
		}
		v.Duration += t.Duration
		for _, c := range creditsOf(t.Edges.Credits, musiccredit.KindFeaturing) {
			tv.Featuring = append(tv.Featuring, people.person(c.Name, c.Mbid))
		}
		for _, c := range creditsOf(t.Edges.Credits, musiccredit.KindWriter) {
			tv.Writers = append(tv.Writers, people.person(c.Name, c.Mbid))
		}
		v.Tracks = append(v.Tracks, tv)
	}
	files := albumFiles(a)
	if t, ok := worstTier(files); ok {
		v.Quality = t.String()
	}
	v.Format = formatLabel(files)
	for _, c := range creditsOf(a.Edges.Credits, musiccredit.KindCredit) {
		v.Credits = append(v.Credits, CreditView{
			PersonView: people.person(c.Name, c.Mbid),
			Role:       string(c.Role),
		})
	}
	for _, c := range creditsOf(a.Edges.Credits, musiccredit.KindPerformer) {
		v.Personnel = append(v.Personnel, PerformerView{
			PersonView:  people.person(c.Name, c.Mbid),
			Instruments: splitList(c.Instruments),
			Guest:       c.Guest,
		})
	}
	return v
}
