package transcoding

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	mockffmpeg "github.com/datahearth/streamline/internal/ffmpeg/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("hardware encoding", Label("unit", "transcoding"), func() {
	It("reads the VAAPI encoders off ffmpeg's listing", func() {
		listing := ` V....D hevc_vaapi           H.265/HEVC (VAAPI) (codec hevc)
 V....D av1_vaapi            AV1 (VAAPI) (codec av1)
 V....D h264_qsv             H.264 (Intel Quick Sync Video acceleration) (codec h264)
`
		Expect(vaapiEncodersIn(listing)).To(Equal(map[string]string{
			"hevc": "hevc_vaapi",
			"av1":  "av1_vaapi",
		}))
	})

	It("answers software without probing when hw_accel is none", func() {
		configtest.Setup(map[string]any{
			"transcoding": map[string]any{"hw_accel": "none"},
		})
		w := NewWorker(Deps{Prober: mockffmpeg.NewMockProber(GinkgoT())})
		Expect(w.hardware(context.Background())).To(BeNil())
		status, reason := w.HWStatus(context.Background())
		Expect(status).To(Equal("off"))
		Expect(reason).To(BeEmpty())
	})

	It("probes once per config value and reports the failure", func() {
		configtest.Setup(map[string]any{
			"transcoding": map[string]any{"hw_accel": "auto"},
		})
		prober := mockffmpeg.NewMockProber(GinkgoT())
		prober.EXPECT().FFmpegPath().Return("/nonexistent/ffmpeg").Once()
		w := NewWorker(Deps{Prober: prober})
		Expect(w.hardware(context.Background())).To(BeNil())
		// The second call answers off the cache: FFmpegPath is expected once.
		status, reason := w.HWStatus(context.Background())
		Expect(status).To(Equal("unavailable"))
		Expect(reason).To(ContainSubstring("list encoders"))
	})
})
