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
		args := BuildArgs("/in.mkv", "/out.mkv", info, pol, ActionTranscode, nil)
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
		args := BuildArgs("/in.mp4", "/out.mkv", info, pol, ActionRemux, nil)
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
		args := BuildArgs("/in.mkv", "/out.mkv", info, pol, ActionTranscode, nil)
		Expect(args).NotTo(ContainElement("-crf"))
		// -preset follows the encoder directly, so the omission dropped the
		// pair rather than leaving a stray value in a flag's position.
		Expect(args[indexOf(args, "-c:v")+2]).To(Equal("-preset"))
	})

	It("builds the golden mp4 invocation, video and audio only", func() {
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
			nil,
		)
		Expect(args).To(Equal([]string{
			"-hide_banner", "-nostats", "-progress", "pipe:1", "-y",
			"-i", "/in.mkv", "-map", "0:v", "-map", "0:a",
			"-c:v", "libx264", "-crf", "20", "-preset", "fast",
			"-c:a:0", "aac",
			"/out.mp4",
		}))
	})

	It("maps video and audio only when remuxing into mp4", func() {
		info := &ffmpeg.Info{}
		pol := config.TranscodePolicy{To: config.TranscodeTo{Container: "mp4"}}
		args := BuildArgs("/in.mkv", "/out.mp4", info, pol, ActionRemux, nil)
		Expect(args).To(Equal([]string{
			"-hide_banner", "-nostats", "-progress", "pipe:1", "-y",
			"-i", "/in.mkv", "-map", "0:v", "-map", "0:a",
			"-c", "copy",
			"/out.mp4",
		}))
	})

	It("passes libsvtav1 a numeric preset", func() {
		info := &ffmpeg.Info{AudioCodecs: []string{"aac"}}
		pol := config.TranscodePolicy{To: config.TranscodeTo{
			Container:  "mkv",
			VideoCodec: "av1",
			CRF:        32,
			Preset:     "medium",
			AudioCodec: "aac",
		}}
		args := BuildArgs("/in.mkv", "/out.mkv", info, pol, ActionTranscode, nil)
		i := indexOf(args, "-c:v")
		Expect(args[i : i+6]).To(Equal([]string{
			"-c:v", "libsvtav1", "-crf", "32", "-preset", "7",
		}))
	})

	It("leaves an x264-style preset alone for libx265", func() {
		info := &ffmpeg.Info{AudioCodecs: []string{"aac"}}
		pol := config.TranscodePolicy{To: config.TranscodeTo{
			Container:  "mkv",
			VideoCodec: "hevc",
			CRF:        22,
			Preset:     "medium",
			AudioCodec: "aac",
		}}
		args := BuildArgs("/in.mkv", "/out.mkv", info, pol, ActionTranscode, nil)
		Expect(args[indexOf(args, "-preset")+1]).To(Equal("medium"))
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
			args := BuildArgs("/in.mkv", "/out.mkv", info, pol, ActionTranscode, nil)
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

	Describe("hardware encoding", func() {
		vaapi := &HW{
			Backend:  "vaapi",
			Device:   "/dev/dri/renderD128",
			Encoders: map[string]string{"hevc": "hevc_vaapi"},
		}
		pol := config.TranscodePolicy{To: config.TranscodeTo{
			Container:  "mkv",
			VideoCodec: "hevc",
			CRF:        22,
			Preset:     "medium",
			AudioCodec: "aac",
		}}
		info := &ffmpeg.Info{AudioCodecs: []string{"aac"}}

		It("builds the golden VAAPI invocation", func() {
			args := BuildArgs(
				"/in.mkv",
				"/out.mkv",
				info,
				pol,
				ActionTranscode,
				vaapi,
			)
			Expect(args).To(Equal([]string{
				"-hide_banner", "-nostats", "-progress", "pipe:1", "-y",
				"-vaapi_device", "/dev/dri/renderD128",
				"-i", "/in.mkv", "-map", "0",
				"-vf", "format=nv12,hwupload", "-c:v", "hevc_vaapi",
				"-rc_mode", "CQP", "-qp", "22", "-compression_level", "4",
				"-c:a:0", "aac",
				"-c:s", "copy", "-c:t", "copy",
				"/out.mkv",
			}))
		})

		It("falls back to software when the backend lacks the codec", func() {
			av1 := pol
			av1.To.VideoCodec = "av1"
			args := BuildArgs(
				"/in.mkv",
				"/out.mkv",
				info,
				av1,
				ActionTranscode,
				vaapi,
			)
			Expect(args).To(ContainElements("-c:v", "libsvtav1"))
			Expect(args).NotTo(ContainElement("-vaapi_device"))
		})

		DescribeTable(
			"keeps a 10-bit source at 10 bits when the device can",
			func(codec string, deviceTenBit bool, upload string, profile []string) {
				hw := &HW{
					Backend: "vaapi",
					Device:  "/dev/dri/renderD128",
					Encoders: map[string]string{
						"hevc": "hevc_vaapi",
						"av1":  "av1_vaapi",
						"h264": "h264_vaapi",
					},
					TenBit: deviceTenBit,
				}
				p := pol
				p.To.VideoCodec = codec
				tenBit := &ffmpeg.Info{AudioCodecs: []string{"aac"}, TenBit: true}
				args := BuildArgs(
					"/in.mkv",
					"/out.mkv",
					tenBit,
					p,
					ActionTranscode,
					hw,
				)
				Expect(args).To(ContainElement("format=" + upload + ",hwupload"))
				if len(profile) == 0 {
					Expect(args).NotTo(ContainElement("-profile:v"))
					return
				}
				i := indexOf(args, "-c:v")
				Expect(args[i+2 : i+2+len(profile)]).To(Equal(profile))
			},
			Entry(
				"hevc → p010 Main10",
				"hevc",
				true,
				"p010",
				[]string{"-profile:v", "main10"},
			),
			Entry(
				"av1 → p010, Main already covers it",
				"av1",
				true,
				"p010",
				[]string{},
			),
			Entry("h264 has no 10-bit profile", "h264", true, "nv12", []string{}),
			Entry(
				"device without Main10 → 8-bit",
				"hevc",
				false,
				"nv12",
				[]string{},
			),
		)

		It("never touches a remux", func() {
			args := BuildArgs("/in.mp4", "/out.mkv", info, pol, ActionRemux, vaapi)
			Expect(args).NotTo(ContainElement("-vaapi_device"))
			Expect(args).To(ContainElements("-c", "copy"))
		})
	})

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
		args := BuildArgs("/in.mkv", "/out.mkv", info, pol, ActionTranscode, nil)
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
