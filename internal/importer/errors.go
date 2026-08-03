package importer

import "errors"

var (
	// ErrPathNotAllowed is returned when a DownloadRecord.save_path is not
	// within any configured Library.AllowedDownloadRoots prefix.
	ErrPathNotAllowed = errors.New("save_path not in allowed download roots")
	// ErrMovieHasFile is returned when a grab imports into a movie that
	// already has a media file and the record did not request replacement.
	ErrMovieHasFile = errors.New("movie already has a media file")
)
