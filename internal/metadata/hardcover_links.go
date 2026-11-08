package metadata

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/internal/otelx"
)

// hcEditionLinksBatch is how many editions one links request reads: as many
// as fit, since every request counts against the day's budget. Each edition
// answers at most hcLinksPerPlatform rows per platform (Audible more, one per
// region), and most carry a handful, so a batch stays a few hundred rows.
const (
	hcEditionLinksBatch = 100
	hcLinksPerPlatform  = 5
	hcAudibleRegions    = 12
)

// Hardcover's platform ids (the platforms table) for the sites the library
// links: an edition's book_mappings rows carry one of them. Read from the
// live API; the names are Hardcover's own.
const (
	hcPlatformGoodreads   = 1
	hcPlatformOpenLibrary = 3
	hcPlatformWikidata    = 18
	hcPlatformGoogle      = 20
	hcPlatformAudible     = 32
)

// hcEditionLinksQuery reads the external ids of a few editions, never of a
// whole book: a popular book has hundreds of editions, and the library links
// only the ones its slots follow. Each platform is its own alias with its own
// limit, so one with many rows (1984's paperback lists eight Goodreads ids)
// cannot crowd out the others. Depth 2 (editions > book_mappings), the
// platform by id rather than through its relation. A mapping Hardcover could
// not load (state cannot_load) is no evidence of anything and is left out.
var hcEditionLinksQuery = func() string {
	alias := func(name string, platform, limit int) string {
		return fmt.Sprintf(`
    %s: book_mappings(where: {platform_id: {_eq: %d}, state: {_eq: "normalized"}},
      order_by: {id: asc}, limit: %d) { ...m }`, name, platform, limit)
	}
	return `
query ($ids: [Int!]!) {
  editions(where: {id: {_in: $ids}}) {
    id` +
		alias("goodreads", hcPlatformGoodreads, hcLinksPerPlatform) +
		alias("openlibrary", hcPlatformOpenLibrary, hcLinksPerPlatform) +
		alias("wikidata", hcPlatformWikidata, hcLinksPerPlatform) +
		alias("google", hcPlatformGoogle, hcLinksPerPlatform) +
		alias("audible", hcPlatformAudible, hcAudibleRegions) + `
  }
}
fragment m on book_mappings { id external_id verified }`
}()

type hcMapping struct {
	ID         uint32 `json:"id"`
	ExternalID string `json:"external_id"`
	Verified   *bool  `json:"verified"`
}

type hcEditionLinksRow struct {
	ID          uint32      `json:"id"`
	Goodreads   []hcMapping `json:"goodreads"`
	OpenLibrary []hcMapping `json:"openlibrary"`
	Wikidata    []hcMapping `json:"wikidata"`
	Google      []hcMapping `json:"google"`
	Audible     []hcMapping `json:"audible"`
}

var (
	goodreadsIDRe   = regexp.MustCompile(`^\d+$`)
	olEditionRe     = regexp.MustCompile(`^/books/(OL\d+M)$`)
	wikidataQRe     = regexp.MustCompile(`^Q\d+$`)
	googleBooksIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{6,}$`)
	audibleRe       = regexp.MustCompile(`^([A-Z0-9]{10}):([a-z]{2})$`)
)

// EditionLinks reads the other sites' ids Hardcover keeps for each edition,
// keyed by Hardcover edition id; an edition Hardcover does not know is
// absent, and one it knows with no usable mapping maps to an empty map. One
// request per hcEditionLinksBatch editions.
func (h *Hardcover) EditionLinks(
	ctx context.Context,
	editionIDs []uint32,
) (map[uint32]map[string]string, error) {
	ctx, span := tracer.Start(ctx, "metadata.hardcover.edition_links",
		trace.WithAttributes(attribute.Int("editions.count", len(editionIDs))))
	defer span.End()

	out := make(map[uint32]map[string]string, len(editionIDs))
	lang := bookLanguage()
	for chunk := range slices.Chunk(editionIDs, hcEditionLinksBatch) {
		var payload struct {
			Editions []hcEditionLinksRow `json:"editions"`
		}
		if err := h.query(
			ctx, hcEditionLinksQuery, map[string]any{"ids": chunk}, &payload,
		); err != nil {
			return nil, otelx.RecordSpanError(span,
				fmt.Errorf("hardcover edition links: %w", err))
		}
		for _, row := range payload.Editions {
			out[row.ID] = row.links(lang)
		}
	}
	return out, nil
}

// links keeps one id per site: a verified mapping over an unverified one,
// then the oldest (the query orders by id), and only a value in the shape its
// site uses. Hardcover stores other things in external_id (a StoryGraph
// title-author string, an AbeBooks image URL, an Open Library work path), and
// a link built from one of those lands nowhere.
func (r hcEditionLinksRow) links(lang string) map[string]string {
	out := map[string]string{}
	pick := func(source string, rows []hcMapping, norm func(string) string) {
		var best string
		bestVerified := false
		for _, m := range rows {
			v := norm(strings.TrimSpace(m.ExternalID))
			if v == "" {
				continue
			}
			verified := m.Verified != nil && *m.Verified
			if best == "" || (verified && !bestVerified) {
				best, bestVerified = v, verified
			}
		}
		if best != "" {
			out[source] = best
		}
	}
	match := func(re *regexp.Regexp) func(string) string {
		return func(v string) string {
			if re.MatchString(v) {
				return v
			}
			return ""
		}
	}
	pick(SourceGoodreads, r.Goodreads, match(goodreadsIDRe))
	pick(SourceOpenLibrary, r.OpenLibrary, func(v string) string {
		if m := olEditionRe.FindStringSubmatch(v); m != nil {
			return m[1]
		}
		return ""
	})
	pick(SourceWikidata, r.Wikidata, match(wikidataQRe))
	pick(SourceGoogleBooks, r.Google, match(googleBooksIDRe))
	if v := audibleFor(r.Audible, lang); v != "" {
		out[SourceAudible] = v
	}
	return out
}

// audibleFor picks the Audible store a link opens: Hardcover keeps one
// mapping per region ("B0FXY6DVJS:us", ":fr", ":de"…), often with a different
// ASIN in each. The library's book language's own store comes first, then the
// US one, then the first listed. The value stays "ASIN:region", from which
// the client builds the store's address.
func audibleFor(rows []hcMapping, lang string) string {
	want := audibleRegionFor(lang)
	byRegion := map[string]string{}
	var first string
	for _, m := range rows {
		v := strings.TrimSpace(m.ExternalID)
		sub := audibleRe.FindStringSubmatch(v)
		if sub == nil {
			continue
		}
		if _, ok := byRegion[sub[2]]; !ok {
			byRegion[sub[2]] = v
		}
		if first == "" {
			first = v
		}
	}
	for _, region := range []string{want, "us"} {
		if v, ok := byRegion[region]; ok {
			return v
		}
	}
	return first
}

// audibleRegionFor maps an ISO 639-1 language to the Audible store that sells
// in it; English is the US store.
func audibleRegionFor(lang string) string {
	switch lang {
	case "en":
		return "us"
	case "ja":
		return "jp"
	case "pt":
		return "br"
	case "hi":
		return "in"
	}
	return lang
}
