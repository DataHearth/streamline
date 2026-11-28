package metadata

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("collectAliases", Label("unit", "metadata"), func() {
	primary := []string{
		"Demon Slayer : Kimetsu no Yaiba La Forteresse Infinie",
		"劇場版「鬼滅の刃」無限城編 第一章 猗窩座再来",
	}

	It("keeps a candidate naming the film in another language", func() {
		Expect(collectAliases(primary, []string{
			"Demon Slayer: Kimetsu no Yaiba Infinity Castle",
		})).To(ConsistOf("Demon Slayer: Kimetsu no Yaiba Infinity Castle"))
	})

	It("drops candidates that only repunctuate a primary title", func() {
		Expect(collectAliases(primary, []string{
			"Demon.Slayer.Kimetsu.no.Yaiba.La.Forteresse.Infinie",
			"demon slayer kimetsu no yaiba la forteresse infinie",
		})).To(BeEmpty())
	})

	It("collapses candidates differing only in punctuation or case", func() {
		Expect(collectAliases(primary, []string{
			"Infinity Castle",
			"infinity-castle",
			"INFINITY CASTLE",
		})).To(ConsistOf("Infinity Castle"))
	})

	It("folds accents so a stripped spelling is the same alias", func() {
		Expect(collectAliases(nil, []string{
			"Amélie",
			"Amelie",
		})).To(ConsistOf("Amélie"))
	})

	It("keeps an accented title rather than mangling it to a short key", func() {
		Expect(collectAliases(nil, []string{"Léon"})).To(ConsistOf("Léon"))
	})

	// preferTitleMatches does not stamp what it keeps as a mismatch, so the
	// unattended grabber acts on anything an alias matches.
	It("drops aliases too short to identify a film", func() {
		Expect(collectAliases(nil, []string{"Up", "Her", "It", "Them"})).
			To(ConsistOf("Them"))
	})

	// library.normalizeTitle keeps the letters of a title's own script when it
	// holds no Latin one, so TitlePrefixMatches compares on these.
	It("keeps candidates written in another script", func() {
		Expect(collectAliases(nil, []string{"鬼滅の刃", "Левиафан", "Λεβιάθαν"})).
			To(ConsistOf("鬼滅の刃", "Левиафан", "Λεβιάθαν"))
	})

	It(
		"drops a candidate in another script that repunctuates a primary title",
		func() {
			Expect(collectAliases(primary, []string{
				"劇場版 鬼滅の刃 無限城編 第一章 猗窩座再来",
			})).To(BeEmpty())
		},
	)

	It("counts characters, not bytes, in another script", func() {
		Expect(collectAliases(nil, []string{"鬼滅", "Йо"})).To(BeEmpty())
	})

	It("folds a two-script candidate to its Latin letters", func() {
		Expect(collectAliases(nil, []string{"勇気のReason", "reason"})).
			To(ConsistOf("勇気のReason"))
	})

	It("trims surrounding whitespace before comparing and storing", func() {
		Expect(collectAliases(nil, []string{"  Infinity Castle  "})).
			To(ConsistOf("Infinity Castle"))
	})

	It("returns nothing for no candidates", func() {
		Expect(collectAliases(primary, nil)).To(BeEmpty())
	})
})
