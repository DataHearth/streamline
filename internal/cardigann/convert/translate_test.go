package convert

import (
	"regexp"
	"time"

	"github.com/dlclark/regexp2/v2"
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
		Entry("block name in a class, as its .NET range",
			`^[\p{IsCyrillic}\d]+/ `, `^[\x{0400}-\x{04FF}\p{Nd}]+/ `),
		Entry("block name alone", `\p{IsCyrillic}+`, `[\x{0400}-\x{04FF}]+`),
		Entry("negated block name", `\P{IsCJKUnifiedIdeographs}`,
			`[^\x{4E00}-\x{9FFF}]`),
		Entry("unicode escape", `([\u4e00-\u9fff])`, `([\x{4e00}-\x{9fff}])`),
		Entry("escaped non-ASCII", "\\\u00a0x", "\u00a0x"),
		Entry("shorthands as the Unicode sets .NET means",
			`(\d+)\s*GB\S`,
			`(\p{Nd}+)[\t-\r\x{85}\p{Z}]*GB[^\t-\r\x{85}\p{Z}]`),
		Entry("word class alone and in a class",
			`[\w.]+\W`,
			`[\p{L}\p{Mn}\p{Nd}\p{Pc}.]+[^\p{L}\p{Mn}\p{Nd}\p{Pc}]`),
		Entry("escaped backslash is left alone", `a\\u0041`, `a\\u0041`),
		Entry("a $ inside a class or escaped is a literal", `[$]\$`, `[$]\$`),
		Entry("a [ inside a class is literal, never POSIX", `[[:]`, `[\[:]`),
	)

	It("matches non-ASCII letters and digits the way .NET does", func() {
		w, engine, err := translateRegex(`^\w+\z`)
		Expect(err).NotTo(HaveOccurred())
		Expect(engine).To(BeEmpty())
		Expect(regexp.MustCompile(w).MatchString("Amélie")).To(BeTrue())
		Expect(regexp.MustCompile(w).MatchString("Медведь")).To(BeTrue())
		d, _, err := translateRegex(`^\d\z`)
		Expect(err).NotTo(HaveOccurred())
		Expect(regexp.MustCompile(d).MatchString("٣")).To(BeTrue())
	})

	DescribeTable(
		"tags what RE2 cannot express for regexp2, unchanged",
		func(in string) {
			out, engine, err := translateRegex(in)
			Expect(err).NotTo(HaveOccurred())
			Expect(engine).To(Equal(cardigann.RegexEngineNET))
			Expect(out).To(Equal(in))
		},
		Entry("lookbehind and lookahead", `(?<=\d)\s+(?=\d)`),
		Entry("negative lookahead", `(?i)(MULTI(?!.*(?:FRENCH|VOSTFR)))`),
		Entry("backreference", `(\.MULTI)\1`),
		Entry("word boundary, ASCII-only in Go", `\bфильм\b`),
		Entry("class subtraction", `[a-z-[aeiou]]`),
		Entry(
			"end anchor, which .NET also matches before a final newline",
			`(\d+)$`,
		),
	)

	It("gives regexp2 .NET's \\w, which leaves out ZWNJ and ZWJ", func() {
		out, engine, err := translateRegex(`(?<=x)\w+`)
		Expect(err).NotTo(HaveOccurred())
		Expect(engine).To(Equal(cardigann.RegexEngineNET))
		re := regexp2.MustCompile(out, regexp2.None)
		m, err := re.FindStringMatch("xmulti" + string(rune(0x200D)) + "zwj")
		Expect(err).NotTo(HaveOccurred())
		Expect(m.String()).To(Equal("multi"))
	})

	It(
		"sends a negated shorthand inside a class to regexp2, as .NET means it",
		func() {
			out, engine, err := translateRegex(`[\W\d]+`)
			Expect(err).NotTo(HaveOccurred())
			Expect(engine).To(Equal(cardigann.RegexEngineNET))
			Expect(out).To(Equal(`[\W\u200C\u200D\d]+`))
		},
	)

	DescribeTable("sends .NET-only meanings to regexp2",
		func(in string) {
			_, engine, err := translateRegex(in)
			Expect(err).NotTo(HaveOccurred())
			Expect(engine).To(Equal(cardigann.RegexEngineNET))
		},
		Entry("named group, numbered after unnamed ones in .NET", `(?<x>a)(b)`),
		Entry("octal escape above \\377, which .NET masks to a byte", `\400`),
	)

	It("keeps an octal escape .NET and Go read alike on RE2", func() {
		out, engine, err := translateRegex(`\101`)
		Expect(err).NotTo(HaveOccurred())
		Expect(engine).To(BeEmpty())
		Expect(out).To(Equal(`\101`))
	})

	It("gives regexp2 a block name as its range too", func() {
		out, engine, err := translateRegex(`(?<=\s)[\p{IsCyrillic}]+`)
		Expect(err).NotTo(HaveOccurred())
		Expect(engine).To(Equal(cardigann.RegexEngineNET))
		Expect(out).To(Equal(`(?<=\s)[\u0400-\u04FF]+`))
	})

	It("fails a pattern neither engine compiles", func() {
		_, _, err := translateRegex(`(unclosed`)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("translatePair", Label("unit", "cardigann"), func() {
	DescribeTable("rewrites .NET substitutions for Go's Expand",
		func(pat, rep, want string) {
			_, out, engine, err := translatePair(pat, rep)
			Expect(err).NotTo(HaveOccurred())
			Expect(engine).To(BeEmpty())
			Expect(out).To(Equal(want))
		},
		Entry("numbered group followed by a letter", `(a)`, "$1x", "${1}x"),
		Entry("group the pattern lacks stays literal", `(a)(b)`, "$12", "$$12"),
		Entry("missing named group stays literal", `(a)`, "x${foo}y", "x$${foo}y"),
		Entry("named group", `(?P<n>a)`, "${n}", "${n}"),
		Entry("whole match", `a`, "[$&]", "[${0}]"),
		Entry("escaped dollar", `a`, "$$5", "$$5"),
		Entry("lone dollar", `a`, "a $ b", "a $$ b"),
		Entry("trailing dollar", `a`, "b$", "b$$"),
		Entry("leading zeros read as a number", `(a)(b)`, "[$01]", "[${1}]"),
		Entry("an unreadable ${...} leaves its inner references live",
			`(x)`, "${a$1b}", "$${a${1}b}"),
	)

	It("sends a pattern that can match empty to regexp2", func() {
		// Go skips an empty match right after a non-empty one; .NET does not.
		_, _, engine, err := translatePair(`a*`, "-")
		Expect(err).NotTo(HaveOccurred())
		Expect(engine).To(Equal(cardigann.RegexEngineNET))
	})

	It("sends a .NET-only substitution to regexp2, untouched", func() {
		_, rep, engine, err := translatePair(`a`, "$`")
		Expect(err).NotTo(HaveOccurred())
		Expect(engine).To(Equal(cardigann.RegexEngineNET))
		Expect(rep).To(Equal("$`"))
	})

	It("leaves a regexp2 replacement in .NET syntax", func() {
		_, rep, engine, err := translatePair(`(?<=x)(a)`, "$1x")
		Expect(err).NotTo(HaveOccurred())
		Expect(engine).To(Equal(cardigann.RegexEngineNET))
		Expect(rep).To(Equal("$1x"))
	})
})

func parseAny(layouts []string, value string) (time.Time, error) {
	var err error
	for _, l := range layouts {
		var t time.Time
		if t, err = time.Parse(l, value); err == nil {
			return t, nil
		}
	}
	return time.Time{}, err
}

var _ = Describe("translateDateLayout", Label("unit", "cardigann"), func() {
	DescribeTable(
		"converts .NET custom formats",
		func(in string, want []string) {
			Expect(translateDateLayout(in)).To(Equal(want))
		},
		Entry(nil, "yyyy-MM-dd HH:mm:ss zzz",
			[]string{"2006-01-02 15:04:05 -07:00", "2006-01-02 15:04:05 -0700"}),
		Entry(nil, "MMM d yyyy hh:mm tt",
			[]string{"Jan 2 2006 03:04 PM", "Jan 2 2006 03:04 pm"}),
		Entry(nil, "dddd, d. MMMM, yyyy", []string{"Monday, 2. January, 2006"}),
		Entry(nil, "dd_MM_yyyy zz", []string{"02_01_2006 -07"}),
		Entry(nil, "yyMMdd", []string{"060102"}),
		Entry("quoted literal", "'at' HH", []string{"at 15"}),
	)

	It("accepts an offset with or without its colon, as .NET does", func() {
		layouts, err := translateDateLayout("ddd, dd MMM yyyy HH:mm:ss zzz")
		Expect(err).NotTo(HaveOccurred())
		for _, v := range []string{
			"Tue, 08 Oct 2024 09:07:01 +08:00", "Tue, 08 Oct 2024 09:07:01 +0800",
		} {
			t, err := parseAny(layouts, v)
			Expect(err).NotTo(HaveOccurred(), v)
			Expect(t.UTC()).To(Equal(time.Date(2024, 10, 8, 1, 7, 1, 0, time.UTC)))
		}
	})

	It("accepts am/pm in either case, as .NET does", func() {
		layouts, err := translateDateLayout("MMM d, yyyy, h:mm tt")
		Expect(err).NotTo(HaveOccurred())
		for _, v := range []string{"Dec 8, 2022, 6:25 PM", "Dec 8, 2022, 6:25 pm"} {
			t, err := parseAny(layouts, v)
			Expect(err).NotTo(HaveOccurred(), v)
			Expect(t.Hour()).To(Equal(18))
		}
	})

	It("takes one digit only where .NET does", func() {
		loose, err := translateDateLayout("d/M/yyyy H:mm")
		Expect(err).NotTo(HaveOccurred())
		t, err := parseAny(loose, "5/3/2024 9:07")
		Expect(err).NotTo(HaveOccurred())
		Expect(t).To(Equal(time.Date(2024, 3, 5, 9, 7, 0, 0, time.UTC)))

		strict, err := translateDateLayout("dd/MM/yyyy hh:mm")
		Expect(err).NotTo(HaveOccurred())
		_, err = parseAny(strict, "5/3/2024 9:07")
		Expect(err).To(HaveOccurred())
	})

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
})

var _ = Describe("translateTemplate", Label("unit", "cardigann"), func() {
	It("re-quotes a re_replace call so Go's lexer accepts it", func() {
		out, net, err := translateTemplate(
			`{{ re_replace .Keywords "(a)\s+" "$1" }}`,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(net).To(BeFalse())
		Expect(out).To(Equal(
			`{{ re_replace .Keywords "(a)[\\t-\\r\\x{85}\\p{Z}]+" "${1}" }}`,
		))
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
