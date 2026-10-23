package bulkimport

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/ffmpeg"
	mockffmpeg "github.com/datahearth/streamline/internal/ffmpeg/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("adopted file quality", Label("unit", "bulkimport"), func() {
	DescribeTable("adoptedTier reads only what the extension can say unprobed",
		func(path, want string) {
			Expect((&Service{}).adoptedTier(context.Background(), path)).
				To(Equal(want))
		},
		Entry("flac is lossless, never hi-res", "/m/a/01.flac", "lossless"),
		Entry("upper-case extension", "/m/a/01.FLAC", "lossless"),
		Entry("mp3 states no tier", "/m/a/01.mp3", ""),
		Entry("m4a could be alac or aac", "/m/a/01.m4a", ""),
	)

	Describe("adoptedTier with ffprobe", func() {
		var (
			probe *mockffmpeg.MockProber
			svc   *Service
		)

		BeforeEach(func() {
			configtest.Setup(map[string]any{
				"ffmpeg": map[string]any{"enabled": true},
			})
			probe = mockffmpeg.NewMockProber(GinkgoT())
			probe.EXPECT().Available().Return(true)
			svc = NewService(
				nil, nil, nil, nil, nil, nil, nil, "", "", nil, nil, nil, nil,
				WithProber(probe),
			)
		})

		It("measures a lossy file the extension cannot classify", func() {
			probe.EXPECT().ProbeAudio(context.Background(), "/m/a/01.mp3").
				Return(&ffmpeg.AudioInfo{Codec: "mp3", BitrateKbps: 320}, nil)
			Expect(svc.adoptedTier(context.Background(), "/m/a/01.mp3")).
				To(Equal("high"))
		})

		It("reports a 24-bit flac as hi-res", func() {
			probe.EXPECT().ProbeAudio(context.Background(), "/m/a/01.flac").
				Return(&ffmpeg.AudioInfo{Codec: "flac", BitDepth: 24}, nil)
			Expect(svc.adoptedTier(context.Background(), "/m/a/01.flac")).
				To(Equal("hires"))
		})

		It("falls back to the extension when the probe fails", func() {
			probe.EXPECT().ProbeAudio(context.Background(), "/m/a/01.flac").
				Return(nil, errors.New("boom"))
			Expect(svc.adoptedTier(context.Background(), "/m/a/01.flac")).
				To(Equal("lossless"))
		})
	})

	DescribeTable("adoptedBookQuality is the upper-case profile format",
		func(ext, want string) {
			Expect(adoptedBookQuality(ext)).To(Equal(want))
		},
		Entry("epub", "epub", "EPUB"),
		Entry("cbz", "cbz", "CBZ"),
		Entry("m4b", "m4b", "M4B"),
		Entry("flac audiobook", "flac", "FLAC"),
		Entry("off the ladder", "ogg", ""),
		Entry("none", "", ""),
	)
})
