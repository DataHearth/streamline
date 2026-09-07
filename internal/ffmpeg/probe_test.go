package ffmpeg

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("parseProbeOutput", Label("unit", "ffmpeg"), func() {
	const good = `{
	  "streams": [
	    {"codec_type":"video","codec_name":"hevc","width":3840,"height":1608},
	    {"codec_type":"audio","codec_name":"eac3","channels":6},
	    {"codec_type":"subtitle","codec_name":"subrip"}
	  ],
	  "format": {"format_name":"matroska,webm","duration":"8130.152000","bit_rate":"24500000"}
	}`

	It("extracts the first video and audio streams", func() {
		info, err := parseProbeOutput([]byte(good))
		Expect(err).NotTo(HaveOccurred())
		Expect(info.VideoCodec).To(Equal("hevc"))
		Expect(info.Width).To(Equal(uint16(3840)))
		Expect(info.Height).To(Equal(uint16(1608)))
		Expect(info.AudioCodec).To(Equal("eac3"))
		Expect(info.AudioChannels).To(Equal(uint8(6)))
		Expect(info.DurationSec).To(Equal(uint32(8130)))
		Expect(info.Container).To(Equal("matroska"))
		Expect(info.BitrateBPS).To(Equal(uint32(24500000)))
	})

	It("rejects output with no video stream", func() {
		_, err := parseProbeOutput(
			[]byte(
				`{"streams":[{"codec_type":"audio","codec_name":"mp3","channels":2}],"format":{"format_name":"mp3","duration":"100.0"}}`,
			),
		)
		Expect(err).To(MatchError(ErrNoVideoStream))
	})

	It("rejects zero duration", func() {
		_, err := parseProbeOutput(
			[]byte(
				`{"streams":[{"codec_type":"video","codec_name":"h264","width":1920,"height":1080}],"format":{"format_name":"matroska","duration":"0.000000"}}`,
			),
		)
		Expect(err).To(MatchError(ErrZeroDuration))
	})

	It("rejects garbage", func() {
		_, err := parseProbeOutput([]byte(`not json`))
		Expect(err).To(MatchError(ErrUnreadable))
	})

	It(
		"falls back to the video stream's own duration when format.duration is absent",
		func() {
			info, err := parseProbeOutput(
				[]byte(
					`{"streams":[{"codec_type":"video","codec_name":"h264","width":1920,"height":1080,"duration":"120.500000"}],"format":{"format_name":"mpegts"}}`,
				),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.DurationSec).To(Equal(uint32(120)))
		},
	)

	It(
		"still rejects zero duration when both format and stream durations are absent",
		func() {
			_, err := parseProbeOutput(
				[]byte(
					`{"streams":[{"codec_type":"video","codec_name":"h264","width":1920,"height":1080}],"format":{"format_name":"mpegts"}}`,
				),
			)
			Expect(err).To(MatchError(ErrZeroDuration))
		},
	)

	It("skips an embedded cover art stream ordered before the real video", func() {
		info, err := parseProbeOutput(
			[]byte(
				`{"streams":[` +
					`{"codec_type":"video","codec_name":"mjpeg","width":300,"height":300,"disposition":{"attached_pic":1}},` +
					`{"codec_type":"video","codec_name":"h264","width":1920,"height":1080}` +
					`],"format":{"format_name":"matroska","duration":"100.0"}}`,
			),
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.VideoCodec).To(Equal("h264"))
		Expect(info.Width).To(Equal(uint16(1920)))
		Expect(info.Height).To(Equal(uint16(1080)))
	})

	// Matroska does not carry the attached_pic disposition — cover art muxed
	// into an mkv as a stream arrives unflagged, so size is the only thing
	// separating it from the feature.
	It("picks the largest video stream when cover art is unflagged", func() {
		info, err := parseProbeOutput(
			[]byte(
				`{"streams":[` +
					`{"codec_type":"video","codec_name":"png","width":320,"height":240},` +
					`{"codec_type":"video","codec_name":"h264","width":1920,"height":800},` +
					`{"codec_type":"audio","codec_name":"aac","channels":6}` +
					`],"format":{"format_name":"matroska,webm","duration":"3.023000"}}`,
			),
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.VideoCodec).To(Equal("h264"))
		Expect(info.Width).To(Equal(uint16(1920)))
		Expect(info.Height).To(Equal(uint16(800)))
	})

	It("keeps the first of two equally sized video streams", func() {
		info, err := parseProbeOutput(
			[]byte(
				`{"streams":[` +
					`{"codec_type":"video","codec_name":"h264","width":1920,"height":1080},` +
					`{"codec_type":"video","codec_name":"hevc","width":1920,"height":1080}` +
					`],"format":{"format_name":"matroska","duration":"100.0"}}`,
			),
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.VideoCodec).To(Equal("h264"))
	})

	It("takes the duration of the stream it selected, not the first", func() {
		info, err := parseProbeOutput(
			[]byte(
				`{"streams":[` +
					`{"codec_type":"video","codec_name":"png","width":320,"height":240,"duration":"0.040000"},` +
					`{"codec_type":"video","codec_name":"h264","width":1920,"height":800,"duration":"5400.000000"}` +
					`],"format":{"format_name":"mpegts"}}`,
			),
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.DurationSec).To(Equal(uint32(5400)))
	})

	It("still selects a video stream that reports no dimensions", func() {
		info, err := parseProbeOutput(
			[]byte(
				`{"streams":[{"codec_type":"video","codec_name":"h264"}],` +
					`"format":{"format_name":"matroska","duration":"100.0"}}`,
			),
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.VideoCodec).To(Equal("h264"))
	})

	It("accepts a video stream that reports no codec name", func() {
		info, err := parseProbeOutput(
			[]byte(
				`{"streams":[{"codec_type":"video","width":1920,"height":1080}],` +
					`"format":{"format_name":"matroska","duration":"100.0"}}`,
			),
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.VideoCodec).To(BeEmpty())
		Expect(info.Width).To(Equal(uint16(1920)))
	})

	It("keeps a selected codec-less stream over smaller cover art", func() {
		info, err := parseProbeOutput(
			[]byte(
				`{"streams":[` +
					`{"codec_type":"video","width":1920,"height":1080},` +
					`{"codec_type":"video","codec_name":"mjpeg","width":300,"height":300}],` +
					`"format":{"format_name":"matroska","duration":"100.0"}}`,
			),
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.VideoCodec).To(BeEmpty())
		Expect(info.Width).To(Equal(uint16(1920)))
	})

	It("rejects a file whose only video streams are attached pics", func() {
		_, err := parseProbeOutput(
			[]byte(
				`{"streams":[{"codec_type":"video","codec_name":"mjpeg","width":300,"height":300,"disposition":{"attached_pic":1}}],"format":{"format_name":"matroska","duration":"100.0"}}`,
			),
		)
		Expect(err).To(MatchError(ErrNoVideoStream))
	})
})

