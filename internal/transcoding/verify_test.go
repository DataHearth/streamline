package transcoding

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/ffmpeg"
)

var _ = Describe("checkOutput", Label("unit", "transcoding"), func() {
	var src, out *ffmpeg.Info
	var v config.TranscodeVerifyConfig

	BeforeEach(func() {
		src = &ffmpeg.Info{
			DurationSec: 7200, Width: 1920, Height: 1080,
			AudioTracks: 2, SubLangs: "eng,fra",
		}
		clone := *src
		out = &clone
		v = config.TranscodeVerifyConfig{MaxSizePercent: 100, MinSizePercent: 5}
	})

	It("accepts an identical, smaller output", func() {
		Expect(
			checkOutput(src, out, 1000, 600, ActionTranscode, "mkv", v),
		).To(Succeed())
	})

	It("rejects a duration drift over two seconds", func() {
		out.DurationSec = 7197
		err := checkOutput(src, out, 1000, 600, ActionTranscode, "mkv", v)
		Expect(
			err,
		).To(MatchError("output rejected: duration 7197s against source 7200s"))
	})

	It("rejects a lost audio track", func() {
		out.AudioTracks = 1
		Expect(checkOutput(src, out, 1000, 600, ActionTranscode, "mkv", v)).
			To(MatchError("output rejected: audio tracks 1 against source 2"))
	})

	It("rejects a changed resolution", func() {
		out.Width, out.Height = 1280, 720
		Expect(checkOutput(src, out, 1000, 600, ActionTranscode, "mkv", v)).
			To(MatchError("output rejected: resolution 1280x720 against source 1920x1080"))
	})

	It("rejects lost subtitles on an mkv target", func() {
		out.SubLangs = "eng"
		Expect(checkOutput(src, out, 1000, 600, ActionTranscode, "mkv", v)).
			To(MatchError(`output rejected: subtitle languages "eng" against source "eng,fra"`))
	})

	It("ignores subtitles on an mp4 target, which drops them by design", func() {
		out.SubLangs = ""
		Expect(
			checkOutput(src, out, 1000, 600, ActionTranscode, "mp4", v),
		).To(Succeed())
	})

	It("rejects a transcode that grew past the ceiling", func() {
		err := checkOutput(src, out, 1000, 1320, ActionTranscode, "mkv", v)
		Expect(
			err,
		).To(MatchError("output rejected: output is 132% of the source (max 100%)"))
		var rej *rejection
		Expect(errors.As(err, &rej)).To(BeTrue())
		Expect(rej.outSize).To(Equal(int64(1320)))
	})

	It("lets a remux grow, since it copies the bytes", func() {
		Expect(
			checkOutput(src, out, 1000, 1005, ActionRemux, "mkv", v),
		).To(Succeed())
	})

	It("rejects an output under the floor on either action", func() {
		Expect(checkOutput(src, out, 1000, 30, ActionRemux, "mkv", v)).
			To(MatchError("output rejected: output is 3% of the source (min 5%)"))
	})

	It("treats a zero bound as disabled", func() {
		v.MaxSizePercent, v.MinSizePercent = 0, 0
		Expect(
			checkOutput(src, out, 1000, 5000, ActionTranscode, "mkv", v),
		).To(Succeed())
		Expect(
			checkOutput(src, out, 1000, 1, ActionTranscode, "mkv", v),
		).To(Succeed())
	})

	It("skips the size band for a zero-byte source", func() {
		Expect(
			checkOutput(src, out, 0, 600, ActionTranscode, "mkv", v),
		).To(Succeed())
	})
})

var _ = Describe("parseVMAF", Label("unit", "transcoding"), func() {
	It("reads the pooled score off ffmpeg's stderr", func() {
		score, ok := parseVMAF(
			"frame= 1440\n[Parsed_libvmaf_0 @ 0x55] VMAF score: 93.412873\nsize=N/A",
		)
		Expect(ok).To(BeTrue())
		Expect(score).To(BeNumerically("~", 93.41, 0.01))
	})

	It("reports no score when the line is absent", func() {
		_, ok := parseVMAF("No such filter: 'libvmaf'")
		Expect(ok).To(BeFalse())
	})
})

var _ = Describe("vmafWindows", Label("unit", "transcoding"), func() {
	It("places three 60s windows at 10, 50 and 90 % of a long file", func() {
		Expect(vmafWindows(7200)).To(Equal([]vmafWindow{
			{
				start:  720,
				length: 60,
			},
			{start: 3600, length: 60},
			{start: 6480, length: 60},
		}))
	})

	It("shrinks the windows and keeps them inside a short file", func() {
		w := vmafWindows(90)
		Expect(w).To(HaveLen(3))
		for _, x := range w {
			Expect(x.length).To(Equal(uint32(30)))
			Expect(x.start + x.length).To(BeNumerically("<=", 90))
		}
	})

	It("yields nothing for a file too short to window", func() {
		Expect(vmafWindows(2)).To(BeEmpty())
	})
})
