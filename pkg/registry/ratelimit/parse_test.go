package ratelimit

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := ParseRetryAfterHeader(tt.header)
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

			retryAfter, allowed, window := ParseQuotaMessage(tt.message)
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

func TestFromErrorMessage(t *testing.T) {
	t.Parallel()

	got := FromErrorMessage(
		"error pulling image configuration: toomanyrequests: retry-after: 160.574µs, allowed: 44000/minute",
	)
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

func TestFromErrorMessageRecognizesPhrases(t *testing.T) {
	t.Parallel()

	require.NotNil(t, FromErrorMessage("Too Many Requests from registry"))
	require.NotNil(t, FromErrorMessage("registry returned 429"))
	require.NotNil(t, FromErrorMessage("HTTP 429 from registry"))
	require.NotNil(t, FromErrorMessage("status: 429"))
}

func TestFromErrorMessageIgnoresUnrelatedErrors(t *testing.T) {
	t.Parallel()

	assert.Nil(t, FromErrorMessage("failed to pull image: connection refused"))
	assert.Nil(t, FromErrorMessage(""))
	assert.Nil(t, FromErrorMessage("sha256:abcdef429abcdef"))
	assert.Nil(t, FromErrorMessage("dial tcp 127.0.0.1:4290: connect: connection refused"))
	assert.Nil(t, FromErrorMessage("wrote 1429 bytes"))
	assert.Nil(t, FromErrorMessage("wrote 429 bytes"))
	assert.Nil(t, FromErrorMessage("dial tcp 10.0.0.1:429: connect: connection refused"))
}

func TestDecisionCapsTinyAndHugeRetryAfter(t *testing.T) {
	t.Parallel()

	wait, giveUp := Decision(&Error{RetryAfter: 331 * time.Microsecond})
	assert.False(t, giveUp)
	assert.Equal(t, minHonorWait, wait)

	wait, giveUp = Decision(&Error{RetryAfter: 2 * time.Hour})
	assert.True(t, giveUp)
	assert.Equal(t, time.Duration(0), wait)

	wait, giveUp = Decision(&Error{RetryAfter: 5 * time.Second})
	assert.False(t, giveUp)
	assert.Equal(t, 5*time.Second, wait)

	wait, giveUp = Decision(nil)
	assert.False(t, giveUp)
	assert.Equal(t, minHonorWait, wait)
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
