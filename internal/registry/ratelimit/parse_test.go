package ratelimit

import (
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cerrdefs "github.com/containerd/errdefs"
)

// Registry error text as engines report it.
const (
	// hubPullLimit is the unauthenticated limit as moby/moby#54133 shows an
	// engine using the containerd image store report it.
	hubPullLimit = "error from registry: You have reached your unauthenticated pull rate limit. " +
		"https://www.docker.com/increase-rate-limit"
	// hubAuthenticatedPullLimit is the authenticated limit with Docker Hub's
	// documented body.
	hubAuthenticatedPullLimit = "error from registry: You have reached your pull rate limit. " +
		"You may increase the limit by authenticating and upgrading: https://www.docker.com/increase-rate-limits"
	// unmarkedRefusal is registry text that names no rate limit.
	unmarkedRefusal = "error from registry: request refused"
)

func TestParseRetryAfterHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		header string
		want   time.Duration
		wantOK bool
	}{
		{
			name:   "integer seconds",
			header: "5",
			want:   5 * time.Second,
			wantOK: true,
		},
		{
			name:   "zero seconds is not a wait",
			header: "0",
			want:   0,
			wantOK: false,
		},
		{
			name:   "empty header",
			header: "",
			wantOK: false,
		},
		{
			name:   "invalid text",
			header: "soon",
			wantOK: false,
		},
		{
			name:   "http date in the future",
			header: time.Now().UTC().Add(8 * time.Second).Format(http.TimeFormat),
			wantOK: true,
		},
		{
			name:   "http date in the past is not a wait",
			header: time.Now().UTC().Add(-time.Minute).Format(http.TimeFormat),
			wantOK: false,
		},
		{
			name:   "negative seconds is not a wait",
			header: "-1",
			wantOK: false,
		},
		{
			name:   "whitespace around integer seconds",
			header: "  3  ",
			want:   3 * time.Second,
			wantOK: true,
		},
		{
			name:   "largest seconds that fit a duration",
			header: "9223372036",
			want:   9223372036 * time.Second,
			wantOK: true,
		},
		{
			name:   "seconds that overflow a duration are capped",
			header: "9223372037",
			want:   time.Duration(math.MaxInt64),
			wantOK: true,
		},
		{
			name:   "seconds beyond int64 are capped",
			header: "99999999999999999999",
			want:   time.Duration(math.MaxInt64),
			wantOK: true,
		},
		{
			name:   "negative seconds beyond int64 are not a wait",
			header: "-99999999999999999999",
			wantOK: false,
		},
		{
			name:   "seconds beyond int64 with a suffix are not a wait",
			header: "99999999999999999999abc",
			wantOK: false,
		},
		{
			name:   "seconds beyond int64 with a trailing sign are not a wait",
			header: "99999999999999999999+1",
			wantOK: false,
		},
		{
			name:   "seconds beyond int64 with inner whitespace are not a wait",
			header: "99999999999999999999 1",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := parseRetryAfterHeader(tt.header)
			assert.Equal(t, tt.wantOK, ok)

			if !tt.wantOK {
				return
			}

			if tt.want > 0 {
				assert.Equal(t, tt.want, got)

				return
			}

			assert.Greater(t, got, 4*time.Second)
			assert.Less(t, got, 12*time.Second)
		})
	}
}

func TestParseQuotaMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		message    string
		retryAfter time.Duration
		allowed    int
		window     time.Duration
	}{
		{
			name:       "ghcr toomanyrequests with microseconds and per minute quota",
			message:    "toomanyrequests: retry-after: 331.163µs, allowed: 44000/minute",
			retryAfter: 331163 * time.Nanosecond,
			allowed:    44000,
			window:     time.Minute,
		},
		{
			name:       "ghcr milliseconds",
			message:    "toomanyrequests: retry-after: 1.15031ms, allowed: 44000/minute",
			retryAfter: time.Duration(1.15031 * float64(time.Millisecond)),
			allowed:    44000,
			window:     time.Minute,
		},
		{
			name:    "plain toomanyrequests without details",
			message: "toomanyrequests",
		},
		{
			name:    "empty message",
			message: "",
		},
		{
			name:    "allowed per hour",
			message: "allowed: 100/hour",
			allowed: 100,
			window:  time.Hour,
		},
		{
			name:    "allowed per second",
			message: "allowed: 10/second",
			allowed: 10,
			window:  time.Second,
		},
		{
			name:    "allowed per secs",
			message: "allowed: 10/secs",
			allowed: 10,
			window:  time.Second,
		},
		{
			name:    "allowed per mins",
			message: "allowed: 20/mins",
			allowed: 20,
			window:  time.Minute,
		},
		{
			name:    "allowed per hrs",
			message: "allowed: 5/hrs",
			allowed: 5,
			window:  time.Hour,
		},
		{
			name:    "allowed per minutes",
			message: "allowed: 8/minutes",
			allowed: 8,
			window:  time.Minute,
		},
		{
			name:    "allowed per min",
			message: "allowed: 8/min",
			allowed: 8,
			window:  time.Minute,
		},
		{
			name:    "allowed per hours",
			message: "allowed: 3/hours",
			allowed: 3,
			window:  time.Hour,
		},
		{
			name:    "allowed per sec",
			message: "allowed: 15/sec",
			allowed: 15,
			window:  time.Second,
		},
		{
			name:    "allowed per seconds",
			message: "allowed: 15/seconds",
			allowed: 15,
			window:  time.Second,
		},
		{
			name:    "unknown allowed unit is ignored",
			message: "allowed: 5/fortnight",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			retryAfter, allowed, window := parseQuotaMessage(tt.message)
			assert.InDelta(t, float64(tt.retryAfter), float64(retryAfter), float64(time.Microsecond))
			assert.Equal(t, tt.allowed, allowed)
			assert.Equal(t, tt.window, window)
		})
	}
}

func TestFromResponse(t *testing.T) {
	t.Parallel()

	header := make(http.Header)
	header.Set("Retry-After", "2")
	header.Set("Ratelimit-Limit", "100;w=3600")

	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     header,
		Body: io.NopCloser(strings.NewReader(
			`toomanyrequests: retry-after: 331.163µs, allowed: 44000/minute`,
		)),
		Request: &http.Request{},
	}

	got := FromResponse(resp, []byte("toomanyrequests: retry-after: 331.163µs, allowed: 44000/minute"))
	require.NotNil(t, got)
	require.ErrorIs(t, got, ErrRateLimited)
	assert.Equal(t, http.StatusTooManyRequests, got.StatusCode)
	assert.Equal(t, 2*time.Second, got.RetryAfter)
	assert.Equal(t, 44000, got.Allowed)
	assert.Equal(t, time.Minute, got.AllowedWindow)
}

// TestFromResponseHugeRetryAfterExceedsHonorWindow verifies that a Retry-After
// too long for a duration stops retries instead of wrapping to a short wait.
func TestFromResponseHugeRetryAfterExceedsHonorWindow(t *testing.T) {
	t.Parallel()

	for _, header := range []string{"9223372037", "99999999999999999999"} {
		t.Run(header, func(t *testing.T) {
			t.Parallel()

			got := FromResponse(&http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{"Retry-After": []string{header}},
				Body:       http.NoBody,
			}, nil)
			require.NotNil(t, got)
			assert.True(t, ExceedsHonorWindow(got))

			wait, giveUp := decision(got)
			assert.True(t, giveUp)
			assert.Equal(t, time.Duration(0), wait)
		})
	}
}

// TestFromError checks that a pull-stream throttle yields its wait and quota.
func TestFromError(t *testing.T) {
	t.Parallel()

	got := FromError(errors.New(
		"error pulling image configuration: toomanyrequests: retry-after: 160.574µs, allowed: 44000/minute",
	))
	require.NotNil(t, got)
	require.ErrorIs(t, got, ErrRateLimited)
	assert.Equal(t, 44000, got.Allowed)
	assert.Equal(t, time.Minute, got.AllowedWindow)
	assert.Greater(t, got.RetryAfter, time.Duration(0))
}

func TestFromResponseUsesHeaderQuotaWhenBodyHasNone(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodHead, "https://ghcr.io/v2/linuxserver/nginx/manifests/latest", nil)
	require.NoError(t, err)

	header := make(http.Header)
	header.Set("Retry-After", "4")
	header.Set("X-Ratelimit-Limit", "3000, 3000;w=60")

	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     header,
		Body:       http.NoBody,
		Request:    req,
	}

	got := FromResponse(resp, nil)
	require.NotNil(t, got)
	assert.Equal(t, "ghcr.io", got.Host)
	assert.Equal(t, 4*time.Second, got.RetryAfter)
	assert.Equal(t, 3000, got.Allowed)
	assert.Equal(t, time.Minute, got.AllowedWindow)
}

