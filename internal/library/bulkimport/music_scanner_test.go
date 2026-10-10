package bulkimport

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanalbum "github.com/datahearth/streamline/ent/importscanalbum"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/library/audiotags"
	"github.com/datahearth/streamline/internal/metadata"
	metamocks "github.com/datahearth/streamline/internal/metadata/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/testutil/dbtest"
)

const audiotagsFixtures = "../audiotags/testdata"

func copyFixture(name, dst string) {
	GinkgoHelper()
	raw, err := os.ReadFile(filepath.Join(audiotagsFixtures, name))
	Expect(err).NotTo(HaveOccurred())
	Expect(os.MkdirAll(filepath.Dir(dst), 0o755)).To(Succeed())
	//nolint:gosec // dst is a spec-owned temp path
	Expect(os.WriteFile(dst, raw, 0o600)).To(Succeed())
}

func writeTrack(dst string, tags audiotags.WriteTags) {
	GinkgoHelper()
	copyFixture("untagged.mp3", dst)
	Expect(audiotags.Write(dst, tags)).To(Succeed())
}

func searchHit(
	mbid, artistMBID string,
	score uint8,
) metadata.ReleaseGroupSearchResult {
	return metadata.ReleaseGroupSearchResult{
		MBID:       mbid,
		Title:      "Nevermind",
		ArtistMBID: artistMBID,
		ArtistName: "Nirvana",
		Score:      score,
	}
}

var _ = Describe("ClassifyAlbum", Label("unit", "bulkimport"), func() {
	It("is unmatched without hits", func() {
		c := ClassifyAlbum(nil, nil)
		Expect(c.Kind).To(Equal(entimportscanalbum.ClassificationUnmatched))
	})

	It("confirms a lone strong hit", func() {
		c := ClassifyAlbum([]metadata.ReleaseGroupSearchResult{
			searchHit("rg-1", "a-1", 100), searchHit("rg-2", "a-1", 60),
		}, nil)
		Expect(c.Kind).To(Equal(entimportscanalbum.ClassificationConfirmed))
		Expect(c.ReleaseGroupMBID).To(Equal("rg-1"))
		Expect(c.ArtistMBID).To(Equal("a-1"))
	})

	It("is ambiguous with two strong hits or only weak ones", func() {
		two := ClassifyAlbum([]metadata.ReleaseGroupSearchResult{
			searchHit("rg-1", "a-1", 100), searchHit("rg-2", "a-1", 97),
		}, nil)
		Expect(two.Kind).To(Equal(entimportscanalbum.ClassificationAmbiguous))
		Expect(two.Candidates).To(HaveLen(2))

		weak := ClassifyAlbum([]metadata.ReleaseGroupSearchResult{
			searchHit("rg-1", "a-1", 80),
		}, nil)
		Expect(weak.Kind).To(Equal(entimportscanalbum.ClassificationAmbiguous))
	})

	It("caps candidates at five", func() {
		hits := make([]metadata.ReleaseGroupSearchResult, 0, 8)
		for range 8 {
			hits = append(hits, searchHit("rg", "a", 50))
		}
		Expect(ClassifyAlbum(hits, nil).Candidates).To(HaveLen(5))
	})

	It("carries the release-group type on its candidates", func() {
		hit := searchHit("rg-1", "a-1", 100)
		hit.Type = metadata.AlbumTypeEP
		c := ClassifyAlbum([]metadata.ReleaseGroupSearchResult{hit}, nil)
		Expect(c.Candidates).To(HaveLen(1))
		Expect(c.Candidates[0].Type).To(Equal("ep"))
	})

	It("flags a strong hit already in the library as existing", func() {
		c := ClassifyAlbum(
			[]metadata.ReleaseGroupSearchResult{searchHit("rg-1", "a-1", 100)},
			map[string]uint32{"rg-1": 7},
		)
		Expect(c.Kind).To(Equal(entimportscanalbum.ClassificationExisting))
		Expect(c.ExistingAlbumID).To(Equal(uint32(7)))
	})
})

