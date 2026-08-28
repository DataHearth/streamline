package hygiene

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/ffmpeg"
	ffmpegmocks "github.com/datahearth/streamline/internal/ffmpeg/mocks"
	libmocks "github.com/datahearth/streamline/internal/library/mocks"
	metamocks "github.com/datahearth/streamline/internal/metadata/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Service.RunMediaProbe", Label("unit", "hygiene"), func() {
	var (
		ctx          context.Context
		tmpDir       string
		existingFile string
		store        *dbmocks.MockStore
		prober       *ffmpegmocks.MockProber
		svc          *Service
	)

	BeforeEach(func() {
		ctx = context.Background()
		tmpDir = GinkgoT().TempDir()
		existingFile = filepath.Join(tmpDir, "movie.mkv")
		Expect(os.WriteFile(existingFile, []byte("data"), 0o644)).To(Succeed())

		store = dbmocks.NewMockStore(GinkgoT())
		prober = ffmpegmocks.NewMockProber(GinkgoT())
		svc = New(
			store,
			metamocks.NewMockProvider(GinkgoT()),
			metamocks.NewMockTVProvider(GinkgoT()),
			libmocks.NewMockImporter(GinkgoT()),
			nil,
		)
		svc.Probe = prober
	})

	It("no-ops when ffmpeg is disabled", func() {
		configtest.Setup(map[string]any{
			"ffmpeg": map[string]any{
				"enabled": false,
			},
		})

		// No db/prober expectations registered: mockery fails the spec if
		// either is called while probing is disabled.
		Expect(svc.RunMediaProbe(ctx)).To(Succeed())
	})

	It("probes unprobed rows and stamps results", func() {
		configtest.Setup(map[string]any{
			"ffmpeg": map[string]any{
				"enabled": true,
			},
		})

		store.EXPECT().ListUnprobedMediaFiles(mock.Anything, probeBatchSize).
			Return([]*ent.MediaFile{
				{ID: 1, Path: existingFile},
				{ID: 2, Path: filepath.Join(tmpDir, "gone.mkv")},
			}, nil).Once()
		prober.EXPECT().Available().Return(true).Once()
		prober.EXPECT().Probe(mock.Anything, existingFile).
			Return(&ffmpeg.Info{
				VideoCodec:  "h264",
				DurationSec: 100,
				Container:   "matroska",
			}, nil).Once()
		store.EXPECT().
			StampMediaFileProbe(mock.Anything, uint32(1), mock.Anything, mock.Anything).
			Return(nil).
			Once()
		// row 2: os.Stat fails → skipped entirely, NOT stamped (drift-check
		// owns missing files; stamping would hide the row from a later probe
		// once the mount returns).

		Expect(svc.RunMediaProbe(ctx)).To(Succeed())
	})

	It("stamps probed_at alone when the probe errors", func() {
		configtest.Setup(map[string]any{
			"ffmpeg": map[string]any{
				"enabled": true,
			},
		})

		store.EXPECT().ListUnprobedMediaFiles(mock.Anything, probeBatchSize).
			Return([]*ent.MediaFile{{ID: 3, Path: existingFile}}, nil).Once()
		prober.EXPECT().Available().Return(true).Once()
		prober.EXPECT().Probe(mock.Anything, existingFile).
			Return(nil, ffmpeg.ErrNoVideoStream).Once()
		store.EXPECT().
			StampMediaFileProbe(mock.Anything, uint32(3), mock.Anything, (*ffmpeg.Info)(nil)).
			Return(nil).Once()

		Expect(svc.RunMediaProbe(ctx)).To(Succeed())
	})

	It("keeps probing later rows after a stamp write fails", func() {
		configtest.Setup(map[string]any{
			"ffmpeg": map[string]any{
				"enabled": true,
			},
		})

		secondFile := filepath.Join(tmpDir, "movie2.mkv")
		Expect(os.WriteFile(secondFile, []byte("data"), 0o644)).To(Succeed())

		store.EXPECT().ListUnprobedMediaFiles(mock.Anything, probeBatchSize).
			Return([]*ent.MediaFile{
				{ID: 1, Path: existingFile},
				{ID: 2, Path: secondFile},
			}, nil).Once()
		prober.EXPECT().Available().Return(true).Once()
		prober.EXPECT().Probe(mock.Anything, existingFile).
			Return(&ffmpeg.Info{VideoCodec: "h264"}, nil).Once()
		store.EXPECT().
			StampMediaFileProbe(mock.Anything, uint32(1), mock.Anything, mock.Anything).
			Return(errors.New("db unavailable")).
			Once()
		// Row 1's stamp write failing must not abort the tick: row 2 is
		// oldest-first behind it, and returning here would re-select row 1
		// at the head of every subsequent tick, starving row 2 forever.
		prober.EXPECT().Probe(mock.Anything, secondFile).
			Return(&ffmpeg.Info{VideoCodec: "h264"}, nil).Once()
		store.EXPECT().
			StampMediaFileProbe(mock.Anything, uint32(2), mock.Anything, mock.Anything).
			Return(nil).
			Once()

		var buf bytes.Buffer
		GinkgoWriter.TeeTo(&buf)
		DeferCleanup(GinkgoWriter.ClearTeeWriters)

		Expect(svc.RunMediaProbe(ctx)).To(Succeed())
		Expect(buf.String()).To(ContainSubstring("stamp media probe failed"))
	})

	It(
		"does not log an error when the row was deleted concurrently between listing and stamping",
		func() {
			configtest.Setup(map[string]any{
				"ffmpeg": map[string]any{
					"enabled": true,
				},
			})

			store.EXPECT().ListUnprobedMediaFiles(mock.Anything, probeBatchSize).
				Return([]*ent.MediaFile{{ID: 9, Path: existingFile}}, nil).Once()
			prober.EXPECT().Available().Return(true).Once()
			prober.EXPECT().Probe(mock.Anything, existingFile).
				Return(&ffmpeg.Info{VideoCodec: "h264"}, nil).Once()
			// The drift-check job can delete the row between ListUnprobedMediaFiles
			// and this StampMediaFileProbe call; that race is routine, not an error.
			store.EXPECT().
				StampMediaFileProbe(mock.Anything, uint32(9), mock.Anything, mock.Anything).
				Return(&ent.NotFoundError{}).
				Once()

			var buf bytes.Buffer
			GinkgoWriter.TeeTo(&buf)
			DeferCleanup(GinkgoWriter.ClearTeeWriters)

			Expect(svc.RunMediaProbe(ctx)).To(Succeed())
			Expect(buf.String()).NotTo(ContainSubstring("stamp media probe failed"))
		},
	)

	It(
		"does not let more skips than the batch size starve a probeable row behind them",
		func() {
			configtest.Setup(map[string]any{
				"ffmpeg": map[string]any{
					"enabled": true,
				},
			})

			// 26 unreachable rows — one more than probeBatchSize — followed by a
			// single probeable row. A naive single-fetch tick would see nothing
			// but skips and never reach it.
			skipRows := make([]*ent.MediaFile, 26)
			for i := range skipRows {
				skipRows[i] = &ent.MediaFile{
					ID:   uint32(i + 1),
					Path: filepath.Join(tmpDir, fmt.Sprintf("gone-%d.mkv", i)),
				}
			}
			allRows := append(append([]*ent.MediaFile{}, skipRows...),
				&ent.MediaFile{ID: 27, Path: existingFile})

			store.EXPECT().ListUnprobedMediaFiles(mock.Anything, probeBatchSize).
				Return(skipRows, nil).Once()
			store.EXPECT().ListUnprobedMediaFiles(mock.Anything, probeBatchSize*2).
				Return(allRows, nil).Once()
			prober.EXPECT().Available().Return(true).Once()
			prober.EXPECT().Probe(mock.Anything, existingFile).
				Return(&ffmpeg.Info{VideoCodec: "h264"}, nil).Once()
			store.EXPECT().
				StampMediaFileProbe(mock.Anything, uint32(27), mock.Anything, mock.Anything).
				Return(nil).
				Once()

			Expect(svc.RunMediaProbe(ctx)).To(Succeed())
		},
	)
})
