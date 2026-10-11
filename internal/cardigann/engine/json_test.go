package engine

import (
	"context"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const jsonDefinition = `---
id: jsonfixture
name: JSON fixture
description: x
language: en-US
type: public
encoding: UTF-8
links:
  - https://api.example/
caps:
  categorymappings:
    - {id: movie, cat: Movies, desc: "Movies"}
  modes:
    search: [q]
settings:
  - name: freeleech
    type: checkbox
    label: Freeleech
    default: true
search:
  paths:
    - path: api/search
      response:
        type: json
        noResultsMessage: "nothing found"
  inputs:
    q: "{{ .Keywords }}"
  rows:
    selector: "results{{ if .Config.freeleech }}:has(free:contains(True)){{ else }}{{ end }}:not(dead)"
  fields:
    category:
      text: movie
    title:
      selector: name
    download:
      selector: links[0]
    size:
      selector: size
    seeders:
      selector: stats.seeders
    date:
      selector: added
      filters:
        - name: append
          args: " +00:00"
        - name: dateparse
          args: "MM/dd/yyyy HH:mm:ss zzz"
    _free:
      selector: free
      case:
        "True": yes
        "False": no
    description:
      text: "{{ .Result._free }}"
`

var _ = Describe(
	"a JSON definition's row filters and value forms",
	Label("unit", "cardigann"),
	func() {
		It(
			"keeps the rows its filters accept and reads values Newtonsoft's way",
			func() {
				trk := newTracker(func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write([]byte(`{"results":[
			  {"name":"Kept","free":true,"links":["/d/1"],"size":1.5e3,"stats":{"seeders":3},"added":"2024-03-05T09:07:00Z"},
			  {"name":"Not free","free":false,"links":["/d/2"],"size":1,"stats":{"seeders":1},"added":"2024-03-05T09:07:00Z"},
			  {"name":"Dead","free":true,"dead":true,"links":["/d/3"],"size":1,"stats":{"seeders":1},"added":"2024-03-05T09:07:00Z"}
			]}`))
				})
				eng, err := New(convertFixture(jsonDefinition), Options{
					Name:   "json-filters",
					Client: trk.client(),
					Now:    func() time.Time { return time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC) },
				})
				Expect(err).NotTo(HaveOccurred())
				releases, err := eng.Search(context.Background(), Query{Term: "x"})
				Expect(err).NotTo(HaveOccurred())
				Expect(releases).To(HaveLen(1))
				r := releases[0]
				Expect(r.Title).To(Equal("Kept"))
				Expect(r.Download).To(Equal("https://api.example/d/1"))
				Expect(r.Size).To(Equal(int64(1500)))
				Expect(r.Seeders).To(Equal(int64(3)))
				Expect(r.Category).To(ConsistOf(2000))
				Expect(
					r.Published,
				).To(BeTemporally("==", time.Date(2024, 3, 5, 9, 7, 0, 0, time.UTC)))
			},
		)

		It("reads the no-results message as an empty answer", func() {
			trk := newTracker(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"error":"nothing found"}`))
			})
			eng, err := New(
				convertFixture(jsonDefinition),
				Options{Name: "json-empty", Client: trk.client()},
			)
			Expect(err).NotTo(HaveOccurred())
			releases, err := eng.Search(context.Background(), Query{Term: "x"})
			Expect(err).NotTo(HaveOccurred())
			Expect(releases).To(BeEmpty())
		})

		It("fails a response its rows selector cannot read", func() {
			trk := newTracker(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"other":[]}`))
			})
			eng, err := New(
				convertFixture(jsonDefinition),
				Options{Name: "json-bad", Client: trk.client()},
			)
			Expect(err).NotTo(HaveOccurred())
			_, err = eng.Search(context.Background(), Query{Term: "x"})
			Expect(err).To(MatchError(ErrParse))
		})

		It("reports a redirect a search path does not follow", func() {
			trk := newTracker(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "/login.php", http.StatusFound)
			})
			eng, err := New(
				convertFixture(jsonDefinition),
				Options{Name: "json-redirect", Client: trk.client()},
			)
			Expect(err).NotTo(HaveOccurred())
			_, err = eng.Search(context.Background(), Query{Term: "x"})
			Expect(err).To(MatchError(ErrStatus))
			Expect(err.Error()).To(ContainSubstring("/login.php"))
		})
	},
)

const xmlDefinition = `---
id: xmlfixture
name: XML fixture
description: x
language: en-US
type: public
encoding: UTF-8
links:
  - https://rss.example/
caps:
  categorymappings:
    - {id: 1, cat: TV, desc: "TV"}
  modes:
    search: [q]
settings: []
search:
  paths:
    - path: rss.xml
      response:
        type: xml
  rows:
    selector: item
  fields:
    category:
      text: 1
    title:
      selector: title
    download:
      selector: link
    details:
      selector: comments
    size:
      selector: enclosure
      attribute: length
    seeders:
      text: 1
    date:
      selector: pubDate
`

var _ = Describe("an XML definition", Label("unit", "cardigann"), func() {
	It("reads RSS items without the HTML parser's void <link>", func() {
		trk := newTracker(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`<?xml version="1.0"?><rss><channel>
			<item><title>Show S01E01</title><link>https://rss.example/get/1</link>
			<comments><![CDATA[https://rss.example/t/1]]></comments>
			<enclosure url="https://rss.example/get/1" length="1048576"/>
			<pubDate>Mon, 05 Feb 2024 10:00:00 +0000</pubDate></item>
			</channel></rss>`))
		})
		eng, err := New(
			convertFixture(xmlDefinition),
			Options{Name: "xml", Client: trk.client()},
		)
		Expect(err).NotTo(HaveOccurred())
		releases, err := eng.Search(context.Background(), Query{})
		Expect(err).NotTo(HaveOccurred())
		Expect(releases).To(HaveLen(1))
		Expect(releases[0].Download).To(Equal("https://rss.example/get/1"))
		Expect(releases[0].Details).To(Equal("https://rss.example/t/1"))
		Expect(releases[0].Size).To(Equal(int64(1 << 20)))
		Expect(releases[0].Category).To(ConsistOf(5000))
	})
})
