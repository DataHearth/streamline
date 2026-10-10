package importer

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/internal/db"
	mockdb "github.com/datahearth/streamline/internal/db/mocks"
	mockdl "github.com/datahearth/streamline/internal/download/mocks"
	"github.com/datahearth/streamline/internal/library"
	musicmocks "github.com/datahearth/streamline/internal/media/music/mocks"
	msmocks "github.com/datahearth/streamline/internal/mediaserver/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Pack folders", Label("unit", "importer"), func() {
	year := func(y int) *time.Time {
		t := time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC)
		return &t
	}
	albums := []*ent.Album{
		{ID: 1, Title: "Nevermind", ReleaseDate: year(1991)},
		{ID: 2, Title: "In Utero", ReleaseDate: year(1993)},
		{ID: 3, Title: "Greatest Hits", ReleaseDate: year(1995)},
		{ID: 4, Title: "Greatest Hits", ReleaseDate: year(2005)},
	}

	DescribeTable(
		"matchPackDir",
		func(dir string, want uint32) {
			got := matchPackDir(dir, albums)
			if want == 0 {
				Expect(got).To(BeNil())
				return
			}
			Expect(got).NotTo(BeNil())
			Expect(got.ID).To(Equal(want))
		},
		Entry("plain title", "/dl/Nirvana/Nevermind", uint32(1)),
		Entry(
			"year and format tags",
			"/dl/Nirvana/Nevermind (1991) [FLAC]",
			uint32(1),
		),
		Entry(
			"artist and year prefix",
			"/dl/Nirvana - 1993 - In Utero [FLAC]",
			uint32(2),
		),
		Entry("dots for spaces", "/dl/In.Utero.1993.FLAC", uint32(0)),
		Entry("dots around a title", "/dl/Nirvana.-.In.Utero", uint32(2)),
		Entry("case and punctuation folded", "/dl/NEVERMIND!", uint32(1)),
		Entry(
			"the year picks between two albums of one title",
			"/dl/Greatest Hits (2005)",
			uint32(4),
		),
		Entry(
			"no year, two albums of one title: ambiguous",
			"/dl/Greatest Hits",
			uint32(0),
		),
		Entry("a folder that names no linked album", "/dl/Bleach", uint32(0)),
	)

	DescribeTable(
		"albumDir keeps disc subfolders with their album",
		func(file, want string) {
			Expect(albumDir(file)).To(Equal(want))
		},
		Entry("flat", "/dl/Nevermind/01.flac", "/dl/Nevermind"),
		Entry("CD1", "/dl/Greatest Hits/CD1/01.flac", "/dl/Greatest Hits"),
		Entry("Disc 2", "/dl/Greatest Hits/Disc 2/01.flac", "/dl/Greatest Hits"),
		Entry("disk3 in lower case", "/dl/Box/disk3/01.flac", "/dl/Box"),
		Entry(
			"a folder that merely starts with cd",
			"/dl/CDs of 1991/01.flac",
			"/dl/CDs of 1991",
		),
	)
})

