package importer

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/schema"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	mockdb "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/ffmpeg"
	mockffmpeg "github.com/datahearth/streamline/internal/ffmpeg/mocks"
	"github.com/datahearth/streamline/internal/library"
	msmocks "github.com/datahearth/streamline/internal/mediaserver/mocks"
	"github.com/datahearth/streamline/internal/quality"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

func albumFileQualityOf(tier string) albumFileQuality {
	t, ok := quality.ParseAudioTier(tier)
	return albumFileQuality{tier: t, known: ok}
}

var _ = Describe("replacesTrack", Label("unit", "importer"), func() {
	profile := config.MusicQualityProfileEntry{
		Name:           "p",
		Tiers:          []string{"hires", "lossless", "high"},
		Preferred:      "lossless",
		UpgradeAllowed: true,
	}
	track := func(quality ...string) *ent.Track {
		tr := &ent.Track{}
		for _, q := range quality {
			tr.Edges.MediaFiles = append(
				tr.Edges.MediaFiles,
				&ent.MediaFile{Quality: q},
			)
		}
		return tr
	}
	rec := func(mode downloadrecord.ReplaceMode) *ent.DownloadRecord {
		return &ent.DownloadRecord{ReplaceMode: mode}
	}
	incoming := func(tier string) albumFileQuality {
		return albumFileQualityOf(tier)
	}

	It("takes every covered track in replace mode all", func() {
		Expect(replacesTrack(
			rec(
				downloadrecord.ReplaceModeAll,
			),
			track("lossless"),
			incoming("high"),
			profile,
		)).To(BeTrue())
	})

	It("leaves a covered track alone with no replace mode", func() {
		Expect(replacesTrack(
			rec(
				downloadrecord.ReplaceModeNone,
			),
			track("high"),
			incoming("lossless"),
			profile,
		)).To(BeFalse())
	})

	It("upgrades a track only when the incoming tier beats its own", func() {
		up := rec(downloadrecord.ReplaceModeUpgrades)
		Expect(
			replacesTrack(up, track("high"), incoming("lossless"), profile),
		).To(BeTrue())
		Expect(
			replacesTrack(up, track("high"), incoming("high"), profile),
		).To(BeFalse())
		Expect(
			replacesTrack(up, track("lossless"), incoming("hires"), profile),
		).To(BeFalse())
	})

	It("neither upgrades nor blocks on a held file of unknown tier", func() {
		up := rec(downloadrecord.ReplaceModeUpgrades)
		Expect(
			replacesTrack(up, track(""), incoming("lossless"), profile),
		).To(BeFalse())
		Expect(
			replacesTrack(up, track("high", ""), incoming("lossless"), profile),
		).To(BeFalse())
	})

	It("compares against the worst file a track holds", func() {
		up := rec(downloadrecord.ReplaceModeUpgrades)
		Expect(
			replacesTrack(
				up,
				track("lossless", "high"),
				incoming("lossless"),
				profile,
			),
		).
			To(BeTrue())
	})
})

