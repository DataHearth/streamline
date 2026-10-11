package update_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/cardigann/definitions"
	"github.com/datahearth/streamline/internal/cardigann/update"
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

type commit struct {
	sha, date string
	files     map[string]string
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
		dir      string
		head     commit
		archives int
		client   *http.Client
		inUse    = func(f string) bool { return f == "alpha" }
	)

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		archives = 0
		client = &http.Client{Transport: roundTripFunc(
			func(r *http.Request) (*http.Response, error) {
				var body []byte
				switch {
				case r.URL.Host == "api.github.com":
					body = fmt.Appendf(nil,
						`{"sha":%q,"commit":{"committer":{"date":%q}}}`,
						head.sha, head.date)
				case r.URL.Host == "codeload.github.com" &&
					strings.HasSuffix(r.URL.Path, "/"+head.sha):
					archives++
					body = tarball(head)
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

	run := func(c commit) update.Result {
		GinkgoHelper()
		head = c
		res, err := update.Run(context.Background(), dir, client, inUse)
		Expect(err).NotTo(HaveOccurred())
		return res
	}
	sha := func(n int) string { return strings.Repeat(fmt.Sprint(n), 40) }

	It(
		"replaces the snapshot, holds a configured tracker's domain change for approval",
		func() {
			res := run(commit{sha(1), "2030-01-01T00:00:00Z", map[string]string{
				"alpha": definition("alpha", "https://alpha.example/"),
				"beta":  definition("beta", "https://beta.example/"),
			}})
			Expect(res.Added).To(ConsistOf("alpha", "beta"))
			cat, err := definitions.Catalog(dir)
			Expect(err).NotTo(HaveOccurred())
			Expect(cat.Upstream.Commit).To(Equal(sha(1)))
			Expect(cat.Definitions).To(HaveLen(2))

			// Up to date: nothing is fetched.
			Expect(run(head).Current).To(BeTrue())
			Expect(archives).To(Equal(1))

			res = run(commit{sha(2), "2030-01-02T00:00:00Z", map[string]string{
				"alpha": definition("alpha", "https://alpha.evil/"),
				"beta":  definition("beta", "https://beta.moved/"),
			}})
			Expect(res.Held).To(ConsistOf("alpha"))
			Expect(res.Changed).To(ConsistOf("beta"))

			a, err := definitions.Load(dir, "alpha")
			Expect(err).NotTo(HaveOccurred())
			Expect(a.Links).To(Equal([]string{"https://alpha.example/"}))
			b, err := definitions.Load(dir, "beta")
			Expect(err).NotTo(HaveOccurred())
			Expect(b.Links).To(Equal([]string{"https://beta.moved/"}))
			notice, err := os.ReadFile(
				filepath.Join(definitions.Current(dir), "NOTICE"),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(notice)).To(ContainSubstring("approval: alpha."))

			pending, err := update.List(dir)
			Expect(err).NotTo(HaveOccurred())
			Expect(pending).To(HaveLen(1))
			Expect(pending[0].Reasons).To(ContainElements(
				"new domains: alpha.evil", "dropped domains: alpha.example",
			))
			diff, err := update.Diff(dir, "alpha")
			Expect(err).NotTo(HaveOccurred())
			Expect(diff).To(ContainSubstring(`-    "https://alpha.example/"`))
			Expect(diff).To(ContainSubstring(`+    "https://alpha.evil/"`))

			Expect(update.Approve(dir, "alpha")).To(Succeed())
			a, err = definitions.Load(dir, "alpha")
			Expect(err).NotTo(HaveOccurred())
			Expect(a.Links).To(Equal([]string{"https://alpha.evil/"}))
			Expect(update.List(dir)).To(BeEmpty())
			cat, err = definitions.Catalog(dir)
			Expect(err).NotTo(HaveOccurred())
			Expect(cat.Held).To(BeEmpty())
			Expect(update.Approve(dir, "alpha")).To(MatchError(update.ErrNoUpdate))
		},
	)

	It("does not propose a version that was turned down again", func() {
		run(commit{sha(1), "2030-01-01T00:00:00Z", map[string]string{
			"alpha": definition("alpha", "https://alpha.example/"),
		}})
		evil := definition("alpha", "https://alpha.evil/")
		run(commit{sha(2), "2030-01-02T00:00:00Z", map[string]string{"alpha": evil}})
		Expect(update.Reject(dir, "alpha")).To(Succeed())
		Expect(update.List(dir)).To(BeEmpty())

		res := run(
			commit{sha(3), "2030-01-03T00:00:00Z", map[string]string{"alpha": evil}},
		)
		Expect(res.Held).To(ConsistOf("alpha"))
		Expect(update.List(dir)).To(BeEmpty())

		run(commit{sha(4), "2030-01-04T00:00:00Z", map[string]string{
			"alpha": definition("alpha", "https://alpha.other/"),
		}})
		Expect(update.List(dir)).To(HaveLen(1))
	})

	It("lets a custom definition override the snapshot's", func() {
		Expect(os.MkdirAll(definitions.Custom(dir), 0o750)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(definitions.Custom(dir), "yts.yml"),
			[]byte(definition("yts", "https://mine.example/")), 0o600)).To(Succeed())
		d, err := definitions.Load(dir, "yts")
		Expect(err).NotTo(HaveOccurred())
		Expect(d.Links).To(Equal([]string{"https://mine.example/"}))
		cat, err := definitions.Catalog(dir)
		Expect(err).NotTo(HaveOccurred())
		for _, s := range cat.Definitions {
			if s.File == "yts" {
				Expect(s.Custom).To(BeTrue())
			}
		}
		// The embedded snapshot still serves everything else.
		_, err = definitions.Load(dir, "nyaasi")
		Expect(err).NotTo(HaveOccurred())
	})
})

func readCloser(b []byte) *readCloserT { return &readCloserT{bytes.NewReader(b)} }

type readCloserT struct{ *bytes.Reader }

func (readCloserT) Close() error { return nil }
