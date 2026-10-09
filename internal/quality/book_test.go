package quality_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/quality"
)

var _ = Describe("book profiles", Label("unit", "quality"), func() {
	ebook := quality.EbookProfile{
		Formats:        []string{"EPUB", "AZW3", "CBZ"},
		Preferred:      "EPUB",
		UpgradeAllowed: true,
	}
	audiobook := quality.AudiobookProfile{
		Formats:        []string{"M4B", "MP3"},
		Preferred:      "M4B",
		MinBitrate:     64,
		UpgradeAllowed: true,
	}

	Describe("ScoreEbook", func() {
		It("ranks over the ladder and rejects an unticked format", func() {
			Expect(quality.ScoreEbook("EPUB", ebook)).
				To(BeNumerically(">", quality.ScoreEbook("AZW3", ebook)))
			Expect(quality.ScoreEbook("AZW3", ebook)).
				To(BeNumerically(">", quality.ScoreEbook("CBZ", ebook)))
			Expect(quality.ScoreEbook("PDF", ebook)).To(Equal(-1))
			Expect(quality.ScoreEbook("", ebook)).To(Equal(-1))
		})
	})

	Describe("ScoreAudiobook", func() {
		It(
			"rejects a stated rate under the floor and accepts an unstated one",
			func() {
				Expect(quality.ScoreAudiobook("M4B", 32, audiobook)).To(Equal(-1))
				Expect(
					quality.ScoreAudiobook("M4B", 0, audiobook),
				).To(BeNumerically(">", 0))
				Expect(quality.ScoreAudiobook("FLAC", 0, audiobook)).To(Equal(-1))
			},
		)

		It("adds a rate bonus that never outweighs a format step", func() {
			Expect(quality.ScoreAudiobook("M4B", 64, audiobook)).
				To(BeNumerically(">", quality.ScoreAudiobook("MP3", 999, audiobook)))
			Expect(quality.ScoreAudiobook("MP3", 128, audiobook)).
				To(BeNumerically(">", quality.ScoreAudiobook("MP3", 64, audiobook)))
		})
	})

	Describe("EbookReplaces", func() {
		DescribeTable("decides",
			func(have, incoming string, want bool) {
				Expect(quality.EbookReplaces(ebook, have, incoming)).To(Equal(want))
			},
			Entry("better ticked format below the ceiling", "CBZ", "AZW3", true),
			Entry("held at the ceiling", "EPUB", "EPUB", false),
			Entry("incoming not ticked", "CBZ", "PDF", false),
			Entry("held format unknown", "", "EPUB", false),
		)

		It("never replaces when upgrades are off", func() {
			p := ebook
			p.UpgradeAllowed = false
			Expect(quality.EbookReplaces(p, "CBZ", "EPUB")).To(BeFalse())
		})
	})

	Describe("AudiobookReplaces", func() {
		It("upgrades a better format below the ceiling", func() {
			Expect(
				quality.AudiobookReplaces(audiobook, "MP3", 128, "M4B", 0),
			).To(BeTrue())
		})

		It(
			"replaces a held folder measured under the floor with an acceptable release",
			func() {
				Expect(
					quality.AudiobookReplaces(audiobook, "M4B", 32, "M4B", 128),
				).To(BeTrue())
			},
		)

		It("does not replace under the floor with an unacceptable release", func() {
			Expect(
				quality.AudiobookReplaces(audiobook, "M4B", 32, "FLAC", 0),
			).To(BeFalse())
		})

		It("leaves a held folder at the ceiling alone", func() {
			Expect(
				quality.AudiobookReplaces(audiobook, "M4B", 128, "M4B", 256),
			).To(BeFalse())
		})
	})
})
