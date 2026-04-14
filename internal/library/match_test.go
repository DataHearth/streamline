package library

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
)

var _ = Describe("TitleMatches", Label("unit", "library"), func() {
	It("ignores case + punctuation", func() {
		Expect(TitleMatches("the.matrix", "The Matrix")).To(BeTrue())
	})
	It("strips leading article 'the'", func() {
		Expect(TitleMatches("Matrix", "The Matrix")).To(BeTrue())
	})
	It("strips leading article 'a'", func() {
		Expect(TitleMatches("Bug's Life", "A Bug's Life")).To(BeTrue())
	})
	It("returns false for genuine difference", func() {
		Expect(TitleMatches("The Matrix", "The Matrix Reloaded")).To(BeFalse())
	})
})

var _ = Describe("MatchEpisode", Label("unit", "library"), func() {
	seasons := []*ent.Season{
		{Number: 1, Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
			{Number: 1, AbsoluteNumber: 1},
			{Number: 2, AbsoluteNumber: 2},
		}}},
		{Number: 2, Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
			{Number: 1, AbsoluteNumber: 3},
		}}},
	}

	It("matches on season+episode for non-anime", func() {
		ep := MatchEpisode(ParseResult{Season: 1, Episode: 2}, seasons, false)
		Expect(ep).NotTo(BeNil())
		Expect(ep.AbsoluteNumber).To(Equal(uint16(2)))
	})

	It("matches on absolute number for anime", func() {
		ep := MatchEpisode(ParseResult{AbsoluteNumber: 3}, seasons, true)
		Expect(ep).NotTo(BeNil())
		Expect(ep.Number).To(Equal(uint16(1)))
	})

	It("returns nil when nothing matches", func() {
		Expect(
			MatchEpisode(ParseResult{Season: 9, Episode: 9}, seasons, false),
		).To(BeNil())
	})
})
