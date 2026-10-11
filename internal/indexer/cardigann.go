package indexer

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"

	"github.com/datahearth/streamline/internal/cardigann/definitions"
	"github.com/datahearth/streamline/internal/cardigann/engine"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/otelx"
)

// Cardigann runs a tracker from an embedded Cardigann definition — the
// native indexer that stands in for a Prowlarr or Jackett entry.
type Cardigann struct {
	engine *engine.Engine
}

func newCardigann(ix config.IndexerEntry) (*Cardigann, error) {
	if ix.Cardigann == nil {
		return nil, fmt.Errorf("indexer %q has no cardigann block", ix.Name)
	}
	def, err := definitions.Load(ix.Cardigann.Definition)
	if err != nil {
		return nil, err
	}
	eng, err := engine.New(def, engine.Options{
		Name:     ix.Name,
		Site:     ix.Cardigann.Site,
		Settings: ix.Cardigann.Settings,
		Client:   otelx.HTTPClient,
	})
	if err != nil {
		return nil, err
	}
	return &Cardigann{engine: eng}, nil
}

func cardigannKind(k MediaKind) (engine.Kind, []int) {
	switch k {
	case KindMovie:
		return engine.KindMovie, []int{2000}
	case KindTV:
		return engine.KindTV, []int{5000}
	case KindMusic:
		return engine.KindMusic, []int{3000}
	case KindEbook:
		return engine.KindBook, []int{7000, 7020}
	case KindAudiobook:
		return engine.KindBook, []int{3030}
	default:
		return engine.KindSearch, nil
	}
}

func (c *Cardigann) Search(
	ctx context.Context,
	params SearchParams,
) ([]SearchResult, error) {
	kind, cats := cardigannKind(params.Kind)
	releases, err := c.engine.Search(ctx, engine.Query{
		Kind:       kind,
		Term:       params.Query,
		Season:     int(params.Season),
		Episode:    int(params.Episode),
		IMDbID:     params.IMDbID,
		TMDBID:     int(params.TMDBID),
		TVDBID:     int(params.TVDBID),
		Categories: cats,
	})
	if err != nil {
		return nil, cardigannError(err)
	}
	return mapReleases(releases), nil
}

// Feed is a search with no term, which the definitions answer with their
// latest releases — what Prowlarr's RSS for a Cardigann tracker is.
func (c *Cardigann) Feed(ctx context.Context) ([]SearchResult, error) {
	releases, err := c.engine.Search(ctx, engine.Query{})
	if err != nil {
		return nil, cardigannError(err)
	}
	return mapReleases(releases), nil
}

// TestConnection runs the feed query: a tracker that answers it in the shape
// its definition describes is reachable and readable, results or not.
func (c *Cardigann) TestConnection(ctx context.Context) error {
	_, err := c.engine.Search(ctx, engine.Query{})
	return cardigannError(err)
}

// cardigannError files an engine failure under the sentinel the handlers and
// metrics read; a transport failure has none of the engine's own.
func cardigannError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled),
		errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.Is(err, engine.ErrStatus):
		return fmt.Errorf("%w: %w", ErrUnexpectedStatus, err)
	case errors.Is(err, engine.ErrParse):
		return fmt.Errorf("%w: %w", ErrBadResponse, err)
	default:
		var transport interface{ Timeout() bool }
		if errors.As(err, &transport) {
			return fmt.Errorf(
				"%w: %w", ErrUnreachable, otelx.RedactTransportError(err),
			)
		}
		return fmt.Errorf("%w: %w", ErrBadResponse, err)
	}
}

func mapReleases(releases []engine.Release) []SearchResult {
	out := make([]SearchResult, 0, len(releases))
	for _, r := range releases {
		link := r.Download
		if link == "" {
			link = r.Magnet
		}
		if r.Title == "" || link == "" {
			continue
		}
		out = append(out, SearchResult{
			Title:       r.Title,
			InfoURL:     r.Details,
			Download:    link,
			Size:        r.Size,
			Seeders:     clampU32(r.Seeders),
			Leechers:    clampU32(r.Leechers),
			Category:    releaseCategory(r.Category),
			PublishDate: r.Published,
			InfoHash:    infoHash(r.InfoHash, r.Magnet, link),
			TMDBID:      clampU32(r.TMDBID),
			TVDBID:      clampU32(r.TVDBID),
			IMDbID:      clampU32(r.IMDbID),
		})
	}
	return out
}

// releaseCategory picks the most specific newznab category a row mapped to,
// the subcategory being what tells an audiobook (3030) from music.
func releaseCategory(cats []int) string {
	if len(cats) == 0 {
		return ""
	}
	i := max(slices.IndexFunc(cats, func(c int) bool { return c%1000 != 0 }), 0)
	return strconv.Itoa(cats[i])
}

func clampU32(n int64) uint32 {
	switch {
	case n < 0:
		return 0
	case n > math.MaxUint32:
		return math.MaxUint32
	default:
		return uint32(n)
	}
}

// CardigannSite is where a cardigann entry's tracker lives — the host its
// release and download links point at.
func CardigannSite(ix config.IndexerEntry) (string, error) {
	if ix.Cardigann == nil {
		return "", fmt.Errorf("indexer %q has no cardigann block", ix.Name)
	}
	def, err := definitions.Load(ix.Cardigann.Definition)
	if err != nil {
		return "", err
	}
	u, err := engine.Site(def, ix.Cardigann.Site)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// DownloadTarget is what a release link resolved to: a magnet, or an
// http(s) URL fetched with Method and Header.
type DownloadTarget struct {
	URL    string
	Method string
	Header http.Header
}

// ResolveDownload finds the enabled cardigann indexer whose site link points
// at and resolves it through that definition's download block. ok is false
// when no cardigann indexer owns the link, which is then fetched as it is.
func ResolveDownload(
	ctx context.Context,
	link string,
) (DownloadTarget, bool, error) {
	for _, ix := range config.EnabledIndexers() {
		if ix.Protocol != "cardigann" {
			continue
		}
		c, err := newCardigann(ix)
		if err != nil {
			return DownloadTarget{}, false, err
		}
		if !c.engine.Owns(link) {
			continue
		}
		d, err := c.engine.Resolve(ctx, link)
		if err != nil {
			return DownloadTarget{}, true, cardigannError(err)
		}
		return DownloadTarget{
			URL:    d.URL,
			Method: d.Method,
			Header: d.Header,
		}, true, nil
	}
	return DownloadTarget{}, false, nil
}
