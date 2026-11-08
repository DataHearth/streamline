package engine

import (
	"context"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/cardigann"
	"github.com/datahearth/streamline/internal/cardigann/convert"
)

const htmlDefinition = `---
id: fixture
name: Fixture
description: A fixture tracker
language: en-US
type: public
encoding: UTF-8
links:
  - https://tracker.example/
caps:
  categorymappings:
    - {id: 1, cat: Movies/HD, desc: "Movies HD"}
    - {id: 2, cat: TV/HD, desc: "TV HD"}
    - {id: 3, cat: Audio, desc: "Music"}
  modes:
    search: [q]
    tv-search: [q, season, ep]
    movie-search: [q]
settings:
  - name: sort
    type: select
    label: Sort
    default: added
    options:
      added: Added
      seeders: Seeders
  - name: freeleech
    type: checkbox
    label: Freeleech only
    default: false
search:
  paths:
    - path: "search/{{ .Keywords }}/1/"
      categories: [1, 2]
    - path: "music.php"
      categories: [3]
  inputs:
    $raw: "{{ range .Categories }}c{{.}}=1&{{end}}"
    sort: "{{ .Config.sort }}"
    free: "{{ if .Config.freeleech }}1{{ else }}{{ end }}"
  keywordsfilters:
    - name: re_replace
      args: ["\\s+", " "]
  rows:
    selector: table.results > tbody > tr
    after: 1
  fields:
    category:
      selector: a[href*="cat="]
      attribute: href
      filters:
        - name: querystring
          args: cat
    title:
      selector: a.title
      remove: span.junk
    details:
      selector: a.title
      attribute: href
    download:
      selector: a.title
      attribute: href
    size:
      selector: td.size
    seeders:
      selector: td.seeds
    leechers:
      selector: td.peers
    date:
      selector: td.date
      filters:
        - name: dateparse
          args: "yyyy-MM-dd HH:mm"
    downloadvolumefactor:
      case:
        img.free: 0
        "*": 1
download:
  selectors:
    - selector: a.broken
      attribute: href
    - selector: a.torrent
      attribute: href
`

const htmlResults = `<html><body><table class="results"><tbody>
<tr class="top">
  <td><a href="/browse?cat=1">Movies</a></td>
  <td><a class="title" href="/t/1">Dune <span class="junk">[ad]</span>Part Two 2024 1080p</a></td>
  <td class="size">7,5 GB</td>
</tr>
<tr class="second">
  <td class="seeds">1.234</td><td class="peers">-</td><td class="date">2024-03-05 09:07</td>
  <td><img class="free"/></td>
</tr>
<tr class="top">
  <td><a href="/browse?cat=2">TV</a></td>
  <td><a class="title" href="/t/2">Show S01E02 720p</a></td>
  <td class="size">700 MiB</td>
</tr>
<tr class="second">
  <td class="seeds">3</td><td class="peers">4</td><td class="date">2024-03-06 10:00</td>
</tr>
</tbody></table></body></html>`

const releasePage = `<html><body>
<a class="broken" href="/not-a-torrent">x</a>
<a class="torrent" href="/dl/1.torrent">get</a>
</body></html>`

func convertFixture(src string) *cardigann.Definition {
	GinkgoHelper()
	def, err := convert.Convert("definitions/v11/fixture.yml", []byte(src))
	Expect(err).NotTo(HaveOccurred())
	Expect(convert.Check(def)).To(Succeed())
	return def
}

