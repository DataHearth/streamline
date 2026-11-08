package update_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"path"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/cardigann"
	"github.com/datahearth/streamline/internal/cardigann/definitions"
	"github.com/datahearth/streamline/internal/cardigann/snapshot"
	"github.com/datahearth/streamline/internal/cardigann/update"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/testutil/cardiganntest"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

const versions = "MIN_VERSION=11\nMAX_VERSION=12\nCURRENT_VERSION=11\n"

func definition(id, link string) string {
	return fmt.Sprintf(`---
id: %s
name: %s
description: A tracker
language: en-US
type: private
encoding: UTF-8
links:
  - %s
caps:
  categorymappings:
    - {id: 1, cat: Movies, desc: Movies}
  modes:
    search: [q]
settings:
  - {name: username, type: text, label: Username}
  - {name: password, type: password, label: Password}
login:
  path: login.php
  method: post
  inputs:
    username: "{{ .Config.username }}"
search:
  paths:
    - path: browse.php
  rows:
    selector: tr
  fields:
    title:
      selector: a
    download:
      selector: a
      attribute: href
`, id, id, link)
}

// spec converts alpha's definition the way a fetch does.
func spec(body string) []byte {
	GinkgoHelper()
	_, defs := snapshot.Build(
		[]snapshot.File{{Version: 11, Name: "alpha.yml", Data: []byte(body)}},
		cardigann.Upstream{},
	)
	Expect(defs).To(HaveKey("alpha"))
	return defs["alpha"]
}

type commit struct {
	sha, date string
	files     map[string]string
	// v12 holds the files upstream also carries under definitions/v12.
	v12 map[string]string
}

