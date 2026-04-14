package bulkimport

import (
	"time"

	entimportscan "github.com/datahearth/streamline/ent/importscan"
)

// StartScanParams is the input for Service.StartScan.
type StartScanParams struct {
	SourcePath string
	Mode       entimportscan.Mode       // in_place | rename
	ImportMode entimportscan.ImportMode // optional — empty means "use library.import_mode default" (only meaningful when Mode == rename)
}

const (
	scanConcurrency         = 4
	cancellationPollEvery   = 250 * time.Millisecond
	bulkInsertBatchSize     = 32
	historyPageSize         = 20
	reviewPageSize          = 50
	failureMessageOnRestart = "server restarted while scan was active"
)
