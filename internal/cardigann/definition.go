// Package cardigann models a Cardigann indexer definition — the YAML format
// Jackett and Prowlarr describe a tracker in — after streamline's converter
// has rewritten it for Go (see internal/cardigann/convert).
//
// The types mirror the upstream v11 schema key for key, YAML and JSON alike,
// so a definition reads the same in Prowlarr/Indexers and in our embedded
// copy. Everything that is ordered upstream stays ordered here: fields are
// evaluated in declaration order (a later field reads an earlier one through
// .Result), and a selector's case map is first-match-wins, so both are
// OrderedMap rather than a Go map.
package cardigann

// SchemaVersion is the Prowlarr/Indexers definition version this package
// models. Upstream keeps one directory per version (definitions/v11/...) and
// freezes a version once it falls below MIN_VERSION, so a sync that finds 11
// retired must be ported to the new schema rather than reading stale files.
const SchemaVersion = 11

// Definition fields whose upstream default is not Go's zero value are
// pointers, so "absent" survives into the JSON: TestLinkTorrent defaults to
// true (fetch each non-magnet link and move to the next download selector
// when it is not a bencoded torrent). Defaults Prowlarr fills in when it
// loads a definition — settings, encoding, login method, search paths,
// header fallbacks, optional fields — are already applied by the converter
// (convert.clean), so a converted definition carries them explicitly.
type Definition struct {
	ID              string    `yaml:"id"              json:"id"`
	Replaces        []string  `yaml:"replaces"        json:"replaces,omitempty"`
	Name            string    `yaml:"name"            json:"name"`
	Description     string    `yaml:"description"     json:"description"`
	Language        string    `yaml:"language"        json:"language"`
	Type            string    `yaml:"type"            json:"type"`
	Encoding        string    `yaml:"encoding"        json:"encoding"`
	FollowRedirect  bool      `yaml:"followredirect"  json:"followredirect,omitempty"`
	TestLinkTorrent *bool     `yaml:"testlinktorrent" json:"testlinktorrent,omitempty"`
	RequestDelay    float64   `yaml:"requestDelay"    json:"requestDelay,omitempty"`
	Links           []string  `yaml:"links"           json:"links"`
	LegacyLinks     []string  `yaml:"legacylinks"     json:"legacylinks,omitempty"`
	Certificates    []string  `yaml:"certificates"    json:"certificates,omitempty"`
	Caps            Caps      `yaml:"caps"            json:"caps"`
	Settings        []Setting `yaml:"settings"        json:"settings"`
	Login           *Login    `yaml:"login"           json:"login,omitempty"`
	Search          Search    `yaml:"search"          json:"search"`
	Download        *Download `yaml:"download"        json:"download,omitempty"`

	// Source and RegexNET are written by the converter, never read from
	// upstream YAML. RegexNET says at least one pattern runs on regexp2.
	Source   *Source `yaml:"-" json:"source,omitempty"`
	RegexNET bool    `yaml:"-" json:"regex_net,omitempty"`
}

// Source records where a converted definition came from and that it was
// changed. The GPL asks a modified file to say so; this is that notice, and
// the sha256 is what lets a later sync tell an upstream edit from a no-op.
type Source struct {
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	Modified string `json:"modified"`
}

type Caps struct {
	Categories        OrderedMap[Str]   `yaml:"categories"        json:"categories,omitempty"`
	CategoryMappings  []CategoryMapping `yaml:"categorymappings"  json:"categorymappings,omitempty"`
	Modes             Modes             `yaml:"modes"             json:"modes"`
	AllowRawSearch    bool              `yaml:"allowrawsearch"    json:"allowrawsearch,omitempty"`
	AllowTVSearchIMDb bool              `yaml:"allowtvsearchimdb" json:"allowtvsearchimdb,omitempty"`
}

type CategoryMapping struct {
	ID      Str    `yaml:"id"      json:"id"`
	Cat     string `yaml:"cat"     json:"cat"`
	Desc    string `yaml:"desc"    json:"desc,omitempty"`
	Default bool   `yaml:"default" json:"default,omitempty"`
}

type Modes struct {
	Search      []string `yaml:"search"       json:"search"`
	TVSearch    []string `yaml:"tv-search"    json:"tv-search,omitempty"`
	MovieSearch []string `yaml:"movie-search" json:"movie-search,omitempty"`
	MusicSearch []string `yaml:"music-search" json:"music-search,omitempty"`
	BookSearch  []string `yaml:"book-search"  json:"book-search,omitempty"`
}

