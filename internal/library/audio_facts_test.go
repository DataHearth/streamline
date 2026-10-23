package library

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/ffmpeg"
	"github.com/datahearth/streamline/internal/quality"
)

var _ = Describe("AudioFactsFromProbe", Label("unit", "library"), func() {
	DescribeTable(
		"reads the tier off a probe",
		func(info ffmpeg.AudioInfo, want string) {
			t, ok := quality.AudioTierOf(AudioFactsFromProbe(&info))
			Expect(ok).To(BeTrue())
			Expect(t.String()).To(Equal(want))
		},
		Entry(
			"16-bit flac",
			ffmpeg.AudioInfo{Codec: "flac", BitDepth: 16},
			"lossless",
		),
		Entry("24-bit flac", ffmpeg.AudioInfo{Codec: "flac", BitDepth: 24}, "hires"),
		Entry("alac 24-bit", ffmpeg.AudioInfo{Codec: "alac", BitDepth: 24}, "hires"),
		Entry(
			"pcm wav",
			ffmpeg.AudioInfo{Codec: "pcm_s16le", BitDepth: 16},
			"lossless",
		),
		Entry("cbr 320", ffmpeg.AudioInfo{Codec: "mp3", BitrateKbps: 320}, "high"),
		Entry("cbr 256 is not mistaken for V0",
			ffmpeg.AudioInfo{Codec: "mp3", BitrateKbps: 256}, "standard"),
		Entry(
			"a V0 average",
			ffmpeg.AudioInfo{Codec: "mp3", BitrateKbps: 245},
			"high",
		),
		Entry(
			"a V2 average",
			ffmpeg.AudioInfo{Codec: "mp3", BitrateKbps: 190},
			"standard",
		),
		Entry("cbr 128", ffmpeg.AudioInfo{Codec: "mp3", BitrateKbps: 128}, "low"),
		Entry("aac 256", ffmpeg.AudioInfo{Codec: "aac", BitrateKbps: 256}, "high"),
	)
})

var _ = Describe("MusicFormatLabel", Label("unit", "library"), func() {
	DescribeTable(
		"labels a file the way an import row prints it",
		func(ext string, info *ffmpeg.AudioInfo, want string) {
			Expect(MusicFormatLabel(ext, info)).To(Equal(want))
		},
		Entry("hi-res flac", ".flac",
			&ffmpeg.AudioInfo{Codec: "flac", BitDepth: 24, SampleRateHz: 96000},
			"FLAC 24/96"),
		Entry("cd flac", ".flac",
			&ffmpeg.AudioInfo{Codec: "flac", BitDepth: 16, SampleRateHz: 44100},
			"FLAC 16/44.1"),
		Entry("flac without a measured depth", ".flac",
			&ffmpeg.AudioInfo{Codec: "flac"}, "FLAC"),
		Entry("cbr mp3", ".mp3",
			&ffmpeg.AudioInfo{Codec: "mp3", BitrateKbps: 320}, "MP3 320"),
		Entry("vbr mp3", ".mp3",
			&ffmpeg.AudioInfo{Codec: "mp3", BitrateKbps: 245}, "MP3 V0"),
		Entry("an unmeasured file", ".flac", nil, "FLAC"),
		Entry("a probe that named no codec", ".mp3", &ffmpeg.AudioInfo{}, "MP3"),
	)
})
