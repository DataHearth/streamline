package bulkimport

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	entimportscanfile "github.com/datahearth/streamline/ent/importscanfile"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/metadata"
)

var _ = Describe("Classify", Label("unit", "bulkimport"), func() {
	parsed := func(title string, year uint16) library.ParseResult {
		return library.ParseResult{Title: title, Year: year}
	}
	hit := func(id uint32, title string, year uint16) metadata.MovieResult {
		return metadata.MovieResult{TMDBID: id, Title: title, Year: year}
	}
	noExisting := map[uint32]uint32{}

	It("0 hits → unmatched", func() {
		c := Classify(parsed("Foo", 2020), nil, noExisting)
		Expect(c.Kind).To(Equal(entimportscanfile.ClassificationUnmatched))
		Expect(c.Candidates).To(BeEmpty())
	})

	It("year+title exact match → confirmed", func() {
		c := Classify(
			parsed("The Matrix", 1999),
			[]metadata.MovieResult{hit(603, "The Matrix", 1999)},
			noExisting,
		)
		Expect(c.Kind).To(Equal(entimportscanfile.ClassificationConfirmed))
		Expect(c.TMDBID).To(Equal(uint32(603)))
		Expect(c.Candidates).To(HaveLen(1))
	})

	It("year mismatch on top hit → ambiguous", func() {
		c := Classify(parsed("It", 2017), []metadata.MovieResult{
			hit(346364, "It", 2019),
			hit(346365, "It", 2017),
		}, noExisting)
		Expect(c.Kind).To(Equal(entimportscanfile.ClassificationAmbiguous))
		Expect(c.Candidates).To(HaveLen(2))
	})

	It("title mismatch on top hit → ambiguous", func() {
		c := Classify(
			parsed("Foo", 2020),
			[]metadata.MovieResult{hit(1, "Bar", 2020)},
			noExisting,
		)
		Expect(c.Kind).To(Equal(entimportscanfile.ClassificationAmbiguous))
	})

	It("missing parsed.year → never confirmed", func() {
		c := Classify(
			parsed("The Matrix", 0),
			[]metadata.MovieResult{hit(603, "The Matrix", 1999)},
			noExisting,
		)
		Expect(c.Kind).To(Equal(entimportscanfile.ClassificationAmbiguous))
	})

	It("existing-row collision wins regardless of confidence", func() {
		c := Classify(parsed("The Matrix", 1999),
			[]metadata.MovieResult{hit(603, "The Matrix", 1999)},
			map[uint32]uint32{603: 42},
		)
		Expect(c.Kind).To(Equal(entimportscanfile.ClassificationExisting))
		Expect(c.TMDBID).To(Equal(uint32(603)))
		Expect(c.ExistingMovieID).To(Equal(uint32(42)))
	})

	It("caps candidates at pickerCandidateLimit", func() {
		var hits []metadata.MovieResult
		for i := uint32(1); i <= 10; i++ {
			hits = append(hits, hit(i, "X", 2000))
		}
		c := Classify(parsed("X", 1999), hits, noExisting)
		Expect(c.Candidates).To(HaveLen(pickerCandidateLimit))
	})
})
