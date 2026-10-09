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

// Summarize builds a definition's catalog row.
func Summarize(d *Definition) Summary {
	s := Summary{
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
	seen := map[string]bool{}
	add := func(cat string) {
		top, _, _ := strings.Cut(cat, "/")
		if top != "" && !seen[top] {
			seen[top] = true
			s.Categories = append(s.Categories, top)
		}
	}
	for _, m := range d.Caps.CategoryMappings {
		add(m.Cat)
	}
	for _, e := range d.Caps.Categories {
		add(string(e.Value))
	}
	slices.Sort(s.Categories)
	return s
}
