package importer

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/ffmpeg"
	mockffmpeg "github.com/datahearth/streamline/internal/ffmpeg/mocks"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/testutil/dbtest"
)

var _ = Describe("Worker audiobook verification", Label("unit", "importer"), func() {
	var (
		ctx    context.Context
		client *ent.Client
		prober *mockffmpeg.MockProber
		w      *Worker
		tmp    string
		src    string
		b      *ent.Book
	)

	write := func(name string) {
		GinkgoHelper()
		p := filepath.Join(src, name)
		Expect(os.MkdirAll(filepath.Dir(p), 0o755)).To(Succeed())
		Expect(os.WriteFile(p, []byte("audio"), 0o644)).To(Succeed())
	}

	record := func() uint32 {
		GinkgoHelper()
		return client.DownloadRecord.Create().
			SetTitle("Elantris").
			SetBookID(b.ID).
			SetBookKind(downloadrecord.BookKindAudiobook).
			SetSavePath(src).
			SetStatus(downloadrecord.StatusImporting).
			SaveX(ctx).ID
	}

	BeforeEach(func() {
		ctx = context.Background()
		tmp = GinkgoT().TempDir()
		src = filepath.Join(tmp, "dl")
		configtest.Setup(map[string]any{
			"library": map[string]any{
				"ebook_path":          filepath.Join(tmp, "ebooks"),
				"audiobook_path":      filepath.Join(tmp, "audio"),
				"import_mode":         "copy",
				"import_max_attempts": 3,
			},
			"book_quality_profiles": []map[string]any{{
				"name": "p",
				"ebook": map[string]any{
					"formats": []string{"EPUB"}, "preferred": "EPUB",
				},
				"audiobook": map[string]any{
					"formats":     []string{"M4B", "MP3"},
					"preferred":   "M4B",
					"min_bitrate": 64,
				},
			}},
			"book_quality_default_profiles": map[string]any{
				"novel": "p", "bd": "p", "comic": "p", "manga": "p",
			},
		})
		client = dbtest.SetupTestDB(ctx)
		DeferCleanup(client.Close)
		prober = mockffmpeg.NewMockProber(GinkgoT())
		prober.EXPECT().Available().Return(true).Maybe()
		w = NewWorker(Deps{
			DB: db.New(client), Library: library.NewImportService(), Prober: prober,
		})
		a := client.Author.Create().
			SetHardcoverID(1).SetName("Brandon Sanderson").SaveX(ctx)
		b = client.Book.Create().
			SetHardcoverID(2).SetTitle("Elantris").
			SetReleaseDate(time.Date(2005, 4, 21, 0, 0, 0, 0, time.UTC)).
			SetAuthor(a).
			SetAudiobookStatus(book.AudiobookStatusDownloading).
			SaveX(ctx)
	})

	It("holds a folder whose dominant format the profile does not tick", func() {
		write("01.flac")
		write("02.flac")
		write("03.mp3")
		id := record()
		prober.EXPECT().ProbeAudio(mock.Anything, mock.Anything).
			Return(&ffmpeg.AudioInfo{Codec: "flac", BitrateKbps: 900, DurationSec: 60}, nil).
			Maybe()

		Expect(w.runImport(ctx, id)).To(Succeed())

		rec := client.DownloadRecord.GetX(ctx, id)
		Expect(rec.Status).To(Equal(downloadrecord.StatusHeld))
		Expect(rec.HoldReasons).To(ContainElement(HaveField("Check", "format")))
		Expect(client.MediaFile.Query().CountX(ctx)).To(BeZero())
		Expect(filepath.Join(tmp, "audio")).NotTo(BeADirectory())
	})

	It("holds a folder whose measured bit rate is under the floor", func() {
		write("01.mp3")
		id := record()
		prober.EXPECT().ProbeAudio(mock.Anything, mock.Anything).
			Return(&ffmpeg.AudioInfo{Codec: "mp3", BitrateKbps: 32, DurationSec: 60}, nil).
			Once()

		Expect(w.runImport(ctx, id)).To(Succeed())

		rec := client.DownloadRecord.GetX(ctx, id)
		Expect(rec.Status).To(Equal(downloadrecord.StatusHeld))
		Expect(rec.HoldReasons).To(ContainElement(SatisfyAll(
			HaveField("Check", "bitrate"),
			HaveField("Expected", "≥ 64 kbps"),
			HaveField("Actual", "32 kbps"),
		)))
	})

	It(
		"imports a folder that clears both checks and records the measured rate",
		func() {
			write("01.mp3")
			id := record()
			prober.EXPECT().ProbeAudio(mock.Anything, mock.Anything).
				Return(&ffmpeg.AudioInfo{Codec: "mp3", BitrateKbps: 128, DurationSec: 60}, nil).
				Once()

			Expect(w.runImport(ctx, id)).To(Succeed())

			files := client.MediaFile.Query().AllX(ctx)
			Expect(files).To(HaveLen(1))
			Expect(files[0].BookKind).To(Equal(mediafile.BookKindAudiobook))
			Expect(files[0].Quality).To(Equal("MP3"))
			Expect(files[0].Bitrate).To(Equal(uint32(128000)))
		},
	)

	It("accepts an off-ladder format the profile cannot speak for", func() {
		write("01.ogg")
		id := record()
		prober.EXPECT().ProbeAudio(mock.Anything, mock.Anything).
			Return(&ffmpeg.AudioInfo{Codec: "vorbis", BitrateKbps: 96, DurationSec: 60}, nil).
			Once()

		Expect(w.runImport(ctx, id)).To(Succeed())

		Expect(client.DownloadRecord.GetX(ctx, id).Status).
			To(Equal(downloadrecord.StatusCompleted))
		Expect(client.MediaFile.Query().OnlyX(ctx).Quality).To(BeEmpty())
	})

	It("skips the bit-rate check when the probe fails", func() {
		write("01.mp3")
		id := record()
		prober.EXPECT().ProbeAudio(mock.Anything, mock.Anything).
			Return(nil, ffmpeg.ErrUnreadable).Once()

		Expect(w.runImport(ctx, id)).To(Succeed())

		Expect(client.DownloadRecord.GetX(ctx, id).Status).
			To(Equal(downloadrecord.StatusCompleted))
	})
})