func tarball(c commit) []byte {
	GinkgoHelper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	add := func(name, body string) {
		Expect(tw.WriteHeader(&tar.Header{
			Name: "Indexers-" + c.sha + "/" + name, Mode: 0o644,
			Size: int64(len(body)), Typeflag: tar.TypeReg,
		})).To(Succeed())
		_, err := tw.Write([]byte(body))
		Expect(err).NotTo(HaveOccurred())
	}
	add("VERSIONS", versions)
	for name, body := range c.files {
		add("definitions/v11/"+name+".yml", body)
	}
	for name, body := range c.v12 {
		add("definitions/v12/"+name+".yml", body)
	}
	Expect(tw.Close()).To(Succeed())
	Expect(gz.Close()).To(Succeed())
	return buf.Bytes()
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(
	r *http.Request,
) (*http.Response, error) {
	return f(r)
}

var _ = Describe("the definition update", Label("unit", "cardigann"), func() {
	var (
		head     commit
		served   map[string]commit
		archives int
		client   *http.Client
	)

	BeforeEach(func() {
		prev := definitions.Install(nil)
		DeferCleanup(func() { definitions.Install(prev) })
		archives = 0
		served = map[string]commit{}
		client = &http.Client{Transport: roundTripFunc(
			func(r *http.Request) (*http.Response, error) {
				var body []byte
				switch {
				case r.URL.Host == "api.github.com":
					body = fmt.Appendf(nil,
						`{"sha":%q,"commit":{"committer":{"date":%q}}}`,
						head.sha, head.date)
				case r.URL.Host == "codeload.github.com" &&
					served[path.Base(r.URL.Path)].sha != "":
					archives++
					body = tarball(served[path.Base(r.URL.Path)])
				default:
					return &http.Response{StatusCode: 404, Body: http.NoBody}, nil
				}
				return &http.Response{
					StatusCode: 200,
					Body:       readCloser(body),
				}, nil
			},
		)}
	})

	sha := func(n int) string { return strings.Repeat(fmt.Sprint(n), 40) }
	// pinTo runs one built-in indexer, A, on alpha at upstream commit.
	pinTo := func(commit string, readOnly bool) {
		GinkgoHelper()
		configtest.SetupFile(map[string]any{
			"read_only": readOnly,
			"indexers": []map[string]any{{
				"name": "A", "protocol": "cardigann", "enabled": true,
				"cardigann": map[string]any{
					"definition": "alpha", "commit": commit,
				},
			}},
		})
	}
	// configure pins A to a kept version: the one body converts to.
	configure := func(body string, readOnly bool) {
		GinkgoHelper()
		pinTo(cardiganntest.Commit(spec(body)), readOnly)
		cardiganntest.Keep(config.Get().DataDir, "alpha", spec(body))
	}
	run := func(c commit) update.Result {
		GinkgoHelper()
		head, served[c.sha] = c, c
		res, err := update.Run(context.Background(), client)
		Expect(err).NotTo(HaveOccurred())
		return res
	}
	carried := func() []byte {
		GinkgoHelper()
		e, ok := config.FindIndexer("A")
		Expect(ok).To(BeTrue())
		b, err := definitions.Pinned(
			config.Get().DataDir, "alpha", e.Cardigann.Commit,
		)
		Expect(err).NotTo(HaveOccurred())
		return b
	}

	It("puts upstream's newest commit in force as the catalog, once", func() {
		head = commit{
			sha: sha(1), date: "2030-01-01T00:00:00Z",
			files: map[string]string{
				"alpha": definition("alpha", "https://alpha.example/"),
				"beta":  "links: [not a definition",
			},
		}
		served[head.sha] = head
		Expect(
			definitions.Catalog(),
		).Error().
			To(MatchError(definitions.ErrNotFetched))
		Expect(update.Ensure(context.Background(), client)).To(Succeed())
		cat, err := definitions.Catalog()
		Expect(err).NotTo(HaveOccurred())
		Expect(cat.Upstream.Commit).To(Equal(sha(1)))
		Expect(cat.Definitions).To(HaveLen(1))
		Expect(cat.Skipped).To(HaveLen(1))
		Expect(cat.Skipped[0].File).To(Equal("beta.yml"))

		Expect(update.Ensure(context.Background(), client)).To(Succeed())
		res, err := update.Fetch(context.Background(), client)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Current).To(BeTrue())
		Expect(archives).To(Equal(1))
	})

	It("moves an indexer to a new version that keeps its hosts and login", func() {
		old := definition("alpha", "https://alpha.one/")
		configure(old, false)
		next := strings.Replace(old, "selector: tr", "selector: tr.row", 1)
		res := run(commit{
			sha: sha(1), date: "2030-01-01T00:00:00Z",
			files: map[string]string{"alpha": next},
		})
		Expect(res.Applied).To(ConsistOf("A"))
		Expect(res.Held).To(BeEmpty())
		Expect(carried()).To(Equal(spec(next)))
		e, _ := config.FindIndexer("A")
		Expect(e.Cardigann.Commit).To(Equal(sha(1)))

		By("removing the version no indexer is pinned to any more")
		Expect(definitions.Pinned(
			config.Get().DataDir, "alpha", cardiganntest.Commit(spec(old)),
		)).Error().To(MatchError(definitions.ErrNotKept))

		By("keeping it across a restart, with nothing fetched")
		definitions.Install(nil)
		Expect(carried()).To(Equal(spec(next)))
	})

	It("pins an indexer written without a commit to the catalog's", func() {
		pinTo("", false)
		body := definition("alpha", "https://alpha.zero/")
		res := run(commit{
			sha: sha(1), date: "2030-01-01T00:00:00Z",
			files: map[string]string{"alpha": body},
		})
		Expect(res.Applied).To(ConsistOf("A"))
		Expect(carried()).To(Equal(spec(body)))
	})

	It("reads a version that is not kept at its commit upstream", func() {
		old := definition("alpha", "https://alpha.seven/")
		served[sha(1)] = commit{sha: sha(1), files: map[string]string{"alpha": old}}
		pinTo(sha(1), false)
		res := run(commit{
			sha: sha(2), date: "2030-01-02T00:00:00Z",
			files: map[string]string{
				"alpha": definition("alpha", "https://alpha.seven.evil/"),
			},
		})
		Expect(res.Held).To(ConsistOf("alpha"))
		Expect(update.List()[0].Diff).
			To(ContainSubstring(`-    "https://alpha.seven/"`))
		Expect(carried()).To(Equal(spec(old)))
	})

	It("holds a domain change for approval, then writes it in", func() {
		old := definition("alpha", "https://alpha.two/")
		configure(old, false)
		next := definition("alpha", "https://alpha.evil/")
		res := run(commit{
			sha: sha(2), date: "2030-01-02T00:00:00Z",
			files: map[string]string{"alpha": next},
		})
		Expect(res.Held).To(ConsistOf("alpha"))
		Expect(carried()).To(Equal(spec(old)))

		pending := update.List()
		Expect(pending).To(HaveLen(1))
		p := pending[0]
		Expect(p.Indexers).To(ConsistOf("A"))
		Expect(p.Reasons).To(ContainElements(
			"new domains: alpha.evil", "dropped domains: alpha.two",
		))
		Expect(p.Diff).To(ContainSubstring(`-    "https://alpha.two/"`))
		Expect(p.Diff).To(ContainSubstring(`+    "https://alpha.evil/"`))
		Expect(p.Commit).To(Equal(sha(2)))

		Expect(update.Approve(context.Background(), "alpha")).To(Succeed())
		Expect(carried()).To(Equal(spec(next)))
		Expect(update.List()).To(BeEmpty())
		Expect(update.Approve(context.Background(), "alpha")).
			To(MatchError(update.ErrNoUpdate))
	})

	It("holds a path that points at another host without naming a scheme", func() {
		old := definition("alpha", "https://alpha.three/")
		configure(old, false)
		res := run(commit{
			sha: sha(3), date: "2030-01-03T00:00:00Z",
			files: map[string]string{"alpha": strings.Replace(
				old, "path: browse.php", "path: //alpha.evil/browse.php", 1,
			)},
		})
		Expect(res.Held).To(ConsistOf("alpha"))
		Expect(update.List()[0].Reasons).To(ConsistOf("new domains: alpha.evil"))
	})

	It("does not propose a version that was turned down again", func() {
		old := definition("alpha", "https://alpha.four/")
		configure(old, false)
		evil := definition("alpha", "https://alpha.four.evil/")
		run(commit{
			sha: sha(4), date: "2030-01-04T00:00:00Z",
			files: map[string]string{"alpha": evil},
		})
		Expect(update.Reject("alpha")).To(Succeed())
		Expect(update.List()).To(BeEmpty())

		run(commit{
			sha: sha(5), date: "2030-01-05T00:00:00Z",
			files: map[string]string{"alpha": evil},
		})
		Expect(update.List()).To(BeEmpty())
		Expect(carried()).To(Equal(spec(old)))

		run(commit{
			sha: sha(6), date: "2030-01-06T00:00:00Z",
			files: map[string]string{
				"alpha": definition("alpha", "https://alpha.four.other/"),
			},
		})
		Expect(update.List()).To(HaveLen(1))
	})

	It("holds every change on a read-only configuration", func() {
		old := definition("alpha", "https://alpha.five/")
		configure(old, true)
		next := strings.Replace(old, "selector: tr", "selector: tr.row", 1)
		res := run(commit{
			sha: sha(7), date: "2030-01-07T00:00:00Z",
			files: map[string]string{"alpha": next},
		})
		Expect(res.Applied).To(BeEmpty())
		Expect(res.Held).To(ConsistOf("alpha"))
		p := update.List()[0]
		Expect(p.Reasons).To(BeEmpty())
		Expect(p.Commit).To(Equal(sha(7)))
		Expect(update.Approve(context.Background(), "alpha")).
			To(MatchError(config.ErrReadOnly))
	})

	It(
		"leaves an indexer on its copy when upstream drops or breaks its definition",
		func() {
			old := definition("alpha", "https://alpha.six/")
			configure(old, false)
			res := run(commit{
				sha: sha(8), date: "2030-01-08T00:00:00Z",
				files: map[string]string{"alpha": "links: [not a definition"},
			})
			Expect(res.Applied).To(BeEmpty())
			Expect(res.Held).To(BeEmpty())
			res = run(commit{
				sha: sha(9), date: "2030-01-09T00:00:00Z",
				files: map[string]string{
					"beta": definition("beta", "https://beta.example/"),
				},
			})
			Expect(res.Applied).To(BeEmpty())
			Expect(res.Held).To(BeEmpty())
			Expect(carried()).To(Equal(spec(old)))
		},
	)

	It("takes a definition's v12 copy over its v11 one", func() {
		head = commit{
			sha: sha(1), date: "2030-01-01T00:00:00Z",
			files: map[string]string{
				"alpha": definition("alpha", "https://alpha.example/"),
				"beta":  definition("beta", "https://beta.example/"),
			},
			v12: map[string]string{
				"alpha": definition("alpha", "https://alpha.v12.example/"),
			},
		}
		served[head.sha] = head
		Expect(update.Ensure(context.Background(), client)).To(Succeed())
		load := func(file string) *cardigann.Definition {
			GinkgoHelper()
			raw, err := definitions.Raw(file)
			Expect(err).NotTo(HaveOccurred())
			d, err := definitions.Decode(raw)
			Expect(err).NotTo(HaveOccurred())
			return d
		}
		a := load("alpha")
		Expect(a.Links).To(Equal([]string{"https://alpha.v12.example/"}))
		Expect(a.Source.Path).To(Equal("definitions/v12/alpha.yml"))
		Expect(load("beta").Source.Path).To(Equal("definitions/v11/beta.yml"))
	})
})

func readCloser(b []byte) *readCloserT { return &readCloserT{bytes.NewReader(b)} }

type readCloserT struct{ *bytes.Reader }

func (readCloserT) Close() error { return nil }
