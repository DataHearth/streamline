package music

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/track"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/indexer"
)

var _ = Describe("AlbumReleaseFiles", Label("unit", "integration", "music"), func() {
	var e *env

	BeforeEach(func() {
		e = newEnv(false)
	})

	release := indexer.SearchResult{
		Title:    "Avatar - Feathers & Flesh (2016) [FLAC]",
		Download: "https://indexer.example/dl/1",
	}

	It("maps the torrent's files onto the album's tracks", func() {
		_, albums := e.seedArtist("Avatar", albumSeed{
			mbid:  "rg-ff",
			title: "Feathers & Flesh",
			tracks: []string{
				"Regret",
				"House of Eternal Hunt",
				"The Eagle Has Landed",
			},
		})
		al := albums[0]
		first := e.client.Track.Query().
			Where(track.HasAlbumWith(album.IDEQ(al.ID)), track.Position(1)).
			OnlyX(e.ctx)
		e.addFile(first, "/music/Avatar/01 - Regret.flac", "lossless")

		e.dl.EXPECT().ReleaseFiles(mock.Anything, release).
			Return([]download.ReleaseFile{
				{Path: "Avatar - Feathers & Flesh/01 - Regret.flac", Size: 30},
				{
					Path: "Avatar - Feathers & Flesh/03 - The Eagle Has Landed.flac",
					Size: 40,
				},
				{Path: "Avatar - Feathers & Flesh/cover.jpg", Size: 1},
			}, nil).Once()

		v, err := e.svc.AlbumReleaseFiles(e.ctx, al.ID, release)
		Expect(err).NotTo(HaveOccurred())
		Expect(v.Files).To(HaveLen(3))
		Expect(v.Files[2].TrackID).To(BeZero())

		Expect(v.Tracks).To(HaveLen(3))
		got := map[string][2]bool{}
		for _, t := range v.Tracks {
			got[t.Title] = [2]bool{t.HasFile, t.InRelease}
		}
		Expect(got).To(Equal(map[string][2]bool{
			"Regret":                {true, true},
			"House of Eternal Hunt": {false, false},
			"The Eagle Has Landed":  {false, true},
		}))
	})

	It("passes the download manager's refusal through", func() {
		_, albums := e.seedArtist("Avatar", albumSeed{
			mbid: "rg-ff", title: "Feathers & Flesh", tracks: []string{"Regret"},
		})
		e.dl.EXPECT().ReleaseFiles(mock.Anything, release).
			Return(nil, download.ErrFileListUnavailable).Once()
		_, err := e.svc.AlbumReleaseFiles(e.ctx, albums[0].ID, release)
		Expect(err).To(MatchError(download.ErrFileListUnavailable))
	})

	It("maps a missing album to ErrAlbumNotFound", func() {
		_, err := e.svc.AlbumReleaseFiles(e.ctx, 999, release)
		Expect(err).To(MatchError(ErrAlbumNotFound))
	})
})
