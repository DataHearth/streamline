package music

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/metadatachange"
	"github.com/datahearth/streamline/ent/track"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/metadata"
)

var _ = Describe("Provider changes", Label("unit", "integration", "music"), func() {
	var (
		e   *env
		alb *ent.Album
	)

	BeforeEach(func() {
		e = newEnv(false)
		_, albums := e.seedArtist("Dream Theater", albumSeed{
			mbid: "rg-1", title: "Train of Thought", monitored: true,
			tracks: []string{"A", "B"},
		})
		alb = albums[0]
		e.client.Album.UpdateOneID(alb.ID).SetReleaseMbid("rel-us").ExecX(e.ctx)
		for _, t := range tracksOf(e, alb.ID) {
			e.client.Track.UpdateOne(t).
				SetReleaseTrackMbid("rt-" + t.Title).
				ExecX(e.ctx)
			e.addFile(t, "/music/"+t.Title+".flac", "lossless")
		}
	})

	release := func(id string, titles ...string) *metadata.ReleaseGroupDetails {
		d := &metadata.ReleaseGroupDetails{
			MBID: "rg-1", ReleaseMBID: id, Canonical: id, EditionListed: true,
			CreditsComplete: true,
			Editions: []metadata.ReleaseEdition{
				{MBID: "rel-us", Country: "US", Tracks: 2},
				{MBID: "rel-jp", Country: "JP", Tracks: 3},
			},
		}
		for i, t := range titles {
			d.Tracks = append(d.Tracks, metadata.TrackInfo{
				MBID: "rec-" + t, ReleaseTrackMBID: "rt-" + t, Title: t,
				Disc: 1, Position: uint16(i + 1),
			})
		}
		return d
	}
	onEdition := func(d *metadata.ReleaseGroupDetails) {
		e.provider.EXPECT().GetReleaseGroupEdition(mock.Anything, "rg-1", "rel-us").
			Return(d, nil).Once()
	}
	refresh := func() {
		GinkgoHelper()
		_, err := e.svc.hydrateAlbum(e.ctx, alb.ID, hydrateRefresh)
		Expect(err).NotTo(HaveOccurred())
	}
	only := func() *ent.MetadataChange {
		GinkgoHelper()
		return e.client.MetadataChange.Query().WithAlbum().OnlyX(e.ctx)
	}
	none := func() {
		GinkgoHelper()
		Expect(e.client.MetadataChange.Query().CountX(e.ctx)).To(BeZero())
	}

	It("asks about a preferred edition and keeps the one the files are of", func() {
		d := release("rel-us", "A", "B")
		d.Canonical = "rel-jp"
		onEdition(d)
		refresh()

		c := only()
		Expect(c.Kind).To(Equal(metadatachange.KindEditionPreferred))
		Expect(c.Fingerprint).To(Equal("rel-jp"))
		Expect(
			c.Payload,
		).To(HaveKeyWithValue("upstream", HaveKeyWithValue("country", "JP")))
		Expect(c.Payload).To(HaveKeyWithValue("files", BeNumerically("==", 2)))
		Expect(e.client.Album.GetX(e.ctx, alb.ID).ReleaseMbid).To(Equal("rel-us"))
	})

	It("keeps the album as it is when its edition was merged away", func() {
		d := release("rel-jp", "A", "B", "C")
		d.EditionListed = false
		onEdition(d)
		e.provider.EXPECT().
			ResolveRelease(mock.Anything, "rel-us").
			Return("rel-jp", nil).
			Once()
		refresh()

		c := only()
		Expect(c.Kind).To(Equal(metadatachange.KindEditionMerged))
		Expect(c.Fingerprint).To(Equal("rel-jp"))
		Expect(tracksOf(e, alb.ID)).To(HaveLen(2))
	})

	It("records an edition MusicBrainz deleted", func() {
		d := release("rel-jp", "A", "B", "C")
		d.EditionListed = false
		onEdition(d)
		e.provider.EXPECT().ResolveRelease(mock.Anything, "rel-us").
			Return("", metadata.ErrNotFound).Once()
		refresh()

		Expect(only().Kind).To(Equal(metadatachange.KindEditionRemoved))
		Expect(tracksOf(e, alb.ID)).To(HaveLen(2))
	})

	It("asks about a track holding a file that left the tracklist", func() {
		onEdition(release("rel-us", "A"))
		refresh()

		c := only()
		Expect(c.Kind).To(Equal(metadatachange.KindTracksUnlisted))
		Expect(c.Payload).To(HaveKeyWithValue("items",
			ContainElement(HaveKeyWithValue("title", "B"))))
		Expect(tracksOf(e, alb.ID)).To(HaveLen(2))
	})

	It("records a release group merged into another", func() {
		d := release("rel-us", "A", "B")
		d.MBID = "rg-2"
		onEdition(d)
		refresh()

		c := only()
		Expect(c.Kind).To(Equal(metadatachange.KindAlbumMerged))
		Expect(c.Fingerprint).To(Equal("rg-2"))
	})

	It("forgets a change once MusicBrainz no longer has it", func() {
		d := release("rel-us", "A", "B")
		d.Canonical = "rel-jp"
		onEdition(d)
		refresh()
		only()

		onEdition(release("rel-us", "A", "B"))
		refresh()
		none()
	})

	It("follows MusicBrainz without asking for an album holding no files", func() {
		e.client.MediaFile.Delete().Where(mediafile.HasTrackWith(
			track.HasAlbumWith(album.ID(alb.ID)))).ExecX(e.ctx)
		e.provider.EXPECT().GetReleaseGroup(mock.Anything, "rg-1").
			Return(release("rel-jp", "A", "B", "C"), nil).Once()
		refresh()

		none()
		Expect(e.client.Album.GetX(e.ctx, alb.ID).ReleaseMbid).To(Equal("rel-jp"))
		Expect(tracksOf(e, alb.ID)).To(HaveLen(3))
	})

	Describe("following", func() {
		It("drops the tracks that left the tracklist", func() {
			onEdition(release("rel-us", "A"))
			refresh()

			Expect(e.svc.FollowChange(e.ctx, only())).To(Succeed())
			Expect(tracksOf(e, alb.ID)).To(ConsistOf(HaveField("Title", "A")))
			none()
		})

		It("moves to the edition MusicBrainz prefers", func() {
			d := release("rel-us", "A", "B")
			d.Canonical = "rel-jp"
			onEdition(d)
			refresh()

			e.provider.EXPECT().ReleaseGroupEditions(mock.Anything, "rg-1").
				Return(d.Editions, nil).Once()
			e.provider.EXPECT().GetReleaseGroup(mock.Anything, "rg-1").
				Return(release("rel-jp", "A", "B", "C"), nil).Once()
			Expect(e.svc.FollowChange(e.ctx, only())).To(Succeed())

			Expect(
				e.client.Album.GetX(e.ctx, alb.ID).ReleaseMbid,
			).To(Equal("rel-jp"))
			Expect(tracksOf(e, alb.ID)).To(HaveLen(3))
			none()
		})

		It("has nothing to follow for an album MusicBrainz deleted", func() {
			e.provider.EXPECT().
				GetReleaseGroupEdition(mock.Anything, "rg-1", "rel-us").
				Return(nil, metadata.ErrNotFound).
				Once()
			refresh()

			c := only()
			Expect(c.Kind).To(Equal(metadatachange.KindAlbumRemoved))
			Expect(e.svc.FollowChange(e.ctx, c)).To(MatchError(db.ErrNotFollowable))
		})
	})
})

func tracksOf(e *env, albumID uint32) []*ent.Track {
	GinkgoHelper()
	return e.client.Track.Query().
		Where(track.HasAlbumWith(album.ID(albumID))).
		Order(ent.Asc(track.FieldPosition)).
		AllX(e.ctx)
}
