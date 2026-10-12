package engine

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/cardigann"
)

var _ = Describe("number and size parsing", Label("unit", "cardigann"), func() {
	DescribeTable("coerceInt reads separators as Prowlarr does",
		func(in string, want int64) {
			n, ok := coerceInt(in)
			Expect(ok).To(BeTrue())
			Expect(n).To(Equal(want))
		},
		Entry("thousands with a dot", "1.234", int64(1234)),
		Entry("thousands with a comma", "1,234", int64(1234)),
		Entry("noise around it", "Seeders: 42 ", int64(42)),
		Entry("a dash is zero", "-", int64(0)),
		Entry("nothing is zero", "", int64(0)),
	)

	DescribeTable("parseSize takes the unit from the letters",
		func(in string, want int64) {
			n, ok := parseSize(in)
			Expect(ok).To(BeTrue())
			Expect(n).To(Equal(want))
		},
		Entry("decimal comma, binary GiB", "1,5 GiB", int64(1.5*(1<<30))),
		Entry("MB", "700MB", int64(700<<20)),
		Entry("bare bytes", "2040109466", int64(2040109466)),
		Entry("KB with thousands", "1.024,5 KB", int64(1024.5*(1<<10))),
	)

	It("leadingInt takes the first run of digits", func() {
		Expect(leadingInt("tt0133093")).To(Equal(int64(133093)))
		Expect(leadingInt("id: 42 / 7")).To(Equal(int64(42)))
		Expect(leadingInt("none")).To(BeZero())
	})
})

var _ = Describe("date parsing", Label("unit", "cardigann"), func() {
	now := time.Date(2026, 10, 11, 15, 30, 0, 0, time.UTC) // a Sunday

	DescribeTable(
		"fromUnknown",
		func(in string, want time.Time) {
			t, err := fromUnknown(in, now)
			Expect(err).NotTo(HaveOccurred())
			Expect(t).To(BeTemporally("==", want))
		},
		Entry("its own layout", "Sun, 11 Oct 2026 10:00:00 +0200",
			time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC)),
		Entry(
			"a unix timestamp",
			"1514764800",
			time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC),
		),
		Entry("time ago", "2 hours, 30 mins ago", now.Add(-150*time.Minute)),
		Entry(
			"today at",
			"Today at 10:05",
			time.Date(2026, 10, 11, 10, 5, 0, 0, time.UTC),
		),
		Entry(
			"yesterday",
			"yesterday 23:00",
			time.Date(2026, 10, 10, 23, 0, 0, 0, time.UTC),
		),
		Entry(
			"a weekday",
			"Friday at 14:22",
			time.Date(2026, 10, 9, 14, 22, 0, 0, time.UTC),
		),
		Entry(
			"missing year",
			"03-05 09:07",
			time.Date(2026, 3, 5, 9, 7, 0, 0, time.UTC),
		),
		Entry(
			"month name",
			"Jan 2, 2024",
			time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		),
		Entry(
			"ISO",
			"2024-03-05 09:07:00",
			time.Date(2024, 3, 5, 9, 7, 0, 0, time.UTC),
		),
	)

	It("refuses a unit it does not know", func() {
		_, err := fromTimeAgo("3 fortnights ago", now)
		Expect(err).To(MatchError(errNoDate))
	})
})