var _ = Describe("Worker pack import", Label("unit", "importer"), func() {
	var (
		storeMk                    *mockdb.MockStore
		msMk                       *msmocks.MockRefresher
		dlMk                       *mockdl.MockDownloader
		w                          *Worker
		dlDir                      string
		musicDir                   string
		logs                       *bytes.Buffer
		nevermind, inUtero, bleach *ent.Album
	)

	// trackAlbum is a hydrated album of two tracks.
	trackAlbum := func(id uint32, title string, y int, titles ...string) *ent.Album {
		t := time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC)
		now := time.Now()
		a := &ent.Album{
			ID: id, Title: title, Mbid: "rg-" + title, ReleaseDate: &t,
			MetadataFetchedAt: &now,
		}
		a.Edges.Artist = &ent.Artist{ID: 4, Name: "Nirvana", Mbid: "artist-4"}
		for i, tt := range titles {
			a.Edges.Tracks = append(a.Edges.Tracks, &ent.Track{
				ID: id*100 + uint32(i), Title: tt, Disc: 1, Position: uint16(i + 1),
			})
		}
		return a
	}

	pack := func(linked ...*ent.Album) *ent.DownloadRecord {
		r := &ent.DownloadRecord{
			ID: 1, TorrentHash: "hash", SavePath: dlDir,
			Status: downloadrecord.StatusImporting, DownloadClientName: "qbit",
			ReplaceMode: downloadrecord.ReplaceModeNone,
		}
		r.Edges.Artist = &ent.Artist{ID: 4, Name: "Nirvana", Mbid: "artist-4"}
		r.Edges.Albums = linked
		return r
	}

	seed := func(folder string, titles ...string) {
		GinkgoHelper()
		dir := filepath.Join(dlDir, folder)
		Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
		for i, t := range titles {
			seedTrackFile(
				dir,
				string(rune('a'+i))+".mp3",
				"tagged.mp3",
				uint16(i+1),
				t,
			)
		}
	}

	expectFind := func(rec *ent.DownloadRecord) {
		storeMk.EXPECT().
			FindImportingDownloadRecordByID(mock.Anything, rec.ID).
			Return(rec, nil).Once()
	}

	BeforeEach(func() {
		tmp := GinkgoT().TempDir()
		dlDir = filepath.Join(tmp, "dl")
		musicDir = filepath.Join(tmp, "music")
		Expect(os.MkdirAll(dlDir, 0o755)).To(Succeed())
		logs = &bytes.Buffer{}
		GinkgoWriter.TeeTo(logs)
		DeferCleanup(GinkgoWriter.ClearTeeWriters)

		configtest.Setup(map[string]any{
			"library": map[string]any{
				"music_path":           musicDir,
				"music_naming":         albumNaming,
				"import_mode":          "copy",
				"import_max_attempts":  3,
				"keep_torrent_seeding": true,
			},
		})
		storeMk = mockdb.NewMockStore(GinkgoT())
		msMk = msmocks.NewMockRefresher(GinkgoT())
		dlMk = mockdl.NewMockDownloader(GinkgoT())
		w = NewWorker(Deps{
			DB: storeMk, Library: library.NewImportService(),
			MediaServer: msMk, Download: dlMk,
		})
		storeMk.EXPECT().
			MarkRequestsAvailableByMBID(mock.Anything, "artist", "artist-4").
			Return(nil).Maybe()

		nevermind = trackAlbum(1, "Nevermind", 1991, "Drain You", "Lithium")
		inUtero = trackAlbum(2, "In Utero", 1993, "Serve the Servants", "Rape Me")
		bleach = trackAlbum(3, "Bleach", 1989, "Blew")
	})

	It(
		"imports each folder into the linked album it names and completes the record",
		func() {
			seed("Nirvana - Nevermind (1991) [FLAC]", "Drain You", "Lithium")
			seed("Nirvana - In Utero (1993) [FLAC]", "Serve the Servants")
			seed("Extras", "Not On Any Album")
			rec := pack(nevermind, inUtero, bleach)
			expectFind(rec)

			var imported []db.RecordAlbumImportSuccessParams
			storeMk.EXPECT().RecordAlbumImportSuccess(mock.Anything, mock.Anything).
				Run(func(_ context.Context, p db.RecordAlbumImportSuccessParams) {
					imported = append(imported, p)
				}).Return(nil).Twice()
			storeMk.EXPECT().
				CompletePackRecord(mock.Anything, uint32(1), []uint32{3}).
				Return(nil).
				Once()
			msMk.EXPECT().
				RefreshAll(mock.Anything, "music", musicDir).
				Return(nil).
				Once()

			Expect(w.runImport(context.Background(), 1)).To(Succeed())

			Expect(imported).To(HaveLen(2))
			byAlbum := map[uint32]db.RecordAlbumImportSuccessParams{}
			for _, p := range imported {
				Expect(
					p.KeepRecord,
				).To(BeTrue(), "the pack record completes once, at the end")
				Expect(p.RecordID).To(Equal(uint32(1)))
				byAlbum[p.AlbumID] = p
			}
			Expect(byAlbum[1].Files).To(HaveLen(2))
			Expect(byAlbum[2].Files).To(HaveLen(1))
			for _, f := range byAlbum[1].Files {
				Expect(
					f.Path,
				).To(HavePrefix(filepath.Join(musicDir, "Nirvana", "Nevermind (1991)")))
			}
		},
	)

	It("keeps a disc subfolder with its album", func() {
		seed("Nevermind/CD1", "Drain You")
		cd2 := filepath.Join(dlDir, "Nevermind", "CD2")
		Expect(os.MkdirAll(cd2, 0o755)).To(Succeed())
		seedTrackFile(cd2, "a.mp3", "tagged.mp3", 2, "Lithium")
		rec := pack(nevermind)
		expectFind(rec)
		storeMk.EXPECT().RecordAlbumImportSuccess(mock.Anything, mock.MatchedBy(
			func(p db.RecordAlbumImportSuccessParams) bool {
				return p.AlbumID == 1 && len(p.Files) == 2
			},
		)).Return(nil).Once()
		storeMk.EXPECT().CompletePackRecord(mock.Anything, uint32(1), []uint32(nil)).
			Return(nil).Once()
		msMk.EXPECT().RefreshAll(mock.Anything, "music", musicDir).Return(nil).Once()

		Expect(w.runImport(context.Background(), 1)).To(Succeed())
	})

	It(
		"fails like an album with no matching track when no folder names a linked album",
		func() {
			seed("Mystery", "Drain You")
			rec := pack(nevermind)
			expectFind(rec)
			storeMk.EXPECT().
				FindImportingDownloadRecordByID(mock.Anything, uint32(1)).
				Return(rec, nil).
				Once()
			storeMk.EXPECT().RecordImportFailure(mock.Anything, mock.MatchedBy(
				func(p db.RecordImportFailureParams) bool {
					return p.Terminal && p.PackAlbumIDs[0] == 1 && p.AlbumID == 0
				},
			)).Return(nil).Once()

			err := w.runImport(context.Background(), 1)
			Expect(err).To(MatchError(ErrNoAlbumTracks))
			w.handleOutcome(context.Background(), 1, err)
		},
	)

	It(
		"leaves the record for a retry when one album fails, and finishes the others",
		func() {
			seed("Nevermind", "Drain You")
			seed("In Utero", "Serve the Servants")
			rec := pack(nevermind, inUtero)
			expectFind(rec)
			boom := errors.New("database is locked")
			storeMk.EXPECT().RecordAlbumImportSuccess(mock.Anything, mock.MatchedBy(
				func(p db.RecordAlbumImportSuccessParams) bool { return p.AlbumID == 1 },
			)).Return(boom).Once()
			storeMk.EXPECT().RecordAlbumImportSuccess(mock.Anything, mock.MatchedBy(
				func(p db.RecordAlbumImportSuccessParams) bool { return p.AlbumID == 2 },
			)).Return(nil).Once()

			err := w.runImport(context.Background(), 1)
			Expect(err).To(MatchError(boom))
		},
	)

	It("treats a folder whose tracks all hold files as done", func() {
		seed("Nevermind", "Drain You")
		for _, t := range nevermind.Edges.Tracks {
			t.Edges.MediaFiles = []*ent.MediaFile{{ID: t.ID, Path: "/m/x"}}
		}
		rec := pack(nevermind)
		expectFind(rec)
		storeMk.EXPECT().CompletePackRecord(mock.Anything, uint32(1), []uint32(nil)).
			Return(nil).Once()
		msMk.EXPECT().RefreshAll(mock.Anything, "music", musicDir).Return(nil).Once()

		Expect(w.runImport(context.Background(), 1)).To(Succeed())
	})

	It("replaces what the albums hold when the record says so", func() {
		seed("Nevermind", "Drain You")
		old := &ent.MediaFile{ID: 70, Path: filepath.Join(musicDir, "old.flac")}
		nevermind.Edges.Tracks[0].Edges.MediaFiles = []*ent.MediaFile{old}
		rec := pack(nevermind)
		rec.ReplaceMode = downloadrecord.ReplaceModeAll
		expectFind(rec)
		storeMk.EXPECT().
			DeleteMediaFile(mock.Anything, uint32(70)).
			Return(nil).
			Once()
		storeMk.EXPECT().RecordAlbumImportSuccess(mock.Anything, mock.MatchedBy(
			func(p db.RecordAlbumImportSuccessParams) bool { return len(p.Files) == 1 },
		)).Return(nil).Once()
		storeMk.EXPECT().CompletePackRecord(mock.Anything, uint32(1), []uint32(nil)).
			Return(nil).Once()
		msMk.EXPECT().RefreshAll(mock.Anything, "music", musicDir).Return(nil).Once()

		Expect(w.runImport(context.Background(), 1)).To(Succeed())
	})

	It("hydrates a stub album before matching files to its tracks", func() {
		hydrator := musicmocks.NewMockAlbumHydrator(GinkgoT())
		w.albums = hydrator
		stub := &ent.Album{ID: 1, Title: "Nevermind", Mbid: "rg-Nevermind"}
		stub.Edges.Artist = &ent.Artist{ID: 4, Name: "Nirvana", Mbid: "artist-4"}
		seed("Nevermind", "Drain You")
		rec := pack(stub)
		expectFind(rec)
		hydrator.EXPECT().HydrateAlbum(mock.Anything, uint32(1)).Return(nil).Once()
		storeMk.EXPECT().
			FindAlbumByID(mock.Anything, uint32(1)).
			Return(nevermind, nil).
			Once()
		storeMk.EXPECT().RecordAlbumImportSuccess(mock.Anything, mock.MatchedBy(
			func(p db.RecordAlbumImportSuccessParams) bool { return len(p.Files) == 1 },
		)).Return(nil).Once()
		storeMk.EXPECT().CompletePackRecord(mock.Anything, uint32(1), []uint32(nil)).
			Return(nil).Once()
		msMk.EXPECT().RefreshAll(mock.Anything, "music", musicDir).Return(nil).Once()

		Expect(w.runImport(context.Background(), 1)).To(Succeed())
	})

	It("hydrates a single album's record the same way", func() {
		hydrator := musicmocks.NewMockAlbumHydrator(GinkgoT())
		w.albums = hydrator
		stub := &ent.Album{ID: 1, Title: "Nevermind", Mbid: "rg-Nevermind"}
		stub.Edges.Artist = &ent.Artist{ID: 4, Name: "Nirvana", Mbid: "artist-4"}
		seedTrackFile(dlDir, "a.mp3", "tagged.mp3", 1, "Drain You")
		rec := fixtureAlbumRecord(dlDir, stub)
		expectFind(rec)
		hydrator.EXPECT().HydrateAlbum(mock.Anything, uint32(1)).Return(nil).Once()
		storeMk.EXPECT().
			FindAlbumByID(mock.Anything, uint32(1)).
			Return(nevermind, nil).
			Once()
		storeMk.EXPECT().RecordAlbumImportSuccess(mock.Anything, mock.MatchedBy(
			func(p db.RecordAlbumImportSuccessParams) bool {
				return !p.KeepRecord && len(p.Files) == 1
			},
		)).Return(nil).Once()
		msMk.EXPECT().RefreshAll(mock.Anything, "music", musicDir).Return(nil).Once()

		Expect(w.runImport(context.Background(), 1)).To(Succeed())
	})

	It("fails the import when hydration fails", func() {
		hydrator := musicmocks.NewMockAlbumHydrator(GinkgoT())
		w.albums = hydrator
		stub := &ent.Album{ID: 1, Title: "Nevermind"}
		stub.Edges.Artist = &ent.Artist{ID: 4, Name: "Nirvana"}
		seedTrackFile(dlDir, "a.mp3", "tagged.mp3", 1, "Drain You")
		rec := fixtureAlbumRecord(dlDir, stub)
		expectFind(rec)
		hydrator.EXPECT().HydrateAlbum(mock.Anything, uint32(1)).
			Return(errors.New("musicbrainz 503")).Once()

		err := w.runImport(context.Background(), 1)
		Expect(err).To(MatchError(ContainSubstring("hydrate album")))
		Expect(classify(err)).To(Equal(retryable))
	})

	It("asks the importer for a pack record it cannot place", func() {
		rec := pack()
		rec.Edges.Artist = nil
		expectFind(rec)
		err := w.runImport(context.Background(), 1)
		Expect(err).To(MatchError(ContainSubstring("album, artist or book")))
	})
})
