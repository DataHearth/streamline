package ffmpeg

import (
	"slices"
	"strings"

	"github.com/datahearth/streamline/internal/langcode"
)

// langSet collects stream languages, deduped and canonicalised. The zero value
// is ready to use and joins to "".
type langSet struct {
	seen []string
}

// add records one stream's tags.language. Blank and "und" are dropped — an
// untagged track is not evidence of any language, and admitting it would let it
// satisfy a negated language condition. "mul" is kept: it is a real code saying
// the track itself carries several languages.
func (l *langSet) add(code string) {
	c := langcode.Canonical(code)
	if c == "" || c == "und" {
		return
	}
	if !slices.Contains(l.seen, c) {
		l.seen = append(l.seen, c)
	}
}

// join renders the set as the stored column value, sorted so two files holding
// the same tracks in a different stream order produce the same string.
func (l *langSet) join() string {
	slices.Sort(l.seen)
	return strings.Join(l.seen, ",")
}
