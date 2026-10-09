package bulkimport

import (
	"context"
	"time"

	entimportscan "github.com/datahearth/streamline/ent/importscan"
	"github.com/datahearth/streamline/ent/schema"
)

// StartScanParams is the input for Service.StartScan.
type StartScanParams struct {
	SourcePath string
	Kind       entimportscan.Kind       // movie | series | music | book — empty defaults to movie
	Mode       entimportscan.Mode       // in_place | rename
	ImportMode entimportscan.ImportMode // optional — empty means "use library.import_mode default" (only meaningful when Mode == rename)

	// Source names where the titles come from; empty means the filesystem.
	// A radarr source is always a movie scan and a sonarr source a series
	// scan, so Kind is derived rather than trusted.
	Source    entimportscan.Source
	SourceURL string
	// APIKey is handed to the fetch goroutine by value and never persisted.
	APIKey   string
	Mappings schema.ScanMappings

	// BeforeCreate runs once the start has passed every check StartScan makes
	// and before the scan row is written; an error from it refuses the start
	// and is returned as is. A migration writes the quality profiles it
	// creates here, so a start refused for any reason leaves none behind.
	BeforeCreate func(context.Context) error
}

const (
	scanConcurrency         = 4
	cancellationPollEvery   = 250 * time.Millisecond
	bulkInsertBatchSize     = 32
	historyPageSize         = 20
	reviewPageSize          = 50
	failureMessageOnRestart = "server restarted while scan was active"
)
