package arr

import "errors"

var (
	ErrUnreachable  = errors.New("could not reach the instance")
	ErrUnauthorized = errors.New("the API key was rejected")
	ErrWrongApp     = errors.New("that instance is not the application you selected")
	ErrInvalidURL   = errors.New("the instance URL must be an http or https address")
)
