package engine

import (
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/cardigann/definitions"
)

// Every embedded definition, login or not, must render its search requests:
// that is the engine contract (strings everywhere, every .Query key present,
// .Config and .Result as maps) checked against what upstream actually wrote.
var _ = Describe("the embedded definitions", Label("unit", "cardigann"), func() {
	It(
		"render every search request for a keyword, movie and episode search",
		func() {
			cat, err := definitions.Catalog()
			Expect(err).NotTo(HaveOccurred())
			queries := []Query{
				{Kind: KindSearch, Term: "Dune"},
				{
					Kind:       KindMovie,
					Term:       "Dune",
					IMDbID:     "tt1160419",
					TMDBID:     438631,
					Categories: []int{2000},
				},
				{
					Kind:       KindTV,
					Term:       "Dark",
					Season:     1,
					Episode:    2,
					TVDBID:     334824,
					IMDbID:     "tt5753856",
					Categories: []int{5000},
				},
				{
					Kind:       KindMusic,
					Term:       "Nirvana Nevermind",
					Categories: []int{3000},
				},
				{Kind: KindBook, Term: "Elantris", Categories: []int{7000, 7020}},
				{},
			}
			var failures []string
			for _, s := range cat.Definitions {
				def, err := definitions.Load(s.File)
				Expect(err).NotTo(HaveOccurred())
				site, err := Site(def, "")
				Expect(err).NotTo(HaveOccurred())
				config, err := Config(def, nil)
				if err != nil {
					failures = append(failures, s.File+": settings: "+err.Error())
					continue
				}
				config["sitelink"] = site.String()
				e := &Engine{
					def:    def,
					site:   site,
					config: config,
					cats:   newCategoryMap(def.Caps),
					clock:  func() time.Time { return time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC) },
				}
				for _, q := range queries {
					v := e.baseVars()
					v.query = e.queryVars(q)
					v.query["Keywords"] = strings.TrimSpace(
						v.query["Q"] + " " + v.query["Episode"],
					)
					if v.keywords, err = e.applyFilters(
						v.query["Keywords"],
						def.Search.KeywordsFilters,
						v,
					); err != nil {
						failures = append(
							failures,
							s.File+": keywords: "+err.Error(),
						)
						break
					}
					v.categories = e.cats.trackers(q.Categories)
					for _, p := range def.Search.Paths {
						r, err := e.searchRequest(p, v)
						if err != nil {
							failures = append(failures, s.File+": "+err.Error())
							break
						}
						if strings.Contains(r.url, "<no value>") {
							failures = append(
								failures,
								s.File+": <no value> in "+r.url,
							)
						}
						if r.method != http.MethodGet &&
							r.method != http.MethodPost {
							failures = append(failures, s.File+": method "+r.method)
						}
					}
				}
			}
			Expect(failures).To(BeEmpty())
		},
	)

	It("build an engine for every definition that needs no login", func() {
		cat, err := definitions.Catalog()
		Expect(err).NotTo(HaveOccurred())
		n := 0
		for _, s := range cat.Definitions {
			if s.Login != "none" {
				continue
			}
			def, err := definitions.Load(s.File)
			Expect(err).NotTo(HaveOccurred())
			_, err = New(def, Options{Name: "build-" + s.File})
			Expect(err).NotTo(HaveOccurred(), s.File)
			n++
		}
		Expect(n).To(BeNumerically(">", 70))
	})
})
