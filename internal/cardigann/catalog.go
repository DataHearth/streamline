package cardigann

import (
	"slices"
	"strings"
)

// Template function names a converted definition may call, beyond Go's
// text/template builtins. re_replace_net is the converter's rename of a
// re_replace whose pattern needs regexp2, so the engine picks the regex
// engine from the call name rather than by trying both.
const (
	TemplateFuncReplace    = "re_replace"
	TemplateFuncReplaceNET = "re_replace_net"
	TemplateFuncJoin       = "join"
)

// Catalog is the embedded index.json: what was converted, from which
// upstream revision, and what was left out and why.
type Catalog struct {
	Schema      int       `json:"schema"`
	Converter   int       `json:"converter"`
	Upstream    Upstream  `json:"upstream"`
	Definitions []Summary `json:"definitions"`
	Skipped     []Skipped `json:"skipped,omitempty"`
}

type Upstream struct {
	Repository string `json:"repository"`
	Commit     string `json:"commit"`
	Date       string `json:"date"`
}

// Summary is what a catalog browser needs about a definition without loading
// it: enough to filter by kind, login burden and what it can search.
type Summary struct {
	// File is the definition's upstream file name without .yml — the key
	// Load takes, and what Prowlarr itself stores an indexer under. It is not
	// always the id: bluebird.yml holds bluebirdhd.
	File        string   `json:"file"`
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Language    string   `json:"language"`
	Type        string   `json:"type"`
	Links       []string `json:"links"`
	Replaces    []string `json:"replaces,omitempty"`
	// Login is the login method, or "none" for a definition without a login
	// block.
	Login string `json:"login"`
	// Captcha means the tracker's own login form carries an image captcha a
	// person has to read.
	Captcha bool `json:"captcha,omitempty"`
	// Challenge means the site sits behind a browser challenge (Cloudflare and
	// the like) that needs a FlareSolverr-compatible solver.
	Challenge   bool     `json:"challenge,omitempty"`
	Categories  []string `json:"categories"`
	MovieSearch []string `json:"movie_search,omitempty"`
	TVSearch    []string `json:"tv_search,omitempty"`
	RegexNET    bool     `json:"regex_net,omitempty"`
}

type Skipped struct {
	File   string `json:"file"`
	Reason string `json:"reason"`
}

// Summarize builds a definition's catalog row; file is its upstream file
// name without the extension.
func Summarize(file string, d *Definition) Summary {
	s := Summary{
		File:        file,
		ID:          d.ID,
		Name:        d.Name,
		Description: d.Description,
		Language:    d.Language,
		Type:        d.Type,
		Links:       d.Links,
		Replaces:    d.Replaces,
		Login:       "none",
		MovieSearch: d.Caps.Modes.MovieSearch,
		TVSearch:    d.Caps.Modes.TVSearch,
		RegexNET:    d.RegexNET,
	}
	if d.Login != nil {
		s.Login = d.Login.Method
		s.Captcha = d.Login.Captcha != nil
	}
	for _, st := range d.Settings {
		if st.Type == "info_flaresolverr" {
			s.Challenge = true
		}
	}
	for _, m := range d.Caps.CategoryMappings {
		s.Categories = append(s.Categories, topCategory(m.Cat))
	}
	for _, e := range d.Caps.Categories {
		s.Categories = append(s.Categories, topCategory(string(e.Value)))
	}
	slices.Sort(s.Categories)
	s.Categories = slices.Compact(s.Categories)
	if len(s.Categories) > 0 && s.Categories[0] == "" {
		s.Categories = s.Categories[1:]
	}
	return s
}

func topCategory(cat string) string {
	top, _, _ := strings.Cut(cat, "/")
	return top
}
