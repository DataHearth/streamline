package metadata

import (
	"context"
	"net/http"
	"net/url"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Wikipedia overviews", Label("unit", "metadata"), func() {
	var (
		w   *Wikipedia
		ctx context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		w = NewWikipedia()
	})

	serve := func(handler func(r *http.Request) *http.Response) {
		w.client.Transport = mbRoundTripper(
			func(r *http.Request) (*http.Response, error) { return handler(r), nil },
		)
	}

	const wikidata = `{"entities":{"Q11649":{"sitelinks":{"enwiki":{"title":"Nirvana (band)"},"frwiki":{"title":"Nirvana (groupe)"}}}}}`

	It("fetches the extract of every language that has an article", func() {
		var hosts []string
		serve(func(r *http.Request) *http.Response {
			switch r.URL.Host {
			case "www.wikidata.org":
				q := r.URL.Query()
				Expect(q.Get("ids")).To(Equal("Q11649"))
				Expect(q.Get("sitefilter")).To(Equal("enwiki|frwiki"))
				return jsonResponse(200, wikidata)
			case "en.wikipedia.org":
				hosts = append(hosts, r.URL.Host)
				Expect(
					r.URL.EscapedPath(),
				).To(Equal("/api/rest_v1/page/summary/Nirvana_%28band%29"))
				return jsonResponse(
					200,
					`{"type":"standard","extract":"Nirvana was an American rock band.","content_urls":{"desktop":{"page":"https://en.wikipedia.org/wiki/Nirvana_(band)"}}}`,
				)
			case "fr.wikipedia.org":
				hosts = append(hosts, r.URL.Host)
				return jsonResponse(
					200,
					`{"type":"standard","extract":"Nirvana est un groupe de rock.","content_urls":{"desktop":{"page":"https://fr.wikipedia.org/wiki/Nirvana_(groupe)"}}}`,
				)
			}
			return jsonResponse(404, `{}`)
		})
		got, err := w.Overviews(ctx, "Q11649", []string{"en", "fr"})
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(map[string]Overview{
			"en": {
				Text:      "Nirvana was an American rock band.",
				SourceURL: "https://en.wikipedia.org/wiki/Nirvana_(band)",
			},
			"fr": {
				Text:      "Nirvana est un groupe de rock.",
				SourceURL: "https://fr.wikipedia.org/wiki/Nirvana_(groupe)",
			},
		}))
		Expect(hosts).To(ConsistOf("en.wikipedia.org", "fr.wikipedia.org"))
	})

	It("leaves out a language with no sitelink", func() {
		serve(func(r *http.Request) *http.Response {
			if r.URL.Host == "www.wikidata.org" {
				return jsonResponse(
					200,
					`{"entities":{"Q1":{"sitelinks":{"enwiki":{"title":"X"}}}}}`,
				)
			}
			if r.URL.Host == "en.wikipedia.org" {
				return jsonResponse(200, `{"type":"standard","extract":"X is."}`)
			}
			Fail("unexpected host " + r.URL.Host)
			return nil
		})
		got, err := w.Overviews(ctx, "Q1", []string{"en", "fr"})
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(HaveLen(1))
		Expect(got["en"].SourceURL).To(Equal("https://en.wikipedia.org/wiki/X"))
	})

	It("ignores a disambiguation page and an empty extract", func() {
		serve(func(r *http.Request) *http.Response {
			if r.URL.Host == "www.wikidata.org" {
				return jsonResponse(200, wikidata)
			}
			if r.URL.Host == "en.wikipedia.org" {
				return jsonResponse(
					200,
					`{"type":"disambiguation","extract":"Nirvana may refer to"}`,
				)
			}
			return jsonResponse(200, `{"type":"standard","extract":"  "}`)
		})
		got, err := w.Overviews(ctx, "Q11649", []string{"en", "fr"})
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(BeEmpty())
	})

	It("treats a missing article as none, not as a failure", func() {
		serve(func(r *http.Request) *http.Response {
			if r.URL.Host == "www.wikidata.org" {
				return jsonResponse(200, wikidata)
			}
			return jsonResponse(404, `{}`)
		})
		got, err := w.Overviews(ctx, "Q11649", []string{"en"})
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(BeEmpty())
	})

	It("fails on a server error", func() {
		serve(func(*http.Request) *http.Response { return jsonResponse(500, `{}`) })
		_, err := w.Overviews(ctx, "Q11649", []string{"en"})
		Expect(err).To(HaveOccurred())
	})

	It("does nothing without a Wikidata id", func() {
		serve(func(*http.Request) *http.Response {
			Fail("no request expected")
			return nil
		})
		got, err := w.Overviews(ctx, "", []string{"en"})
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(BeEmpty())
		_ = url.URL{}
	})
})
