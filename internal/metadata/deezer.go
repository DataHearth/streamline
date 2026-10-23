package metadata

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/time/rate"

	"github.com/datahearth/streamline/internal/otelx"
)

const (
	deezerBaseURL = "https://api.deezer.com"

	// maxDeezerResponse bounds one Deezer answer: a single album object or the
	// first page of a search.
	maxDeezerResponse = 1 << 20
)

// Deezer is the CoverProvider implementation. Public endpoints, no key; the
// documented quota is 50 requests per 5 seconds.
type Deezer struct {
	client  *http.Client
	limiter *rate.Limiter
}

func NewDeezer() *Deezer {
	c := *otelx.HTTPClient
	return &Deezer{
		client:  &c,
		limiter: rate.NewLimiter(rate.Every(100*time.Millisecond), 1),
	}
}

type deezerAlbum struct {
	CoverXL string `json:"cover_xl"`
	Artist  struct {
		Name string `json:"name"`
	} `json:"artist"`
}

// get reports found=false for a 404; Deezer answers an unknown id with a 200
// {"error":…} body instead, which decodes to an album with no cover.
func (d *Deezer) get(
	ctx context.Context,
	path string,
	params url.Values,
	out any,
) (bool, error) {
	ctx, span := tracer.Start(ctx, "metadata.deezer.get",
		trace.WithAttributes(attribute.String("deezer.endpoint", path)))
	defer span.End()

	if err := d.limiter.Wait(ctx); err != nil {
		return false, otelx.RecordSpanError(span, err)
	}
	u := deezerBaseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false, otelx.RecordSpanError(span, err)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return false, otelx.RecordSpanError(span, err)
	}
	defer resp.Body.Close()

	recordProviderStatus(ctx, resp.StatusCode)
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, otelx.RecordSpanError(span,
			fmt.Errorf("deezer: %s returned %d", path, resp.StatusCode))
	}
	return true, otelx.RecordSpanError(
		span,
		otelx.DecodeJSON(resp.Body, maxDeezerResponse, out),
	)
}

// DeezerArtist is the slice of a Deezer artist the lookup art proxy needs.
type DeezerArtist struct {
	ID         uint32 `json:"id"`
	Name       string `json:"name"`
	PictureURL string `json:"picture_xl"`
}

// ArtistByID returns the artist with the Deezer id, or nil when Deezer has
// none. The id comes from MusicBrainz's own url-rel, so no name guess is
// involved.
func (d *Deezer) ArtistByID(ctx context.Context, id uint32) (*DeezerArtist, error) {
	var a DeezerArtist
	found, err := d.get(ctx, "/artist/"+strconv.FormatUint(uint64(id), 10), nil, &a)
	if err != nil || !found || a.ID == 0 {
		return nil, err
	}
	return &a, nil
}

// SearchArtists returns Deezer's first page of artist hits for name. Whether a
// hit is the right artist is the caller's call; Deezer ranks namesakes
// together, so only an unambiguous result is safe to use.
func (d *Deezer) SearchArtists(
	ctx context.Context,
	name string,
) ([]DeezerArtist, error) {
	var payload struct {
		Data []DeezerArtist `json:"data"`
	}
	found, err := d.get(ctx, "/search/artist", url.Values{"q": {name}}, &payload)
	if err != nil || !found {
		return nil, err
	}
	return payload.Data, nil
}

// CoverByUPC returns the 1000px cover of the album carrying the barcode, or
// "" when Deezer has none.
func (d *Deezer) CoverByUPC(ctx context.Context, barcode string) (string, error) {
	if barcode == "" {
		return "", nil
	}
	var album deezerAlbum
	found, err := d.get(ctx, "/album/upc:"+url.PathEscape(barcode), nil, &album)
	if err != nil || !found {
		return "", err
	}
	return album.CoverXL, nil
}

// SearchCover returns Deezer's first hit for the artist and album, or nil when
// there is none. Whether the hit is the right album is the caller's call.
func (d *Deezer) SearchCover(
	ctx context.Context,
	artist, album string,
) (*CoverHit, error) {
	var payload struct {
		Data []deezerAlbum `json:"data"`
	}
	q := `artist:` + luceneQuote(artist) + ` album:` + luceneQuote(album)
	found, err := d.get(ctx, "/search/album", url.Values{"q": {q}}, &payload)
	if err != nil || !found || len(payload.Data) == 0 {
		return nil, err
	}
	hit := payload.Data[0]
	if hit.CoverXL == "" {
		return nil, nil
	}
	return &CoverHit{ArtistName: hit.Artist.Name, CoverURL: hit.CoverXL}, nil
}
