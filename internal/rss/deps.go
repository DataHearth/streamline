package rss

import (
	"context"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/episode"
	"github.com/datahearth/streamline/internal/indexer"
)

// IndexerSearcher is the subset of indexer.Service that rss.MissingSearcher
// needs.
type IndexerSearcher interface {
	SearchMovie(
		ctx context.Context,
		titles []string,
		tmdbID uint32,
	) ([]indexer.SearchResult, error)
}

// Downloader is the subset of download.Manager that rss.MissingSearcher
// needs.
type Downloader interface {
	Grab(
		ctx context.Context,
		result indexer.SearchResult,
		movieID uint32,
	) (*ent.DownloadRecord, error)
}

// IndexerFeeder is the subset of indexer.Manager that rss.FeedScanner needs.
// The enabled-indexer set is read from config; this only fetches a feed by name.
type IndexerFeeder interface {
	Feed(ctx context.Context, indexerName string) ([]indexer.SearchResult, error)
}

// TVIndexerSearcher is the subset of indexer.Manager that
// rss.EpisodeMissingSearcher needs.
type TVIndexerSearcher interface {
	SearchSeason(
		ctx context.Context,
		titles []string,
		tvdbID uint32,
		season uint16,
	) ([]indexer.SearchResult, error)
	SearchEpisode(
		ctx context.Context,
		titles []string,
		tvdbID uint32,
		season, episode uint16,
	) ([]indexer.SearchResult, error)
}

// EpisodeGrabber is the subset of download.Downloader that
// rss.EpisodeMissingSearcher needs.
type EpisodeGrabber interface {
	GrabEpisode(
		ctx context.Context,
		result indexer.SearchResult,
		episodeID uint32,
	) (*ent.DownloadRecord, error)
}

// WantedEpisodeLister is the subset of db.Store that
// rss.EpisodeMissingSearcher needs.
type WantedEpisodeLister interface {
	ListWantedEpisodes(ctx context.Context) ([]*ent.TVShow, error)
	SetEpisodeStatus(ctx context.Context, id uint32, status episode.Status) error
	SetEpisodeLastSearchAt(ctx context.Context, id uint32, when time.Time) error
	IncrementEpisodeGrabFailures(ctx context.Context, id uint32) error
	ResetEpisodeGrabFailures(ctx context.Context, id uint32) error
}
