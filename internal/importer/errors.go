package importer

import "errors"

// ErrPathNotAllowed is returned when a DownloadRecord.save_path is not
// within any configured Library.AllowedDownloadRoots prefix.
var ErrPathNotAllowed = errors.New("save_path not in allowed download roots")
