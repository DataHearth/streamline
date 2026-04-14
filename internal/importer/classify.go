package importer

import (
	"errors"

	"github.com/datahearth/streamline/internal/library"
)

type classification int

const (
	retryable classification = iota
	terminal
)

func classify(err error) classification {
	switch {
	case errors.Is(err, library.ErrNoMedia),
		errors.Is(err, library.ErrMultipleMedia),
		errors.Is(err, library.ErrSampleOnly),
		errors.Is(err, library.ErrDestExists),
		errors.Is(err, library.ErrUnsafePath),
		errors.Is(err, ErrPathNotAllowed):
		return terminal
	default:
		return retryable
	}
}
