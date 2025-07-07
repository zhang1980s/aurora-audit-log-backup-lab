package errors

import (
	"errors"
	"fmt"
)

// Common error types
var (
	ErrNotFound      = errors.New("resource not found")
	ErrInvalidInput  = errors.New("invalid input")
	ErrUnauthorized  = errors.New("unauthorized")
	ErrInternal      = errors.New("internal error")
	ErrDependency    = errors.New("dependency failure")
	ErrDownload      = errors.New("download failure")
	ErrUpload        = errors.New("upload failure")
	ErrConfiguration = errors.New("configuration error")
)

// Wrap adds context to an error
func Wrap(err error, message string) error {
	return fmt.Errorf("%s: %w", message, err)
}

// Is checks if an error is of a specific type
func Is(err, target error) bool {
	return errors.Is(err, target)
}

// New creates a new error
func New(message string) error {
	return errors.New(message)
}

// IsNotFound checks if an error is a not found error
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

// IsInvalidInput checks if an error is an invalid input error
func IsInvalidInput(err error) bool {
	return errors.Is(err, ErrInvalidInput)
}

// IsDependency checks if an error is a dependency failure
func IsDependency(err error) bool {
	return errors.Is(err, ErrDependency)
}

// IsDownload checks if an error is a download failure
func IsDownload(err error) bool {
	return errors.Is(err, ErrDownload)
}

// IsUpload checks if an error is an upload failure
func IsUpload(err error) bool {
	return errors.Is(err, ErrUpload)
}

// IsConfiguration checks if an error is a configuration error
func IsConfiguration(err error) bool {
	return errors.Is(err, ErrConfiguration)
}
