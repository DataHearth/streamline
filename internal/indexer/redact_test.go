package indexer

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("redactURL", Label("unit", "indexers"), func() {
	It("drops the query string carrying the api key", func() {
		Expect(
			redactURL("http://idx.example:9117/api?apikey=super-secret&t=caps"),
		).
			To(Equal("http://idx.example:9117/api"))
	})

	It("drops userinfo and the fragment", func() {
		Expect(
			redactURL("https://user:pw@idx.example/api?apikey=super-secret#f"),
		).
			To(Equal("https://idx.example/api"))
	})

	It("yields a placeholder when the url cannot be parsed", func() {
		Expect(redactURL("http://idx.example/%zz?apikey=super-secret")).
			To(Equal("<indexer url>"))
	})
})
