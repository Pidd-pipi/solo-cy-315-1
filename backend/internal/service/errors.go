package service

import "errors"

var (
	// ErrNotFound indicates a requested record does not exist.
	ErrNotFound = errors.New("not found")
	// ErrInvalid indicates malformed business input.
	ErrInvalid = errors.New("invalid input")
	// ErrConflict indicates a constraint violation.
	ErrConflict = errors.New("resource conflict")
	// ErrVersionAlreadyPublished indicates the schedule version is already published.
	ErrVersionAlreadyPublished = errors.New("schedule version already published")
	// ErrVersionNotLatest indicates only the latest schedule version can be published.
	ErrVersionNotLatest = errors.New("only the latest schedule version can be published")
)
