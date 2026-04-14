package library

import "errors"

var (
	ErrNoMedia       = errors.New("no media file found")
	ErrMultipleMedia = errors.New("multiple media files require manual handling")
	ErrSampleOnly    = errors.New("only sample files present")
	ErrDestExists    = errors.New("destination already exists")
	ErrUnsafePath    = errors.New("template output escapes library root")
)
