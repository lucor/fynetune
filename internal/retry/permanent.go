// Package retry marks errors that will not be fixed by repeating an operation.
package retry

import "errors"

type permanent interface{ NonRetryable() bool }

type permanentError struct{ err error }

func (e permanentError) Error() string    { return e.err.Error() }
func (e permanentError) Unwrap() error    { return e.err }
func (permanentError) NonRetryable() bool { return true }

// Permanent marks err as non-retryable while preserving errors.Is/errors.As.
func Permanent(err error) error {
	if err == nil || IsPermanent(err) {
		return err
	}
	return permanentError{err: err}
}

// IsPermanent reports whether err or one of its causes is marked non-retryable.
func IsPermanent(err error) bool {
	var marker permanent
	return errors.As(err, &marker) && marker.NonRetryable()
}
