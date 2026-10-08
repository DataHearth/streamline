package bulkimport

import "errors"

const (
	codeHardcoverKeyRejected   = "hardcover_key_rejected"
	codeHardcoverNotConfigured = "hardcover_not_configured"
)

var (
	ErrInvalidPath = errors.New(
		"source path invalid (not absolute, doesn't exist, or not a directory)",
	)
	ErrPathOutsideLibrary = errors.New(
		"source path is outside library_path (in_place mode requires inside, rename mode requires outside)",
	)
	ErrLibraryPathMissing = errors.New(
		"configured library path for this scan kind does not exist or is not a directory",
	)
	ErrUnsupportedKind    = errors.New("no scanner for this scan kind")
	ErrRootOutsideLibrary = errors.New(
		"every mapped root folder must sit inside the library path for an in_place migration, and outside it for a rename one",
	)
	ErrMissingSourceURL   = errors.New("a migration needs the instance URL")
	ErrScanRunning        = errors.New("another scan is already active")
	ErrScanNotFound       = errors.New("scan not found")
	ErrScanFileNotFound   = errors.New("scan file not found")
	ErrScanNotReviewable  = errors.New("scan is not in awaiting_review state")
	ErrScanNotCancellable = errors.New("scan is not in a cancellable state")
	ErrScanNotDeletable   = errors.New("scan must be cancelled before delete")
	ErrRenameUnsupported  = errors.New(
		"rename mode is not supported for music and book imports",
	)
)