var _ = Describe(
	"Worker album tier verification",
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
		)

		BeforeEach(func() {
			tmp := GinkgoT().TempDir()
			dlDir = filepath.Join(tmp, "dl")
			musicDir = filepath.Join(tmp, "music")
			Expect(os.MkdirAll(dlDir, 0o755)).To(Succeed())
			configtest.Setup(map[string]any{
				"library": map[string]any{
					"music_path":          musicDir,
					"music_naming":        albumNaming,
					"import_mode":         "copy",
					"import_max_attempts": 3,
				},
				"music_quality_profiles": []map[string]any{{
					"name": "hifi", "tiers": []string{"hires", "lossless"},
					"preferred": "lossless",
				}},
				"music_quality_default_profile": "hifi",
			})
			storeMk = mockdb.NewMockStore(GinkgoT())
			msMk = msmocks.NewMockRefresher(GinkgoT())
			prober = mockffmpeg.NewMockProber(GinkgoT())
			w = NewWorker(Deps{
				DB: storeMk, Library: library.NewImportService(),
				MediaServer: msMk, Prober: prober,
			})
			alb = fixtureAlbum()
			prober.EXPECT().Available().Return(true).Maybe()
		})

		It(
			"parks the record held, moving nothing, when a measured tier is not ticked",
			func() {
				seedTrackFile(
					dlDir,
					"a.flac",
					"tagged.flac",
					1,
					albumTracks[0].title,
				)
				rec := fixtureAlbumRecord(dlDir, alb)
				storeMk.EXPECT().
					FindImportingDownloadRecordByID(mock.Anything, rec.ID).
					Return(rec, nil).
					Once()
				prober.EXPECT().ProbeAudio(mock.Anything, mock.Anything).
					Return(&ffmpeg.AudioInfo{Codec: "mp3", BitrateKbps: 128, DurationSec: 200}, nil).
					Once()
				var reasons []schema.HoldReason
				storeMk.EXPECT().
					HoldDownloadRecord(mock.Anything, rec.ID, mock.Anything).
					Run(func(_ context.Context, _ uint32, r []schema.HoldReason) { reasons = r }).
					Return(nil).
					Once()

				Expect(w.runImport(context.Background(), rec.ID)).To(Succeed())

				Expect(reasons).To(HaveLen(1))
				Expect(reasons[0].Check).To(Equal("tier"))
				Expect(reasons[0].Expected).To(Equal("hires/lossless"))
				Expect(reasons[0].Actual).To(Equal("low"))
				Expect(musicDir).NotTo(BeADirectory())
			},
		)

		It(
			"imports a measured tier the profile ticks and stores the tier as quality",
			func() {
				seedTrackFile(
					dlDir,
					"a.flac",
					"tagged.flac",
					1,
					albumTracks[0].title,
				)
				rec := fixtureAlbumRecord(dlDir, alb)
				storeMk.EXPECT().
					FindImportingDownloadRecordByID(mock.Anything, rec.ID).
					Return(rec, nil).
					Once()
				prober.EXPECT().ProbeAudio(mock.Anything, mock.Anything).
					Return(&ffmpeg.AudioInfo{
						Codec:        "flac",
						BitDepth:     24,
						SampleRateHz: 96000,
						DurationSec:  200,
					}, nil).Once()
				var got db.RecordAlbumImportSuccessParams
				storeMk.EXPECT().
					RecordAlbumImportSuccess(mock.Anything, mock.Anything).
					Run(func(_ context.Context, p db.RecordAlbumImportSuccessParams) { got = p }).
					Return(nil).
					Once()
				storeMk.EXPECT().
					MarkRequestsAvailableByMBID(mock.Anything, "artist", "artist-4").
					Return(nil).Once()
				msMk.EXPECT().
					RefreshAll(mock.Anything, "music", musicDir).
					Return(nil).
					Once()

				Expect(w.runImport(context.Background(), rec.ID)).To(Succeed())

				Expect(got.Files).To(HaveLen(1))
				Expect(got.Files[0].Quality).To(Equal("hires"))
				Expect(got.Files[0].Format).To(Equal("flac"))
			},
		)

		It("skips the check, with the file imported, when the probe fails", func() {
			seedTrackFile(dlDir, "a.flac", "tagged.flac", 1, albumTracks[0].title)
			rec := fixtureAlbumRecord(dlDir, alb)
			storeMk.EXPECT().FindImportingDownloadRecordByID(mock.Anything, rec.ID).
				Return(rec, nil).Once()
			prober.EXPECT().ProbeAudio(mock.Anything, mock.Anything).
				Return(nil, ffmpeg.ErrUnreadable).Once()
			var got db.RecordAlbumImportSuccessParams
			storeMk.EXPECT().RecordAlbumImportSuccess(mock.Anything, mock.Anything).
				Run(func(_ context.Context, p db.RecordAlbumImportSuccessParams) { got = p }).
				Return(nil).Once()
			storeMk.EXPECT().
				MarkRequestsAvailableByMBID(mock.Anything, "artist", "artist-4").
				Return(nil).Once()
			msMk.EXPECT().
				RefreshAll(mock.Anything, "music", musicDir).
				Return(nil).
				Once()

			Expect(w.runImport(context.Background(), rec.ID)).To(Succeed())

			Expect(got.Files).To(HaveLen(1))
			Expect(got.Files[0].Quality).To(Equal("lossless"))
		})

		It("lets a bypassed record through whatever the measurement says", func() {
			seedTrackFile(dlDir, "a.flac", "tagged.flac", 1, albumTracks[0].title)
			rec := fixtureAlbumRecord(dlDir, alb)
			rec.VerificationBypassed = true
			storeMk.EXPECT().FindImportingDownloadRecordByID(mock.Anything, rec.ID).
				Return(rec, nil).Once()
			prober.EXPECT().ProbeAudio(mock.Anything, mock.Anything).
				Return(&ffmpeg.AudioInfo{Codec: "mp3", BitrateKbps: 128, DurationSec: 200}, nil).
				Once()
			storeMk.EXPECT().RecordAlbumImportSuccess(mock.Anything, mock.Anything).
				Return(nil).Once()
			storeMk.EXPECT().
				MarkRequestsAvailableByMBID(mock.Anything, "artist", "artist-4").
				Return(nil).Once()
			msMk.EXPECT().
				RefreshAll(mock.Anything, "music", musicDir).
				Return(nil).
				Once()

			Expect(w.runImport(context.Background(), rec.ID)).To(Succeed())
		})
	},
)
