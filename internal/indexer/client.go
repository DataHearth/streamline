package indexer

import (
	"context"
	"time"
)

type SearchResult struct {
	Title       string
	InfoURL     string
	Download    string
	Size        int64
	Seeders     uint32
	Leechers    uint32
	Category    string
	PublishDate time.Time
	// Indexer is the configured name of the indexer this result came from.
	// Stamped by the search service during cross-indexer aggregation.
	Indexer string

	// TMDBID/TVDBID are the provider ids the *tracker* published alongside the
	// release, not anything we asked for: Prowlarr re-emits the torznab attrs
	// on ReleaseResource, and the direct Torznab path reads the same attrs.
	//
	// Zero means the tracker said nothing — never "no match". An absent attr
	// decodes to zero exactly as an unrecorded quality condition reads as
	// unknown rather than false, and filterProviderIDs turns on that
	// distinction.
	TMDBID uint32
	TVDBID uint32

	// TitleMismatch marks a result preferTitleMatches kept only because
	// nothing in the set named the show at all. Browsing wants those — a show
	// held under a translated title matches none of its releases — but an
	// automatic grab must not take one: the scope filters match on numbers
	// alone, so a season-pack search for "Détective Conan" that nothing named
	// came back holding The.Shield.S06 and Monk.S05, and the scorer has no way
	// to tell those from the show it asked for.
	//
	// The zero value is the permissive one on purpose: a result that never
	// passed through preferTitleMatches (an RSS feed item, a whole-series pack)
	// carries no verdict and must not read as a rejected one.
	TitleMismatch bool
}

// MediaKind is the library kind a search is scoped to. It decides the newznab
// category root and Prowlarr's search type — both of which used to be derived
// from whichever id happened to be set, so the id-less retry in searchAll sent
// neither and keyword-searched the whole catalogue.
type MediaKind uint8

const (
	KindUnknown MediaKind = iota
	KindMovie
	KindTV
)

type SearchParams struct {
	Query   string
	Kind    MediaKind
	TMDBID  uint32
	TVDBID  uint32
	Season  uint16
	Episode uint16
}

// narrowed reports whether the query carries anything beyond the bare title.
// It is what makes the empty-result retry worth issuing: a query that named
// nothing else has no narrower form to fall back from.
func (p SearchParams) narrowed() bool {
	return p.TMDBID > 0 || p.TVDBID > 0 || p.Season > 0 || p.Episode > 0
}

type Client interface {
	Search(ctx context.Context, params SearchParams) ([]SearchResult, error)
	Feed(ctx context.Context) ([]SearchResult, error)
	TestConnection(ctx context.Context) error
}
