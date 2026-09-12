package repository

import "errors"

var (
	// ErrNotFound is returned when a requested record does not exist.
	ErrNotFound = errors.New("not found")
	// ErrConstraint is returned when a database constraint rejects a write.
	ErrConstraint = errors.New("constraint violation")
	// ErrAlreadyPublished is returned when publishing a version that is
	// already published.
	ErrAlreadyPublished = errors.New("version already published")
	// ErrNotLatest is returned when publishing a version that is not the
	// latest one.
	ErrNotLatest = errors.New("version is not the latest")
)
