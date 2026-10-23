package mediaserver

import "errors"

var (
	ErrServerNotFound    = errors.New("media server not found")
	ErrInvalidServerType = errors.New("invalid server type")
	ErrTestFailed        = errors.New("connection test failed")
	ErrMovieNotFound     = errors.New("movie not found on media server")
	ErrShowNotFound      = errors.New("series not found on media server")
)
