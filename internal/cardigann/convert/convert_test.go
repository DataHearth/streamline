package convert

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/cardigann"
)

const fixture = `---
id: example
name: Example
description: "An example tracker"
language: en-US
type: private
encoding: UTF-8
links:
  - https://example.org/
caps:
  categorymappings:
    - {id: 1, cat: Movies/HD, desc: "Movies"}
    - {id: 2, cat: TV, desc: "TV"}
  modes:
    search: [q]
    movie-search: [q, imdbid]
settings:
  - name: username
    type: text
    label: Username
  - name: password
    type: password
    label: Password
  - name: cookie
    type: text
    label: Cookie
  - name: 2facode
    type: text
    label: 2FA code
  - name: sort
    type: select
    label: Sort
    default: 2
    options:
      2: created
      1: title
login:
  method: form
  path: login.php
  inputs:
    username: "{{ .Config.username }}"
    code: "{{ .Config.2facode }}"
search:
  paths:
    - path: browse.php
  inputs:
    search: "{{ .Keywords }}"
  rows:
    selector: "table > tr"
  fields:
    title:
      selector: "a[href^=\"\/details\"]"
      filters:
        - name: re_replace
          args: ["(?<=\\d)\\s+(?=\\d)", ""]
    date:
      selector: td.date
      filters:
        - name: dateparse
          args: "yyyy-MM-dd HH:mm"
    size:
      selector: td.size
    seeders:
      text: 1
`