var _ = Describe("parseProbeOutput streams", Label("unit", "ffmpeg"), func() {
	const multi = `{
	  "streams": [
	    {"codec_type":"video","codec_name":"hevc","width":1920,"height":1080},
	    {"codec_type":"audio","codec_name":"aac","channels":2,"tags":{"language":"jpn"}},
	    {"codec_type":"audio","codec_name":"aac","channels":6,"tags":{"language":"fre"}},
	    {"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"fre"}},
	    {"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng"},
	     "disposition":{"forced":1}}
	  ],
	  "format": {"format_name":"matroska","duration":"1440.0"}
	}`

	It("counts audio tracks and folds their languages", func() {
		info, err := parseProbeOutput([]byte(multi))
		Expect(err).NotTo(HaveOccurred())
		Expect(info.AudioTracks).To(Equal(uint8(2)))
		Expect(info.AudioLangs).To(Equal("fra,jpn"))
	})

	It(
		"excludes forced subtitles, which carry signs rather than the script",
		func() {
			info, err := parseProbeOutput([]byte(multi))
			Expect(err).NotTo(HaveOccurred())
			Expect(info.SubLangs).To(Equal("fra"))
		},
	)

	It("reports empty language lists for an untagged file", func() {
		info, err := parseProbeOutput([]byte(`{
		  "streams":[
		    {"codec_type":"video","codec_name":"h264","width":1280,"height":720},
		    {"codec_type":"audio","codec_name":"aac","channels":2}
		  ],
		  "format":{"format_name":"matroska","duration":"60.0"}}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(info.AudioTracks).To(Equal(uint8(1)))
		Expect(info.AudioLangs).To(BeEmpty())
		Expect(info.SubLangs).To(BeEmpty())
	})
})

var _ = Describe("parseProbeOutput HDR", Label("unit", "ffmpeg"), func() {
	It(
		"reports SDR with the stream's own video bitrate and ordered audio codecs",
		func() {
			info, err := parseProbeOutput([]byte(`{
		  "streams": [
		    {"codec_type":"video","codec_name":"h264","width":1920,"height":1080,"bit_rate":"9000000"},
		    {"codec_type":"audio","codec_name":"dts","channels":6},
		    {"codec_type":"audio","codec_name":"aac","channels":2}
		  ],
		  "format": {"format_name":"matroska","duration":"3600.0","bit_rate":"9500000"}
		}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(info.HDR).To(BeFalse())
			Expect(info.VideoBitrateBPS).To(Equal(uint32(9000000)))
			Expect(info.AudioCodecs).To(Equal([]string{"dts", "aac"}))
		},
	)

	It("detects PQ/DV HDR and falls back to the format bitrate", func() {
		info, err := parseProbeOutput([]byte(`{
		  "streams": [
		    {"codec_type":"video","codec_name":"hevc","width":3840,"height":2160,
		     "color_transfer":"smpte2084",
		     "side_data_list":[{"side_data_type":"DOVI configuration record"}]},
		    {"codec_type":"audio","codec_name":"truehd","channels":8},
		    {"codec_type":"audio","codec_name":"eac3","channels":6}
		  ],
		  "format": {"format_name":"matroska","duration":"7200.0","bit_rate":"45000000"}
		}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(info.HDR).To(BeTrue())
		Expect(info.VideoBitrateBPS).To(Equal(uint32(45000000)))
		Expect(info.AudioCodecs).To(Equal([]string{"truehd", "eac3"}))
	})
})
