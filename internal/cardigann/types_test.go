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
