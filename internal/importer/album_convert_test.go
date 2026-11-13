package importer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/internal/db"
	mockdb "github.com/datahearth/streamline/internal/db/mocks"
	mockdl "github.com/datahearth/streamline/internal/download/mocks"
	"github.com/datahearth/streamline/internal/ffmpeg"
	mockffmpeg "github.com/datahearth/streamline/internal/ffmpeg/mocks"
	"github.com/datahearth/streamline/internal/library"
	msmocks "github.com/datahearth/streamline/internal/mediaserver/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var (
	wavProbe = &ffmpeg.AudioInfo{
		Codec:        "pcm_s16le",
		BitDepth:     16,
		SampleRateHz: 44100,
		DurationSec:  200,
	}
	flacProbe = &ffmpeg.AudioInfo{
		Codec:        "flac",
		BitDepth:     16,
		SampleRateHz: 44100,
		DurationSec:  200,
	}
)

var _ = Describe("convertWAV", Label("unit", "importer"), func() {
	var (
		prober *mockffmpeg.MockProber
		w      *Worker
		dir    string
		src    string
		dst    string
	)

	BeforeEach(func() {
		prober = mockffmpeg.NewMockProber(GinkgoT())
		w = &Worker{probe: prober}
		dir = GinkgoT().TempDir()
		src = filepath.Join(GinkgoT().TempDir(), "01 - Song.wav")
		dst = filepath.Join(dir, "01 - Song.flac")
	})

	converts := func() {
		prober.EXPECT().ConvertToFLAC(mock.Anything, src, dst).
			RunAndReturn(func(context.Context, string, string) error {
				return os.WriteFile(dst, []byte("flac"), 0o600)
			}).Once()
		prober.EXPECT().ProbeAudio(mock.Anything, src).Return(wavProbe, nil).Once()
	}

	It("returns the FLAC it wrote beside the source's name", func() {
		converts()
		prober.EXPECT().ProbeAudio(mock.Anything, dst).Return(flacProbe, nil).Once()

		got, info, err := w.convertWAV(context.Background(), src, dir)

		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(dst))
		Expect(info).To(Equal(flacProbe))
		Expect(dst).To(BeAnExistingFile())
	})

	DescribeTable("refuses a result that is not the source's audio",
		func(result ffmpeg.AudioInfo) {
			converts()
			prober.EXPECT().
				ProbeAudio(mock.Anything, dst).
				Return(&result, nil).
				Once()

			_, _, err := w.convertWAV(context.Background(), src, dir)

			Expect(err).To(MatchError(ErrConversionFailed))
			Expect(dst).NotTo(BeAnExistingFile())
		},
		Entry("another codec",
			ffmpeg.AudioInfo{Codec: "mp3", SampleRateHz: 44100, DurationSec: 200}),
		Entry("another sample rate",
			ffmpeg.AudioInfo{Codec: "flac", SampleRateHz: 48000, DurationSec: 200}),
		Entry("cut short",
			ffmpeg.AudioInfo{Codec: "flac", SampleRateHz: 44100, DurationSec: 197}),
	)

	It("fails when ffmpeg does", func() {
		prober.EXPECT().ConvertToFLAC(mock.Anything, src, dst).
			Return(errors.New("boom")).Once()

		_, _, err := w.convertWAV(context.Background(), src, dir)

		Expect(err).To(MatchError(ErrConversionFailed))
	})
})

