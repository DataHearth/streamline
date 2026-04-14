package hygiene

import "errors"

// ErrAutoImportSkipped signals that the auto-import gate rejected a confirmed
// match (e.g. because the movie already has a tracked file). The orphan falls
// through to the queue path.
var ErrAutoImportSkipped = errors.New(
	"hygiene: auto-import skipped (confidence gate)",
)
