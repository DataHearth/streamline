package engine

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/datahearth/streamline/internal/cardigann"
)

// Kind is the search a query is: it picks the definition's search mode and
// the ids that mode may carry.
type Kind uint8

const (
	KindSearch Kind = iota
	KindMovie
	KindTV
	KindMusic
	KindBook
)

// Query is one search. Ids the definition's mode for the kind does not list
// are not sent; when one that it does is, the term is left out, as the *arr
// clients leave it out of an id search.
type Query struct {
	Kind    Kind
	Term    string
	Season  int
	Episode int
	// IMDbID is the full "tt" form.
	IMDbID     string
	TMDBID     int
	TVDBID     int
	Categories []int
}

// Release is one result row.
type Release struct {
	Title     string
	Details   string
	Download  string
	Magnet    string
	InfoHash  string
	Size      int64
	Seeders   int64
	Leechers  int64
	Category  []int
	Published time.Time
	IMDbID    int64
	TMDBID    int64
	TVDBID    int64
}

func (k Kind) mode(m cardigann.Modes) (string, []string) {
	switch k {
	case KindMovie:
		if m.MovieSearch != nil {
			return "movie", m.MovieSearch
		}
	case KindTV:
		if m.TVSearch != nil {
			return "tvsearch", m.TVSearch
		}
	case KindMusic:
		if m.MusicSearch != nil {
			return "music", m.MusicSearch
		}
	case KindBook:
		if m.BookSearch != nil {
			return "book", m.BookSearch
		}
	default:
	}
	return "search", m.Search
}

// queryVars builds the .Query map (GetQueryVariableDefaults plus the
// criteria's own keys): every key Prowlarr knows is present, so a template
// reading one this search does not fill gets "".
func (e *Engine) queryVars(q Query) map[string]string {
	qv := map[string]string{}
	for _, k := range []string{
		"Type", "Q", "Limit", "Offset", "Extended", "APIKey", "Genre",
		"Movie", "Year", "IMDBID", "IMDBIDShort", "TMDBID",
		"Series", "Ep", "Season", "TVDBID", "TVRageID", "TVMazeID",
		"TraktID", "DoubanID", "Episode",
		"Album", "Artist", "Label", "Track",
		"Author", "Title", "Publisher", "Keywords",
	} {
		qv[k] = ""
	}
	typ, params := q.Kind.mode(e.def.Caps.Modes)
	qv["Type"] = typ
	supports := func(p string) bool { return slices.Contains(params, p) }

	idSearch := false
	if typ == "movie" || typ == "tvsearch" {
		if imdb := imdbNumber(q.IMDbID); imdb > 0 && supports("imdbid") {
			qv["IMDBID"] = fmt.Sprintf("tt%07d", imdb)
			qv["IMDBIDShort"] = strconv.FormatInt(imdb, 10)
			idSearch = true
		}
		if q.TMDBID > 0 && supports("tmdbid") {
			qv["TMDBID"] = strconv.Itoa(q.TMDBID)
			idSearch = true
		}
	}
	if typ == "tvsearch" {
		if q.TVDBID > 0 && supports("tvdbid") {
			qv["TVDBID"] = strconv.Itoa(q.TVDBID)
			idSearch = true
		}
		if q.Season > 0 && supports("season") {
			qv["Season"] = strconv.Itoa(q.Season)
			qv["Episode"] = fmt.Sprintf("S%02d", q.Season)
			if q.Episode > 0 && supports("ep") {
				qv["Ep"] = strconv.Itoa(q.Episode)
				qv["Episode"] += fmt.Sprintf("E%02d", q.Episode)
			}
		}
	}
	if !idSearch {
		qv["Q"] = q.Term
	}
	return qv
}