var _ = Describe("listAudioFiles", Label("unit", "importer"), func() {
	It("lists WAV files only when they convert", func() {
		dir := GinkgoT().TempDir()
		for _, n := range []string{"01.flac", "02.wav"} {
			Expect(os.WriteFile(filepath.Join(dir, n), nil, 0o600)).To(Succeed())
		}
		without, err := listAudioFiles(dir, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(without).To(Equal([]string{filepath.Join(dir, "01.flac")}))
		with, err := listAudioFiles(dir, true)
		Expect(err).NotTo(HaveOccurred())
		Expect(with).To(HaveLen(2))
	})
})

var _ = Describe(
	"Worker album import of WAV files",
	Label("unit", "importer"),
	func() {
		var (
			storeMk  *mockdb.MockStore
			msMk     *msmocks.MockRefresher
			prober   *mockffmpeg.MockProber
			w        *Worker
			dlDir    string
			musicDir string
			alb      *ent.Album
			rec      *ent.DownloadRecord
			// failing names the source whose conversion fails.
			failing string
		)

		wav := func(position int) string {
			GinkgoHelper()
			t := albumTracks[position-1]
			p := filepath.Join(
				dlDir,
				"0"+string(rune('0'+position))+" - "+t.title+".wav",
			)
			Expect(os.WriteFile(p, []byte("not really audio"), 0o600)).To(Succeed())
			return p
		}

		convertDirs := func() []string {
			GinkgoHelper()
			left, err := filepath.Glob(filepath.Join(musicDir, ".convert-*"))
			Expect(err).NotTo(HaveOccurred())
			return left
		}

		BeforeEach(func() {
			tmp := GinkgoT().TempDir()
			dlDir = filepath.Join(tmp, "dl")
			musicDir = filepath.Join(tmp, "music")
			Expect(os.MkdirAll(dlDir, 0o755)).To(Succeed())
			Expect(os.MkdirAll(musicDir, 0o755)).To(Succeed())
			configtest.Setup(map[string]any{"library": map[string]any{
				"music_path":   musicDir,
				"music_naming": albumNaming,
				"import_mode":  "hardlink",
			}})
			failing = ""

			storeMk = mockdb.NewMockStore(GinkgoT())
			msMk = msmocks.NewMockRefresher(GinkgoT())
			dlMk := mockdl.NewMockDownloader(GinkgoT())
			dlMk.EXPECT().ListTorrentFiles(mock.Anything, "qbit", "hash").
				Return(nil, nil).Maybe()
			storeMk.EXPECT().
				MarkRequestsAvailableByMBID(mock.Anything, mock.Anything, mock.Anything).
				Return(nil).
				Maybe()

			fixture, err := os.ReadFile(
				filepath.Join(
					"..",
					"library",
					"audiotags",
					"testdata",
					"tagged.flac",
				),
			)
			Expect(err).NotTo(HaveOccurred())
			prober = mockffmpeg.NewMockProber(GinkgoT())
			prober.EXPECT().Available().Return(true).Maybe()
			prober.EXPECT().FFmpegPath().Return("/bin/ffmpeg").Maybe()
			prober.EXPECT().ProbeAudio(mock.Anything, mock.Anything).
				RunAndReturn(func(_ context.Context, p string) (*ffmpeg.AudioInfo, error) {
					if strings.HasSuffix(p, ".wav") {
						return wavProbe, nil
					}
					return flacProbe, nil
				}).
				Maybe()
			prober.EXPECT().
				ConvertToFLAC(mock.Anything, mock.Anything, mock.Anything).
				RunAndReturn(func(_ context.Context, src, dst string) error {
					if src == failing {
						return errors.New("boom")
					}
					return os.WriteFile(dst, fixture, 0o600)
				}).
				Maybe()

			w = NewWorker(Deps{
				DB:          storeMk,
				Library:     library.NewImportService(),
				MediaServer: msMk,
				Download:    dlMk,
				Prober:      prober,
			})
			alb = fixtureAlbum()
			rec = fixtureAlbumRecord(dlDir, alb)
			rec.VerificationBypassed = true
		})

		imports := func() db.RecordAlbumImportSuccessParams {
			GinkgoHelper()
			storeMk.EXPECT().FindImportingDownloadRecordByID(mock.Anything, rec.ID).
				Return(rec, nil).Once()
			var got db.RecordAlbumImportSuccessParams
			storeMk.EXPECT().RecordAlbumImportSuccess(mock.Anything, mock.Anything).
				Run(func(_ context.Context, p db.RecordAlbumImportSuccessParams) { got = p }).
				Return(nil).Once()
			msMk.EXPECT().
				RefreshAll(mock.Anything, "music", musicDir, mock.Anything).
				Return(nil).
				Once()
			Expect(w.runImport(context.Background(), rec.ID)).To(Succeed())
			return got
		}

		It("places a FLAC for each WAV and leaves the sources alone", func() {
			sources := []string{wav(1), wav(2)}

			got := imports()

			Expect(got.Files).To(HaveLen(2))
			for _, f := range got.Files {
				Expect(f.Format).To(Equal("flac"))
				Expect(f.Path).To(HaveSuffix(".flac"))
				Expect(f.Path).To(BeAnExistingFile())
			}
			Expect(got.Files[0].TrackID).To(Equal(albumTracks[0].id))
			for _, s := range sources {
				Expect(s).To(BeAnExistingFile())
			}
			Expect(convertDirs()).To(BeEmpty())
		})

		It("records what the converted file measures, not the WAV", func() {
			wav(1)
			prober.ExpectedCalls = slices.DeleteFunc(prober.ExpectedCalls,
				func(c *mock.Call) bool { return c.Method == "ProbeAudio" })
			prober.EXPECT().ProbeAudio(mock.Anything, mock.Anything).
				RunAndReturn(func(_ context.Context, p string) (*ffmpeg.AudioInfo, error) {
					if strings.HasSuffix(p, ".wav") {
						return &ffmpeg.AudioInfo{
							Codec: "pcm_f32le", BitDepth: 32,
							SampleRateHz: 44100, DurationSec: 200,
						}, nil
					}
					return &ffmpeg.AudioInfo{
						Codec: "flac", BitDepth: 24,
						SampleRateHz: 44100, DurationSec: 200,
					}, nil
				}).
				Maybe()

			got := imports()

			Expect(got.Files).To(HaveLen(1))
			Expect(got.Files[0].BitDepth).To(Equal(uint8(24)))
		})

		It("imports the other tracks when one conversion fails", func() {
			wav(1)
			failing = wav(2)

			got := imports()

			Expect(got.Files).To(HaveLen(1))
			Expect(got.Files[0].TrackID).To(Equal(albumTracks[0].id))
			Expect(convertDirs()).To(BeEmpty())
		})

		It(
			"fills a track once when the release holds it as FLAC and as WAV",
			func() {
				seedTrackFile(
					dlDir,
					"00 - a.flac",
					"tagged.flac",
					1,
					albumTracks[0].title,
				)
				wav(1)

				got := imports()

				Expect(got.Files).To(HaveLen(1))
				prober.AssertNotCalled(GinkgoT(), "ConvertToFLAC",
					mock.Anything, mock.Anything, mock.Anything)
			},
		)

		It(
			"offers the WAV files of a held record and places them where assigned",
			func() {
				first := wav(1)
				held := fixtureAlbumRecord(dlDir, alb)
				held.Status = downloadrecord.StatusHeld
				storeMk.EXPECT().FindHeldAlbumRecord(mock.Anything, held.ID).
					Return(held, nil).Once()
				plan, err := w.AlbumTrackPlan(context.Background(), held.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(plan.Files).To(HaveLen(1))
				Expect(plan.Files[0].Path).To(Equal(first))

				rec.TrackAssignments = map[string]uint32{first: albumTracks[1].id}
				got := imports()

				Expect(got.Files).To(HaveLen(1))
				Expect(got.Files[0].TrackID).To(Equal(albumTracks[1].id))
				Expect(got.Files[0].Path).To(HaveSuffix("02 - In Bloom.flac"))
			},
		)
	},
)
