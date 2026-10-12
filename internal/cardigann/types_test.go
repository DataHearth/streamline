package cardigann_test

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/cardigann"
)

var _ = Describe("OrderedMap", Label("unit", "cardigann"), func() {
	m := cardigann.OrderedMap[cardigann.Str]{
		{Key: "td > a", Value: "b"},
		{Key: "a", Value: "<i>"},
	}

	It("encodes in declaration order without HTML escaping", func() {
		b, err := cardigann.EncodeJSON(m)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(b)).To(Equal("{\n  \"td > a\": \"b\",\n  \"a\": \"<i>\"\n}\n"))
	})

	It("decodes back in the same order", func() {
		b, err := json.Marshal(m)
		Expect(err).NotTo(HaveOccurred())
		var back cardigann.OrderedMap[cardigann.Str]
		Expect(json.Unmarshal(b, &back)).To(Succeed())
		Expect(back).To(Equal(m))
	})
})

var _ = Describe("Decode", Label("unit", "cardigann"), func() {
	It("accepts a filter's args as a bare scalar or a list", func() {
		d, err := cardigann.Decode([]byte(`
id: x
links: [https://x/]
search:
  keywordsfilters:
    - name: append
      args: 7
    - name: replace
      args: ["a", 1]
  rows: {selector: tr}
  fields: {}
`))
		Expect(err).NotTo(HaveOccurred())
		Expect(d.Search.KeywordsFilters[0].Args).To(Equal(cardigann.StrList{"7"}))
		Expect(
			d.Search.KeywordsFilters[1].Args,
		).To(Equal(cardigann.StrList{"a", "1"}))
	})

	It("keeps an escaped backslash before a slash", func() {
		d, err := cardigann.Decode([]byte(`
id: x
links: [https://x/]
search:
  rows: {selector: "a\\\\/b \/c"}
  fields: {}
`))
		Expect(err).NotTo(HaveOccurred())
		Expect(d.Search.Rows.Selector).To(Equal(`a\\/b /c`))
	})

	It("reads \\/ as / only in a double-quoted scalar, as YAML 1.2 does", func() {
		d, err := cardigann.Decode([]byte(`
id: x
links: [https://x/]
search:
  keywordsfilters:
    - name: replace
      args: ['\/', "\/"]
  rows: {selector: div.w-1\/2 > a}
  fields: {}
`))
		Expect(err).NotTo(HaveOccurred())
		Expect(
			d.Search.KeywordsFilters[0].Args,
		).To(Equal(cardigann.StrList{`\/`, "/"}))
		Expect(d.Search.Rows.Selector).To(Equal(`div.w-1\/2 > a`))
	})

	DescribeTable(
		"refuses what upstream never writes and a hostile file would",
		func(src, want string) {
			_, err := cardigann.Decode([]byte(src))
			Expect(err).To(MatchError(ContainSubstring(want)))
		},
		Entry("an alias", `
id: x
links: &l [https://x/]
legacylinks: *l
search: {rows: {selector: tr}, fields: {}}
`, "anchors and aliases"),
		Entry("a duplicate key in an ordered map", `
id: x
links: [https://x/]
search:
  inputs: {a: "1", a: "2"}
  rows: {selector: tr}
  fields: {}
`, `duplicate key "a"`),
		Entry("an explicit tag, which would skip the strict check", `
id: x
links: [https://x/]
search: !!null {rows: {selector: tr}, fields: {}, futurekey: 1}
`, "explicit YAML tag"),
		Entry(
			"a second document",
			"id: x\n---\nbogus: 1\n",
			"want one YAML document",
		),
		Entry("an unknown top-level key, named as such", `
id: x
links: [https://x/]
bogus: 1
search: {rows: {selector: tr}, fields: {}}
`, `unknown key "bogus" at top level`),
		Entry("a duplicate key in a struct", `
id: x
id: y
links: [https://x/]
search: {rows: {selector: tr}, fields: {}}
`, `duplicate key "id"`),
	)

	It("names the path of a key it does not know", func() {
		_, err := cardigann.Decode([]byte(`
id: x
links: [https://x/]
search:
  rows: {selector: tr}
  fields:
    title: {selector: a, bogus: 1}
`))
		Expect(err).To(MatchError(ContainSubstring(
			`unknown key "bogus" at search.fields.title`,
		)))
	})
})
