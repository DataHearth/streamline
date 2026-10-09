package pick

import (
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPick(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Edition Pick Suite")
}

func ed(id uint32, lang, format, title string, pop uint32, original bool) Edition {
	return Edition{
		ID: id, Language: lang, Format: format, Title: title,
		Popularity: pop, Original: original,
	}
}

var _ = Describe("Slot", Label("unit", "books"), func() {
	eds := []Edition{
		ed(1, "en", "ebook", "Elantris", 5, true),
		ed(2, "fr", "ebook", "Elantris FR", 9, false),
		ed(3, "fr", "ebook", "Elantris FR 2", 20, false),
		ed(4, "en", "audiobook", "Elantris (audio)", 1, true),
		ed(5, "de", "audiobook", "Elantris DE", 7, false),
	}

	It("takes the preferred language's most popular edition of the format", func() {
		got, ok := Slot(eds, "ebook", "fr")
		Expect(ok).To(BeTrue())
		Expect(got.ID).To(Equal(uint32(3)))
	})

	It("falls back to the original edition, then to the most popular", func() {
		got, ok := Slot(eds, "ebook", "es")
		Expect(ok).To(BeTrue())
		Expect(got.ID).To(Equal(uint32(1)))

		noOriginal := []Edition{
			ed(1, "fr", "ebook", "A", 1, false),
			ed(2, "de", "ebook", "B", 9, false),
		}
		got, ok = Slot(noOriginal, "ebook", "es")
		Expect(ok).To(BeTrue())
		Expect(got.ID).To(Equal(uint32(2)))
	})

	It("keeps each format to its own editions", func() {
		got, ok := Slot(eds, "audiobook", "fr")
		Expect(ok).To(BeTrue())
		Expect(got.ID).To(Equal(uint32(4)))
		Expect(got.Format).To(Equal("audiobook"))
	})

	It("reports a book with no edition of the format", func() {
		_, ok := Slot(eds[:3], "audiobook", "en")
		Expect(ok).To(BeFalse())
		_, ok = Slot(nil, "ebook", "en")
		Expect(ok).To(BeFalse())
	})

	It("breaks a popularity tie on the lower id", func() {
		tied := []Edition{
			ed(7, "en", "ebook", "B", 5, false),
			ed(3, "en", "ebook", "A", 5, false),
		}
		got, _ := Slot(tied, "ebook", "en")
		Expect(got.ID).To(Equal(uint32(3)))
	})
})

var _ = Describe("SlotIn", Label("unit", "books"), func() {
	It("finds the edition of a language and publisher only", func() {
		eds := []Edition{
			{
				ID:         1,
				Language:   "fr",
				Publisher:  "Glenat",
				Format:     "ebook",
				Popularity: 9,
			},
			{
				ID:         2,
				Language:   "fr",
				Publisher:  "Kana",
				Format:     "ebook",
				Popularity: 1,
			},
			{
				ID:         3,
				Language:   "fr",
				Publisher:  "Kana",
				Format:     "audiobook",
				Popularity: 5,
			},
		}
		got, ok := SlotIn(eds, "ebook", "fr", "Kana")
		Expect(ok).To(BeTrue())
		Expect(got.ID).To(Equal(uint32(2)))
		_, ok = SlotIn(eds, "ebook", "fr", "Nobody")
		Expect(ok).To(BeFalse())
	})
})

var _ = Describe("Titles", Label("unit", "books"), func() {
	eds := []Edition{
		ed(1, "ja", "ebook", "ワンピース", 5, true),
		ed(2, "en", "ebook", "One Piece", 9, false),
		ed(3, "fr", "audiobook", "One Piece (livre audio)", 2, false),
	}

	It(
		"is the preferred language's edition title, the original shown beside it",
		func() {
			title, original := Titles(eds, "en", "fallback")
			Expect(title).To(Equal("One Piece"))
			Expect(original).To(Equal("ワンピース"))
		},
	)

	It("reads an audiobook-only language after the ebooks", func() {
		title, _ := Titles(eds, "fr", "fallback")
		Expect(title).To(Equal("One Piece (livre audio)"))
	})

	It("falls back to the original edition, then to the given title", func() {
		title, original := Titles(eds, "es", "fallback")
		Expect(title).To(Equal("ワンピース"))
		Expect(original).To(BeEmpty())

		title, original = Titles(
			[]Edition{ed(1, "en", "ebook", "X", 1, false)},
			"es",
			"Hardcover Title",
		)
		Expect(title).To(Equal("Hardcover Title"))
		Expect(original).To(BeEmpty())
	})

	It("has no original title when it equals the displayed one", func() {
		title, original := Titles(eds, "ja", "fallback")
		Expect(title).To(Equal("ワンピース"))
		Expect(original).To(BeEmpty())
	})
})

var _ = Describe("DisplayAuthor", Label("unit", "books"), func() {
	DescribeTable(
		"the makers of a title",
		func(credits []Credit, want string) {
			Expect(DisplayAuthor(credits)).To(Equal(want))
		},
		Entry("authors", []Credit{{"A", "author"}, {"B", "author"}}, "A & B"),
		Entry(
			"up to three",
			[]Credit{{"A", "author"}, {"B", "author"}, {"C", "author"}},
			"A & B & C",
		),
		Entry(
			"then et al.",
			[]Credit{
				{"A", "author"},
				{"B", "author"},
				{"C", "author"},
				{"D", "author"},
			},
			"A & B & C et al.",
		),
		Entry(
			"writers when there are no authors",
			[]Credit{{"W", "writer"}, {"I", "artist"}},
			"W",
		),
		Entry(
			"artists when there is nothing else",
			[]Credit{{"I", "artist"}, {"C", "colorist"}},
			"I",
		),
		Entry(
			"authors win over writers",
			[]Credit{{"W", "writer"}, {"A", "author"}},
			"A",
		),
		Entry("a name once", []Credit{{"A", "author"}, {"A", "author"}}, "A"),
		Entry("nobody", []Credit{{"C", "colorist"}}, ""),
		Entry("no credits", nil, ""),
	)
})

var _ = Describe("MonitorsVolume", Label("unit", "books"), func() {
	since := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	after, before := since.Add(time.Hour), since.Add(-time.Hour)

	DescribeTable("the series policy",
		func(policy string, release *time.Time, want bool) {
			Expect(MonitorsVolume(policy, release, since)).To(Equal(want))
		},
		Entry("all", "all", &before, true),
		Entry("all with no date", "all", nil, true),
		Entry("future, later", "future", &after, true),
		Entry("future, earlier", "future", &before, false),
		Entry("future, unknown date", "future", nil, false),
		Entry("none", "none", &after, false),
		Entry("unknown", "sometimes", &after, false),
	)
})
