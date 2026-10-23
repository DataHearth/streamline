package music

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/track"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/indexer"
)

var _ = Describe("ArtistPackFiles", Label("unit", "integration", "music"), func() {
	var e *env

	BeforeEach(func() {
		e = newEnv(false)
	})

	release := indexer.SearchResult{
		Title:    "Avatar - Discography (2006-2020) [FLAC]",
		Download: "https://indexer.example/dl/2",
	}
	at := func(y int) *time.Time {
		t := time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC)
		return &t
	}

	It(
		"matches folders to the albums the grab would link, then files to tracks",
		func() {
			a, albums := e.seedArtist("Avatar",
				albumSeed{
					mbid:      "rg-ff",
					title:     "Feathers & Flesh",
					date:      at(2016),
					monitored: true,
					tracks: []string{
						"Regret",
						"House of Eternal Hunt",
						"The Eagle Has Landed",
					},
				},
				albumSeed{
					mbid:      "rg-hta",
					title:     "Hail the Apocalypse",
					date:      at(2014),
					monitored: true,
					tracks:    []string{"Hail the Apocalypse"},
				},
				albumSeed{
					mbid:      "rg-ac",
					title:     "Avatar Country",
					date:      at(2018),
					monitored: true,
					tracks:    []string{"Glory to Our King", "Legend of the King"},
				},
			)
			ff, hta, ac := albums[0], albums[1], albums[2]
			// One track of Feathers & Flesh is on disk; Hail the Apocalypse is
			// complete, so no grab links it.
			regret := e.client.Track.Query().
				Where(track.HasAlbumWith(album.IDEQ(ff.ID)), track.Position(1)).
				OnlyX(e.ctx)
			e.addFile(regret, "/music/Avatar/FF/01.flac", "lossless")
			e.client.Album.UpdateOneID(hta.ID).
				SetStatus(album.StatusAvailable).
				ExecX(e.ctx)

			e.dl.EXPECT().ReleaseFiles(mock.Anything, release).
				Return([]download.ReleaseFile{
					{Path: "Feathers & Flesh (2016)/01 - Regret.flac", Size: 1},
					{
						Path: "Feathers & Flesh (2016)/03 - The Eagle Has Landed.flac",
						Size: 1,
					},
					{Path: "Feathers & Flesh (2016)/cover.jpg", Size: 1},
					{
						Path: "Hail the Apocalypse (2014)/CD1/01 - Hail the Apocalypse.flac",
						Size: 1,
					},
					{Path: "Bonus/01 - Live.flac", Size: 1},
				}, nil).Once()

			v, err := e.svc.ArtistPackFiles(e.ctx, a.ID, release)
			Expect(err).NotTo(HaveOccurred())

			Expect(v.Folders).To(Equal([]PackFolderView{
				{Path: "Feathers & Flesh (2016)", AlbumID: ff.ID, Waiting: true},
				// A disc subfolder belongs to its parent; the album is complete.
				{Path: "Hail the Apocalypse (2014)", AlbumID: hta.ID},
				{Path: "Bonus"},
			}))

			Expect(v.Albums).To(HaveLen(2))
			byID := map[uint32]PackAlbumView{}
			for _, al := range v.Albums {
				byID[al.ID] = al
			}
			Expect(byID).To(HaveKey(ff.ID))
			Expect(byID).To(HaveKey(ac.ID))
			Expect(byID[ff.ID].InRelease).To(BeTrue())
			Expect(byID[ac.ID].InRelease).To(BeFalse())
			got := map[string][2]bool{}
			for _, t := range byID[ff.ID].Tracks {
				got[t.Title] = [2]bool{t.HasFile, t.InRelease}
			}
			Expect(got).To(Equal(map[string][2]bool{
				"Regret":                {true, true},
				"House of Eternal Hunt": {false, false},
				"The Eagle Has Landed":  {false, true},
			}))

			// Only a waiting folder's files are given an album and a track.
			Expect(v.Files[1].AlbumID).To(Equal(ff.ID))
			Expect(v.Files[1].TrackID).NotTo(BeZero())
			Expect(v.Files[2].TrackID).To(BeZero())
			Expect(v.Files[3].AlbumID).To(BeZero())
		},
	)

	It("lists no album when nothing is waiting, the case a grab refuses", func() {
		a, albums := e.seedArtist("Avatar", albumSeed{
			mbid:      "rg-ff",
			title:     "Feathers & Flesh",
			date:      at(2016),
			monitored: true,
			tracks:    []string{"Regret"},
		})
		e.client.Album.UpdateOneID(albums[0].ID).
			SetStatus(album.StatusAvailable).ExecX(e.ctx)
		e.dl.EXPECT().ReleaseFiles(mock.Anything, release).
			Return([]download.ReleaseFile{
				{Path: "Feathers & Flesh (2016)/01 - Regret.flac", Size: 1},
			}, nil).Once()

		v, err := e.svc.ArtistPackFiles(e.ctx, a.ID, release)
		Expect(err).NotTo(HaveOccurred())
		Expect(v.Albums).To(BeEmpty())
		Expect(v.Folders).To(Equal([]PackFolderView{
			{Path: "Feathers & Flesh (2016)", AlbumID: albums[0].ID},
		}))
	})

	It("maps a missing artist to ErrArtistNotFound", func() {
		_, err := e.svc.ArtistPackFiles(e.ctx, 999, release)
		Expect(err).To(MatchError(ErrArtistNotFound))
	})
})