func TestFromResponseNilResponseUsesBody(t *testing.T) {
	t.Parallel()

	got := FromResponse(nil, []byte("toomanyrequests: retry-after: 250ms, allowed: 100/hour"))
	require.NotNil(t, got)
	assert.Equal(t, 250*time.Millisecond, got.RetryAfter)
	assert.Equal(t, 100, got.Allowed)
	assert.Equal(t, time.Hour, got.AllowedWindow)
}

func TestFromResponsePrefersHeaderRetryAfterOverBody(t *testing.T) {
	t.Parallel()

	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Retry-After": []string{"9"}},
		Body:       http.NoBody,
	}

	got := FromResponse(resp, []byte("toomanyrequests: retry-after: 331.163µs, allowed: 44000/minute"))
	require.NotNil(t, got)
	assert.Equal(t, 9*time.Second, got.RetryAfter)
	assert.Equal(t, 44000, got.Allowed)
}

func TestFromResponseUsesRateLimitLimitHeader(t *testing.T) {
	t.Parallel()

	header := make(http.Header)
	header.Set("Ratelimit-Limit", "100;w=3600")

	got := FromResponse(&http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     header,
		Body:       http.NoBody,
	}, nil)
	require.NotNil(t, got)
	assert.Equal(t, 100, got.Allowed)
	assert.Equal(t, time.Hour, got.AllowedWindow)
}

func TestParseRateLimitLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		header  string
		allowed int
		window  time.Duration
	}{
		{
			name:    "empty",
			header:  "",
			allowed: 0,
		},
		{
			name:    "docker hub style",
			header:  "100;w=3600",
			allowed: 100,
			window:  time.Hour,
		},
		{
			name:    "comma pair with window",
			header:  "3000, 3000;w=60",
			allowed: 3000,
			window:  time.Minute,
		},
		{
			name:    "budget without window",
			header:  "50",
			allowed: 50,
		},
		{
			name:   "zero budget",
			header: "0;w=60",
		},
		{
			name:   "non numeric",
			header: "unlimited",
		},
		{
			name:    "invalid window is ignored",
			header:  "10;w=nope",
			allowed: 10,
		},
		{
			name:    "zero window is ignored",
			header:  "10;w=0",
			allowed: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			allowed, window := parseRateLimitLimit(tt.header)
			assert.Equal(t, tt.allowed, allowed)
			assert.Equal(t, tt.window, window)
		})
	}
}

func TestReadBody(t *testing.T) {
	t.Parallel()

	assert.Nil(t, ReadBody(nil, 16))
	assert.Nil(t, ReadBody(&http.Response{}, 16))

	resp := &http.Response{Body: io.NopCloser(strings.NewReader("toomanyrequests extra"))}
	assert.Equal(t, "toomanyrequests extra", string(ReadBody(resp, 0)))

	resp = &http.Response{Body: io.NopCloser(strings.NewReader("abcdefghij"))}
	assert.Equal(t, "abcde", string(ReadBody(resp, 5)))

	failing := &http.Response{Body: failingReadCloser{}}
	assert.Empty(t, ReadBody(failing, 16))
}

// TestFromError_recognizesPhrases checks the 429 phrases that mark a pull error
// as rate limited.
func TestFromError_recognizesPhrases(t *testing.T) {
	t.Parallel()

	for _, msg := range []string{
		"Too Many Requests from registry",
		"registry returned 429",
		"HTTP 429 from registry",
		"status: 429",
	} {
		assert.NotNil(t, FromError(errors.New(msg)), msg)
	}
}

// TestFromError_recognizesDockerHubPullLimits covers Docker Hub's pull rate
// limit. Docker Engine 29.10 and later classify it as resource exhausted from
// the registry's 429, also when a caller wraps it around text that names no
// limit. Older engines using the containerd image store send the text alone,
// which has no 429 or "too many requests" token.
func TestFromError_recognizesDockerHubPullLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{name: "unauthenticated limit classified as resource exhausted", err: cerrdefs.ErrResourceExhausted.WithMessage(hubPullLimit)},
		{
			name: "classified limit wrapped by a caller",
			err:  fmt.Errorf("pull failed: %w", cerrdefs.ErrResourceExhausted.WithMessage(unmarkedRefusal)),
		},
		{name: "unauthenticated limit text from an older engine", err: errors.New(hubPullLimit)},
		{name: "authenticated limit text from an older engine", err: errors.New(hubAuthenticatedPullLimit)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := FromError(tt.err)
			require.NotNil(t, got)
			require.ErrorIs(t, got, ErrRateLimited)
			assert.Equal(t, http.StatusTooManyRequests, got.StatusCode)
			assert.Equal(t, tt.err.Error(), got.Message)
			assert.Zero(t, got.RetryAfter)
		})
	}
}

