// Package retry marks errors that will not be fixed by repeating an operation.
package retry

import "errors"

type nonRetryable interface{ NonRetryable() bool }

type noRetryError struct{ err error }

func (e noRetryError) Error() string    { return e.err.Error() }
func (e noRetryError) Unwrap() error    { return e.err }
func (noRetryError) NonRetryable() bool { return true }

// NoRetry marks err as non-retryable while preserving errors.Is/errors.As.
func NoRetry(err error) error {
	if err == nil || !ShouldRetry(err) {
		return err
	}
	return noRetryError{err: err}
}

// ShouldRetry reports whether err should be retried. A nil error is not retryable.
func ShouldRetry(err error) bool {
	if err == nil {
		return false
	}
	var marker nonRetryable
	return !errors.As(err, &marker) || !marker.NonRetryable()
}
