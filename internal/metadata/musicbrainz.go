package metadata

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/internal/buildinfo"
	"github.com/datahearth/streamline/internal/otelx"
)

const (
	mbBaseURL = "https://musicbrainz.org/ws/2"

	// maxMusicReleaseResponse bounds the heavy release call, whose
	// relationship expansion of a large release outgrows maxProviderResponse's
	// intent while staying far below it in practice.
	maxMusicReleaseResponse = 8 << 20

	genreListTTL = 24 * time.Hour

	// mbDefaultRetryAfter is how long a 503 with no Retry-After asks callers
	// to leave MusicBrainz alone.
	mbDefaultRetryAfter = time.Minute

	maxLookupGenres = 5
)

var (
	// ErrNotFound is a provider answering 404 for an id.
	ErrNotFound = errors.New("metadata: not found")

	deezerArtistRe = regexp.MustCompile(`deezer\.com/(?:[a-z]{2}/)?artist/(\d+)`)
	wikidataIDRe   = regexp.MustCompile(`/(Q\d+)$`)
	featJoinRe     = regexp.MustCompile(`(?i)\b(feat\.?|featuring|ft\.?)(\s|$)`)
)

// MusicBrainz is the MusicProvider implementation. No API key; MusicBrainz
// mandates a descriptive User-Agent and at most 1 request/second, shared by
// every caller through one two-class limiter.
type MusicBrainz struct {
	client  *http.Client
	limiter *mbLimiter

	genreMu      sync.Mutex
	genres       map[string]struct{}
	genresLoaded time.Time
}

func NewMusicBrainz() *MusicBrainz {
	c := *otelx.HTTPClient
	return &MusicBrainz{
		client:  &c,
		limiter: newMBLimiter(mbInterval, musicBackgroundGap),
	}
}

func (m *MusicBrainz) userAgent() string {
	version := buildinfo.Version
	if version == "" {
		version = "dev"
	}
	return "streamline/" + version + " (https://github.com/datahearth/streamline)"
}

// fetch waits for a token, issues the GET and hands the body to read. A 503 is
// MusicBrainz enforcing its limit and surfaces as *RateLimitedError; a 404 as
// ErrNotFound.
func (m *MusicBrainz) fetch(
	ctx context.Context,
	path string,
	params url.Values,
	read func(io.Reader) error,
) error {
	ctx, span := tracer.Start(ctx, "metadata.musicbrainz.get",
		trace.WithAttributes(attribute.String("musicbrainz.endpoint", path)))
	defer span.End()

	if err := m.limiter.Wait(ctx); err != nil {
		return otelx.RecordSpanError(span, err)
	}
	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, mbBaseURL+path+"?"+params.Encode(), nil,
	)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	req.Header.Set("User-Agent", m.userAgent())

	resp, err := m.client.Do(req)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	defer resp.Body.Close()

	recordProviderStatus(ctx, resp.StatusCode)
	switch resp.StatusCode {
	case http.StatusOK:
		return otelx.RecordSpanError(span, read(resp.Body))
	case http.StatusNotFound:
		return otelx.RecordSpanError(span,
			fmt.Errorf("musicbrainz: %s: %w", path, ErrNotFound))
	case http.StatusServiceUnavailable, http.StatusTooManyRequests:
		retry := mbDefaultRetryAfter
		if v := resp.Header.Get("Retry-After"); v != "" {
			retry = parseRetryAfter(v, time.Now())
		}
		slog.WarnContext(ctx, "musicbrainz rate limited",
			"musicbrainz.endpoint", path, "http.status_code", resp.StatusCode,
			"retry_after", retry.Round(time.Second))
		return otelx.RecordSpanError(span, &RateLimitedError{RetryAfter: retry})
	}
	slog.WarnContext(ctx, "musicbrainz request non-200",
		"musicbrainz.endpoint", path, "http.status_code", resp.StatusCode)
	return otelx.RecordSpanError(span,
		fmt.Errorf("musicbrainz: %s returned %d", path, resp.StatusCode))
}

