package arr

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("MapRoot", Label("unit", "arr"), func() {
	roots := []RootMapping{
		{From: "/media/movies", To: "/srv/films"},
		{From: "/media/movies-4k", To: "/srv/films-4k"},
	}

	It("takes the longest matching prefix, not the first", func() {
		got, ok := MapRoot("/media/movies-4k/Dune/Dune.mkv", roots)
		Expect(ok).To(BeTrue())
		Expect(got).To(Equal("/srv/films-4k/Dune/Dune.mkv"))
	})

	It("does not let a shorter root swallow a longer sibling", func() {
		got, ok := MapRoot("/media/movies/Dune/Dune.mkv", roots)
		Expect(ok).To(BeTrue())
		Expect(got).To(Equal("/srv/films/Dune/Dune.mkv"))
	})

	It("prefers the deeper of two nested roots", func() {
		got, ok := MapRoot("/media/movies/kids/Up.mkv", []RootMapping{
			{From: "/media/movies", To: "/a"},
			{From: "/media/movies/kids", To: "/b"},
		})
		Expect(ok).To(BeTrue())
		Expect(got).To(Equal("/b/Up.mkv"))
	})

	It("maps the root itself", func() {
		got, ok := MapRoot("/media/movies/", roots)
		Expect(ok).To(BeTrue())
		Expect(got).To(Equal("/srv/films"))
	})

	It("leaves a path under no mapping verbatim", func() {
		got, ok := MapRoot("/elsewhere/Dune.mkv", roots)
		Expect(ok).To(BeFalse())
		Expect(got).To(Equal("/elsewhere/Dune.mkv"))
	})

	It("is the identity when from and to are equal", func() {
		got, ok := MapRoot("/media/movies/a.mkv",
			[]RootMapping{{From: "/media/movies", To: "/media/movies"}})
		Expect(ok).To(BeTrue())
		Expect(got).To(Equal("/media/movies/a.mkv"))
	})
})

var _ = Describe("FieldValue", Label("unit", "arr"), func() {
	fields := []Field{
		{Name: "baseUrl", Value: "https://idx.example.com", Privacy: "normal"},
		{Name: "port", Value: float64(8080), Privacy: "normal"},
		{Name: "apiKey", Value: "", Privacy: "apiKey"},
		{Name: "useSsl", Value: true, Privacy: "normal"},
	}

	It("reads a string field", func() {
		v, ok := FieldValue(fields, "baseUrl")
		Expect(ok).To(BeTrue())
		Expect(v).To(Equal("https://idx.example.com"))
	})

	It("renders a JSON number without a decimal tail", func() {
		v, ok := FieldValue(fields, "port")
		Expect(ok).To(BeTrue())
		Expect(v).To(Equal("8080"))
	})

	It("renders a bool", func() {
		v, ok := FieldValue(fields, "useSsl")
		Expect(ok).To(BeTrue())
		Expect(v).To(Equal("true"))
	})

	It("reports an empty secret as present but blank", func() {
		v, ok := FieldValue(fields, "apiKey")
		Expect(ok).To(BeTrue())
		Expect(v).To(BeEmpty())
	})

	It("reports an absent field", func() {
		_, ok := FieldValue(fields, "nope")
		Expect(ok).To(BeFalse())
	})
})

var _ = Describe("TranslateProfile", Label("unit", "arr"), func() {
	// known stands in for quality.IsBuiltinName / config.FindCustomFormat.
	known := func(n string) bool { return n == "Repack" }

	leaf := func(id uint32, name string, res int, allowed bool) QualityItem {
		return QualityItem{
			Quality: &Quality{ID: id, Name: name, Resolution: res},
			Allowed: allowed,
		}
	}

	It("takes the band of the lowest allowed quality and the cutoff", func() {
		got, notes := TranslateProfile(QualityProfile{
			Name:              "HD-1080p",
			UpgradeAllowed:    true,
			Cutoff:            9,
			MinFormatScore:    10,
			CutoffFormatScore: 100,
			Items: []QualityItem{
				leaf(4, "HDTV-720p", 720, true),
				leaf(9, "Bluray-1080p", 1080, true),
				leaf(19, "Bluray-2160p", 2160, false),
			},
		}, known)

		Expect(got.Name).To(Equal("HD-1080p"))
		Expect(got.MinResolution).To(Equal("720p"))
		Expect(got.PreferredResolution).To(Equal("1080p"))
		Expect(got.UpgradeAllowed).To(BeTrue())
		Expect(got.MinScore).To(Equal(10))
		Expect(got.UpgradeUntilScore).To(Equal(100))
		Expect(notes).To(ContainElement(ContainSubstring("source")))
	})

	It("floors anything below 720p and says so", func() {
		got, notes := TranslateProfile(QualityProfile{
			Name:   "SD",
			Cutoff: 2,
			Items: []QualityItem{
				leaf(1, "SDTV", 480, true),
				leaf(2, "DVD", 480, true),
			},
		}, known)

		Expect(got.MinResolution).To(Equal("720p"))
		Expect(got.PreferredResolution).To(Equal("720p"))
		Expect(notes).To(ContainElement(ContainSubstring("480")))
	})

	It("resolves a cutoff that names a group to the group's top resolution", func() {
		got, _ := TranslateProfile(QualityProfile{
			Name:   "Any HD",
			Cutoff: 1000,
			Items: []QualityItem{
				{
					ID:      1000,
					Name:    "WEB 1080p",
					Allowed: true,
					Items: []QualityItem{
						leaf(15, "WEBRip-1080p", 1080, false),
						leaf(3, "WEBDL-720p", 720, false),
					},
				},
			},
		}, known)

		Expect(got.MinResolution).To(Equal("720p"))
		Expect(got.PreferredResolution).To(Equal("1080p"))
	})

	It("never yields a preferred band below the minimum", func() {
		got, _ := TranslateProfile(QualityProfile{
			Name:   "Odd",
			Cutoff: 4,
			Items: []QualityItem{
				leaf(4, "HDTV-720p", 720, false),
				leaf(9, "Bluray-1080p", 1080, true),
			},
		}, known)

		Expect(got.MinResolution).To(Equal("1080p"))
		Expect(got.PreferredResolution).To(Equal("1080p"))
	})

	It("keeps only format scores whose name resolves, and lists the rest", func() {
		got, notes := TranslateProfile(QualityProfile{
			Name:   "P",
			Cutoff: 9,
			Items:  []QualityItem{leaf(9, "Bluray-1080p", 1080, true)},
			FormatItems: []FormatItem{
				{Name: "Repack", Score: 5},
				{Name: "x265 (no HDR)", Score: -10000},
				{Name: "Unscored", Score: 0},
			},
		}, known)

		Expect(got.Formats).To(HaveLen(1))
		Expect(got.Formats[0].Name).To(Equal("Repack"))
		Expect(got.Formats[0].Score).To(Equal(5))
		Expect(notes).To(ContainElement(ContainSubstring("x265 (no HDR)")))
		Expect(notes).NotTo(ContainElement(ContainSubstring("Unscored")))
	})
})