var _ = Describe("filters", Label("unit", "cardigann"), func() {
	e := &Engine{
		def:   &cardigann.Definition{ID: "x"},
		clock: func() time.Time { return time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC) },
	}
	v := &vars{
		config: map[string]string{
			"sitelink": "https://s/",
		},
		query:  map[string]string{},
		result: map[string]string{"year": "1999"},
	}

	DescribeTable(
		"apply as Prowlarr's do",
		func(value string, f cardigann.Filter, want string) {
			out, err := e.applyFilters(value, []cardigann.Filter{f}, v)
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(Equal(want))
		},
		Entry(
			"querystring",
			"/dl.php?id=7&x=1#a",
			cardigann.Filter{Name: "querystring", Args: []string{"id"}},
			"7",
		),
		Entry(
			"split, negative position",
			"a/b/c",
			cardigann.Filter{Name: "split", Args: []string{"/", "-1"}},
			"c",
		),
		Entry(
			"regexp group",
			"Size: 1.2 GB",
			cardigann.Filter{Name: "regexp", Args: []string{`([\d.]+ GB)`}},
			"1.2 GB",
		),
		Entry(
			"regexp no match",
			"x",
			cardigann.Filter{Name: "regexp", Args: []string{`(\d+)`}},
			"",
		),
		Entry(
			"re_replace with a template",
			"Title",
			cardigann.Filter{
				Name: "re_replace",
				Args: []string{"$", " ({{ .Result.year }})"},
			},
			"Title (1999)",
		),
		Entry(
			"re_replace on regexp2",
			"a b",
			cardigann.Filter{
				Name:   "re_replace",
				Args:   []string{`(?<=a)\s`, "_"},
				Engine: cardigann.RegexEngineNET,
			},
			"a_b",
		),
		Entry(
			"base64decode, URL alphabet without padding",
			"aGk_Pw",
			cardigann.Filter{Name: "base64decode"},
			"hi??",
		),
		Entry(
			"base64decode, standard alphabet padded",
			"aGk/Pw==",
			cardigann.Filter{Name: "base64decode"},
			"hi??",
		),
		Entry(
			"base64encode, URL alphabet without padding",
			"hi??",
			cardigann.Filter{Name: "base64encode"},
			"aGk_Pw",
		),
		Entry(
			"replace",
			"a:b",
			cardigann.Filter{Name: "replace", Args: []string{":", "-"}},
			"a-b",
		),
		Entry(
			"trim a character",
			"--x--",
			cardigann.Filter{Name: "trim", Args: []string{"-"}},
			"x",
		),
		Entry("trim spaces", "  x \n", cardigann.Filter{Name: "trim"}, "x"),
		Entry(
			"prepend",
			"x",
			cardigann.Filter{
				Name: "prepend",
				Args: []string{"{{ .Config.sitelink }}"},
			},
			"https://s/x",
		),
		Entry(
			"diacritics",
			"Pokémon Ça",
			cardigann.Filter{Name: "diacritics", Args: []string{"replace"}},
			"Pokemon Ca",
		),
		Entry(
			"validate",
			"1080p, x264 HEVC",
			cardigann.Filter{Name: "validate", Args: []string{"hevc, 1080p, 720p"}},
			"hevc, 1080p",
		),
		Entry(
			"urlencode",
			"a b&ç",
			cardigann.Filter{Name: "urlencode"},
			"a+b%26%C3%A7",
		),
		Entry(
			"urldecode",
			"a+b%26%C3%A7",
			cardigann.Filter{Name: "urldecode"},
			"a b&ç",
		),
		Entry(
			"htmldecode",
			"&lt;b&gt;",
			cardigann.Filter{Name: "htmldecode"},
			"<b>",
		),
		Entry(
			"dateparse",
			"2024-03-05",
			cardigann.Filter{Name: "dateparse", Args: []string{"2006-01-02"}},
			"Tue, 05 Mar 2024 00:00:00 +0000",
		),
		Entry(
			"a failed dateparse keeps the value",
			"soon",
			cardigann.Filter{Name: "dateparse", Args: []string{"2006-01-02"}},
			"soon",
		),
		Entry(
			"jsonjoinarray",
			`{"a":{"b":["x",1,true]}}`,
			cardigann.Filter{Name: "jsonjoinarray", Args: []string{"a.b", "|"}},
			"x|1|True",
		),
	)
})

var _ = Describe("templates", Label("unit", "cardigann"), func() {
	v := &vars{
		config: map[string]string{
			"sort": "seeders",
		},
		query:      map[string]string{"IMDBID": ""},
		result:     map[string]string{},
		keywords:   "a b/c",
		categories: []string{"1", "2"},
		year:       "2026",
	}

	It("renders a missing key and .False as empty, never <no value>", func() {
		out, err := render(
			`{{ .Config.nope }}|{{ .Result.title }}|{{ if eq .Query.IMDBID .False }}none{{ end }}`,
			v,
			plainText,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(Equal("||none"))
	})

	It("url-encodes substituted values but not the text around them", func() {
		out, err := render(
			`s/{{ .Keywords }}?c={{ join .Categories "," }}&{{ range .Categories }}x[{{.}}]{{end}}`,
			v,
			urlEncoded,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(Equal("s/a+b%2Fc?c=1%2C2&x[1]x[2]"))
	})

	It("exposes .Today.Year", func() {
		out, err := render(`{{ .Today.Year }}`, v, plainText)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(Equal("2026"))
	})
})

var _ = Describe("andMatch", Label("unit", "cardigann"), func() {
	It("keeps titles holding every word of the term but the common ones", func() {
		out := andMatch([]Release{
			{
				Title: "The.Dark.Knight.2008",
			},
			{Title: "Dark Shadows"},
			{Title: "Knight Rider"},
		}, "The Dark Knight")
		Expect(out).To(HaveLen(1))
		Expect(out[0].Title).To(Equal("The.Dark.Knight.2008"))
	})
})
