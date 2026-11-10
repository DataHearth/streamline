package audiotags

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Read", Label("unit", "audiotags"), func() {
	It("reads ID3 tags from an MP3", func() {
		info, err := Read("testdata/tagged.mp3")
		Expect(err).NotTo(HaveOccurred())
		Expect(info.AlbumArtist).To(Equal("Nirvana"))
		Expect(info.Album).To(Equal("Nevermind"))
		Expect(info.Title).To(Equal("Smells Like Teen Spirit"))
		Expect(info.Track).To(Equal(uint16(1)))
		Expect(info.Disc).To(Equal(uint8(1)))
		Expect(info.Format).To(Equal("mp3"))
	})

	It("reads vorbis comments from a FLAC", func() {
		info, err := Read("testdata/tagged.flac")
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Album).To(Equal("Nevermind"))
		Expect(info.Format).To(Equal("flac"))
	})

	DescribeTable("reads the other formats TagLib handles",
		func(fixture, format string) {
			info, err := Read(fixture)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.AlbumArtist).To(Equal("Nirvana"))
			Expect(info.Album).To(Equal("Nevermind"))
			Expect(info.Title).To(Equal("Smells Like Teen Spirit"))
			Expect(info.Track).To(Equal(uint16(1)))
			Expect(info.Year).To(Equal(uint16(1991)))
			Expect(info.Format).To(Equal(format))
		},
		Entry("M4A", "testdata/tagged.m4a", "m4a"),
		Entry("Ogg Vorbis", "testdata/tagged.ogg", "ogg"),
		Entry("Opus", "testdata/tagged.opus", "opus"),
	)

	It("errors when the file cannot be opened", func() {
		_, err := Read("testdata/missing.mp3")
		Expect(err).To(MatchError(os.ErrNotExist))
	})

	It("returns zero values, not an error, for untagged files", func() {
		info, err := Read("testdata/untagged.mp3")
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Album).To(BeEmpty())
	})
})

var _ = Describe("Picture", Label("unit", "audiotags"), func() {
	It("returns the embedded image and its MIME type", func() {
		data, mime, err := Picture("testdata/withpicture.mp3")
		Expect(err).NotTo(HaveOccurred())
		Expect(mime).To(Equal("image/jpeg"))
		Expect(data).NotTo(BeEmpty())
		Expect(data[:2]).To(Equal([]byte{0xff, 0xd8}))
	})

	It("returns nil data, not an error, for files without a picture", func() {
		for _, f := range []string{
			"testdata/tagged.mp3", "testdata/tagged.flac", "testdata/untagged.mp3",
		} {
			data, mime, err := Picture(f)
			Expect(err).NotTo(HaveOccurred(), f)
			Expect(data).To(BeNil(), f)
			Expect(mime).To(BeEmpty(), f)
		}
	})

	It("errors when the file cannot be opened", func() {
		_, _, err := Picture("testdata/missing.mp3")
		Expect(err).To(MatchError(os.ErrNotExist))
	})
})

var _ = Describe("IsAudioFile", Label("unit", "audiotags"), func() {
	DescribeTable("takes only a visible file with an audio extension for a track",
		func(path string, want bool) {
			Expect(IsAudioFile(path)).To(Equal(want))
		},
		Entry("a track", "/m/A/01 Song.FLAC", true),
		Entry("a cover", "/m/A/cover.jpg", false),
		Entry("a write's leftover copy", "/m/A/.streamline-tags-123.flac", false),
		Entry("a macOS resource fork", "/m/A/._01 Song.flac", false),
	)
})