var _ = Describe("StartScan kind dispatch", Label("unit", "bulkimport"), func() {
	var (
		ctx   context.Context
		store *dbmocks.MockStore
		svc   *Service
	)

	BeforeEach(func() {
		ctx = context.Background()
		store = dbmocks.NewMockStore(GinkgoT())
		svc = NewService(
			store,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			"/lib",
			"/lib-tv",
			nil,
			nil,
			nil,
			nil,
		)
	})

	It("rejects a kind with no runner before creating anything", func() {
		_, err := svc.StartScan(ctx, StartScanParams{
			SourcePath: GinkgoT().TempDir(),
			Kind:       entimportscan.Kind("podcast"),
			Mode:       entimportscan.ModeInPlace,
		})
		Expect(err).To(MatchError(ErrUnsupportedKind))
	})

	It("rejects committing a scan whose kind has no runner", func() {
		store.EXPECT().FindImportScan(mock.Anything, uint32(1)).
			Return(&ent.ImportScan{
				ID:     1,
				Kind:   entimportscan.Kind("podcast"),
				Status: entimportscan.StatusAwaitingReview,
			}, nil).Once()
		Expect(svc.Commit(ctx, 1)).To(MatchError(ErrUnsupportedKind))
	})

	DescribeTable("rejects rename mode before any other check",
		func(kind entimportscan.Kind) {
			_, err := svc.StartScan(ctx, StartScanParams{
				SourcePath: GinkgoT().TempDir(),
				Kind:       kind,
				Mode:       entimportscan.ModeRename,
			})
			Expect(err).To(MatchError(ErrRenameUnsupported))
		},
		Entry("music", entimportscan.KindMusic),
		Entry("book, even with no Hardcover key", entimportscan.KindBook),
	)

	It("holds an in_place music scan to library.music_path", func() {
		configtest.Setup(map[string]any{
			"library": map[string]any{"music_path": GinkgoT().TempDir()},
		})
		_, err := svc.StartScan(ctx, StartScanParams{
			SourcePath: GinkgoT().TempDir(),
			Kind:       entimportscan.KindMusic,
			Mode:       entimportscan.ModeInPlace,
		})
		Expect(err).To(MatchError(ErrPathOutsideLibrary))
	})
})

