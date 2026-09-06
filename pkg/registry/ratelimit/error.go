package ratelimit

import (
	"errors"
	"fmt"
	"time"
)

// ErrRateLimited indicates a registry rejected a request for exceeding its rate limit.
var ErrRateLimited = errors.New("registry rate limited")

const (
	// minHonorWait is the floor applied to tiny Retry-After values such as 331µs.
	minHonorWait = 100 * time.Millisecond
	// maxHonorWait is the longest Retry-After this process will sleep in one update cycle.
	maxHonorWait = 30 * time.Second
	// maxRetryElapsed is the production in-cycle bound for retrying 429s.
	maxRetryElapsed = 30 * time.Second
	// DefaultBodyLimit is how many response bytes we read when parsing a 429.
	DefaultBodyLimit = 4096
	// retryAfterCaptureCount is the expected regex group count for retry-after.
	retryAfterCaptureCount = 2
	// allowedCaptureCount is the expected regex group count for allowed quotas.
	allowedCaptureCount = 3
	// equalJitterDivisor splits a wait into the equal-jitter half range.
	equalJitterDivisor = 2
)

// retryElapsed bounds how long a single operation keeps retrying 429s.
// Tests shorten it so elapsed-budget exhaustion does not sleep 30s.
var retryElapsed = maxRetryElapsed

// Error is a registry 429 with the wait and quota the registry advertised.
type Error struct {
	// StatusCode is the HTTP status when the limit came from an HTTP response.
	StatusCode int
	// RetryAfter is the wait parsed from Retry-After or the response body.
	RetryAfter time.Duration
	// Allowed is the advertised request budget. Zero when the registry omitted it.
	Allowed int
	// AllowedWindow is the period for Allowed. Zero when the registry omitted it.
	AllowedWindow time.Duration
	// Host is the registry host that produced the limit when known.
	Host string
	// Message is the raw registry or Docker-stream text.
	Message string
}

// Error describes the rate limit for logs and error wrapping.
//
// Returns:
//   - string: Human-readable rate-limit error.
func (e *Error) Error() string {
	if e == nil {
		return ErrRateLimited.Error()
	}

	if e.Allowed > 0 && e.AllowedWindow > 0 {
		return fmt.Sprintf(
			"%s: retry-after %s allowed %d per %s",
			ErrRateLimited.Error(),
			e.RetryAfter,
			e.Allowed,
			e.AllowedWindow,
		)
	}

	if e.RetryAfter > 0 {
		return fmt.Sprintf("%s: retry-after %s", ErrRateLimited.Error(), e.RetryAfter)
	}

	if e.Message != "" {
		return fmt.Sprintf("%s: %s", ErrRateLimited.Error(), e.Message)
	}

	return ErrRateLimited.Error()
}

// Unwrap exposes ErrRateLimited so errors.Is matches.
//
// Returns:
//   - error: The sentinel rate-limit error.
func (e *Error) Unwrap() error {
	return ErrRateLimited
}

// Is reports whether err is a registry rate-limit error.
//
// Parameters:
//   - err: Error to inspect. May be wrapped.
//
// Returns:
//   - bool: True when err unwraps to ErrRateLimited.
func Is(err error) bool {
	return errors.Is(err, ErrRateLimited)
}

// Decision converts a parsed 429 into a sleep or a give-up.
//
// Tiny Retry-After values are raised to minHonorWait so a 331µs GHCR token
// does not cause an immediate retry. Waits longer than maxHonorWait are not
// slept in-process. The next scheduled Watchtower run can try again.
//
// Parameters:
//   - info: Parsed rate-limit details. Nil uses the minimum wait.
//
// Returns:
//   - time.Duration: How long to wait before the next attempt.
//   - bool: True when the caller should stop retrying this cycle.
func Decision(info *Error) (time.Duration, bool) {
	wait := minHonorWait
	if info != nil && info.RetryAfter > 0 {
		wait = info.RetryAfter
	}

	if wait > maxHonorWait {
		return 0, true
	}

	if wait < minHonorWait {
		wait = minHonorWait
	}

	return wait, false
}