var _ = Describe("Convert", Label("unit", "cardigann"), func() {
	var d *cardigann.Definition

	BeforeEach(func() {
		var err error
		d, err = Convert("definitions/v11/example.yml", []byte(fixture))
		Expect(err).NotTo(HaveOccurred())
	})

	It("keeps fields in declaration order", func() {
		keys := make([]string, 0, len(d.Search.Fields))
		for _, f := range d.Search.Fields {
			keys = append(keys, f.Key)
		}
		Expect(keys).To(Equal([]string{"title", "date", "size", "seeders"}))
	})

	It("reads a YAML 1.2 \\/ escape as a slash", func() {
		title, _ := d.Search.Fields.Get("title")
		Expect(title.Selector).To(Equal(`a[href^="/details"]`))
	})

	It(
		"tags a lookaround pattern for regexp2 and says so on the definition",
		func() {
			title, _ := d.Search.Fields.Get("title")
			Expect(title.Filters[0].Engine).To(Equal(cardigann.RegexEngineNET))
			Expect(d.RegexNET).To(BeTrue())
		},
	)

	It("converts date layouts", func() {
		date, _ := d.Search.Fields.Get("date")
		Expect(date.Filters[0].Args).To(Equal(cardigann.StrList{"2006-01-02 15:04"}))
	})

	It("rewrites lookups Go's template lexer cannot read", func() {
		code, _ := d.Login.Inputs.Get("code")
		Expect(string(code)).To(Equal(`{{ (index .Config "2facode") }}`))
	})

	It("flags credentials typed as plain text as secrets", func() {
		secret := map[string]bool{}
		for _, s := range d.Settings {
			secret[s.Name] = s.Secret
		}
		Expect(secret).To(Equal(map[string]bool{
			"username": false, "password": true, "cookie": true,
			"2facode": true, "sort": false,
		}))
	})

	It("keeps select options in order and scalars as text", func() {
		sort := d.Settings[4]
		Expect(*sort.Default).To(Equal(cardigann.Str("2")))
		Expect(sort.Options[0].Key).To(Equal("2"))
		Expect(sort.Options[1].Key).To(Equal("1"))
	})

	It("records provenance and the change notice", func() {
		Expect(d.Source.Path).To(Equal("definitions/v11/example.yml"))
		Expect(d.Source.SHA256).To(HaveLen(64))
		Expect(d.Source.Modified).To(Equal(Modified))
	})

	It("summarises for the catalog", func() {
		s := cardigann.Summarize("example", d)
		Expect(s.File).To(Equal("example"))
		Expect(s.Login).To(Equal("form"))
		Expect(s.Categories).To(Equal([]string{"Movies", "TV"}))
		Expect(s.MovieSearch).To(Equal([]string{"q", "imdbid"}))
		Expect(s.RegexNET).To(BeTrue())
	})

	It("defaults a login block without a method to form, as upstream does", func() {
		src := strings.Replace(fixture, "  method: form\n", "", 1)
		d, err := Convert("x.yml", []byte(src))
		Expect(err).NotTo(HaveOccurred())
		Expect(d.Login.Method).To(Equal("form"))
	})

	It("keeps upstream's non-zero defaults distinguishable from absent", func() {
		Expect(d.TestLinkTorrent).To(BeNil()) // absent: upstream reads true
		src := strings.Replace(
			fixture,
			"  rows:\n",
			"  paths:\n    - path: api\n      response: {type: json, noResultsMessage: \"\"}\n  rows:\n",
			1,
		)
		src = strings.Replace(src, "  paths:\n    - path: browse.php\n", "", 1)
		e, err := Convert("x.yml", []byte(src))
		Expect(err).NotTo(HaveOccurred())
		Expect(e.Search.Paths[0].Response.NoResultsMessage).To(HaveValue(BeEmpty()))
	})

	It("refuses a delay JSON cannot spell", func() {
		src := strings.Replace(
			fixture,
			"encoding: UTF-8\n",
			"encoding: UTF-8\nrequestDelay: .inf\n",
			1,
		)
		_, err := Convert("x.yml", []byte(src))
		Expect(err).To(MatchError(ContainSubstring("not a finite number")))
	})

	It("produces definitions Check accepts, and Check catches a broken one", func() {
		Expect(Check(d)).To(Succeed())
		code, _ := d.Login.Inputs.Get("code")
		d.Login.Inputs[1].Value = code + `{{ re_replace .Keywords "(" "" }}`
		Expect(Check(d)).To(MatchError(ContainSubstring("Login.Inputs")))
	})

	It("applies the defaults Prowlarr fills in when it loads a definition", func() {
		src := strings.Replace(
			fixture,
			"  paths:\n    - path: browse.php\n",
			"  path: browse.php\n  headers: {Authorization: [\"Bearer {{ .Config.cookie }}\"]}\n",
			1,
		)
		src = strings.Replace(src, "encoding: UTF-8\n", "", 1)
		src = strings.Replace(
			src,
			"    size:\n",
			"    poster:\n      selector: img\n    size:\n",
			1,
		)
		e, err := Convert("x.yml", []byte(src))
		Expect(err).NotTo(HaveOccurred())
		Expect(e.Encoding).To(Equal("UTF-8"))
		Expect(e.Search.Path).To(BeEmpty())
		Expect(e.Search.Paths).To(HaveLen(1))
		Expect(e.Search.Paths[0].Path).To(Equal("browse.php"))
		Expect(e.Search.Paths[0].InheritInputs).To(HaveValue(BeTrue()))
		Expect(e.Login.Headers).To(Equal(e.Search.Headers))
		poster, _ := e.Search.Fields.Get("poster")
		Expect(poster.Optional).To(BeTrue())
		title, _ := e.Search.Fields.Get("title")
		Expect(title.Optional).To(BeFalse())
	})

	It(
		"gives a definition without settings Prowlarr's username and password",
		func() {
			before, rest, _ := strings.Cut(fixture, "settings:\n")
			_, after, _ := strings.Cut(rest, "login:\n")
			e, err := Convert("x.yml", []byte(before+"login:\n"+after))
			Expect(err).NotTo(HaveOccurred())
			Expect(e.Settings).To(HaveLen(2))
			Expect(e.Settings[1].Name).To(Equal("password"))
			Expect(e.Settings[1].Secret).To(BeTrue())
		},
	)

	It("has Check refuse a template reading an undeclared setting or field", func() {
		d.Search.Inputs[0].Value = "{{ .Config.nosuch }}"
		Expect(
			Check(d),
		).To(MatchError(ContainSubstring("undeclared .Config.nosuch")))
		d.Search.Inputs[0].Value = `{{ (index .Result "no-such") }}`
		Expect(
			Check(d),
		).To(MatchError(ContainSubstring(`undeclared .Result.no-such`)))
		d.Search.Inputs[0].Value = "{{ .Config.sitelink }}{{ .Result.title }}"
		Expect(Check(d)).To(Succeed())
	})

	It("rejects a key the model does not know", func() {
		src := strings.Replace(fixture, "  rows:\n", "  bogus: 1\n  rows:\n", 1)
		_, err := Convert("x.yml", []byte(src))
		Expect(err).To(MatchError(ContainSubstring(`unknown key "bogus" at search`)))
	})

	It("rejects a filter the engine has no implementation for", func() {
		src := strings.Replace(fixture, "name: dateparse", "name: frobnicate", 1)
		_, err := Convert("x.yml", []byte(src))
		Expect(err).To(MatchError(ContainSubstring(`unknown filter "frobnicate"`)))
	})
})
