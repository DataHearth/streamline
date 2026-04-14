package hygiene

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	entimportscanshow "github.com/datahearth/streamline/ent/importscanshow"
	"github.com/datahearth/streamline/internal/metadata"
)

var _ = Describe("classifyShow", Label("unit", "hygiene"), func() {
	It("confirms a single strong title+year match", func() {
		c := classifyShow("Breaking Bad", 2008, []metadata.TVResult{
			{TVDBID: 81189, Title: "Breaking Bad", Year: 2008},
		}, nil)
		Expect(c.Kind).To(Equal(entimportscanshow.ClassificationConfirmed))
		Expect(c.TVDBID).To(Equal(uint32(81189)))
	})

	It("marks existing when the match is already tracked", func() {
		c := classifyShow("Breaking Bad", 2008, []metadata.TVResult{
			{TVDBID: 81189, Title: "Breaking Bad", Year: 2008},
		}, map[uint32]uint32{81189: 7})
		Expect(c.Kind).To(Equal(entimportscanshow.ClassificationExisting))
		Expect(c.ExistingTvshowID).To(Equal(uint32(7)))
	})

	It("is ambiguous with multiple matches", func() {
		c := classifyShow("The Office", 0, []metadata.TVResult{
			{TVDBID: 1, Title: "The Office", Year: 2005},
			{TVDBID: 2, Title: "The Office", Year: 2001},
		}, nil)
		Expect(c.Kind).To(Equal(entimportscanshow.ClassificationAmbiguous))
	})

	It("is unmatched with no results", func() {
		Expect(classifyShow("zzz", 0, nil, nil).Kind).
			To(Equal(entimportscanshow.ClassificationUnmatched))
	})
})
