package metadata

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ClassifyBookKind", Label("unit", "metadata"), func() {
	DescribeTable(
		"kind",
		func(genres []string, language, want string) {
			Expect(ClassifyBookKind(genres, language)).To(Equal(want))
		},
		Entry("no genre is a novel", nil, "en", BookKindNovel),
		Entry(
			"a novel genre is a novel",
			[]string{"Fantasy", "Fiction"},
			"fr",
			BookKindNovel,
		),
		Entry("manga by genre", []string{"Manga"}, "en", BookKindManga),
		Entry("manga by original language", []string{"Comics"}, "ja", BookKindManga),
		Entry("bd by a french original", []string{"Comics"}, "fr", BookKindBD),
		Entry(
			"bd by a dutch original",
			[]string{"Graphic Novels"},
			"nl",
			BookKindBD,
		),
		Entry(
			"a french bande dessinee",
			[]string{"Bande dessinée"},
			"fr",
			BookKindBD,
		),
		Entry("a comic otherwise", []string{"Comics"}, "en", BookKindComic),
		Entry(
			"a graphic novel with no language",
			[]string{"Graphic Novel"},
			"",
			BookKindComic,
		),
		Entry("case is folded", []string{"MANGA"}, "", BookKindManga),
		Entry(
			"a japanese novel stays a novel",
			[]string{"Literary"},
			"ja",
			BookKindNovel,
		),
	)
})

var _ = Describe("RoleForContribution", Label("unit", "metadata"), func() {
	DescribeTable("mapping",
		func(contribution, want string, ok bool) {
			got, found := RoleForContribution(contribution)
			Expect(found).To(Equal(ok))
			Expect(got).To(Equal(want))
		},
		Entry("a null contribution is an author", "", RoleAuthor, true),
		Entry("author", "Author", RoleAuthor, true),
		Entry("writer", "writer", RoleWriter, true),
		Entry("script", "Script", RoleWriter, true),
		Entry("scripter", "Scripter", RoleWriter, true),
		Entry("illustrator", "Illustrator", RoleArtist, true),
		Entry("penciller", "Penciller", RoleArtist, true),
		Entry("inker", "Inker", RoleArtist, true),
		Entry("colorist", "Colorist", RoleColorist, true),
		Entry("colourist", "Colourist", RoleColorist, true),
		Entry("cover artist", "Cover Artist", RoleCover, true),
		Entry("cover designer", " Cover Designer ", RoleCover, true),
		Entry("translator", "Translator", RoleTranslator, true),
		Entry("narrator", "Narrator", RoleNarrator, true),
		Entry("reader", "Reader", RoleNarrator, true),
		Entry("editor is ignored", "Editor", "", false),
		Entry("foreword is ignored", "Foreword", "", false),
		Entry("letterer is ignored", "Letterer", "", false),
		Entry("contributor is ignored", "Contributor", "", false),
	)

	It("lists the maker roles", func() {
		for _, r := range []string{RoleAuthor, RoleWriter, RoleArtist, RoleColorist, RoleCover} {
			Expect(IsMakerRole(r)).To(BeTrue(), r)
		}
		Expect(IsMakerRole(RoleTranslator)).To(BeFalse())
		Expect(IsMakerRole(RoleNarrator)).To(BeFalse())
	})
})