type Setting struct {
	Name     string          `yaml:"name"     json:"name"`
	Label    string          `yaml:"label"    json:"label,omitempty"`
	Type     string          `yaml:"type"     json:"type"`
	Default  *Str            `yaml:"default"  json:"default,omitempty"`
	Options  OrderedMap[Str] `yaml:"options"  json:"options,omitempty"`
	Defaults []string        `yaml:"defaults" json:"defaults,omitempty"`

	// Secret is the converter's call, not upstream's: a password-typed setting
	// is obviously one, but cookies, API keys and passkeys are all plain
	// "text" in the schema, so the type alone would leave them in clear.
	Secret bool `yaml:"-" json:"secret,omitempty"`
}

type Login struct {
	Method            string               `yaml:"method"            json:"method,omitempty"`
	Cookies           []string             `yaml:"cookies"           json:"cookies,omitempty"`
	Path              string               `yaml:"path"              json:"path,omitempty"`
	SubmitPath        string               `yaml:"submitpath"        json:"submitpath,omitempty"`
	Form              string               `yaml:"form"              json:"form,omitempty"`
	Captcha           *Captcha             `yaml:"captcha"           json:"captcha,omitempty"`
	Inputs            OrderedMap[Str]      `yaml:"inputs"            json:"inputs,omitempty"`
	Selectors         bool                 `yaml:"selectors"         json:"selectors,omitempty"`
	SelectorInputs    OrderedMap[Selector] `yaml:"selectorinputs"    json:"selectorinputs,omitempty"`
	GetSelectorInputs OrderedMap[Selector] `yaml:"getselectorinputs" json:"getselectorinputs,omitempty"`
	Error             []ErrorBlock         `yaml:"error"             json:"error,omitempty"`
	Test              *PageTest            `yaml:"test"              json:"test,omitempty"`
	Headers           OrderedMap[StrList]  `yaml:"headers"           json:"headers,omitempty"`
}

type Captcha struct {
	Type     string `yaml:"type"     json:"type"`
	Selector string `yaml:"selector" json:"selector"`
	Input    string `yaml:"input"    json:"input"`
}

type PageTest struct {
	Path     string `yaml:"path"     json:"path"`
	Selector string `yaml:"selector" json:"selector,omitempty"`
}

type ErrorBlock struct {
	Path     string    `yaml:"path"     json:"path,omitempty"`
	Selector string    `yaml:"selector" json:"selector"`
	Message  *Selector `yaml:"message"  json:"message,omitempty"`
}

// Selector is upstream's SelectorBlock: where a value lives on the page and
// how to clean it. Text and Default are pointers because "set to empty" and
// "not set" differ — a field with text: "" yields an empty value, one without
// text reads its selector.
type Selector struct {
	Selector  string          `yaml:"selector"  json:"selector,omitempty"`
	Attribute string          `yaml:"attribute" json:"attribute,omitempty"`
	Optional  bool            `yaml:"optional"  json:"optional,omitempty"`
	Default   *Str            `yaml:"default"   json:"default,omitempty"`
	Case      OrderedMap[Str] `yaml:"case"      json:"case,omitempty"`
	Remove    string          `yaml:"remove"    json:"remove,omitempty"`
	Text      *Str            `yaml:"text"      json:"text,omitempty"`
	Filters   []Filter        `yaml:"filters"   json:"filters,omitempty"`
}

type Search struct {
	Path                 string               `yaml:"path"                 json:"path,omitempty"`
	Paths                []SearchPath         `yaml:"paths"                json:"paths,omitempty"`
	AllowEmptyInputs     bool                 `yaml:"allowEmptyInputs"     json:"allowEmptyInputs,omitempty"`
	Inputs               OrderedMap[Str]      `yaml:"inputs"               json:"inputs,omitempty"`
	Headers              OrderedMap[StrList]  `yaml:"headers"              json:"headers,omitempty"`
	KeywordsFilters      []Filter             `yaml:"keywordsfilters"      json:"keywordsfilters,omitempty"`
	Error                []ErrorBlock         `yaml:"error"                json:"error,omitempty"`
	PreprocessingFilters []Filter             `yaml:"preprocessingfilters" json:"preprocessingfilters,omitempty"`
	Rows                 Rows                 `yaml:"rows"                 json:"rows"`
	Fields               OrderedMap[Selector] `yaml:"fields"               json:"fields"`
}

