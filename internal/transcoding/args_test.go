package transcoding

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/ffmpeg"
)

var _ = Describe("BuildArgs", Label("unit", "transcoding"), func() {
	It("builds the golden transcode invocation", func() {
		info := &ffmpeg.Info{AudioCodecs: []string{"dts", "mp3"}}
		pol := config.TranscodePolicy{To: config.TranscodeTo{
			Container:        "mkv",
			VideoCodec:       "hevc",
			CRF:              22,
			Preset:           "medium",
			AudioCodec:       "aac",
			AudioPassthrough: config.DefaultAudioPassthrough,
		}}
		args := BuildArgs("/in.mkv", "/out.mkv", info, pol, ActionTranscode)
		Expect(args).To(Equal([]string{
			"-hide_banner", "-nostats", "-progress", "pipe:1", "-y",
			"-i", "/in.mkv", "-map", "0",
			"-c:v", "libx265", "-crf", "22", "-preset", "medium",
			"-c:a:0", "copy", "-c:a:1", "aac",
			"-c:s", "copy", "-c:t", "copy",
			"/out.mkv",
		}))
	})

	It("builds the golden remux invocation", func() {
		info := &ffmpeg.Info{}
		pol := config.TranscodePolicy{To: config.TranscodeTo{Container: "mkv"}}
		args := BuildArgs("/in.mp4", "/out.mkv", info, pol, ActionRemux)
		Expect(args).To(Equal([]string{
			"-hide_banner", "-nostats", "-progress", "pipe:1", "-y",
			"-i", "/in.mp4", "-map", "0",
			"-c", "copy",
			"/out.mkv",
		}))
	})

	It("omits -crf entirely at 0, leaving the encoder's default", func() {
		info := &ffmpeg.Info{AudioCodecs: []string{"aac"}}
		pol := config.TranscodePolicy{To: config.TranscodeTo{
			Container:  "mkv",
			VideoCodec: "hevc",
			Preset:     "medium",
			AudioCodec: "aac",
		}}
		args := BuildArgs("/in.mkv", "/out.mkv", info, pol, ActionTranscode)
		Expect(args).NotTo(ContainElement("-crf"))
		// -preset follows the encoder directly, so the omission dropped the
		// pair rather than leaving a stray value in a flag's position.
		Expect(args[indexOf(args, "-c:v")+2]).To(Equal("-preset"))
	})

	It("omits -c:t copy for a non-mkv target", func() {
		info := &ffmpeg.Info{AudioCodecs: []string{"aac"}}
		pol := config.TranscodeTo{
			Container:  "mp4",
			VideoCodec: "h264",
			CRF:        20,
			Preset:     "fast",
			AudioCodec: "aac",
		}
		args := BuildArgs(
			"/in.mkv",
			"/out.mp4",
			info,
			config.TranscodePolicy{To: pol},
			ActionTranscode,
		)
		Expect(args).NotTo(ContainElement("-c:t"))
		Expect(args).To(ContainElement("-c:s"))
	})

	DescribeTable("encoder mapping",
		func(codec, encoder string) {
			info := &ffmpeg.Info{}
			pol := config.TranscodePolicy{To: config.TranscodeTo{
				Container:  "mkv",
				VideoCodec: codec,
				CRF:        22,
				Preset:     "medium",
				AudioCodec: "aac",
			}}
			args := BuildArgs("/in.mkv", "/out.mkv", info, pol, ActionTranscode)
			idx := -1
			for i, a := range args {
				if a == "-c:v" {
					idx = i
				}
			}
			Expect(idx).To(BeNumerically(">=", 0))
			Expect(args[idx+1]).To(Equal(encoder))
		},
		Entry("h264", "h264", "libx264"),
		Entry("hevc", "hevc", "libx265"),
		Entry("av1", "av1", "libsvtav1"),
	)

	It("passes through an audio codec on the policy's explicit list", func() {
		info := &ffmpeg.Info{AudioCodecs: []string{"ac3"}}
		pol := config.TranscodePolicy{To: config.TranscodeTo{
			Container:        "mkv",
			VideoCodec:       "hevc",
			CRF:              22,
			Preset:           "medium",
			AudioCodec:       "aac",
			AudioPassthrough: []string{"ac3"},
		}}
		args := BuildArgs("/in.mkv", "/out.mkv", info, pol, ActionTranscode)
		Expect(args).To(ContainElement("-c:a:0"))
		i := indexOf(args, "-c:a:0")
		Expect(args[i+1]).To(Equal("copy"))
	})
})

func indexOf(s []string, v string) int {
	GinkgoHelper()
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}