func (m *MusicBrainz) getMax(
	ctx context.Context,
	path string,
	params url.Values,
	limit int64,
	out any,
) error {
	params.Set("fmt", "json")
	return m.fetch(ctx, path, params, func(r io.Reader) error {
		return otelx.DecodeJSON(r, limit, out)
	})
}

func (m *MusicBrainz) get(
	ctx context.Context,
	path string,
	params url.Values,
	out any,
) error {
	return m.getMax(ctx, path, params, maxProviderResponse, out)
}

// genreSet is MusicBrainz's controlled genre list, refreshed at most once a
// day. A failed fetch keeps the stale list, or none, and is not an error: a
// search is worth more without genres than not at all.
func (m *MusicBrainz) genreSet(ctx context.Context) map[string]struct{} {
	m.genreMu.Lock()
	defer m.genreMu.Unlock()
	if m.genres != nil && time.Since(m.genresLoaded) < genreListTTL {
		return m.genres
	}
	set := map[string]struct{}{}
	err := m.fetch(ctx, "/genre/all", url.Values{"fmt": {"txt"}},
		func(r io.Reader) error {
			sc := bufio.NewScanner(io.LimitReader(r, maxProviderResponse))
			for sc.Scan() {
				if g := strings.ToLower(strings.TrimSpace(sc.Text())); g != "" {
					set[g] = struct{}{}
				}
			}
			return sc.Err()
		})
	if err != nil {
		slog.WarnContext(ctx, "musicbrainz genre list not fetched", "error", err)
		return m.genres
	}
	m.genres, m.genresLoaded = set, time.Now()
	return set
}

type mbTag struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type mbArea struct {
	Name string `json:"name"`
}

type mbArtist struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	SortName       string   `json:"sort-name"`
	Disambiguation string   `json:"disambiguation"`
	Score          uint8    `json:"score"`
	Type           string   `json:"type"`
	Area           *mbArea  `json:"area"`
	BeginArea      *mbArea  `json:"begin-area"`
	LifeSpan       lifeSpan `json:"life-span"`
	Tags           []mbTag  `json:"tags"`
	Genres         []mbTag  `json:"genres"`
	Relations      []mbRel  `json:"relations"`
}

type lifeSpan struct {
	Begin string `json:"begin"`
	End   string `json:"end"`
	Ended bool   `json:"ended"`
}

func (a mbArtist) toResult() ArtistResult {
	return ArtistResult{
		MBID:           a.ID,
		Name:           a.Name,
		SortName:       a.SortName,
		Disambiguation: a.Disambiguation,
		Score:          a.Score,
		Type:           artistKind(a.Type),
		Area:           joinAreas(a.BeginArea, a.Area),
		Since:          yearOf(a.LifeSpan.Begin),
	}
}

func artistKind(t string) string {
	switch t {
	case "Person":
		return "person"
	case "Group", "Orchestra", "Choir":
		return "group"
	}
	return ""
}

func joinAreas(areas ...*mbArea) string {
	var parts []string
	for _, a := range areas {
		if a != nil && a.Name != "" && !slices.Contains(parts, a.Name) {
			parts = append(parts, a.Name)
		}
	}
	return strings.Join(parts, ", ")
}

// yearOf reads the year of a MusicBrainz date (YYYY, YYYY-MM or YYYY-MM-DD).
func yearOf(date string) uint16 {
	if len(date) < 4 {
		return 0
	}
	y, err := strconv.ParseUint(date[:4], 10, 16)
	if err != nil {
		return 0
	}
	return uint16(y)
}

// topTag is the highest-count tag for which keep is true.
func topTag(tags []mbTag, keep func(string) bool) string {
	best, bestCount := "", -1
	for _, t := range tags {
		if keep(strings.ToLower(t.Name)) && t.Count > bestCount {
			best, bestCount = t.Name, t.Count
		}
	}
	return titleCase(best)
}

func titleCase(s string) string {
	prev := ' '
	return strings.Map(func(r rune) rune {
		out := r
		if unicode.IsSpace(prev) || prev == '-' || prev == '/' {
			out = unicode.ToUpper(r)
		}
		prev = r
		return out
	}, s)
}

