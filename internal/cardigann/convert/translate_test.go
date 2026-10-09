package convert

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/cardigann"
)

var _ = Describe("translateRegex", Label("unit", "cardigann"), func() {
	DescribeTable("rewrites .NET spellings for RE2",
		func(in, want string) {
			out, engine, err := translateRegex(in)
			Expect(err).NotTo(HaveOccurred())
			Expect(engine).To(BeEmpty())
			Expect(out).To(Equal(want))
		},
		Entry("block name", `^[\p{IsCyrillic}\W\d]+/ `, `^[\p{Cyrillic}\W\d]+/ `),
		Entry("CJK block", `([\p{IsCJKUnifiedIdeographs}\W]+)`, `([\p{Han}\W]+)`),
		Entry("unicode escape", `([\u4e00-\u9fff])`, `([\x{4e00}-\x{9fff}])`),
		Entry("escaped non-ASCII", "\\\u00a0(\\d+)", "\u00a0(\\d+)"),
		Entry("already RE2", `(\d+)\s*GB`, `(\d+)\s*GB`),
		Entry("escaped backslash is left alone", `a\\u0041`, `a\\u0041`),
	)

	DescribeTable("tags what RE2 cannot express for regexp2, unchanged",
		func(in string) {
			out, engine, err := translateRegex(in)
			Expect(err).NotTo(HaveOccurred())
			Expect(engine).To(Equal(cardigann.RegexEngineNET))
			Expect(out).To(Equal(in))
		},
		Entry("lookbehind and lookahead", `(?<=\d)\s+(?=\d)`),
		Entry("negative lookahead", `(?i)\b(MULTI(?!.*(?:FRENCH|VOSTFR)))\b`),
		Entry("backreference", `(\.MULTI)\1`),
	)

	It("fails a pattern neither engine compiles", func() {
		_, _, err := translateRegex(`(unclosed`)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("translateReplacement", Label("unit", "cardigann"), func() {
	DescribeTable("rewrites .NET substitutions for Go's Expand",
		func(in, want string) {
			out, err := translateReplacement(in, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(Equal(want))
		},
		Entry("numbered group followed by a letter", "$1x", "${1}x"),
		Entry("multi-digit group", "$12", "${12}"),
		Entry("whole match", "[$&]", "[${0}]"),
		Entry("named group", "${name}", "${name}"),
		Entry("escaped dollar", "$$5", "$$5"),
		Entry("lone dollar", "a $ b", "a $$ b"),
	)

	It("leaves a regexp2 replacement in .NET syntax", func() {
		Expect(translateReplacement("$1x", cardigann.RegexEngineNET)).
			To(Equal("$1x"))
	})

	It("refuses a substitution Go has no form of", func() {
		_, err := translateReplacement("$`", "")
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("translateDateLayout", Label("unit", "cardigann"), func() {
	DescribeTable(
		"converts .NET custom formats",
		func(in, want string) {
			Expect(translateDateLayout(in)).To(Equal(want))
		},
		Entry(nil, "yyyy-MM-dd HH:mm:ss zzz", "2006-01-02 15:04:05 -07:00"),
		Entry(nil, "yyyy-MM-ddHH:mm:ss zzz", "2006-01-0215:04:05 -07:00"),
		Entry(
			nil,
			"ddd, dd MMM yyyy HH:mm:ss zzz",
			"Mon, 02 Jan 2006 15:04:05 -07:00",
		),
		Entry(nil, "MMM d yyyy hh:mm tt", "Jan 2 2006 03:04 PM"),
		Entry(nil, "dddd, d. MMMM, yyyy", "Monday, 2. January, 2006"),
		Entry(nil, "dd_MM_yyyy zzz", "02_01_2006 -07:00"),
		Entry(nil, "yyMMdd", "060102"),
		Entry("quoted literal", "'at' HH", "at 15"),
	)

	DescribeTable("refuses what Go cannot parse the same way",
		func(in string) {
			_, err := translateDateLayout(in)
			Expect(err).To(HaveOccurred())
		},
		Entry("one-digit year", "d/M/y"),
		Entry("one-letter AM/PM", "h t"),
		Entry("fractional seconds", "HH:mm:ss.fff"),
		Entry("literal that reads as a Go field", "'2'HH"),
	)

	It("parses unpadded hours and days with the padded spelling", func() {
		layout, err := translateDateLayout("d/M/yyyy HH:mm")
		Expect(err).NotTo(HaveOccurred())
		t, err := time.Parse(layout, "5/3/2024 9:07")
		Expect(err).NotTo(HaveOccurred())
		Expect(t).To(Equal(time.Date(2024, 3, 5, 9, 7, 0, 0, time.UTC)))
	})
})

var _ = Describe("translateTemplate", Label("unit", "cardigann"), func() {
	It("re-quotes a re_replace call so Go's lexer accepts it", func() {
		out, net, err := translateTemplate(`{{ re_replace .Keywords "\s+" "$1" }}`)
		Expect(err).NotTo(HaveOccurred())
		Expect(net).To(BeFalse())
		Expect(out).To(Equal(`{{ re_replace .Keywords "\\s+" "${1}" }}`))
	})

	It("renames a re_replace whose pattern needs regexp2", func() {
		out, net, err := translateTemplate(`{{ re_replace .Keywords "(?<=x)y" "" }}`)
		Expect(err).NotTo(HaveOccurred())
		Expect(net).To(BeTrue())
		Expect(out).To(HavePrefix("{{ " + cardigann.TemplateFuncReplaceNET + " "))
	})

	It("turns a lookup Go cannot lex into an index call", func() {
		out, _, err := translateTemplate(
			`{{ .Config.2facode }}-{{ .Config.cat-id }}-{{ .Config.sort }}`,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(Equal(
			`{{ (index .Config "2facode") }}-{{ (index .Config "cat-id") }}-` +
				`{{ .Config.sort }}`,
		))
	})

	It("passes a string without actions through", func() {
		Expect(translateTemplate("td > a")).To(Equal("td > a"))
	})

	It("fails a template Go cannot parse", func() {
		_, _, err := translateTemplate(`{{ if and (.Keywords) (.X)) }}a{{ end }}`)
		Expect(err).To(HaveOccurred())
	})
})
