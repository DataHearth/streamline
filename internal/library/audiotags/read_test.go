package audiotags

import (
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

	It("returns zero values, not an error, for untagged files", func() {
		info, err := Read("testdata/untagged.mp3")
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Album).To(BeEmpty())
	})
})
