package download

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SeedingSet", Label("unit", "download"), func() {
	set := NewSeedingSet(
		true,
		[]string{"/dl/Album", "/dl/single.flac"},
		[]string{"ABC"},
	)

	It("owns what lies inside a torrent's content, and nothing beside it", func() {
		Expect(set.Owns("/dl/Album/01.flac")).To(BeTrue())
		Expect(set.Owns("/dl/single.flac")).To(BeTrue())
		Expect(set.Owns("/dl/Album Deluxe/01.flac")).To(BeFalse())
		Expect(set.Owns("/music/Album/01.flac")).To(BeFalse())
	})

	It("matches hashes whatever their case", func() {
		Expect(set.Live("abc")).To(BeTrue())
		Expect(set.Live("def")).To(BeFalse())
	})
})
