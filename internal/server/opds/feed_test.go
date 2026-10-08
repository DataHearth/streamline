package opds

import (
	"net/http/httptest"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = g.Describe("feed", g.Label("unit"), func() {
	g.It("writes a navigation feed with the OPDS media type", func() {
		rec := httptest.NewRecorder()
		Expect(writeFeed(rec, kindNavigation, &feed{
			ID:    "urn:streamline:opds:root",
			Title: "Streamline",
			Links: []link{selfLink("/opds", kindNavigation)},
			Entries: []entry{
				{
					ID:    "urn:streamline:opds:authors",
					Title: "Authors",
					Links: []link{
						{
							Rel:  "subsection",
							Href: "/opds/authors",
							Type: navigationType,
						},
					},
				},
			},
		})).To(Succeed())
		Expect(
			rec.Header().Get("Content-Type"),
		).To(ContainSubstring("kind=navigation"))
		body := rec.Body.String()
		Expect(
			body,
		).To(ContainSubstring(`<feed xmlns="http://www.w3.org/2005/Atom"`))
		Expect(body).To(ContainSubstring("<title>Authors</title>"))
	})

	g.It("writes acquisition entries with download and cover links", func() {
		rec := httptest.NewRecorder()
		Expect(writeFeed(rec, kindAcquisition, &feed{
			ID: "urn:streamline:opds:recent", Title: "Recently added",
			Entries: []entry{{
				ID: "urn:streamline:book:7", Title: "Elantris",
				Authors: []atomAuthor{{Name: "Brandon Sanderson"}},
				Links: []link{
					{
						Rel:  "http://opds-spec.org/acquisition",
						Href: "/opds/download/7/epub",
						Type: "application/epub+zip",
					},
					{
						Rel:  "http://opds-spec.org/image",
						Href: "/posters/books/7/poster.jpg",
						Type: "image/jpeg",
					},
				},
			}},
		})).To(Succeed())
		Expect(
			rec.Body.String(),
		).To(ContainSubstring(`rel="http://opds-spec.org/acquisition"`))
	})
})
