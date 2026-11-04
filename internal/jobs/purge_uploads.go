package jobs

import (
	"context"

	"github.com/datahearth/streamline/internal/scheduler"
	"github.com/datahearth/streamline/internal/upload"
)

// UploadKeeper lists the staging folders an uploaded record still needs.
type UploadKeeper interface {
	ListKeptUploadPaths(ctx context.Context) ([]string, error)
}

// PurgeUploads removes every staging folder that is neither being received
// nor needed by a live uploaded record. Intended to run hourly.
func PurgeUploads(store UploadKeeper) scheduler.JobFunc {
	return func(ctx context.Context) error {
		return upload.Sweep(ctx, store.ListKeptUploadPaths)
	}
}
