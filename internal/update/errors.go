package update

import "errors"

// skipError marks the reason a container was deliberately left untouched, such
// as a pre-update hook asking to skip it or an update canceled before reaching
// it, so the container is reported as skipped rather than failed. Its message
// is the reason's message.
type skipError struct {
	// reason explains why the container was skipped.
	reason error
}

// Error returns the reason's message.
func (e skipError) Error() string {
	return e.reason.Error()
}

// Unwrap returns the reason.
func (e skipError) Unwrap() error {
	return e.reason
}

// skipped marks reason as the cause of a deliberate skip.
//
// Parameters:
//   - reason: Why the container was left untouched.
//
// Returns:
//   - error: The reason, marked as a skip.
func skipped(reason error) error {
	return skipError{reason: reason}
}

// isSkip reports whether err marks a deliberate skip rather than a failure.
// Only errors marked by skipped are skips, so a failure that wraps a
// cancellation, such as a recreation canceled after a container was stopped,
// stays a failure.
//
// Parameters:
//   - err: The outcome of a container's stop or restart.
//
// Returns:
//   - bool: True when err marks a skip.
func isSkip(err error) bool {
	_, ok := errors.AsType[skipError](err)

	return ok
}
