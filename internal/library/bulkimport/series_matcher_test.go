package bulkimport

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	entimportscanshow "github.com/datahearth/streamline/ent/importscanshow"
	"github.com/datahearth/streamline/internal/metadata"
)

var _ = Describe("ClassifyShow", Label("unit", "bulkimport"), func() {
	const anyFolder = "/srv/Series/Breaking Bad"

	It("confirms a single strong title+year match", func() {
		c := ClassifyShow(anyFolder, "Breaking Bad", 2008, []metadata.TVResult{
			{TVDBID: 81189, Title: "Breaking Bad", Year: 2008},
		}, nil)
		Expect(c.Kind).To(Equal(entimportscanshow.ClassificationConfirmed))
		Expect(c.TVDBID).To(Equal(uint32(81189)))
	})

	It("confirms a sole title match on a folder carrying no year", func() {
		c := ClassifyShow(anyFolder, "Breaking Bad", 0, []metadata.TVResult{
			{TVDBID: 81189, Title: "Breaking Bad", Year: 2008},
			{TVDBID: 2, Title: "Breaking Band", Year: 2016},
			{TVDBID: 3, Title: "Metástasis", Year: 2014},
		}, nil)
		Expect(c.Kind).To(Equal(entimportscanshow.ClassificationConfirmed))
		Expect(c.TVDBID).To(Equal(uint32(81189)))
	})

	It("sees through TVDB's in-title year disambiguator", func() {
		c := ClassifyShow(anyFolder, "Foundation", 2021, []metadata.TVResult{
			{TVDBID: 1, Title: "Foundation", Year: 2011},
			{TVDBID: 2, Title: "Foundation (2021)", Year: 2021},
			{TVDBID: 3, Title: "The Foundation", Year: 1984},
		}, nil)
		Expect(c.Kind).To(Equal(entimportscanshow.ClassificationConfirmed))
		Expect(c.TVDBID).To(Equal(uint32(2)))
	})

	It("matches a romaji folder through a TVDB alias", func() {
		c := ClassifyShow(anyFolder, "Kimetsu no Yaiba", 0, []metadata.TVResult{
			{
				TVDBID:  348545,
				Title:   "Demon Slayer: Kimetsu no Yaiba",
				Year:    2019,
				Aliases: []string{"Kimetsu no Yaiba"},
			},
		}, nil)
		Expect(c.Kind).To(Equal(entimportscanshow.ClassificationConfirmed))
		Expect(c.TVDBID).To(Equal(uint32(348545)))
	})

	It("ranks an exact match above TVDB's own order before truncating", func() {
		hits := []metadata.TVResult{
			{TVDBID: 1, Title: "El Dorado", Year: 2010},
			{TVDBID: 2, Title: "Dorota inspiruje", Year: 2018},
			{TVDBID: 3, Title: "Chasing Dorota", Year: 2009},
			{TVDBID: 4, Title: "Doroga Domoy", Year: 2014},
			{TVDBID: 5, Title: "Domoto Brothers", Year: 2001},
			{TVDBID: 6, Title: "Dororo (2019)", Year: 2019},
		}
		c := ClassifyShow(anyFolder, "Dororo", 0, hits, nil)
		Expect(c.Kind).To(Equal(entimportscanshow.ClassificationConfirmed))
		Expect(c.TVDBID).To(Equal(uint32(6)))
	})

	It("marks existing when the match is already tracked", func() {
		c := ClassifyShow(anyFolder, "Breaking Bad", 2008, []metadata.TVResult{
			{TVDBID: 81189, Title: "Breaking Bad", Year: 2008},
		}, map[uint32]uint32{81189: 7})
		Expect(c.Kind).To(Equal(entimportscanshow.ClassificationExisting))
		Expect(c.ExistingTvshowID).To(Equal(uint32(7)))
	})

	It("is ambiguous when same-titled shows cannot be told apart", func() {
		c := ClassifyShow(anyFolder, "The Office", 0, []metadata.TVResult{
			{TVDBID: 1, Title: "The Office", Year: 2005},
			{TVDBID: 2, Title: "The Office", Year: 2001},
		}, nil)
		Expect(c.Kind).To(Equal(entimportscanshow.ClassificationAmbiguous))
	})

	It("is unmatched with no results", func() {
		Expect(ClassifyShow(anyFolder, "zzz", 0, nil, nil).Kind).
			To(Equal(entimportscanshow.ClassificationUnmatched))
	})

	It("confirms on a folder-embedded tvdb id", func() {
		c := ClassifyShow(
			"/srv/streamline/tv/Drifters (2016) {tvdb-311072}",
			"Drifters", 2016,
			[]metadata.TVResult{{TVDBID: 274564, Title: "Drifters", Year: 2013}},
			nil,
		)
		Expect(c.Kind).To(Equal(entimportscanshow.ClassificationConfirmed))
		Expect(c.TVDBID).To(Equal(uint32(311072)))
	})
})

var _ = Describe(
	"ClassifyShow with same-titled tracked shows",
	Label("unit", "bulkimport"),
	func() {
		// Two library rows normalise to "Spiral": TVDB writes the 2017 entry's
		// year inside its title. Picking the first tracked candidate reported the
		// 2005 folder as `existing` against the 2017 row and adopted 76 files
		// into it.
		hits := []metadata.TVResult{
			{TVDBID: 82152, Title: "Spiral", Year: 2005},
			{TVDBID: 333715, Title: "Spiral (2017)", Year: 2017},
		}
		tracked := map[uint32]uint32{82152: 15, 333715: 127}

		It("resolves the year-carrying folder to its own row", func() {
			c := ClassifyShow(
				"/srv/streamline/tv/Spiral (2005)", "Spiral", 2005, hits, tracked,
			)
			Expect(c.Kind).To(Equal(entimportscanshow.ClassificationExisting))
			Expect(c.ExistingTvshowID).To(Equal(uint32(15)))
		})

		It("refuses to guess when the folder carries no year", func() {
			c := ClassifyShow(
				"/srv/streamline/tv/Spiral",
				"Spiral",
				0,
				hits,
				tracked,
			)
			Expect(c.Kind).To(Equal(entimportscanshow.ClassificationAmbiguous))
			Expect(c.ExistingTvshowID).To(BeZero())
		})
	},
)
