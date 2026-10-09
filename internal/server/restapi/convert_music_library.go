package restapi

import (
	"errors"
	"fmt"
	"math"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/media/music"
	"github.com/datahearth/streamline/internal/metadata"
)

func optU16(v uint16) *uint16 {
	if v == 0 {
		return nil
	}
	return &v
}

func optU32(v uint32) *uint32 {
	if v == 0 {
		return nil
	}
	return &v
}

func optStrings(v []string) *[]string {
	if len(v) == 0 {
		return nil
	}
	return &v
}

func toMusicPerson(p music.PersonView) MusicPerson {
	return MusicPerson{
		ArtistId: optU32(p.ArtistID),
		Mbid:     optString(p.MBID),
		Name:     p.Name,
	}
}

func toMusicPeople(in []music.PersonView) *[]MusicPerson {
	if len(in) == 0 {
		return nil
	}
	out := make([]MusicPerson, len(in))
	for i, p := range in {
		out[i] = toMusicPerson(p)
	}
	return &out
}

func toMusicTrack(t music.TrackView) MusicTrack {
	out := MusicTrack{
		Disc:      t.Track.Disc,
		Duration:  t.Track.Duration,
		Featuring: toMusicPeople(t.Featuring),
		HasFile:   t.HasFile,
		Id:        t.Track.ID,
		Mbid:      optString(t.Track.Mbid),
		Number:    t.Track.Position,
		Title:     t.Track.Title,
		Writers:   toMusicPeople(t.Writers),
	}
	if t.Track.Bonus {
		bonus := true
		out.Bonus = &bonus
	}
	return out
}

func toMusicAlbumDate(a music.AlbumView) *openapi_types.Date {
	if a.Album.ReleaseDate == nil {
		return nil
	}
	return &openapi_types.Date{Time: *a.Album.ReleaseDate}
}

func toMusicProgress(a music.AlbumView) *float32 {
	if a.Progress == nil {
		return nil
	}
	p := float32(math.Min(math.Max(*a.Progress, 0), 100))
	return &p
}

func toMusicAlbumSummary(a music.AlbumView, artistID uint32) MusicAlbumSummary {
	return MusicAlbumSummary{
		ArtistId:      artistID,
		Id:            a.Album.ID,
		Monitored:     a.Album.Monitored,
		Progress:      toMusicProgress(a),
		ReleaseDate:   toMusicAlbumDate(a),
		Status:        MusicAlbumStatus(a.Status),
		Title:         a.Album.Title,
		TrackCount:    a.TrackCount,
		TracksHave:    a.TracksHave,
		TracksPending: a.TracksPending,
		Type:          MusicAlbumType(a.Album.Type),
	}
}

func toMusicAlbum(a music.AlbumView, artistID uint32) MusicAlbum {
	row := a.Album
	out := MusicAlbum{
		ArtistId:      artistID,
		CatalogNumber: optString(row.CatalogNumber),
		Country:       optString(row.Country),
		Credits:       make([]MusicCredit, 0, len(a.Credits)),
		Format:        optString(a.Format),
		Id:            row.ID,
		Label:         optString(row.Label),
		Mbid:          row.Mbid,
		Monitored:     row.Monitored,
		Personnel:     make([]MusicPerformer, 0, len(a.Personnel)),
		Progress:      toMusicProgress(a),
		ReleaseDate:   toMusicAlbumDate(a),
		Status:        MusicAlbumStatus(a.Status),
		Studio:        optString(row.Studio),
		Title:         row.Title,
		TrackCount:    a.TrackCount,
		Tracks:        make([]MusicTrack, 0, len(a.Tracks)),
		TracksHave:    a.TracksHave,
		TracksPending: a.TracksPending,
		Type:          MusicAlbumType(row.Type),
	}
	if media := splitMedia(row.Media); len(media) > 0 {
		out.Media = &media
	}
	if a.Quality != "" {
		q := MusicTier(a.Quality)
		out.Quality = &q
	}
	if a.Size > 0 {
		size := a.Size
		out.Size = &size
	}
	if a.Duration > 0 {
		d := a.Duration
		out.Duration = &d
	}
	for _, c := range a.Credits {
		out.Credits = append(out.Credits, MusicCredit{
			ArtistId: optU32(c.ArtistID),
			Mbid:     optString(c.MBID),
			Name:     c.Name,
			Role:     MusicCreditRole(c.Role),
		})
	}
	for _, p := range a.Personnel {
		instruments := p.Instruments
		if instruments == nil {
			instruments = []string{}
		}
		out.Personnel = append(out.Personnel, MusicPerformer{
			ArtistId:    optU32(p.ArtistID),
			Guest:       p.Guest,
			Instruments: instruments,
			Mbid:        optString(p.MBID),
			Name:        p.Name,
		})
	}
	for _, t := range a.Tracks {
		out.Tracks = append(out.Tracks, toMusicTrack(t))
	}
	return out
}

func splitMedia(s string) []MusicMedium {
	parts := splitComma(s)
	out := make([]MusicMedium, 0, len(parts))
	for _, m := range parts {
		out = append(out, MusicMedium(m))
	}
	return out
}

func splitComma(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return out
}

func toMusicArtistKind(t string) *MusicArtistKind {
	if t == "" {
		return nil
	}
	k := MusicArtistKind(t)
	return &k
}

