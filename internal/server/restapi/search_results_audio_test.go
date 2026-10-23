package restapi

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
)

var _ = Describe("music and book search results", Label("unit", "server"), func() {
	raw := func(title string) indexer.SearchResult {
		return indexer.SearchResult{
			Title:    title,
			Download: "https://idx/dl",
			Seeders:  4,
		}
	}

	Describe("toMusicSearchResult", func() {
		It("carries the display label, the tier and an empty format list", func() {
			title := "Nirvana - Nevermind (1991) [FLAC 24bit-96kHz] 1080p x265"
			p := library.ParseMusicRelease(title)
			item := toMusicSearchResult(raw(title), p, 520, "")

			Expect(*item.Source).To(Equal("FLAC 24/96"))
			Expect(*item.AudioTier).To(Equal(MusicTier("hires")))
			Expect(item.Resolution).To(BeNil())
			Expect(item.Codec).To(BeNil())
			Expect(*item.MatchedFormats).To(BeEmpty())
			Expect(*item.Score).To(Equal(520))
			Expect(item.Rejected).To(BeNil())
		})

		It("flags a rejected release and keeps its reason", func() {
			p := library.ParseMusicRelease("Nirvana - Nevermind MP3 128")
			item := toMusicSearchResult(raw("Nirvana - Nevermind MP3 128"), p, -1,
				"tier not in the profile")

			Expect(*item.Rejected).To(BeTrue())
			Expect(*item.RejectReason).To(Equal("tier not in the profile"))
			Expect(*item.Score).To(BeZero())
			Expect(*item.AudioTier).To(Equal(MusicTier("low")))
		})

		It("omits tier and source for a name that states no quality", func() {
			p := library.ParseMusicRelease("Nirvana - Nevermind")
			item := toMusicSearchResult(raw("Nirvana - Nevermind"), p, -1,
				"quality is not stated in the release name")

			Expect(item.AudioTier).To(BeNil())
			Expect(item.Source).To(BeNil())
			Expect(*item.Rejected).To(BeTrue())
		})
	})

	Describe("toBookSearchResult", func() {
		It("carries the container, the slot and a stated audiobook rate", func() {
			title := "Elantris Unabridged M4B 64kbps"
			item := toBookSearchResult(
				raw(title),
				library.ParseBookRelease(title),
				464,
				"",
			)

			Expect(*item.Source).To(Equal("M4B"))
			Expect(*item.Slot).To(Equal(BookFormatAudiobook))
			Expect(*item.BitrateKbps).To(Equal(uint32(64)))
		})

		It("marks an ebook and leaves the rate off", func() {
			title := "Elantris EPUB"
			item := toBookSearchResult(
				raw(title),
				library.ParseBookRelease(title),
				600,
				"",
			)

			Expect(*item.Source).To(Equal("EPUB"))
			Expect(*item.Slot).To(Equal(BookFormatEbook))
			Expect(item.BitrateKbps).To(BeNil())
		})
	})
})