// imdbNumber reads an IMDb id with or without its "tt"; 0 when it is not one.
func imdbNumber(id string) int64 {
	n, err := strconv.ParseInt(
		strings.TrimPrefix(strings.TrimSpace(id), "tt"),
		10,
		64,
	)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// Search runs every search path the query's categories select and returns
// what their responses hold.
func (e *Engine) Search(ctx context.Context, q Query) ([]Release, error) {
	v := e.baseVars()
	v.query = e.queryVars(q)

	var tokens []string
	for _, k := range []string{"Q", "Series", "Movie", "Year", "Episode"} {
		if s := strings.TrimSpace(v.query[k]); s != "" {
			tokens = append(tokens, v.query[k])
		}
	}
	v.query["Keywords"] = strings.Join(tokens, " ")
	kw, err := e.applyFilters(
		v.query["Keywords"], e.def.Search.KeywordsFilters, v,
	)
	if err != nil {
		return nil, fmt.Errorf("keywords: %w", err)
	}
	v.keywords = kw

	mapped := e.cats.trackers(q.Categories)
	var (
		out  []Release
		seen []string
	)
	for i, path := range e.def.Search.Paths {
		pv := v.clone()
		pv.categories = mapped
		if len(path.Categories) > 0 && len(mapped) > 0 {
			hit := slices.ContainsFunc(mapped, func(c string) bool {
				return slices.Contains(path.Categories, c)
			})
			if path.Categories[0] == "!" {
				hit = !hit
			}
			if !hit {
				continue
			}
			pv.categories = slices.DeleteFunc(
				slices.Clone(mapped),
				func(c string) bool {
					return !slices.Contains(path.Categories, c)
				},
			)
		}
		req, err := e.searchRequest(path, pv)
		if err != nil {
			return nil, fmt.Errorf("search path %d: %w", i, err)
		}
		if req.method == http.MethodGet {
			if slices.Contains(seen, req.url) {
				continue
			}
			seen = append(seen, req.url)
		}
		resp, err := e.do(ctx, req)
		if err != nil {
			return nil, err
		}
		releases, err := e.parse(ctx, resp, path, pv)
		if err != nil {
			return nil, fmt.Errorf("search path %d: %w", i, err)
		}
		out = append(out, releases...)
	}
	if e.andMatch() {
		out = andMatch(out, v.query["Q"])
	}
	return out, nil
}

func (e *Engine) searchRequest(
	path cardigann.SearchPath,
	v *vars,
) (request, error) {
	rendered, err := render(path.Path, v, urlEncoded)
	if err != nil {
		return request{}, err
	}
	u, err := e.resolve(strings.ReplaceAll(rendered, "+", "%20"), nil)
	if err != nil {
		return request{}, err
	}
	method := http.MethodGet
	if strings.EqualFold(path.Method, "post") {
		method = http.MethodPost
	}

	var inputs []cardigann.OrderedMap[cardigann.Str]
	if path.InheritInputs == nil || *path.InheritInputs {
		inputs = append(inputs, e.def.Search.Inputs)
	}
	inputs = append(inputs, path.Inputs)

	var pairs [][2]string
	for _, block := range inputs {
		for _, in := range block {
			if in.Key == "$raw" {
				raw, err := render(string(in.Value), v, urlEncoded)
				if err != nil {
					return request{}, err
				}
				for part := range strings.SplitSeq(raw, "&") {
					key, value, _ := strings.Cut(part, "=")
					if key != "" {
						pairs = append(pairs, [2]string{key, value})
					}
				}
				continue
			}
			value, err := render(string(in.Value), v, plainText)
			if err != nil {
				return request{}, err
			}
			if strings.TrimSpace(value) != "" || e.def.Search.AllowEmptyInputs {
				pairs = append(pairs, [2]string{in.Key, value})
			}
		}
	}

	r := request{method: method, url: u.String(), redirect: path.FollowRedirect}
	if len(pairs) > 0 {
		qs, err := e.queryString(pairs, "")
		if err != nil {
			return request{}, err
		}
		if method == http.MethodPost {
			r.form = qs
		} else {
			// Prowlarr appends "?" unconditionally; a path that already
			// carries a query gets "&" here so the result stays a URL.
			sep := "?"
			if strings.Contains(r.url, "?") {
				sep = "&"
			}
			r.url += sep + qs
		}
	}
	if r.headers, err = e.headers(e.def.Search.Headers, v); err != nil {
		return request{}, err
	}
	return r, nil
}

func (e *Engine) andMatch() bool {
	return slices.ContainsFunc(
		e.def.Search.Rows.Filters,
		func(f cardigann.Filter) bool {
			return f.Name == "andmatch"
		},
	)
}

var (
	nonWordRe   = regexp.MustCompile(`[^\p{L}\p{Nd}_]+`)
	commonWords = []string{"and", "the", "an"}
)

// andMatch keeps the releases whose title holds every word of the term, as
// the andmatch row filter asks (IndexerBase.FilterReleasesByQuery). An id
// search carries no term and keeps everything.
func andMatch(releases []Release, term string) []Release {
	var words []string
	for _, w := range nonWordRe.Split(strings.ToLower(term), -1) {
		if w != "" && !slices.Contains(commonWords, w) {
			words = append(words, w)
		}
	}
	if len(words) == 0 {
		return releases
	}
	return slices.DeleteFunc(releases, func(r Release) bool {
		title := strings.ToLower(r.Title)
		for _, w := range words {
			if !strings.Contains(title, w) {
				return true
			}
		}
		return false
	})
}
