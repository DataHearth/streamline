package bulkimport

import (
	"context"

	"github.com/datahearth/streamline/ent"
)

func (s *Service) runCommitMusic(ctx context.Context, scan *ent.ImportScan) {
	s.markScanFailed(ctx, scan.ID, "music commit is not implemented yet")
}
