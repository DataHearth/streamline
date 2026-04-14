package indexer

import (
	"context"
	"time"
)

type SearchResult struct {
	Title       string
	InfoURL     string
	Download    string
	Size        int64
	Seeders     uint32
	Leechers    uint32
	Category    string
	PublishDate time.Time
	// Indexer is the configured name of the indexer this result came from.
	// Stamped by the search service during cross-indexer aggregation.
	Indexer string
}

type SearchParams struct {
	Query   string
	IMDBID  string
	TMDBID  uint32
	TVDBID  uint32
	Season  uint16
	Episode uint16
}

type Client interface {
	Search(ctx context.Context, params SearchParams) ([]SearchResult, error)
	Feed(ctx context.Context) ([]SearchResult, error)
	TestConnection(ctx context.Context) error
}
