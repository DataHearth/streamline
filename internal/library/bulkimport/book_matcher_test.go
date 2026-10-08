package bulkimport

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	entimportscanbook "github.com/datahearth/streamline/ent/importscanbook"
	"github.com/datahearth/streamline/internal/metadata"
)

var _ = Describe("ClassifyBook", Label("unit", "bulkimport"), func() {
	elantris := metadata.BookSearchResult{
		HardcoverID: 1, Title: "Elantris", Author: "Brandon Sanderson", Year: 2005,
	}

	DescribeTable("classification",
		func(
			title, author string,
			hits []metadata.BookSearchResult,
			indexed map[uint32]uint32,
			wantKind entimportscanbook.Classification,
			wantBook uint32,
			wantExisting uint32,
		) {
			c := ClassifyBook(title, author, hits, indexed)
			Expect(c.Kind).To(Equal(wantKind))
			Expect(c.BookHardcoverID).To(Equal(wantBook))
			Expect(c.ExistingBookID).To(Equal(wantExisting))
		},
		Entry("no hits is unmatched",
			"Elantris", "Brandon Sanderson", nil, nil,
			entimportscanbook.ClassificationUnmatched, uint32(0), uint32(0)),
		Entry("one title+author hit is confirmed",
			"Elantris", "Brandon Sanderson",
			[]metadata.BookSearchResult{
				elantris,
				{
					HardcoverID: 2,
					Title:       "Elantris Companion",
					Author:      "Someone Else",
				},
			},
			nil,
			entimportscanbook.ClassificationConfirmed, uint32(1), uint32(0)),
		Entry("an inverted author name still matches",
			"Elantris", "Sanderson, Brandon",
			[]metadata.BookSearchResult{elantris}, nil,
			entimportscanbook.ClassificationConfirmed, uint32(1), uint32(0)),
		Entry("a title match by another author is ambiguous",
			"Elantris", "Nobody",
			[]metadata.BookSearchResult{elantris}, nil,
			entimportscanbook.ClassificationAmbiguous, uint32(0), uint32(0)),
		Entry("an unknown author never confirms",
			"Elantris", "",
			[]metadata.BookSearchResult{elantris}, nil,
			entimportscanbook.ClassificationAmbiguous, uint32(0), uint32(0)),
		Entry("a tracked hit is existing",
			"Elantris", "Brandon Sanderson",
			[]metadata.BookSearchResult{elantris},
			map[uint32]uint32{1: 42},
			entimportscanbook.ClassificationExisting, uint32(1), uint32(42)),
	)

	It("caps ambiguous candidates at pickerCandidateLimit", func() {
		hits := make([]metadata.BookSearchResult, 0, 8)
		for i := range 8 {
			hits = append(hits, metadata.BookSearchResult{
				HardcoverID: uint32(
					i + 10,
				),
				Title:  "Elantris",
				Author: "Brandon Sanderson",
			})
		}
		c := ClassifyBook("Elantris", "Brandon Sanderson", hits, nil)
		Expect(c.Kind).To(Equal(entimportscanbook.ClassificationAmbiguous))
		Expect(c.Candidates).To(HaveLen(pickerCandidateLimit))
	})

	It("ranks the matching author's candidate first", func() {
		c := ClassifyBook(
			"Elantris",
			"Brandon Sanderson",
			[]metadata.BookSearchResult{
				{HardcoverID: 7, Title: "Elantris", Author: "Imposter"},
				{HardcoverID: 8, Title: "Elantris", Author: "Brandon Sanderson"},
				{HardcoverID: 9, Title: "Elantris", Author: "Brandon Sanderson"},
			},
			nil,
		)
		Expect(c.Candidates[0].BookHardcoverID).To(Equal(uint32(8)))
	})
})

var _ = Describe("guessBookTitleAuthor", Label("unit", "bulkimport"), func() {
	DescribeTable("splits names",
		func(path string, audiobook bool, wantTitle, wantAuthor string) {
			title, author := guessBookTitleAuthor(path, audiobook)
			Expect(title).To(Equal(wantTitle))
			Expect(author).To(Equal(wantAuthor))
		},
		Entry(
			"Calibre stem, title first",
			"/b/Brandon Sanderson/Elantris (1)/Elantris - Brandon Sanderson.epub",
			false,
			"Elantris",
			"Brandon Sanderson",
		),
		Entry("unsplittable stem falls back to the folder as author",
			"/b/loose/Some Unknown Book.epub", false,
			"Some Unknown Book", "loose"),
		Entry("brackets are stripped",
			"/b/x/Warbreaker [retail].epub", false,
			"Warbreaker", "x"),
		Entry("audiobook folder is author/title",
			"/b/Brandon Sanderson/Elantris", true,
			"Elantris", "Brandon Sanderson"),
	)
})