func (m *MusicBrainz) SearchArtists(
	ctx context.Context,
	query string,
) ([]ArtistResult, error) {
	var payload struct {
		Artists []mbArtist `json:"artists"`
	}
	params := url.Values{"query": {query}, "limit": {"20"}}
	if err := m.get(ctx, "/artist", params, &payload); err != nil {
		return nil, err
	}
	var genres map[string]struct{}
	for _, a := range payload.Artists {
		if len(a.Tags) > 0 {
			genres = m.genreSet(ctx)
			break
		}
	}
	results := make([]ArtistResult, 0, len(payload.Artists))
	for _, a := range payload.Artists {
		r := a.toResult()
		r.Genre = topTag(a.Tags, func(n string) bool {
			_, ok := genres[n]
			return ok
		})
		results = append(results, r)
	}
	return results, nil
}

type mbReleaseGroup struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	PrimaryType      string   `json:"primary-type"`
	SecondaryTypes   []string `json:"secondary-types"`
	FirstReleaseDate string   `json:"first-release-date"`
}

func (rg mbReleaseGroup) toInfo() ReleaseGroupInfo {
	info := ReleaseGroupInfo{MBID: rg.ID, Title: rg.Title, Type: mapAlbumType(rg)}
	// MusicBrainz dates may be YYYY, YYYY-MM, or YYYY-MM-DD.
	for _, layout := range []string{"2006-01-02", "2006-01", "2006"} {
		if t, err := time.Parse(layout, rg.FirstReleaseDate); err == nil {
			info.ReleaseDate = &t
			break
		}
	}
	return info
}

func luceneQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// luceneEscape backslash-escapes the characters Lucene reads as syntax, so a
// person's free text cannot turn into an operator.
func luceneEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(`+-&|!(){}[]^"~*?:\/`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

const releaseGroupSearchLimit = 20

func (m *MusicBrainz) SearchReleaseGroups(
	ctx context.Context,
	artist, album string,
) ([]ReleaseGroupSearchResult, error) {
	ctx, span := tracer.Start(ctx, "metadata.musicbrainz.search_release_groups",
		trace.WithAttributes(
			attribute.String("musicbrainz.artist", artist),
			attribute.String("musicbrainz.album", album),
		))
	defer span.End()

	q := "releasegroup:" + luceneQuote(album)
	if artist != "" {
		q += " AND artist:" + luceneQuote(artist)
	}
	res, err := m.searchReleaseGroups(ctx, q, 10)
	return res, otelx.RecordSpanError(span, err)
}

// SearchReleaseGroupsFreeText matches the text against release-group titles and
// artist credits in one request, for a person typing into a picker.
func (m *MusicBrainz) SearchReleaseGroupsFreeText(
	ctx context.Context,
	query string,
) ([]ReleaseGroupSearchResult, error) {
	ctx, span := tracer.Start(
		ctx,
		"metadata.musicbrainz.search_release_groups_free_text",
	)
	defer span.End()

	text := luceneEscape(strings.TrimSpace(query))
	q := "releasegroup:(" + text + ") OR artist:(" + text + ")"
	res, err := m.searchReleaseGroups(ctx, q, releaseGroupSearchLimit)
	return res, otelx.RecordSpanError(span, err)
}

func (m *MusicBrainz) searchReleaseGroups(
	ctx context.Context,
	q string,
	limit int,
) ([]ReleaseGroupSearchResult, error) {
	var payload struct {
		ReleaseGroups []struct {
			mbReleaseGroup
			Score        uint8 `json:"score"`
			ArtistCredit []struct {
				Artist struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"artist"`
			} `json:"artist-credit"`
		} `json:"release-groups"`
	}
	params := url.Values{"query": {q}, "limit": {strconv.Itoa(limit)}}
	if err := m.get(ctx, "/release-group", params, &payload); err != nil {
		return nil, err
	}
	results := make([]ReleaseGroupSearchResult, 0, len(payload.ReleaseGroups))
	for _, rg := range payload.ReleaseGroups {
		res := ReleaseGroupSearchResult{
			ReleaseGroupInfo: rg.toInfo(),
			Score:            rg.Score,
		}
		if len(rg.ArtistCredit) > 0 {
			res.ArtistMBID = rg.ArtistCredit[0].Artist.ID
			res.ArtistName = rg.ArtistCredit[0].Artist.Name
		}
		results = append(results, res)
	}
	return results, nil
}

func mapAlbumType(rg mbReleaseGroup) AlbumType {
	for _, s := range rg.SecondaryTypes {
		switch s {
		case "Compilation":
			return AlbumTypeCompilation
		case "Live":
			return AlbumTypeLive
		}
	}
	switch rg.PrimaryType {
	case "Album":
		return AlbumTypeAlbum
	case "EP":
		return AlbumTypeEP
	case "Single":
		return AlbumTypeSingle
	}
	return AlbumTypeOther
}

type mbRel struct {
	Type       string   `json:"type"`
	TargetType string   `json:"target-type"`
	Direction  string   `json:"direction"`
	Attributes []string `json:"attributes"`
	Begin      string   `json:"begin"`
	End        string   `json:"end"`
	Ended      bool     `json:"ended"`
	Artist     *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"artist"`
	URL *struct {
		Resource string `json:"resource"`
	} `json:"url"`
	Place *struct {
		Name string `json:"name"`
	} `json:"place"`
	Work *struct {
		Relations []mbRel `json:"relations"`
	} `json:"work"`
}

func (m *MusicBrainz) GetArtist(
	ctx context.Context,
	mbid string,
) (*ArtistDetails, error) {
	ctx, span := tracer.Start(ctx, "metadata.musicbrainz.get_artist",
		trace.WithAttributes(attribute.String("musicbrainz.mbid", mbid)))
	defer span.End()

	var artist mbArtist
	if err := m.get(
		ctx,
		"/artist/"+url.PathEscape(mbid),
		url.Values{"inc": {"genres+artist-rels+url-rels"}},
		&artist,
	); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	details := &ArtistDetails{
		ArtistResult: artist.toResult(),
		Origin:       joinAreas(artist.BeginArea, artist.Area),
	}
	slices.SortStableFunc(artist.Genres, func(a, b mbTag) int {
		return b.Count - a.Count
	})
	for _, g := range artist.Genres {
		if len(details.Genres) == maxLookupGenres {
			break
		}
		details.Genres = append(details.Genres, titleCase(g.Name))
	}
	if len(details.Genres) > 0 {
		details.Genre = details.Genres[0]
	}
	for _, rel := range artist.Relations {
		switch {
		case rel.Type == "member of band" && rel.Direction == "backward" &&
			rel.Artist != nil:
			details.Members = mergeMember(details.Members, memberOf(rel))
		case rel.URL != nil:
			readURLRel(rel, details)
		}
	}

	for offset := 0; ; {
		var page struct {
			Count         int              `json:"release-group-count"`
			ReleaseGroups []mbReleaseGroup `json:"release-groups"`
		}
		params := url.Values{
			"artist": {mbid},
			"limit":  {"100"},
			"offset": {fmt.Sprint(offset)},
		}
		if err := m.get(ctx, "/release-group", params, &page); err != nil {
			return nil, otelx.RecordSpanError(span, err)
		}
		for _, rg := range page.ReleaseGroups {
			details.ReleaseGroups = append(details.ReleaseGroups, rg.toInfo())
		}
		offset += len(page.ReleaseGroups)
		if offset >= page.Count || len(page.ReleaseGroups) == 0 {
			break
		}
	}
	return details, nil
}

var ignoredMemberAttributes = []string{"original", "additional"}

// mergeMember folds one more membership relation into the list. MusicBrainz files
// a relation per instrument and per stint, so one person arrives several times.
func mergeMember(members []ArtistMemberInfo, m ArtistMemberInfo) []ArtistMemberInfo {
	i := slices.IndexFunc(
		members,
		func(x ArtistMemberInfo) bool { return x.MBID == m.MBID },
	)
	if i < 0 {
		return append(members, m)
	}
	cur := &members[i]
	for _, inst := range m.Instruments {
		if !slices.Contains(cur.Instruments, inst) {
			cur.Instruments = append(cur.Instruments, inst)
		}
	}
	if m.FromYear != 0 && (cur.FromYear == 0 || m.FromYear < cur.FromYear) {
		cur.FromYear = m.FromYear
	}
	if !cur.Ended || !m.Ended {
		cur.Ended, cur.ToYear = false, 0
	} else {
		cur.ToYear = max(cur.ToYear, m.ToYear)
	}
	return members
}

func memberOf(rel mbRel) ArtistMemberInfo {
	mem := ArtistMemberInfo{
		Name:     rel.Artist.Name,
		MBID:     rel.Artist.ID,
		FromYear: yearOf(rel.Begin),
		ToYear:   yearOf(rel.End),
		Ended:    rel.Ended,
	}
	for _, a := range rel.Attributes {
		if !slices.Contains(ignoredMemberAttributes, strings.ToLower(a)) {
			mem.Instruments = append(mem.Instruments, a)
		}
	}
	return mem
}

func readURLRel(rel mbRel, d *ArtistDetails) {
	switch {
	case rel.Type == "wikidata":
		if m := wikidataIDRe.FindStringSubmatch(rel.URL.Resource); m != nil {
			d.WikidataID = m[1]
		}
	case d.DeezerID == 0:
		if m := deezerArtistRe.FindStringSubmatch(rel.URL.Resource); m != nil {
			if id, err := strconv.ParseUint(m[1], 10, 32); err == nil {
				d.DeezerID = uint32(id)
			}
		}
	}
}

type mbRelease struct {
	ID        string          `json:"id"`
	Status    string          `json:"status"`
	Date      string          `json:"date"`
	Barcode   string          `json:"barcode"`
	Country   string          `json:"country"`
	LabelInfo []mbLabelInfo   `json:"label-info"`
	Media     []mbMedium      `json:"media"`
	Relations []mbRel         `json:"relations"`
	RG        *mbReleaseGroup `json:"release-group"`
}

type mbLabelInfo struct {
	CatalogNumber string `json:"catalog-number"`
	Label         *struct {
		Name string `json:"name"`
	} `json:"label"`
}

type mbMedium struct {
	Position uint8     `json:"position"`
	Title    string    `json:"title"`
	Format   string    `json:"format"`
	Tracks   []mbTrack `json:"tracks"`
}

type mbTrack struct {
	Position     uint16     `json:"position"`
	Title        string     `json:"title"`
	Length       uint32     `json:"length"`
	Recording    mbRecord   `json:"recording"`
	ArtistCredit []mbCredit `json:"artist-credit"`
}

type mbRecord struct {
	ID        string  `json:"id"`
	Relations []mbRel `json:"relations"`
}

type mbCredit struct {
	Name       string `json:"name"`
	JoinPhrase string `json:"joinphrase"`
	Artist     struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"artist"`
}

// canonicalRelease is the earliest official release; an undated one ranks
// after every dated one (ISO dates sort lexically, so "" would otherwise win).
// Falls back to the first listed release when none is official.
func canonicalRelease(releases []mbRelease) string {
	best, bestDate := "", ""
	for _, r := range releases {
		if r.Status != "Official" {
			continue
		}
		date := r.Date
		if date == "" {
			date = "9999"
		}
		if best == "" || date < bestDate {
			best, bestDate = r.ID, date
		}
	}
	if best == "" && len(releases) > 0 {
		return releases[0].ID
	}
	return best
}

const (
	heavyReleaseInc = "recordings+artist-credits+labels+artist-rels+" +
		"recording-level-rels+work-rels+work-level-rels+place-rels"
	lightReleaseInc = "recordings+artist-credits+labels"
)

func (m *MusicBrainz) GetReleaseGroup(
	ctx context.Context,
	mbid string,
) (*ReleaseGroupDetails, error) {
	ctx, span := tracer.Start(ctx, "metadata.musicbrainz.get_release_group",
		trace.WithAttributes(attribute.String("musicbrainz.mbid", mbid)))
	defer span.End()

	var rg struct {
		mbReleaseGroup
		Releases     []mbRelease `json:"releases"`
		ArtistCredit []mbCredit  `json:"artist-credit"`
	}
	if err := m.get(ctx, "/release-group/"+url.PathEscape(mbid),
		url.Values{"inc": {"releases+media+artist-credits"}}, &rg); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	details := &ReleaseGroupDetails{
		ReleaseGroupInfo: rg.toInfo(),
		ReleaseMBID:      canonicalRelease(rg.Releases),
	}
	if len(rg.ArtistCredit) > 0 {
		details.ArtistMBID = rg.ArtistCredit[0].Artist.ID
		details.ArtistName = rg.ArtistCredit[0].Artist.Name
	}
	var formats []string
	for _, r := range rg.Releases {
		for _, md := range r.Media {
			formats = append(formats, md.Format)
		}
	}
	if details.ReleaseMBID == "" {
		details.Media = mapMedia(formats)
		return details, nil
	}

	rel, complete, err := m.getRelease(ctx, details.ReleaseMBID)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	if len(formats) == 0 {
		for _, md := range rel.Media {
			formats = append(formats, md.Format)
		}
	}
	details.Media = mapMedia(formats)
	details.CreditsComplete = complete
	fillRelease(details, rel, complete)
	return details, nil
}

// getRelease asks for the heavy relationship expansion and, when that fails
// for any reason but a rate limit or a missing release, retries once with the
// light call: a large release can 503 or time out under the expansion, and its
// tracks are worth having without its credits.
func (m *MusicBrainz) getRelease(
	ctx context.Context,
	releaseMBID string,
) (mbRelease, bool, error) {
	path := "/release/" + url.PathEscape(releaseMBID)
	var rel mbRelease
	err := m.getMax(ctx, path, url.Values{"inc": {heavyReleaseInc}},
		maxMusicReleaseResponse, &rel)
	if err == nil {
		return rel, true, nil
	}
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrRateLimited) ||
		ctx.Err() != nil {
		return mbRelease{}, false, err
	}
	slog.WarnContext(ctx, "musicbrainz heavy release call failed, retrying light",
		"musicbrainz.release", releaseMBID, "error", err)
	rel = mbRelease{}
	if lerr := m.getMax(ctx, path, url.Values{"inc": {lightReleaseInc}},
		maxMusicReleaseResponse, &rel); lerr != nil {
		return mbRelease{}, false, lerr
	}
	return rel, false, nil
}

func mapMedia(formats []string) []string {
	have := map[string]bool{}
	for _, f := range formats {
		switch {
		case slices.Contains(
			[]string{"CD", "CD-R", "HDCD", "SACD", "Enhanced CD"}, f,
		):
			have["cd"] = true
		case strings.Contains(f, "Vinyl"):
			have["vinyl"] = true
		case f == "Digital Media":
			have["digital"] = true
		case f == "Cassette":
			have["cassette"] = true
		}
	}
	var out []string
	for _, k := range []string{"cd", "vinyl", "digital", "cassette"} {
		if have[k] {
			out = append(out, k)
		}
	}
	return out
}

func validCountry(c string) string {
	if len(c) != 2 || c[0] == 'X' {
		return ""
	}
	for _, r := range c {
		if r < 'A' || r > 'Z' {
			return ""
		}
	}
	return c
}

var roleByType = map[string]string{
	"producer":            "producer",
	"recording":           "recording",
	"engineer":            "recording",
	"recording engineer":  "recording",
	"mix":                 "mix",
	"mastering":           "mastering",
	"design/illustration": "artwork",
	"graphic design":      "artwork",
	"art direction":       "artwork",
	"photography":         "artwork",
}

var writerTypes = []string{"writer", "composer", "lyricist"}

func fillRelease(d *ReleaseGroupDetails, rel mbRelease, heavy bool) {
	d.Barcode = rel.Barcode
	d.Country = validCountry(rel.Country)
	if len(rel.LabelInfo) > 0 {
		li := rel.LabelInfo[0]
		d.CatalogNumber = li.CatalogNumber
		if li.Label != nil {
			d.Label = li.Label.Name
		}
	}

	performers := map[string]*PerformerInfo{}
	var performerOrder []string
	creditSeen := map[string]bool{}
	addCredit := func(rel mbRel) {
		role, ok := roleByType[rel.Type]
		if !ok || rel.TargetType != "artist" || rel.Artist == nil {
			return
		}
		key := creditKey(rel.Artist.ID, rel.Artist.Name) + "|" + role
		if creditSeen[key] {
			return
		}
		creditSeen[key] = true
		d.Credits = append(d.Credits, CreditInfo{
			Name: rel.Artist.Name, MBID: rel.Artist.ID,
			Role: role,
		})
	}
	placeCount := map[string]int{}

	for _, r := range rel.Relations {
		addCredit(r)
	}
	for _, md := range rel.Media {
		disc := md.Position
		if disc == 0 {
			disc = 1
		}
		mediumBonus := strings.Contains(strings.ToLower(md.Title), "bonus")
		for _, t := range md.Tracks {
			ti := TrackInfo{
				MBID:      t.Recording.ID,
				Title:     t.Title,
				Disc:      disc,
				Position:  t.Position,
				Duration:  t.Length / 1000,
				Featuring: featuring(t.ArtistCredit),
				Bonus: mediumBonus || strings.HasSuffix(
					strings.ToLower(strings.TrimSpace(t.Title)), "(bonus track)"),
			}
			seenOnRecording := map[string]bool{}
			writerSeen := map[string]bool{}
			performerBearing := false
			for _, rr := range t.Recording.Relations {
				addCredit(rr)
				switch {
				case rr.Type == "recording" && rr.TargetType == "place" &&
					rr.Place != nil:
					placeCount[rr.Place.Name]++
				case (rr.Type == "instrument" || rr.Type == "vocal") &&
					rr.Artist != nil:
					performerBearing = true
					key := creditKey(rr.Artist.ID, rr.Artist.Name)
					p := performers[key]
					if p == nil {
						p = &PerformerInfo{
							Name: rr.Artist.Name, MBID: rr.Artist.ID,
						}
						performers[key] = p
						performerOrder = append(performerOrder, key)
					}
					if !seenOnRecording[key] {
						seenOnRecording[key] = true
						p.Recordings++
					}
					instruments := rr.Attributes
					if len(instruments) == 0 && rr.Type == "vocal" {
						instruments = []string{"vocals"}
					}
					for _, in := range instruments {
						if !slices.Contains(p.Instruments, in) {
							p.Instruments = append(p.Instruments, in)
						}
					}
				case rr.Type == "performance" && rr.Work != nil:
					for _, wr := range rr.Work.Relations {
						if !slices.Contains(writerTypes, wr.Type) ||
							wr.Artist == nil {
							continue
						}
						key := creditKey(wr.Artist.ID, wr.Artist.Name)
						if writerSeen[key] {
							continue
						}
						writerSeen[key] = true
						ti.Writers = append(ti.Writers, PersonInfo{
							Name: wr.Artist.Name, MBID: wr.Artist.ID,
						})
					}
				}
			}
			if performerBearing {
				d.PerformerRecordings++
			}
			d.Tracks = append(d.Tracks, ti)
		}
	}
	if !heavy {
		return
	}
	for _, key := range performerOrder {
		d.Performers = append(d.Performers, *performers[key])
	}
	slices.SortStableFunc(d.Performers, func(a, b PerformerInfo) int {
		return b.Recordings - a.Recordings
	})
	best, bestN := "", 0
	for name, n := range placeCount {
		if n > bestN || (n == bestN && name < best) {
			best, bestN = name, n
		}
	}
	d.Studio = best
}

func creditKey(mbid, name string) string {
	if mbid != "" {
		return mbid
	}
	return strings.ToLower(name)
}

// featuring returns the credited artists after the first "feat." join phrase.
// Artists before it are co-primary credits of a collaboration, not guests.
func featuring(credits []mbCredit) []PersonInfo {
	for i := 1; i < len(credits); i++ {
		if !featJoinRe.MatchString(credits[i-1].JoinPhrase) {
			continue
		}
		out := make([]PersonInfo, 0, len(credits)-i)
		for _, c := range credits[i:] {
			name := c.Artist.Name
			if name == "" {
				name = c.Name
			}
			if name != "" && utf8.ValidString(name) {
				out = append(out, PersonInfo{Name: name, MBID: c.Artist.ID})
			}
		}
		return out
	}
	return nil
}
