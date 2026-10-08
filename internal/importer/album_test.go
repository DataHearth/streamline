package importer

import (
	"bytes"
	"context"
	"fmt"
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
	"github.com/datahearth/streamline/internal/library/audiotags"
	msmocks "github.com/datahearth/streamline/internal/mediaserver/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

const albumNaming = "{Artist}/{Album} ({Year})/{Disc}{Track:02} - {Title}.{ext}"

// albumTracks are the three tracks every album fixture holds.
var albumTracks = []struct {
	id       uint32
	position uint16
	title    string
}{
	{11, 1, "Smells Like Teen Spirit"},
	{12, 2, "In Bloom"},
	{13, 3, "Come as You Are"},
}

// seedTrackFile writes a tagged copy of a testdata fixture into dir, tagged as
// the given track so the importer can match it. A fixture is renamed to ext
// after tagging: Read sniffs content, Write dispatches on the extension.
func seedTrackFile(dir, name, fixture string, position uint16, title string) string {
	GinkgoHelper()
	data, err := os.ReadFile(
		filepath.Join("..", "library", "audiotags", "testdata", fixture),
	)
	Expect(err).NotTo(HaveOccurred())
	path := filepath.Join(dir, name)
	//nolint:gosec // path is a fresh temp dir joined with a fixed name
	Expect(os.WriteFile(path, data, 0o600)).To(Succeed())
	Expect(audiotags.Write(path, audiotags.WriteTags{
		Artist: "Source Artist", Album: "Source Album", Title: title,
		Track: position, Disc: 1,
	})).To(Succeed())
	return path
}

func fixtureAlbum() *ent.Album {
	released := time.Date(1991, 9, 24, 0, 0, 0, 0, time.UTC)
	alb := &ent.Album{
		ID: 5, Title: "Nevermind", Mbid: "rg-5", ReleaseDate: &released,
	}
	alb.Edges.Artist = &ent.Artist{ID: 4, Name: "Nirvana", Mbid: "artist-4"}
	for _, t := range albumTracks {
		alb.Edges.Tracks = append(alb.Edges.Tracks, &ent.Track{
			ID: t.id, Title: t.title, Disc: 1, Position: t.position,
			Mbid: "rec-" + t.title,
		})
	}
	return alb
}

func fixtureAlbumRecord(savePath string, alb *ent.Album) *ent.DownloadRecord {
	r := &ent.DownloadRecord{
		ID:                 1,
		TorrentHash:        "hash",
		SavePath:           savePath,
		Status:             downloadrecord.StatusImporting,
		DownloadClientName: "qbit",
		ReplaceMode:        downloadrecord.ReplaceModeNone,
	}
	r.Edges.Album = alb
	return r
}

var _ = Describe("Worker album import", Label("unit", "importer"), func() {
	var (
		storeMk  *mockdb.MockStore
		msMk     *msmocks.MockRefresher
		dlMk     *mockdl.MockDownloader
		w        *Worker
		dlDir    string
		musicDir string
		logs     *bytes.Buffer
		alb      *ent.Album
	)

	setup := func(mode string, keepSeeding bool) {
		configtest.Setup(map[string]any{
			"library": map[string]any{
				"music_path":           musicDir,
				"music_naming":         albumNaming,
				"import_mode":          mode,
				"import_max_attempts":  3,
				"keep_torrent_seeding": keepSeeding,
			},
		})
		w = NewWorker(Deps{
			DB:          storeMk,
			Library:     library.NewImportService(),
			MediaServer: msMk,
			Download:    dlMk,
		})
	}

	destOf := func(position uint16, title, ext string) string {
		return filepath.Join(
			musicDir, "Nirvana", "Nevermind (1991)",
			fmt.Sprintf("%02d - %s.%s", position, title, ext),
		)
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

		storeMk = mockdb.NewMockStore(GinkgoT())
		msMk = msmocks.NewMockRefresher(GinkgoT())
		dlMk = mockdl.NewMockDownloader(GinkgoT())
		alb = fixtureAlbum()
		setup("copy", true)
	})

	It("imports matched tracks, tags the library copies and leaves the gap", func() {
		seedTrackFile(dlDir, "a.mp3", "tagged.mp3", 1, albumTracks[0].title)
		seedTrackFile(dlDir, "b.flac", "tagged.flac", 2, albumTracks[1].title)
		rec := fixtureAlbumRecord(dlDir, alb)

		expectFind(rec)
		var got db.RecordAlbumImportSuccessParams
		storeMk.EXPECT().
			RecordAlbumImportSuccess(mock.Anything, mock.Anything).
			Run(func(_ context.Context, p db.RecordAlbumImportSuccessParams) { got = p }).
			Return(nil).Once()
		msMk.EXPECT().RefreshAll(mock.Anything, "music", musicDir).Return(nil).Once()

		Expect(w.runImport(context.Background(), 1)).To(Succeed())

		Expect(got.RecordID).To(Equal(uint32(1)))
		Expect(got.AlbumID).To(Equal(uint32(5)))
		Expect(got.Files).To(ConsistOf(
			HaveField("TrackID", uint32(11)),
			HaveField("TrackID", uint32(12)),
		))
		for _, f := range got.Files {
			Expect(f.Quality).To(Equal(f.Format))
			Expect(f.Path).To(HaveSuffix("." + f.Format))
			Expect(f.Path).To(HavePrefix(musicDir))
			Expect(f.Size).To(BeNumerically(">", 0))
			info, err := audiotags.Read(f.Path)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Artist).To(Equal("Nirvana"))
			Expect(info.Album).To(Equal("Nevermind"))
			Expect(info.Year).To(Equal(uint16(1991)))
			Expect(info.Disc).To(Equal(uint8(1)))
		}
		first := got.Files[0]
		info, err := audiotags.Read(first.Path)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Title).To(Equal(albumTracks[0].title))
		Expect(info.Track).To(Equal(uint16(1)))
		Expect(logs.String()).To(ContainSubstring("tracks still missing"))
	})

	It("imports a full album without reporting a gap", func() {
		for i, t := range albumTracks {
			seedTrackFile(
				dlDir, string(rune('a'+i))+".mp3", "tagged.mp3", t.position, t.title,
			)
		}
		rec := fixtureAlbumRecord(dlDir, alb)
		expectFind(rec)
		storeMk.EXPECT().
			RecordAlbumImportSuccess(mock.Anything, mock.MatchedBy(
				func(p db.RecordAlbumImportSuccessParams) bool { return len(p.Files) == 3 },
			)).Return(nil).Once()
		msMk.EXPECT().RefreshAll(mock.Anything, "music", musicDir).Return(nil).Once()

		Expect(w.runImport(context.Background(), 1)).To(Succeed())
		Expect(logs.String()).NotTo(ContainSubstring("tracks still missing"))
	})

	It("lets the first file claim a track and ignores unmatched ones", func() {
		seedTrackFile(dlDir, "a.mp3", "tagged.mp3", 1, albumTracks[0].title)
		seedTrackFile(dlDir, "b.mp3", "tagged.mp3", 1, albumTracks[0].title)
		seedTrackFile(dlDir, "c.mp3", "tagged.mp3", 9, "Not On The Album")
		rec := fixtureAlbumRecord(dlDir, alb)
		expectFind(rec)
		storeMk.EXPECT().
			RecordAlbumImportSuccess(mock.Anything, mock.MatchedBy(
				func(p db.RecordAlbumImportSuccessParams) bool { return len(p.Files) == 1 },
			)).Return(nil).Once()
		msMk.EXPECT().RefreshAll(mock.Anything, "music", musicDir).Return(nil).Once()

		Expect(w.runImport(context.Background(), 1)).To(Succeed())
	})

	Describe("seeding safety", func() {
		sum := func(p string) []byte {
			GinkgoHelper()
			b, err := os.ReadFile(p)
			Expect(err).NotTo(HaveOccurred())
			return b
		}

		It("copies instead of hardlinking so tagging cannot touch the seed", func() {
			setup("hardlink", true)
			src := seedTrackFile(
				dlDir,
				"a.mp3",
				"tagged.mp3",
				1,
				albumTracks[0].title,
			)
			before := sum(src)
			rec := fixtureAlbumRecord(dlDir, alb)
			expectFind(rec)
			var got db.RecordAlbumImportSuccessParams
			storeMk.EXPECT().
				RecordAlbumImportSuccess(mock.Anything, mock.Anything).
				Run(func(_ context.Context, p db.RecordAlbumImportSuccessParams) { got = p }).
				Return(nil).Once()
			msMk.EXPECT().
				RefreshAll(mock.Anything, "music", musicDir).
				Return(nil).
				Once()

			Expect(w.runImport(context.Background(), 1)).To(Succeed())

			Expect(sum(src)).To(Equal(before))
			srcInfo, err := os.Stat(src)
			Expect(err).NotTo(HaveOccurred())
			dstInfo, err := os.Stat(got.Files[0].Path)
			Expect(err).NotTo(HaveOccurred())
			Expect(os.SameFile(srcInfo, dstInfo)).To(BeFalse())
		})

		It("copies instead of moving while the torrent keeps seeding", func() {
			setup("move", true)
			src := seedTrackFile(
				dlDir,
				"a.mp3",
				"tagged.mp3",
				1,
				albumTracks[0].title,
			)
			before := sum(src)
			rec := fixtureAlbumRecord(dlDir, alb)
			expectFind(rec)
			storeMk.EXPECT().
				RecordAlbumImportSuccess(mock.Anything, mock.Anything).
				Return(nil).Once()
			msMk.EXPECT().
				RefreshAll(mock.Anything, "music", musicDir).
				Return(nil).
				Once()

			Expect(w.runImport(context.Background(), 1)).To(Succeed())

			Expect(sum(src)).To(Equal(before))
		})

		It("keeps move when the torrent is not seeded", func() {
			setup("move", false)
			src := seedTrackFile(
				dlDir,
				"a.mp3",
				"tagged.mp3",
				1,
				albumTracks[0].title,
			)
			rec := fixtureAlbumRecord(dlDir, alb)
			expectFind(rec)
			storeMk.EXPECT().
				RecordAlbumImportSuccess(mock.Anything, mock.Anything).
				Return(nil).Once()
			msMk.EXPECT().
				RefreshAll(mock.Anything, "music", musicDir).
				Return(nil).
				Once()
			dlMk.EXPECT().RemoveTorrent(mock.Anything, "qbit", "hash", true).
				Return(nil).Once()

			Expect(w.runImport(context.Background(), 1)).To(Succeed())

			Expect(src).NotTo(BeAnExistingFile())
		})
	})

	It("imports an untaggable format untagged and only warns", func() {
		tagged := seedTrackFile(
			dlDir,
			"a.mp3",
			"tagged.mp3",
			1,
			albumTracks[0].title,
		)
		opus := filepath.Join(dlDir, "a.opus")
		Expect(os.Rename(tagged, opus)).To(Succeed())
		rec := fixtureAlbumRecord(dlDir, alb)
		expectFind(rec)
		var got db.RecordAlbumImportSuccessParams
		storeMk.EXPECT().
			RecordAlbumImportSuccess(mock.Anything, mock.Anything).
			Run(func(_ context.Context, p db.RecordAlbumImportSuccessParams) { got = p }).
			Return(nil).Once()
		msMk.EXPECT().RefreshAll(mock.Anything, "music", musicDir).Return(nil).Once()

		Expect(w.runImport(context.Background(), 1)).To(Succeed())

		Expect(got.Files).To(HaveLen(1))
		Expect(got.Files[0].Path).To(Equal(destOf(1, albumTracks[0].title, "opus")))
		Expect(got.Files[0].Path).To(BeAnExistingFile())
		Expect(logs.String()).To(ContainSubstring("tag write failed"))
	})

	Describe("an existing library file", func() {
		var existing string

		BeforeEach(func() {
			existing = destOf(1, albumTracks[0].title, "mp3")
			Expect(os.MkdirAll(filepath.Dir(existing), 0o755)).To(Succeed())
			Expect(os.WriteFile(existing, []byte("old"), 0o600)).To(Succeed())
			seedTrackFile(dlDir, "a.mp3", "tagged.mp3", 1, albumTracks[0].title)
		})

		It("is refused when the destination holds an untracked file", func() {
			rec := fixtureAlbumRecord(dlDir, alb)
			expectFind(rec)

			err := w.runImport(context.Background(), 1)

			Expect(err).To(MatchError(library.ErrDestExists))
			Expect(classify(err)).To(Equal(terminal))
			Expect(os.ReadFile(existing)).To(Equal([]byte("old")))
		})

		It("is refused when the track already holds a file", func() {
			alb.Edges.Tracks[0].Edges.MediaFiles = []*ent.MediaFile{
				{ID: 77, Path: existing},
			}
			rec := fixtureAlbumRecord(dlDir, alb)
			expectFind(rec)

			err := w.runImport(context.Background(), 1)

			Expect(err).To(MatchError(library.ErrDestExists))
			Expect(os.ReadFile(existing)).To(Equal([]byte("old")))
		})

		It(
			"is set aside, replaced and its row deleted when the record replaces",
			func() {
				alb.Edges.Tracks[0].Edges.MediaFiles = []*ent.MediaFile{
					{ID: 77, Path: existing},
				}
				rec := fixtureAlbumRecord(dlDir, alb)
				rec.ReplaceMode = downloadrecord.ReplaceModeAll
				expectFind(rec)
				storeMk.EXPECT().
					DeleteMediaFile(mock.Anything, uint32(77)).
					Return(nil).
					Once()
				storeMk.EXPECT().
					RecordAlbumImportSuccess(mock.Anything, mock.Anything).
					Return(nil).Once()
				msMk.EXPECT().
					RefreshAll(mock.Anything, "music", musicDir).
					Return(nil).
					Once()

				Expect(w.runImport(context.Background(), 1)).To(Succeed())

				data, err := os.ReadFile(existing)
				Expect(err).NotTo(HaveOccurred())
				Expect(data).NotTo(Equal([]byte("old")))
				Expect(existing + replacedSuffix).NotTo(BeAnExistingFile())
			},
		)

		It("stays in place when it cannot be set aside", func() {
			alb.Edges.Tracks[0].Edges.MediaFiles = []*ent.MediaFile{
				{ID: 77, Path: existing},
			}
			rec := fixtureAlbumRecord(dlDir, alb)
			rec.ReplaceMode = downloadrecord.ReplaceModeAll
			// A directory in the way of the aside target makes the rename fail.
			Expect(os.MkdirAll(existing+replacedSuffix, 0o755)).To(Succeed())
			Expect(os.WriteFile(
				filepath.Join(existing+replacedSuffix, "x"), []byte("x"), 0o600,
			)).To(Succeed())
			expectFind(rec)

			Expect(w.runImport(context.Background(), 1)).NotTo(Succeed())
			Expect(os.ReadFile(existing)).To(Equal([]byte("old")))
		})
	})

	It("fails terminally when no audio file matches a track", func() {
		seedTrackFile(dlDir, "a.mp3", "tagged.mp3", 9, "Nope")
		Expect(os.WriteFile(filepath.Join(dlDir, "cover.jpg"), []byte("x"), 0o600)).
			To(Succeed())
		rec := fixtureAlbumRecord(dlDir, alb)
		expectFind(rec)

		err := w.runImport(context.Background(), 1)

		Expect(err).To(MatchError(ErrNoAlbumTracks))
		Expect(classify(err)).To(Equal(terminal))
	})

	It("hands the album to the failure write on a terminal outcome", func() {
		rec := fixtureAlbumRecord(dlDir, alb)
		expectFind(rec)
		storeMk.EXPECT().
			RecordImportFailure(mock.Anything, mock.MatchedBy(
				func(p db.RecordImportFailureParams) bool {
					return p.RecordID == 1 && p.AlbumID == 5 && p.Terminal &&
						p.Reason != ""
				},
			)).Return(nil).Once()

		w.handleOutcome(context.Background(), 1, ErrNoAlbumTracks)
	})
})
