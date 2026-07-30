package bulkimport

import "errors"

var (
	ErrInvalidPath = errors.New(
		"source path invalid (not absolute, doesn't exist, or not a directory)",
	)
	ErrPathOutsideLibrary = errors.New(
		"source path is outside library_path (in_place mode requires inside, rename mode requires outside)",
	)
	ErrLibraryPathMissing = errors.New(
		"configured library path (library.movie_path / library.series_path) does not exist or is not a directory",
	)
	ErrScanRunning        = errors.New("another scan is already active")
	ErrScanNotFound       = errors.New("scan not found")
	ErrScanFileNotFound   = errors.New("scan file not found")
	ErrScanNotReviewable  = errors.New("scan is not in awaiting_review state")
	ErrScanNotCancellable = errors.New("scan is not in a cancellable state")
	ErrScanNotDeletable   = errors.New("scan must be cancelled before delete")
)
