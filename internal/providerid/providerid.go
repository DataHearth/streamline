// Package providerid holds the three rules for a metadata provider's id —
// TMDB, TVDB, Hardcover — now that every layer above the provider adapters
// carries it as a string. A MusicBrainz MBID was always a string and needs
// none of them.
//
// Leaf package: the indexer adapters read tracker-published ids and must not
// import internal/metadata for it.
package providerid

import (
	"cmp"
	"strconv"
	"strings"
)

// Format renders a provider's numeric id as the string the rest of the code
// carries. Zero is no id, and renders empty.
func Format(n uint32) string {
	if n == 0 {
		return ""
	}
	return strconv.FormatUint(uint64(n), 10)
}

// Parse reads back an id Format wrote, for the APIs that take a number
// (Hardcover's GraphQL Int variables). ok is false for anything that is not a
// positive 32-bit decimal: no provider holds anything under it.
func Parse(id string) (n uint32, ok bool) {
	v, err := strconv.ParseUint(id, 10, 32)
	if err != nil || v == 0 {
		return 0, false
	}
	return uint32(v), true
}

// Compare orders two ids the way their numbers order — a shorter decimal is
// a smaller one — so a tie broken on the id, or an answer sorted by it, comes
// out as it did while the ids were integers. Ids that are not numbers order
// by length then bytes, which is still a total order.
func Compare(a, b string) int {
	if c := cmp.Compare(len(a), len(b)); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

// Valid reports whether id is a provider id in the one form the code stores
// and compares: a positive decimal with no sign, space or leading zero, as
// Format writes it. Every id a request carries in goes through this, because
// the ids are compared as strings — "0603" would pass every duplicate check
// against "603" and be stored beside it, and a tracker tagging its release
// tmdbid 603 would never match it.
func Valid(id string) bool {
	n, ok := Parse(id)
	return ok && Format(n) == id
}
