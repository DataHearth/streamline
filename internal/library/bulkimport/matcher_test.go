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
	const anyPath = "/srv/Films/Foo/Foo.mkv"

	It("0 hits → unmatched", func() {
		c := Classify(anyPath, parsed("Foo", 2020), nil, noExisting)
		Expect(c.Kind).To(Equal(entimportscanfile.ClassificationUnmatched))
		Expect(c.Candidates).To(BeEmpty())
	})

	It("year+title exact match → confirmed", func() {
		c := Classify(
			anyPath,
			parsed("The Matrix", 1999),
			[]metadata.MovieResult{hit(603, "The Matrix", 1999)},
			noExisting,
		)
		Expect(c.Kind).To(Equal(entimportscanfile.ClassificationConfirmed))
		Expect(c.TMDBID).To(Equal(uint32(603)))
		Expect(c.Candidates).To(HaveLen(1))
	})

	It("picks the year-matching hit even when it is not TMDB's top result", func() {
		c := Classify(anyPath, parsed("It", 2017), []metadata.MovieResult{
			hit(346364, "It", 2019),
			hit(346365, "It", 2017),
		}, noExisting)
		Expect(c.Kind).To(Equal(entimportscanfile.ClassificationConfirmed))
		Expect(c.TMDBID).To(Equal(uint32(346365)))
	})

	It("stays ambiguous when several same-titled hits share the year", func() {
		c := Classify(anyPath, parsed("It", 2017), []metadata.MovieResult{
			hit(1, "It", 2017),
			hit(2, "It", 2017),
		}, noExisting)
		Expect(c.Kind).To(Equal(entimportscanfile.ClassificationAmbiguous))
		Expect(c.Candidates).To(HaveLen(2))
	})

	It("title mismatch on every hit → ambiguous", func() {
		c := Classify(
			anyPath,
			parsed("Foo", 2020),
			[]metadata.MovieResult{hit(1, "Bar", 2020)},
			noExisting,
		)
		Expect(c.Kind).To(Equal(entimportscanfile.ClassificationAmbiguous))
	})

	It("confirms a sole title match even with no parsed year", func() {
		c := Classify(
			anyPath,
			parsed("The Matrix", 0),
			[]metadata.MovieResult{
				hit(603, "The Matrix", 1999),
				hit(9, "Matrix Reloaded", 2003),
			},
			noExisting,
		)
		Expect(c.Kind).To(Equal(entimportscanfile.ClassificationConfirmed))
		Expect(c.TMDBID).To(Equal(uint32(603)))
	})

	It("existing-row collision wins regardless of confidence", func() {
		c := Classify(anyPath, parsed("The Matrix", 1999),
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
		c := Classify(anyPath, parsed("X", 1999), hits, noExisting)
		Expect(c.Candidates).To(HaveLen(pickerCandidateLimit))
	})

	Context("with a path-embedded tmdb id", func() {
		const idPath = "/srv/streamline/movies/Fantasia 2000 (2000) {tmdb-49948}/x.mkv"

		It("confirms on the id even when the parsed title is ambiguous", func() {
			c := Classify(idPath, parsed("Fantasia", 2000), []metadata.MovieResult{
				hit(756, "Fantasia", 1940),
				hit(49948, "Fantasia 2000", 2000),
			}, noExisting)
			Expect(c.Kind).To(Equal(entimportscanfile.ClassificationConfirmed))
			Expect(c.TMDBID).To(Equal(uint32(49948)))
			Expect(c.Candidates).To(HaveLen(1))
			Expect(c.Candidates[0].Title).To(Equal("Fantasia 2000"))
		})

		It("still reports existing when that id is already in the library", func() {
			c := Classify(idPath, parsed("Fantasia", 2000), nil,
				map[uint32]uint32{49948: 12})
			Expect(c.Kind).To(Equal(entimportscanfile.ClassificationExisting))
			Expect(c.ExistingMovieID).To(Equal(uint32(12)))
		})
	})
})

var _ = Describe("Classify across languages", Label("unit", "bulkimport"), func() {
	It("matches an English folder against a translated TMDB title", func() {
		// metadata.language=fr answers with the French title and nothing else;
		// only the original title can equal the folder.
		c := Classify(
			"/srv/Films/2001 A Space Odyssey (1968)/x.mkv",
			library.ParseResult{Title: "2001 A Space Odyssey", Year: 1968},
			[]metadata.MovieResult{{
				TMDBID:        62,
				Title:         "2001 : L'Odyssée de l'espace",
				OriginalTitle: "2001: A Space Odyssey",
				Year:          1968,
			}},
			map[uint32]uint32{},
		)
		Expect(c.Kind).To(Equal(entimportscanfile.ClassificationConfirmed))
		Expect(c.TMDBID).To(Equal(uint32(62)))
	})
})