// TestFromError_recognizesQuotaOnlyThrottle covers registries that
// throttle without a 429 or "too many requests" token, advertising the wait
// only via retry-after text. The first message is a verbatim Docker daemon
// pull error from lscr.io.
func TestFromError_recognizesQuotaOnlyThrottle(t *testing.T) {
	t.Parallel()

	msg := "Error response from daemon: error from registry: " +
		"retry-after: 92.923\u00b5s, allowed: 44000/minute"

	info := FromError(errors.New(msg))
	require.NotNil(t, info)
	assert.Equal(t, 92923*time.Nanosecond, info.RetryAfter)
	assert.Equal(t, 44000, info.Allowed)
	assert.Equal(t, time.Minute, info.AllowedWindow)

	require.NotNil(t, FromError(errors.New("error from registry: retry-after: 1.08ms")))
	require.NotNil(t, FromError(errors.New(
		"error from registry: retry-after: 802.695\u00b5s, allowed: 44000/minute",
	)))

	compound := FromError(errors.New("error from registry: retry-after: 1m30s"))
	require.NotNil(t, compound)
	assert.Equal(t, 90*time.Second, compound.RetryAfter)
}

// TestFromError_ignoresUnrelatedErrors checks that pull errors without a
// rate-limit marker, including a bare 429 in byte counts, ports and digests,
// are not rate limits.
func TestFromError_ignoresUnrelatedErrors(t *testing.T) {
	t.Parallel()

	assert.Nil(t, FromError(nil))
	// Another classification does not count without a rate-limit marker.
	assert.Nil(t, FromError(cerrdefs.ErrNotFound.WithMessage(unmarkedRefusal)))

	for _, msg := range []string{
		"failed to pull image: connection refused",
		"",
		"sha256:abcdef429abcdef",
		"dial tcp 127.0.0.1:4290: connect: connection refused",
		"wrote 1429 bytes",
		"wrote 429 bytes",
		"dial tcp 10.0.0.1:429: connect: connection refused",
		"error from registry: allowed: 44000/minute",
		"retry-after: 5",
		"retry-after: 5seconds",
		"retry-after: 5S",
		"Retry-After: Wed, 21 Oct 2015 07:28:00 GMT",
		"not allowed: 5/minute",
		"disallowed: 10/hour",
		"max allowed: 100/minute",
	} {
		assert.Nil(t, FromError(errors.New(msg)), msg)
	}
}

func TestDecisionCapsTinyAndHugeRetryAfter(t *testing.T) {
	t.Parallel()

	wait, giveUp := decision(&Error{RetryAfter: 331 * time.Microsecond})
	assert.False(t, giveUp)
	assert.Equal(t, minHonorWait, wait)

	wait, giveUp = decision(&Error{RetryAfter: 2 * time.Hour})
	assert.True(t, giveUp)
	assert.Equal(t, time.Duration(0), wait)

	wait, giveUp = decision(&Error{RetryAfter: 5 * time.Second})
	assert.False(t, giveUp)
	assert.Equal(t, 5*time.Second, wait)

	wait, giveUp = decision(nil)
	assert.False(t, giveUp)
	assert.Equal(t, minHonorWait, wait)
}

// TestDecision_stopsAtDockerHubPullLimit checks that Docker Hub's pull rate
// limit, which gives no wait, stops retries because its window lasts six hours.
// A registry that names a pull rate limit and sends a usable wait is honored.
func TestDecision_stopsAtDockerHubPullLimit(t *testing.T) {
	t.Parallel()

	wait, giveUp := decision(&Error{Message: "You have reached your unauthenticated Pull Rate Limit."})
	assert.True(t, giveUp)
	assert.Equal(t, time.Duration(0), wait)

	wait, giveUp = decision(&Error{Message: "pull rate limit exceeded", RetryAfter: 2 * time.Second})
	assert.False(t, giveUp)
	assert.Equal(t, 2*time.Second, wait)
}

// failingReadCloser fails on the first Read so ReadBody can exercise its error path.
type failingReadCloser struct{}

// Read always fails.
//
// Parameters:
//   - p: Unused destination buffer.
//
// Returns:
//   - int: Bytes read. Always 0.
//   - error: Always a read failure.
func (failingReadCloser) Read(_ []byte) (int, error) {
	return 0, errReadFailed
}

// Close satisfies io.ReadCloser.
//
// Returns:
//   - error: Always nil.
func (failingReadCloser) Close() error {
	return nil
}

var errReadFailed = io.ErrUnexpectedEOF