var _ = Describe("Music scan", Label("integration", "bulkimport"), func() {
	var (
		ctx    context.Context
		root   string
		client *ent.Client
		store  db.Store
		mb     *metamocks.MockMusicProvider
		svc    *Service
	)

	BeforeEach(func() {
		ctx = context.Background()
		root = GinkgoT().TempDir()
		configtest.Setup(map[string]any{
			"library": map[string]any{"music_path": root},
		})
		client = dbtest.SetupTestDB(ctx)
		DeferCleanup(client.Close)
		store = db.New(client)
		mb = metamocks.NewMockMusicProvider(GinkgoT())
		svc = NewService(
			store,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			"/lib",
			"/lib-tv",
			mb,
			nil,
			nil,
			nil,
		)

		copyFixture(
			"tagged.mp3",
			filepath.Join(
				root,
				"Nirvana",
				"Nevermind",
				"01 - Smells Like Teen Spirit.mp3",
			),
		)
		copyFixture("untagged.mp3", filepath.Join(root, "Unknown", "random.mp3"))
	})

	scan := func() []*ent.ImportScanAlbum {
		GinkgoHelper()
		sc, err := store.CreateImportScan(ctx, db.CreateImportScanParams{
			SourcePath: root,
			Kind:       entimportscan.KindMusic,
			Mode:       entimportscan.ModeInPlace,
		})
		Expect(err).NotTo(HaveOccurred())
		svc.runScanMusic(ctx, sc)

		cur, err := store.FindImportScan(ctx, sc.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(cur.Status).To(Equal(entimportscan.StatusAwaitingReview))
		rows, err := client.ImportScanAlbum.Query().All(ctx)
		Expect(err).NotTo(HaveOccurred())
		return rows
	}

	byFolder := func(rows []*ent.ImportScanAlbum, name string) *ent.ImportScanAlbum {
		GinkgoHelper()
		for _, r := range rows {
			if filepath.Base(r.FolderPath) == name {
				return r
			}
		}
		Fail("no scanned album for folder " + name)
		return nil
	}

	It("groups a tagged folder into one album from its majority tags", func() {
		mb.EXPECT().SearchReleaseGroups(mock.Anything, "Nirvana", "Nevermind").
			Return(nil, nil).Once()
		mb.EXPECT().SearchReleaseGroups(mock.Anything, mock.Anything, mock.Anything).
			Return(nil, nil).Once()

		rows := scan()
		Expect(rows).To(HaveLen(2))
		nm := byFolder(rows, "Nevermind")
		Expect(nm.TaggedArtist).To(Equal("Nirvana"))
		Expect(nm.TaggedAlbum).To(Equal("Nevermind"))
		Expect(nm.FileCount).To(Equal(uint16(1)))
		Expect(nm.TaggedYear).To(BeNumerically(">", 0))
		Expect(nm.Format).To(Equal("MP3"))
		Expect(nm.Size).To(BeNumerically(">", 0))
	})

	It("folds disc subfolders into one album under their parent", func() {
		for disc, dir := range map[string]string{"1": "CD1", "2": "CD 2"} {
			writeTrack(
				filepath.Join(root, "Pink Floyd", "The Wall", dir, "01.mp3"),
				audiotags.WriteTags{
					AlbumArtist: "Pink Floyd", Album: "The Wall",
					Title: "Track " + disc, Track: 1,
				},
			)
		}
		mb.EXPECT().SearchReleaseGroups(mock.Anything, "Pink Floyd", "The Wall").
			Return(nil, nil).Once()
		mb.EXPECT().SearchReleaseGroups(mock.Anything, "Nirvana", "Nevermind").
			Return(nil, nil).Once()
		mb.EXPECT().SearchReleaseGroups(mock.Anything, "", "Unknown").
			Return(nil, nil).Once()

		rows := scan()
		Expect(rows).To(HaveLen(3))
		wall := byFolder(rows, "The Wall")
		Expect(wall.FileCount).To(Equal(uint16(2)))
		Expect(wall.TaggedAlbum).To(Equal("The Wall"))
	})

	It("falls back to folder names and leaves an untagged folder unmatched", func() {
		mb.EXPECT().SearchReleaseGroups(mock.Anything, "Nirvana", "Nevermind").
			Return(nil, nil).Once()
		mb.EXPECT().SearchReleaseGroups(mock.Anything, "", "Unknown").
			Return(nil, nil).Once()

		u := byFolder(scan(), "Unknown")
		Expect(u.TaggedAlbum).To(Equal("Unknown"))
		Expect(
			u.Classification,
		).To(Equal(entimportscanalbum.ClassificationUnmatched))
	})

	It("confirms a lone strong hit and keeps a failed lookup unmatched", func() {
		mb.EXPECT().SearchReleaseGroups(mock.Anything, "Nirvana", "Nevermind").
			Return([]metadata.ReleaseGroupSearchResult{searchHit("rg-1", "a-1", 100)}, nil).
			Once()
		mb.EXPECT().SearchReleaseGroups(mock.Anything, "", "Unknown").
			Return(nil, context.DeadlineExceeded).Once()

		rows := scan()
		nm := byFolder(rows, "Nevermind")
		Expect(
			nm.Classification,
		).To(Equal(entimportscanalbum.ClassificationConfirmed))
		Expect(nm.ReleaseGroupMbid).To(Equal("rg-1"))
		Expect(nm.ArtistMbid).To(Equal("a-1"))
		Expect(byFolder(rows, "Unknown").Classification).
			To(Equal(entimportscanalbum.ClassificationUnmatched))
	})

	It("classifies a release group already in the library as existing", func() {
		_, err := store.CreateArtist(ctx, db.CreateArtistParams{
			MBID: "a-1",
			Name: "Nirvana",
			Albums: []db.AlbumSeed{
				{MBID: "rg-1", Title: "Nevermind", Type: "album"},
			},
		})
		Expect(err).NotTo(HaveOccurred())
		held, err := store.AlbumMBIDIndex(ctx)
		Expect(err).NotTo(HaveOccurred())

		mb.EXPECT().SearchReleaseGroups(mock.Anything, "Nirvana", "Nevermind").
			Return([]metadata.ReleaseGroupSearchResult{searchHit("rg-1", "a-1", 100)}, nil).
			Once()
		mb.EXPECT().SearchReleaseGroups(mock.Anything, "", "Unknown").
			Return(nil, nil).Once()

		nm := byFolder(scan(), "Nevermind")
		Expect(
			nm.Classification,
		).To(Equal(entimportscanalbum.ClassificationExisting))
		Expect(nm.ExistingAlbumID).To(HaveValue(Equal(held["rg-1"])))
	})

	It("runs the music scanner for kind music", func() {
		mb.EXPECT().SearchReleaseGroups(mock.Anything, mock.Anything, mock.Anything).
			Return(nil, nil).Times(2)

		sc, err := svc.StartScan(ctx, StartScanParams{
			SourcePath: root,
			Kind:       entimportscan.KindMusic,
			Mode:       entimportscan.ModeInPlace,
		})
		Expect(err).NotTo(HaveOccurred())
		Eventually(func() entimportscan.Status {
			cur, err := store.FindImportScan(ctx, sc.ID)
			Expect(err).NotTo(HaveOccurred())
			return cur.Status
		}).Should(Equal(entimportscan.StatusAwaitingReview))
		n, err := client.ImportScanAlbum.Query().Count(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(Equal(2))
	})
})

var _ = Describe("walkAlbumFolders", Label("unit", "bulkimport"), func() {
	It(
		"never folds disc folders that sit directly under the scan root, slash or not",
		func() {
			root := GinkgoT().TempDir()
			copyFixture("untagged.mp3", filepath.Join(root, "CD1", "01.mp3"))
			copyFixture("untagged.mp3", filepath.Join(root, "CD2", "01.mp3"))

			for _, given := range []string{root, root + string(filepath.Separator)} {
				folders, walkErrors := walkAlbumFolders(
					context.Background(),
					given,
					nil,
				)
				Expect(walkErrors).To(BeZero())
				Expect(folders).To(HaveLen(2), "root %q", given)
			}
		},
	)
})
