package book

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/language"
	"golang.org/x/text/language/display"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/media/book/pick"
	"github.com/datahearth/streamline/internal/metadata"
)

const (
	slotEbook     = string(mediafile.BookKindEbook)
	slotAudiobook = string(mediafile.BookKindAudiobook)

	maxRatingTenths = 50
	labelSeparator  = " · "
	// detailTTL is how long a lookup detail is served from memory. It matches
	// Hardcover's own memo so the two expire together.
	detailTTL = 10 * time.Minute
	// detailCap bounds the memo; a reviewer expanding a page of requests is
	// the heaviest user.
	detailCap = 128
)

var slotKinds = []string{slotEbook, slotAudiobook}

func validSlotKind(kind string) bool {
	return kind == slotEbook || kind == slotAudiobook
}

// slotFlags reads a book monitor value as the two slots it asks for.
func slotFlags(monitor string) (ebook, audiobook bool, err error) {
	switch monitor {
	case MonitorBoth:
		return true, true, nil
	case MonitorEbook:
		return true, false, nil
	case MonitorAudiobook:
		return false, true, nil
	case MonitorNone:
		return false, false, nil
	}
	return false, false, fmt.Errorf("%w: %q", ErrInvalidMonitor, monitor)
}

// monitorOf derives the book monitor value from the two slot flags.
func monitorOf(b *ent.Book) string {
	switch {
	case b.EbookMonitored && b.AudiobookMonitored:
		return MonitorBoth
	case b.EbookMonitored:
		return MonitorEbook
	case b.AudiobookMonitored:
		return MonitorAudiobook
	}
	return MonitorNone
}

func validPolicy(p string) bool {
	return p == PolicyAll || p == PolicyFuture || p == PolicyNone
}

func validKind(kind string) bool {
	return slices.Contains(config.BookKinds, kind)
}

func validLanguage(code string) bool {
	if len(code) != 2 {
		return false
	}
	for _, r := range code {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}

// checkProfile accepts the empty name (the default) and any configured one.
func checkProfile(name string) error {
	if name == "" {
		return nil
	}
	if _, ok := config.LookupBookQualityProfile(name); !ok {
		return fmt.Errorf("%w: %q", ErrUnknownProfile, name)
	}
	return nil
}

// ratingTenths turns Hardcover's 0..5 average into tenths, nil when unrated.
func ratingTenths(r float64) *uint8 {
	if r <= 0 {
		return nil
	}
	v := saturateU8(min(int(math.Round(r*10)), maxRatingTenths))
	return &v
}

func yearPtr(y uint16) *uint16 {
	if y == 0 {
		return nil
	}
	return &y
}

// positionLabel renders a volume number without a trailing ".0".
func positionLabel(p float64) string {
	return strconv.FormatFloat(p, 'f', -1, 64)
}

// languageLabel is a language in its own name, capitalised.
func languageLabel(code string) string {
	tag, err := language.Parse(code)
	if err != nil {
		return strings.ToUpper(code)
	}
	name := display.Self.Name(tag)
	r, n := utf8.DecodeRuneInString(name)
	if r == utf8.RuneError {
		return strings.ToUpper(code)
	}
	return string(unicode.ToUpper(r)) + name[n:]
}

// editionLabel is how a series names a (language, publisher) choice.
func editionLabel(lang, publisher string) string {
	if publisher == "" {
		return languageLabel(lang)
	}
	return languageLabel(lang) + labelSeparator + publisher
}

func recordCredits(credits []metadata.BookCredit) []pick.Credit {
	out := make([]pick.Credit, 0, len(credits))
	for _, c := range credits {
		out = append(out, pick.Credit{Name: c.Name, Role: c.Role})
	}
	return out
}

// orderOf is a position as a credit's order, saturating.
func orderOf(i int) uint8 { return saturateU8(i) }

// saturateU8 narrows to uint8, clamping at both ends.
func saturateU8(v int) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= math.MaxUint8 {
		return math.MaxUint8
	}
	return uint8(v)
}

