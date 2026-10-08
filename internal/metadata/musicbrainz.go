package metadata

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/time/rate"

	"github.com/datahearth/streamline/internal/buildinfo"
	"github.com/datahearth/streamline/internal/otelx"
)

const mbBaseURL = "https://musicbrainz.org/ws/2"

// MusicBrainz is the MusicProvider implementation. No API key; MusicBrainz
// mandates a descriptive User-Agent and at most 1 request/second.
type MusicBrainz struct {
	client  *http.Client
	limiter *rate.Limiter
}

func NewMusicBrainz() *MusicBrainz {
	c := *otelx.HTTPClient
	return &MusicBrainz{
		client:  &c,
		limiter: rate.NewLimiter(rate.Every(time.Second), 1),
	}
}

func (m *MusicBrainz) userAgent() string {
	version := buildinfo.Version
	if version == "" {
		version = "dev"
	}
	return "streamline/" + version + " (https://github.com/datahearth/streamline)"
}

func (m *MusicBrainz) get(
	ctx context.Context,
	path string,
	params url.Values,
	out any,
) error {
	ctx, span := tracer.Start(ctx, "metadata.musicbrainz.get",
		trace.WithAttributes(attribute.String("musicbrainz.endpoint", path)))
	defer span.End()

	if err := m.limiter.Wait(ctx); err != nil {
		return otelx.RecordSpanError(span, err)
	}
	params.Set("fmt", "json")
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
	if resp.StatusCode != http.StatusOK {
		slog.WarnContext(ctx, "musicbrainz request non-200",
			"musicbrainz.endpoint", path, "http.status_code", resp.StatusCode)
		return otelx.RecordSpanError(span,
			fmt.Errorf("musicbrainz: %s returned %d", path, resp.StatusCode))
	}
	return otelx.RecordSpanError(
		span,
		otelx.DecodeJSON(resp.Body, maxProviderResponse, out),
	)
}

type mbArtist struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	SortName       string `json:"sort-name"`
	Disambiguation string `json:"disambiguation"`
	Score          uint8  `json:"score"`
}

func (a mbArtist) toResult() ArtistResult {
	return ArtistResult{
		MBID:           a.ID,
		Name:           a.Name,
		SortName:       a.SortName,
		Disambiguation: a.Disambiguation,
		Score:          a.Score,
	}
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
	results := make([]ArtistResult, 0, len(payload.Artists))
	for _, a := range payload.Artists {
		results = append(results, a.toResult())
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
	params := url.Values{"query": {q}, "limit": {"10"}}
	if err := m.get(ctx, "/release-group", params, &payload); err != nil {
		return nil, otelx.RecordSpanError(span, err)
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
		url.Values{},
		&artist,
	); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	details := &ArtistDetails{ArtistResult: artist.toResult()}
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

type mbRelease struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Date   string `json:"date"`
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

func (m *MusicBrainz) GetReleaseGroup(
	ctx context.Context,
	mbid string,
) (*ReleaseGroupDetails, error) {
	ctx, span := tracer.Start(ctx, "metadata.musicbrainz.get_release_group",
		trace.WithAttributes(attribute.String("musicbrainz.mbid", mbid)))
	defer span.End()

	var rg struct {
		mbReleaseGroup
		Releases []mbRelease `json:"releases"`
	}
	if err := m.get(ctx, "/release-group/"+url.PathEscape(mbid),
		url.Values{"inc": {"releases"}}, &rg); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	details := &ReleaseGroupDetails{
		ReleaseGroupInfo: rg.toInfo(),
		ReleaseMBID:      canonicalRelease(rg.Releases),
	}
	if details.ReleaseMBID == "" {
		return details, nil
	}

	var rel struct {
		Media []struct {
			Position uint8 `json:"position"`
			Tracks   []struct {
				Position  uint16 `json:"position"`
				Title     string `json:"title"`
				Length    uint32 `json:"length"`
				Recording struct {
					ID string `json:"id"`
				} `json:"recording"`
			} `json:"tracks"`
		} `json:"media"`
	}
	if err := m.get(ctx, "/release/"+url.PathEscape(details.ReleaseMBID),
		url.Values{"inc": {"recordings"}}, &rel); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	for _, medium := range rel.Media {
		disc := medium.Position
		if disc == 0 {
			disc = 1
		}
		for _, t := range medium.Tracks {
			details.Tracks = append(details.Tracks, TrackInfo{
				MBID:     t.Recording.ID,
				Title:    t.Title,
				Disc:     disc,
				Position: t.Position,
				Duration: t.Length / 1000,
			})
		}
	}
	return details, nil
}