// toMusicArtistDetail maps the artist and its full tree.
func toMusicArtistDetail(v *music.ArtistView) MusicArtistDetail {
	a := v.Artist
	out := MusicArtistDetail{
		AddedAt:        a.CreateTime,
		AlbumCount:     v.AlbumCount,
		Albums:         make([]MusicAlbum, 0, len(v.Albums)),
		Genre:          optString(a.Genre),
		Hydrating:      v.Hydrating,
		Id:             a.ID,
		Mbid:           a.Mbid,
		Members:        make([]MusicMember, 0, len(v.Members)),
		Monitor:        MusicMonitor(a.Monitor),
		Name:           a.Name,
		Origin:         optString(a.Origin),
		Overview:       optString(v.Overview),
		OverviewSource: optString(v.OverviewSource),
		Path:           a.Path,
		QualityProfile: a.QualityProfile,
		Since:          optU16(a.Since),
		Size:           v.Size,
		SortName:       a.SortName,
		Status:         MusicArtistStatus(v.Status),
		TracksHave:     v.TracksHave,
		Type:           toMusicArtistKind(string(a.Type)),
	}
	for _, al := range v.Albums {
		out.Albums = append(out.Albums, toMusicAlbum(al, a.ID))
	}
	for _, m := range v.Members {
		instruments := m.Instruments
		if instruments == nil {
			instruments = []string{}
		}
		out.Members = append(out.Members, MusicMember{
			ArtistId:    optU32(m.ArtistID),
			From:        optU16(m.From),
			Instruments: instruments,
			Mbid:        optString(m.MBID),
			Name:        m.Name,
			To:          optU16(m.To),
		})
	}
	return out
}

// toMusicArtist maps a list item: the artist with its albums as tiles.
func toMusicArtist(v music.ArtistView) MusicArtist {
	a := v.Artist
	out := MusicArtist{
		AddedAt:        a.CreateTime,
		AlbumCount:     v.AlbumCount,
		Albums:         make([]MusicAlbumSummary, 0, len(v.Albums)),
		Genre:          optString(a.Genre),
		Hydrating:      v.Hydrating,
		Id:             a.ID,
		Mbid:           a.Mbid,
		Monitor:        MusicMonitor(a.Monitor),
		Name:           a.Name,
		Origin:         optString(a.Origin),
		Overview:       optString(v.Overview),
		OverviewSource: optString(v.OverviewSource),
		Path:           a.Path,
		QualityProfile: a.QualityProfile,
		Since:          optU16(a.Since),
		Size:           v.Size,
		SortName:       a.SortName,
		Status:         MusicArtistStatus(v.Status),
		TracksHave:     v.TracksHave,
		Type:           toMusicArtistKind(string(a.Type)),
	}
	for _, al := range v.Albums {
		out.Albums = append(out.Albums, toMusicAlbumSummary(al, a.ID))
	}
	return out
}

func toMusicSearchHit(h music.LookupHit) MusicArtistSearchResult {
	return MusicArtistSearchResult{
		AlreadyAdded:   h.AlreadyAdded,
		Area:           optString(h.Area),
		Disambiguation: optString(h.Disambiguation),
		Genre:          optString(h.Genre),
		LibraryId:      optU32(h.LibraryID),
		Mbid:           h.MBID,
		Name:           h.Name,
		Score:          h.Score,
		Since:          optU16(h.Since),
		SortName:       h.SortName,
		Type:           toMusicArtistKind(h.Type),
	}
}

func toMusicLookupDetail(d *music.LookupDetail) MusicArtistLookupDetail {
	out := MusicArtistLookupDetail{
		AlreadyAdded:   d.AlreadyAdded,
		Area:           optString(d.Area),
		Disambiguation: optString(d.Disambiguation),
		Genre:          optString(d.Genre),
		Genres:         optStrings(d.Genres),
		LibraryId:      optU32(d.LibraryID),
		Mbid:           d.MBID,
		Members:        optStrings(d.Members),
		Name:           d.Name,
		Overview:       optString(d.Overview),
		Since:          optU16(d.Since),
		SortName:       d.SortName,
		Type:           toMusicArtistKind(d.Type),
	}
	if len(d.Releases) > 0 {
		rels := make([]MusicLookupRelease, len(d.Releases))
		for i, r := range d.Releases {
			rels[i] = MusicLookupRelease{
				Mbid:  r.MBID,
				Title: r.Title,
				Type:  MusicAlbumType(r.Type),
				Year:  optU16(r.Year),
			}
		}
		out.Releases = &rels
	}
	return out
}

func toMusicCounts(c db.ArtistCounts) MusicArtistCounts {
	return MusicArtistCounts{
		Albums:         c.Albums,
		Available:      c.Available,
		Downloading:    c.Downloading,
		Monitored:      c.Monitored,
		MonitoredTotal: c.MonitoredTotal,
		StatusTotal:    c.StatusTotal,
		Total:          c.Total,
		Unmonitored:    c.Unmonitored,
		Wanted:         c.Wanted,
	}
}

// errMusicBrainzRateLimited is the shared 429 for MusicBrainz answering 503,
// with Retry-After rounded up to whole seconds. It never sets an *_auth_warn.
func errMusicBrainzRateLimited(err error) RateLimitedJSONResponse {
	var wait int
	if rl, ok := errors.AsType[*metadata.RateLimitedError](err); ok {
		wait = int(math.Ceil(rl.RetryAfter.Seconds()))
	}
	code := codeRateLimited
	return RateLimitedJSONResponse{
		Body: Error{
			Message: fmt.Sprintf(
				"MusicBrainz is rate limiting requests; try again in %d seconds.",
				wait,
			),
			Code: &code,
		},
		Headers: RateLimitedResponseHeaders{RetryAfter: &wait},
	}
}
