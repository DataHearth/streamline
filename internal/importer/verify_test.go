package importer

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/ffmpeg"
	"github.com/datahearth/streamline/internal/library"
)

var _ = Describe("verifyFile", Label("unit", "importer"), func() {
	info := func(w uint16, durSec uint32, codec string) *ffmpeg.Info {
		return &ffmpeg.Info{
			Width:       w,
			Height:      800,
			DurationSec: durSec,
			VideoCodec:  codec,
			Container:   "matroska",
		}
	}
	parsed1080 := library.ParseResult{Resolution: "1080p"}

	It("passes a clean file", func() {
		Expect(
			verifyFile(
				"f.mkv",
				parsed1080,
				info(1920, 6000, "h264"),
				nil,
				100,
				nil,
				0.5,
			),
		).
			To(BeEmpty())
	})

	It("holds a 720p file sold as 1080p", func() {
		rs := verifyFile(
			"f.mkv",
			parsed1080,
			info(1280, 6000, "h264"),
			nil,
			100,
			nil,
			0.5,
		)
		Expect(rs).To(HaveLen(1))
		Expect(rs[0].File).To(Equal("f.mkv"))
		Expect(rs[0].Check).To(Equal("resolution"))
		Expect(rs[0].Expected).To(Equal("1080p"))
		Expect(rs[0].Actual).To(Equal("720p"))
	})

	It("never holds a higher-than-claimed resolution", func() {
		Expect(verifyFile(
			"f.mkv", library.ParseResult{Resolution: "720p"},
			info(3840, 6000, "h264"), nil, 100, nil, 0.5,
		)).To(BeEmpty())
	})

	It("reads a 4K claim as 2160p and an odd-cased claim as its bucket", func() {
		Expect(verifyFile(
			"f.mkv", library.ParseResult{Resolution: "4K"},
			info(1920, 6000, "h264"), nil, 100, nil, 0.5,
		)).To(HaveLen(1))
		Expect(verifyFile(
			"f.mkv", library.ParseResult{Resolution: "1080P"},
			info(1280, 6000, "h264"), nil, 100, nil, 0.5,
		)).To(HaveLen(1))
	})

	It("skips the resolution check when nothing was claimed", func() {
		Expect(verifyFile(
			"f.mkv",
			library.ParseResult{},
			info(640, 6000, "h264"),
			nil,
			100,
			nil,
			0.5,
		)).To(BeEmpty())
	})

	It("holds a truncated file", func() {
		rs := verifyFile(
			"f.mkv",
			parsed1080,
			info(1920, 1200, "h264"),
			nil,
			100,
			nil,
			0.5,
		)
		Expect(rs).To(HaveLen(1))
		Expect(rs[0].Check).To(Equal("duration"))
		Expect(rs[0].Expected).To(Equal("≥ 50m"))
		Expect(rs[0].Actual).To(Equal("20m"))
	})

	It("skips the duration check when runtime is unknown", func() {
		Expect(
			verifyFile(
				"f.mkv",
				parsed1080,
				info(1920, 60, "h264"),
				nil,
				0,
				nil,
				0.5,
			),
		).
			To(BeEmpty())
	})

	It("holds a disallowed codec, case-insensitively", func() {
		rs := verifyFile(
			"f.mkv", parsed1080, info(1920, 6000, "XviD"), nil, 100,
			[]string{"hevc", "h264"}, 0.5,
		)
		Expect(rs).To(HaveLen(1))
		Expect(rs[0].Check).To(Equal("codec"))
		Expect(rs[0].Expected).To(Equal("hevc/h264"))
		Expect(rs[0].Actual).To(Equal("XviD"))
	})

	It("allows a codec differing only in case", func() {
		Expect(verifyFile(
			"f.mkv",
			parsed1080,
			info(1920, 6000, "HEVC"),
			nil,
			100,
			[]string{"hevc"},
			0.5,
		)).To(BeEmpty())
	})

	It("reports corrupt and nothing else on probe error", func() {
		rs := verifyFile(
			"f.mkv",
			parsed1080,
			nil,
			ffmpeg.ErrNoVideoStream,
			100,
			[]string{"hevc"},
			0.5,
		)
		Expect(rs).To(HaveLen(1))
		Expect(rs[0].Check).To(Equal("corrupt"))
		Expect(rs[0].Actual).To(Equal(ffmpeg.ErrNoVideoStream.Error()))
	})

	It("verifies nothing when the file was not probed at all", func() {
		Expect(
			verifyFile("f.mkv", parsed1080, nil, nil, 100, []string{"hevc"}, 0.5),
		).
			To(BeEmpty())
	})

	It("accumulates independent failures", func() {
		rs := verifyFile(
			"f.mkv",
			parsed1080,
			info(1280, 1200, "XviD"),
			nil,
			100,
			[]string{"hevc"},
			0.5,
		)
		Expect(rs).To(HaveLen(3))
	})
})
