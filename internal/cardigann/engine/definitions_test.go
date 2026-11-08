package engine

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/cardigann"
	"github.com/datahearth/streamline/internal/cardigann/convert"
	"github.com/datahearth/streamline/internal/cardigann/definitions"
	"github.com/datahearth/streamline/internal/cardigann/snapshot"
)

// upstream reads Prowlarr/Indexers' newest commit once per run. These specs
// need the network, so they run only with STREAMLINE_CARDIGANN_UPSTREAM set
// — the weekly cardigann-upstream workflow does — and skip otherwise.
var upstream = sync.OnceValues(func() (*definitions.Set, error) {
	ctx := context.Background()
	up, err := snapshot.Latest(ctx, http.DefaultClient)
	if err != nil {
		return nil, err
	}
	cat, defs, _, err := snapshot.Fetch(ctx, http.DefaultClient, up)
	if err != nil {
		return nil, err
	}
	return definitions.NewSet(cat, defs), nil
})

func upstreamSet() (*definitions.Set, cardigann.Catalog) {
	GinkgoHelper()
	if os.Getenv("STREAMLINE_CARDIGANN_UPSTREAM") == "" {
		Skip("STREAMLINE_CARDIGANN_UPSTREAM is not set")
	}
	set, err := upstream()
	Expect(err).NotTo(HaveOccurred())
	prev := definitions.Install(set)
	DeferCleanup(func() { definitions.Install(prev) })
	cat, err := definitions.Catalog()
	Expect(err).NotTo(HaveOccurred())
	return set, cat
}

func upstreamDefinition(set *definitions.Set, file string) *cardigann.Definition {
	GinkgoHelper()
	raw, err := set.Raw(file)
	Expect(err).NotTo(HaveOccurred(), file)
	def, err := definitions.Decode(raw)
	Expect(err).NotTo(HaveOccurred(), file)
	return def
}

// Every upstream definition, login or not, must render its search requests:
// that is the engine contract (strings everywhere, every .Query key present,
// .Config and .Result as maps) checked against what upstream actually wrote.
var _ = Describe("the upstream definitions", Label("unit", "cardigann"), func() {
	It("round-trip byte for byte, and Check accepts each", func() {
		set, cat := upstreamSet()
		for _, s := range cat.Definitions {
			raw, err := set.Raw(s.File)
			Expect(err).NotTo(HaveOccurred())
			d := upstreamDefinition(set, s.File)
			Expect(cardigann.Summarize(s.File, d)).To(Equal(s), s.File)
			b, err := cardigann.EncodeJSON(d)
			Expect(err).NotTo(HaveOccurred())
			Expect(
				bytes.Equal(b, raw),
			).To(BeTrue(), "%s does not round-trip", s.File)
			Expect(convert.Check(d)).To(Succeed(), s.File)
		}
	})

	It(
		"render every search request for a keyword, movie and episode search",
		func() {
			set, cat := upstreamSet()
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
				def := upstreamDefinition(set, s.File)
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

	It("build an engine for every definition and render its login", func() {
		set, cat := upstreamSet()
		var failures []string
		for _, s := range cat.Definitions {
			def := upstreamDefinition(set, s.File)
			e, err := New(def, Options{Name: "build-" + s.File})
			Expect(err).NotTo(HaveOccurred(), s.File)
			login := def.Login
			if login == nil {
				continue
			}
			if !slices.Contains(
				[]string{"form", "post", "get", "oneurl", "cookie"},
				login.Method,
			) {
				failures = append(failures, s.File+": method "+login.Method)
			}
			v := e.baseVars()
			if _, err := e.loginURL(v); err != nil {
				failures = append(failures, s.File+": path: "+err.Error())
			}
			for _, in := range login.Inputs {
				if _, err := render(string(in.Value), v, plainText); err != nil {
					failures = append(failures, s.File+": input: "+err.Error())
				}
			}
			if _, err := e.headers(login.Headers, v); err != nil {
				failures = append(failures, s.File+": headers: "+err.Error())
			}
		}
		Expect(failures).To(BeEmpty())
	})
})