// SearchPath leaves InheritInputs a pointer: unset means true, which a plain
// bool would flatten into an explicit false. FollowRedirect is plain on
// purpose — both upstream engines default a path to not following redirects,
// whatever the definition-level followredirect says (that one only governs
// the form login's landing page). An unfollowed redirect on a search is
// upstream's cue to log in again.
type SearchPath struct {
	Path           string          `yaml:"path"           json:"path"`
	Method         string          `yaml:"method"         json:"method,omitempty"`
	FollowRedirect bool            `yaml:"followredirect" json:"followredirect,omitempty"`
	Categories     StrList         `yaml:"categories"     json:"categories,omitempty"`
	Inputs         OrderedMap[Str] `yaml:"inputs"         json:"inputs,omitempty"`
	InheritInputs  *bool           `yaml:"inheritinputs"  json:"inheritinputs,omitempty"`
	QuerySeparator string          `yaml:"queryseparator" json:"queryseparator,omitempty"`
	Response       *Response       `yaml:"response"       json:"response,omitempty"`
}

// Response keeps NoResultsMessage a pointer: set — even to "" — it is the
// body upstream reads as "no results" instead of a parse failure, and an
// empty body matches an empty message.
type Response struct {
	Type             string  `yaml:"type"             json:"type"`
	NoResultsMessage *string `yaml:"noResultsMessage" json:"noResultsMessage,omitempty"`
}

type Rows struct {
	After                           int             `yaml:"after"                           json:"after,omitempty"`
	DateHeaders                     *Selector       `yaml:"dateheaders"                     json:"dateheaders,omitempty"`
	Selector                        string          `yaml:"selector"                        json:"selector,omitempty"`
	Attribute                       string          `yaml:"attribute"                       json:"attribute,omitempty"`
	Optional                        bool            `yaml:"optional"                        json:"optional,omitempty"`
	Multiple                        bool            `yaml:"multiple"                        json:"multiple,omitempty"`
	MissingAttributeEqualsNoResults bool            `yaml:"missingAttributeEqualsNoResults" json:"missingAttributeEqualsNoResults,omitempty"`
	Case                            OrderedMap[Str] `yaml:"case"                            json:"case,omitempty"`
	Remove                          string          `yaml:"remove"                          json:"remove,omitempty"`
	Text                            *Str            `yaml:"text"                            json:"text,omitempty"`
	Filters                         []Filter        `yaml:"filters"                         json:"filters,omitempty"`
	Count                           *Selector       `yaml:"count"                           json:"count,omitempty"`
}

type Download struct {
	Method    string              `yaml:"method"    json:"method,omitempty"`
	Before    *Before             `yaml:"before"    json:"before,omitempty"`
	Selectors []Selector          `yaml:"selectors" json:"selectors,omitempty"`
	InfoHash  *InfoHash           `yaml:"infohash"  json:"infohash,omitempty"`
	Headers   OrderedMap[StrList] `yaml:"headers"   json:"headers,omitempty"`
}

type Before struct {
	Path           string          `yaml:"path"           json:"path,omitempty"`
	PathSelector   *SelectorField  `yaml:"pathselector"   json:"pathselector,omitempty"`
	Method         string          `yaml:"method"         json:"method,omitempty"`
	Inputs         OrderedMap[Str] `yaml:"inputs"         json:"inputs,omitempty"`
	QuerySeparator string          `yaml:"queryseparator" json:"queryseparator,omitempty"`
}

type InfoHash struct {
	Hash              *SelectorField `yaml:"hash"              json:"hash,omitempty"`
	Title             *SelectorField `yaml:"title"             json:"title,omitempty"`
	UseBeforeResponse bool           `yaml:"usebeforeresponse" json:"usebeforeresponse,omitempty"`
}

type SelectorField struct {
	Selector          string   `yaml:"selector"          json:"selector,omitempty"`
	Attribute         string   `yaml:"attribute"         json:"attribute,omitempty"`
	UseBeforeResponse bool     `yaml:"usebeforeresponse" json:"usebeforeresponse,omitempty"`
	Filters           []Filter `yaml:"filters"           json:"filters,omitempty"`
}

// Filter is one step of a value's cleanup chain. Args is normalised to a
// string list whatever shape upstream wrote it in (a bare scalar, an int, a
// list) — the filter decides how to read its own arguments.
type Filter struct {
	Name string  `yaml:"name" json:"name"`
	Args StrList `yaml:"args" json:"args,omitempty"`

	// Engine is set by the converter on regexp and re_replace filters whose
	// pattern Go's regexp cannot compile; see RegexEngineNET.
	Engine string `yaml:"-" json:"engine,omitempty"`
}

// RegexEngineNET marks a pattern for regexp2 in .NET-compatible mode. It is
// chosen at conversion time, never by trial at runtime, so which definitions
// run on the backtracking engine is visible in the embedded data itself.
const RegexEngineNET = "regexp2"
