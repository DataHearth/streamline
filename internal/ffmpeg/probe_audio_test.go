package ffmpeg

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("parseAudioProbeOutput", Label("unit", "ffmpeg"), func() {
	It("reads depth, rate and codec of a hi-res flac, ignoring cover art", func() {
		info, err := parseAudioProbeOutput([]byte(`{
		  "streams": [
		    {"codec_type":"audio","codec_name":"flac","bits_per_raw_sample":"24","bits_per_sample":0,"sample_rate":"96000"},
		    {"codec_type":"video","codec_name":"mjpeg"}
		  ],
		  "format": {"format_name":"flac","duration":"245.3","bit_rate":"3500000"}
		}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Codec).To(Equal("flac"))
		Expect(info.BitDepth).To(Equal(uint8(24)))
		Expect(info.SampleRateHz).To(Equal(uint32(96000)))
		Expect(info.BitrateKbps).To(Equal(uint32(3500)))
		Expect(info.DurationSec).To(Equal(uint32(245)))
	})

	It("takes the container's rate for an mp3, which has no stream rate", func() {
		info, err := parseAudioProbeOutput([]byte(`{
		  "streams": [{"codec_type":"audio","codec_name":"mp3","sample_rate":"44100"}],
		  "format": {"format_name":"mp3","duration":"200.0","bit_rate":"320000"}
		}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(info.BitrateKbps).To(Equal(uint32(320)))
		Expect(info.BitDepth).To(BeZero())
	})

	It("prefers the stream's own rate", func() {
		info, err := parseAudioProbeOutput([]byte(`{
		  "streams": [{"codec_type":"audio","codec_name":"aac","bit_rate":"256000"}],
		  "format": {"format_name":"mov","duration":"100.0","bit_rate":"300000"}
		}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(info.BitrateKbps).To(Equal(uint32(256)))
	})

	It("falls back to bits_per_sample when the raw sample depth is absent", func() {
		info, err := parseAudioProbeOutput([]byte(`{
		  "streams": [{"codec_type":"audio","codec_name":"alac","bits_per_sample":24}],
		  "format": {"format_name":"mov","duration":"100.0"}
		}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(info.BitDepth).To(Equal(uint8(24)))
	})

	It("rejects output with no audio stream", func() {
		_, err := parseAudioProbeOutput([]byte(
			`{"streams":[{"codec_type":"video","codec_name":"h264"}],"format":{"duration":"10"}}`,
		))
		Expect(err).To(MatchError(ErrNoAudioStream))
	})

	It("rejects a file with no duration", func() {
		_, err := parseAudioProbeOutput([]byte(
			`{"streams":[{"codec_type":"audio","codec_name":"mp3"}],"format":{}}`,
		))
		Expect(err).To(MatchError(ErrZeroDuration))
	})

	It("rejects output that is not JSON", func() {
		_, err := parseAudioProbeOutput([]byte(`nope`))
		Expect(err).To(MatchError(ErrUnreadable))
	})
})
