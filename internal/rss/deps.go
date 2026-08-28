package rss

import (
	"context"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
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
	) ([]indexer.SearchResult, int, error)
}

// EpisodeGrabber is the subset of download.Downloader that
// rss.EpisodeMissingSearcher needs.
type EpisodeGrabber interface {
	GrabEpisode(
		ctx context.Context,
		result indexer.SearchResult,
		episodeID uint32,
		wantedEpisodes []uint32,
	) (*ent.DownloadRecord, error)
}

// EligibleEpisodeLister is the subset of db.Store that
// rss.EpisodeMissingSearcher needs.
type EligibleEpisodeLister interface {
	ListEligibleEpisodesForSync(
		ctx context.Context,
		maxGrabFailures uint8,
		notSearchedSince time.Time,
		airedBefore time.Time,
	) ([]*ent.TVShow, error)
	SetEpisodeStatus(ctx context.Context, id uint32, status episode.Status) error
	SetEpisodeLastSearchAt(ctx context.Context, id uint32, when time.Time) error
	IncrementEpisodeGrabFailures(ctx context.Context, id uint32) error
	ResetEpisodeGrabFailures(ctx context.Context, id uint32) error
	// SeasonEpisodeCounts sizes a season pack: the episode edges above are
	// filtered to what a pass may act on, and a pack costs every episode it
	// holds.
	SeasonEpisodeCounts(
		ctx context.Context,
		showIDs []uint32,
	) (map[uint32]map[uint16]int, error)
	// UpgradeCandidateShow loads the on-disk episodes one pack grab may
	// replace, for the show it is about to grab for.
	UpgradeCandidateShow(ctx context.Context, showID uint32) (*ent.TVShow, error)
	// SetDownloadRecordReplaceMode tells the import which of the episodes a
	// grab covers it may overwrite.
	SetDownloadRecordReplaceMode(
		ctx context.Context,
		id uint32,
		mode downloadrecord.ReplaceMode,
	) error
}

// TVFeedStore is what rss.TVFeedScanner needs on top of the missing-search
// surface: every show's on-disk episodes in one query, since a feed tick
// matches its items against the whole library rather than one show.
type TVFeedStore interface {
	EligibleEpisodeLister
	ListUpgradeCandidateShows(ctx context.Context) ([]*ent.TVShow, error)
}