func dbCredits(credits []metadata.BookCredit) []db.CreditSeed {
	out := make([]db.CreditSeed, 0, len(credits))
	for i, c := range credits {
		out = append(out, db.CreditSeed{
			AuthorHardcoverID: c.AuthorHardcoverID,
			Name:              c.Name,
			ImageURL:          c.ImageURL,
			Role:              c.Role,
			Order:             orderOf(i),
		})
	}
	return out
}

func dbEditions(eds []metadata.EditionRecord) []db.EditionSeed {
	out := make([]db.EditionSeed, 0, len(eds))
	for _, e := range eds {
		out = append(out, db.EditionSeed{
			HardcoverID:     e.HardcoverID,
			Language:        e.Language,
			Title:           e.Title,
			Publisher:       e.Publisher,
			Year:            e.Year,
			Format:          e.Format,
			Original:        e.Original,
			Pages:           e.Pages,
			DurationSeconds: e.DurationSeconds,
			Narrator:        e.Narrator,
			Translator:      e.Translator,
			ISBN13:          e.ISBN13,
			ASIN:            e.ASIN,
			Popularity:      e.Popularity,
		})
	}
	return out
}

func recordEditionViews(eds []metadata.EditionRecord) []pick.Edition {
	out := make([]pick.Edition, 0, len(eds))
	for _, e := range eds {
		out = append(out, pick.Edition{
			ID:         e.HardcoverID,
			Language:   e.Language,
			Title:      e.Title,
			Publisher:  e.Publisher,
			Format:     e.Format,
			Original:   e.Original,
			Popularity: e.Popularity,
		})
	}
	return out
}

func entEditionViews(rows []*ent.BookEdition) []pick.Edition {
	out := make([]pick.Edition, 0, len(rows))
	for _, e := range rows {
		out = append(out, pick.Edition{
			ID:         e.ID,
			Language:   e.Language,
			Title:      e.Title,
			Publisher:  e.Publisher,
			Format:     string(e.Format),
			Original:   e.Original,
			Popularity: e.Popularity,
		})
	}
	return out
}

func hasSlotFile(b *ent.Book, kind string) bool {
	for _, f := range b.Edges.MediaFiles {
		if string(f.BookKind) == kind {
			return true
		}
	}
	return false
}

func slotEdition(b *ent.Book, kind string) *ent.BookEdition {
	if kind == slotAudiobook {
		return b.Edges.AudiobookEdition
	}
	return b.Edges.EbookEdition
}

func slotStatus(b *ent.Book, kind string) string {
	if kind == slotAudiobook {
		return string(b.AudiobookStatus)
	}
	return string(b.EbookStatus)
}

func slotMonitored(b *ent.Book, kind string) bool {
	if kind == slotAudiobook {
		return b.AudiobookMonitored
	}
	return b.EbookMonitored
}

func replacingLanguage(b *ent.Book, kind string) string {
	if kind == slotAudiobook {
		return b.AudiobookReplacingLanguage
	}
	return b.EbookReplacingLanguage
}

// unreleased reports a book whose release date is still ahead: nothing to find
// yet, so it is never searched and its status ignores its slots.
func unreleased(b *ent.Book, now time.Time) bool {
	return b.ReleaseDate != nil && b.ReleaseDate.After(now)
}

// detailMemo keeps lookup details for detailTTL. Each detail is built from
// one or two Hardcover requests, so an expanding reviewer or a repeated
// highlight costs nothing after the first.
type detailMemo struct {
	mu    sync.Mutex
	items map[string]detailEntry
}

type detailEntry struct {
	v   *LookupDetail
	exp time.Time
}

func (m *detailMemo) get(key string) (*LookupDetail, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.items[key]
	if !ok || time.Now().After(e.exp) {
		delete(m.items, key)
		return nil, false
	}
	return e.v, true
}

func (m *detailMemo) put(key string, v *LookupDetail) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.items == nil {
		m.items = make(map[string]detailEntry)
	}
	if len(m.items) >= detailCap {
		now := time.Now()
		for k, e := range m.items {
			if now.After(e.exp) {
				delete(m.items, k)
			}
		}
		if len(m.items) >= detailCap {
			clear(m.items)
		}
	}
	m.items[key] = detailEntry{v: v, exp: time.Now().Add(detailTTL)}
}