var _ = Describe("an HTML definition", Label("unit", "cardigann"), func() {
	var trk *tracker

	newEngine := func(settings map[string]string) *Engine {
		GinkgoHelper()
		eng, err := New(convertFixture(htmlDefinition), Options{
			Name:     "html-" + CurrentSpecReport().LeafNodeText,
			Settings: settings,
			Client:   trk.client(),
			Now:      func() time.Time { return time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC) },
		})
		Expect(err).NotTo(HaveOccurred())
		return eng
	}

	BeforeEach(func() {
		trk = newTracker(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/t/1":
				_, _ = w.Write([]byte(releasePage))
			case "/not-a-torrent":
				_, _ = w.Write([]byte("<html>login</html>"))
			case "/dl/1.torrent":
				_, _ = w.Write([]byte("d8:announce0:e"))
			default:
				_, _ = w.Write([]byte(htmlResults))
			}
		})
	})

	It("encodes the substituted keywords in the path, not the path itself", func() {
		_, err := newEngine(nil).Search(context.Background(), Query{
			Kind: KindMovie, Term: "Dune  Part/Two", Categories: []int{2000},
		})
		Expect(err).NotTo(HaveOccurred())
		reqs := trk.Requests()
		Expect(reqs).To(HaveLen(1))
		Expect(reqs[0].EscapedPath()).To(Equal("/search/Dune%20Part%2FTwo/1/"))
		// The path's categories intersect the search's; $raw splits into
		// pairs, and an empty input is dropped.
		Expect(reqs[0].RawQuery).To(Equal("c1=1&sort=added"))
	})

	It("sends a select's option and a ticked checkbox", func() {
		_, err := newEngine(
			map[string]string{"sort": "seeders", "freeleech": "true"},
		).
			Search(context.Background(), Query{
				Kind: KindMovie, Term: "x", Categories: []int{2000},
			})
		Expect(err).NotTo(HaveOccurred())
		Expect(trk.Requests()[0].RawQuery).To(Equal("c1=1&sort=seeders&free=1"))
	})

	It("picks the path the search's categories select", func() {
		_, err := newEngine(nil).Search(context.Background(), Query{
			Kind: KindMusic, Term: "x", Categories: []int{3000},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(trk.Requests()[0].Path).To(Equal("/music.php"))
	})

	It("adds the episode to the keywords of a TV search", func() {
		_, err := newEngine(nil).Search(context.Background(), Query{
			Kind:       KindTV,
			Term:       "Show",
			Season:     1,
			Episode:    2,
			Categories: []int{5000},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(trk.Requests()[0].EscapedPath()).To(Equal("/search/Show%20S01E02/1/"))
	})

	It("merges each row with the one after it and reads every field", func() {
		releases, err := newEngine(nil).Search(context.Background(), Query{
			Kind: KindMovie, Term: "x", Categories: []int{2000},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(releases).To(HaveLen(2))

		r := releases[0]
		Expect(r.Title).To(Equal("Dune Part Two 2024 1080p"))
		Expect(r.Category).To(ConsistOf(2040))
		Expect(r.Details).To(Equal("https://tracker.example/t/1"))
		Expect(r.Download).To(Equal("https://tracker.example/t/1"))
		Expect(r.Size).To(Equal(int64(7.5 * (1 << 30))))
		Expect(r.Seeders).To(Equal(int64(1234)))
		Expect(r.Leechers).To(BeZero())
		Expect(
			r.Published,
		).To(BeTemporally("==", time.Date(2024, 3, 5, 9, 7, 0, 0, time.UTC)))

		Expect(releases[1].Size).To(Equal(int64(700 << 20)))
		Expect(releases[1].Category).To(ConsistOf(5040))
	})

	It("resolves a download through the first selector holding a torrent", func() {
		d, err := newEngine(
			nil,
		).Resolve(context.Background(), "https://tracker.example/t/1")
		Expect(err).NotTo(HaveOccurred())
		Expect(d.URL).To(Equal("https://tracker.example/dl/1.torrent"))
		Expect(d.Method).To(Equal(http.MethodGet))
	})

	It(
		"refuses a setting the definition does not declare, or an option it lacks",
		func() {
			def := convertFixture(htmlDefinition)
			_, err := New(def, Options{Settings: map[string]string{"nope": "1"}})
			Expect(err).To(MatchError(ContainSubstring(`unknown setting "nope"`)))
			_, err = New(def, Options{Settings: map[string]string{"sort": "size"}})
			Expect(err).To(MatchError(ContainSubstring(`"size" is not one of`)))
			_, err = New(
				def,
				Options{Settings: map[string]string{"freeleech": "yes"}},
			)
			Expect(err).To(MatchError(ContainSubstring("is not true or false")))
		},
	)

	It("refuses a site that is not one of the definition's links", func() {
		_, err := New(
			convertFixture(htmlDefinition),
			Options{Site: "https://evil.example/"},
		)
		Expect(
			err,
		).To(MatchError(ContainSubstring("is not one of the definition's links")))
	})
})
