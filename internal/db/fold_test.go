package db

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("foldText", Label("unit", "db"), func() {
	It("ignores case", func() {
		Expect(foldText("Détective Conan")).To(Equal(foldText("DÉTECTIVE CONAN")))
	})

	It("strips accents so an unaccented query matches", func() {
		Expect(foldText("Détective Conan")).To(Equal(foldText("detective conan")))
	})

	It("strips punctuation so an unpunctuated query matches", func() {
		Expect(foldText("Moi, quand je me réincarne en Slime")).
			To(Equal(foldText("Moi quand je me reincarne en slime")))
	})

	It("collapses punctuation to a single space", func() {
		Expect(
			foldText("Moi, quand je me réincarne"),
		).To(Equal("moi quand je me reincarne"))
		Expect(
			foldText("  Spider-Man: No Way Home "),
		).To(Equal("spider man no way home"))
	})

	It("does not join words a separator kept apart", func() {
		Expect(foldText("Spider-Man")).To(Equal(foldText("spider man")))
		Expect(foldText("Spider-Man")).NotTo(Equal(foldText("spiderman")))
	})
})
