package transcoding

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/ffmpeg"
)

var _ = Describe("Evaluate", Label("unit", "transcoding"), func() {
	DescribeTable(
		"decision table",
		func(path string, info *ffmpeg.Info, pol config.TranscodePolicy, wantAction Action, wantReason string) {
			action, reason := Evaluate(path, info, pol)
			Expect(action).To(Equal(wantAction))
			if wantReason != "" {
				Expect(reason).To(ContainSubstring(wantReason))
			}
		},
		Entry("compliant h264/mkv under bitrate cap",
			"/x.mkv",
			&ffmpeg.Info{VideoCodec: "h264", VideoBitrateBPS: 8_000_000},
			config.TranscodePolicy{If: config.TranscodeIf{
				VideoCodecs: []string{
					"h264",
					"hevc",
				},
				Containers:      []string{"mkv"},
				MaxVideoBitrate: "12M",
			}},
			ActionNone, "",
		),
		Entry("h264/mkv over bitrate cap",
			"/x.mkv",
			&ffmpeg.Info{VideoCodec: "h264", VideoBitrateBPS: 14_500_000},
			config.TranscodePolicy{If: config.TranscodeIf{
				VideoCodecs: []string{
					"h264",
					"hevc",
				},
				Containers:      []string{"mkv"},
				MaxVideoBitrate: "12M",
			}},
			ActionTranscode, "video bitrate",
		),
		Entry("mpeg4 codec not allowed",
			"/x.avi",
			&ffmpeg.Info{VideoCodec: "mpeg4", VideoBitrateBPS: 4_000_000},
			config.TranscodePolicy{If: config.TranscodeIf{
				VideoCodecs: []string{
					"h264",
					"hevc",
				},
				Containers:      []string{"mkv"},
				MaxVideoBitrate: "12M",
			}},
			ActionTranscode, "codec",
		),
		Entry(
			"h264/mp4 wrong container only",
			"/x.mp4",
			&ffmpeg.Info{VideoCodec: "h264", VideoBitrateBPS: 8_000_000},
			config.TranscodePolicy{
				If: config.TranscodeIf{Containers: []string{"mkv"}},
			},
			ActionRemux,
			"container",
		),
		Entry("HDR suspends codec/bitrate rules, container already fine",
			"/x.mkv",
			&ffmpeg.Info{VideoCodec: "hevc", VideoBitrateBPS: 60_000_000, HDR: true},
			config.TranscodePolicy{If: config.TranscodeIf{MaxVideoBitrate: "12M"}},
			ActionNone, "",
		),
		Entry(
			"HDR still remuxed for container",
			"/x.mp4",
			&ffmpeg.Info{VideoCodec: "hevc", VideoBitrateBPS: 60_000_000, HDR: true},
			config.TranscodePolicy{
				If: config.TranscodeIf{
					Containers:      []string{"mkv"},
					MaxVideoBitrate: "12M",
				},
			},
			ActionRemux,
			"container",
		),
		Entry("h264 below min_video_bitrate keeps its codec",
			"/x.mkv",
			&ffmpeg.Info{VideoCodec: "h264", VideoBitrateBPS: 1_500_000},
			config.TranscodePolicy{If: config.TranscodeIf{
				VideoCodecs:     []string{"hevc", "av1"},
				Containers:      []string{"mkv"},
				MaxVideoBitrate: "12M",
				MinVideoBitrate: "2M",
			}},
			ActionNone, "",
		),
		Entry("h264 at min_video_bitrate is transcoded",
			"/x.mkv",
			&ffmpeg.Info{VideoCodec: "h264", VideoBitrateBPS: 2_000_000},
			config.TranscodePolicy{If: config.TranscodeIf{
				VideoCodecs:     []string{"hevc", "av1"},
				Containers:      []string{"mkv"},
				MaxVideoBitrate: "12M",
				MinVideoBitrate: "2M",
			}},
			ActionTranscode, "codec",
		),
		Entry("h264 above min_video_bitrate is transcoded",
			"/x.mkv",
			&ffmpeg.Info{VideoCodec: "h264", VideoBitrateBPS: 9_000_000},
			config.TranscodePolicy{If: config.TranscodeIf{
				VideoCodecs:     []string{"hevc", "av1"},
				Containers:      []string{"mkv"},
				MaxVideoBitrate: "12M",
				MinVideoBitrate: "2M",
			}},
			ActionTranscode, "codec",
		),
		Entry("the container rule still fires below min_video_bitrate",
			"/x.mp4",
			&ffmpeg.Info{VideoCodec: "h264", VideoBitrateBPS: 1_500_000},
			config.TranscodePolicy{If: config.TranscodeIf{
				VideoCodecs:     []string{"hevc", "av1"},
				Containers:      []string{"mkv"},
				MinVideoBitrate: "2M",
			}},
			ActionRemux, "container",
		),
		Entry("empty if never triggers a rule",
			"/x.avi",
			&ffmpeg.Info{VideoCodec: "mpeg4", VideoBitrateBPS: 999_000_000},
			config.TranscodePolicy{},
			ActionNone, "",
		),
	)

	It("treats an unparseable max_video_bitrate as no rule", func() {
		action, _ := Evaluate(
			"/x.mkv",
			&ffmpeg.Info{VideoCodec: "h264", VideoBitrateBPS: 999_000_000},
			config.TranscodePolicy{
				If: config.TranscodeIf{MaxVideoBitrate: "not-a-bitrate"},
			},
		)
		Expect(action).To(Equal(ActionNone))
	})

	It("treats an unparseable min_video_bitrate as no exemption", func() {
		action, _ := Evaluate(
			"/x.mkv",
			&ffmpeg.Info{VideoCodec: "h264", VideoBitrateBPS: 1_000},
			config.TranscodePolicy{
				If: config.TranscodeIf{
					VideoCodecs:     []string{"hevc"},
					MinVideoBitrate: "not-a-bitrate",
				},
			},
		)
		Expect(action).To(Equal(ActionTranscode))
	})
})
