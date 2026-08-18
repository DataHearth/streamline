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
})
