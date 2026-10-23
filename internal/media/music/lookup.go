package music

import (
	"cmp"
	"context"
	"log/slog"
	"slices"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/internal/artwork"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/utils/numeric"
)

// overviewLangs are the locales an overview is stored and served in.
var overviewLangs = []string{"en", "fr"}

// LookupHit is one MusicBrainz artist search hit with its local state.
type LookupHit struct {
	metadata.ArtistResult
	AlreadyAdded bool
	// LibraryID is the library artist's id when AlreadyAdded.
	LibraryID uint32
}

// LookupRelease is one release group in a lookup detail.
type LookupRelease struct {
	MBID  string
	Title string
	Year  uint16
	Type  string
}

// LookupDetail is the hit fields and the detail of one MusicBrainz artist in
// one object: the answer to a selection in the add flow, and the artist arm of
// a request's metadata. It has no score.
type LookupDetail struct {
	LookupHit
	Overview string
	Genres   []string
	// Members are the names of the current members of a group.
	Members  []string
	Releases []LookupRelease
}

// lookupBody is the provider half of a lookup detail: what the cache keeps.
// The local half, whether the artist is already in the library, is read fresh.
type lookupBody struct {
	details  *metadata.ArtistDetails
	overview string
}

// SearchArtists searches MusicBrainz for artists, one request, and marks the
// hits already in the library with one query. It remembers each hit's name so
// the lookup art proxy can find a photo for it.
func (s *Service) SearchArtists(
	ctx context.Context,
	query string,
) ([]LookupHit, error) {
	ctx, span := tracer.Start(ctx, "music.search_artists")
	defer span.End()

	results, err := s.metadata.SearchArtists(ctx, query)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	mbids := make([]string, len(results))
	for i, r := range results {
		mbids[i] = r.MBID
	}
	ids, err := s.db.ArtistIDsByMBID(ctx, mbids)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	hits := make([]LookupHit, len(results))
	for i, r := range results {
		hits[i] = LookupHit{ArtistResult: r}
		hits[i].LibraryID = ids[r.MBID]
		hits[i].AlreadyAdded = hits[i].LibraryID != 0
		artwork.Remember(artwork.KindArtists, r.MBID, artwork.Source{Name: r.Name})
	}
	span.SetAttributes(attribute.Int("hits", len(hits)))
	return hits, nil
}

// LookupArtist returns the detail of one MusicBrainz artist: the hit fields
// from the artist call, the Wikipedia overview in lang (English when that
// language has no article), genres, current members and the release groups.
// It shares the ten-minute artist cache with Add, so a lookup followed by an
// add costs one set of MusicBrainz requests.
func (s *Service) LookupArtist(
	ctx context.Context,
	mbid, lang string,
) (*LookupDetail, error) {
	ctx, span := tracer.Start(ctx, "music.lookup_artist",
		trace.WithAttributes(attribute.String("mbid", mbid)))
	defer span.End()

	lang = normalizeLang(lang)
	body, err := s.lookups.do(mbid+"|"+lang, func() (*lookupBody, error) {
		details, err := s.artistDetails(ctx, mbid)
		if err != nil {
			return nil, err
		}
		return &lookupBody{
			details:  details,
			overview: s.lookupOverview(ctx, details.WikidataID, lang),
		}, nil
	})
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	d := body.details
	artwork.Remember(artwork.KindArtists, d.MBID,
		artwork.Source{DeezerID: d.DeezerID, Name: d.Name})

	ids, err := s.db.ArtistIDsByMBID(ctx, []string{d.MBID})
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	out := &LookupDetail{
		ArtistResult: d.ArtistResult, LibraryID: ids[d.MBID],
		Overview: body.overview,
		Genres:   d.Genres,
	}
	out.AlreadyAdded = out.LibraryID != 0
	for _, m := range d.Members {
		if !m.Ended {
			out.Members = append(out.Members, m.Name)
		}
	}
	for _, rg := range d.ReleaseGroups {
		r := LookupRelease{MBID: rg.MBID, Title: rg.Title, Type: string(rg.Type)}
		if rg.ReleaseDate != nil {
			r.Year = numeric.SaturateU16(rg.ReleaseDate.Year())
		}
		out.Releases = append(out.Releases, r)
	}
	slices.SortStableFunc(out.Releases, func(a, b LookupRelease) int {
		switch {
		case a.Year == 0 && b.Year != 0:
			return 1
		case a.Year != 0 && b.Year == 0:
			return -1
		}
		return cmp.Or(cmp.Compare(b.Year, a.Year), strings.Compare(a.Title, b.Title))
	})
	return out, nil
}

// lookupOverview is the overview in lang, falling back to English. Wikipedia
// failing is not the lookup failing: the detail pane hides the block.
func (s *Service) lookupOverview(
	ctx context.Context,
	wikidataID, lang string,
) string {
	if s.overviews == nil || wikidataID == "" {
		return ""
	}
	langs := []string{lang}
	if lang != "en" {
		langs = append(langs, "en")
	}
	got, err := s.overviews.Overviews(ctx, wikidataID, langs)
	if err != nil {
		slog.WarnContext(ctx, "lookup overview not fetched",
			"wikidata.id", wikidataID, "error", err)
		return ""
	}
	for _, l := range langs {
		if o, ok := got[l]; ok {
			return o.Text
		}
	}
	return ""
}

func normalizeLang(lang string) string {
	if slices.Contains(overviewLangs, lang) {
		return lang
	}
	return "en"
}
