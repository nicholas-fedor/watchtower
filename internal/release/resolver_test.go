package release

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/registry/ratelimit"
	registryConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/registry"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

const testDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"

// spyFetcher records the tags it was asked for and replays canned results.
//
// It is mutex guarded so it can be shared by the concurrent lookup tests, where
// several goroutines may reach it before any cache entry exists.
type spyFetcher struct {
	// mu guards calls.
	mu sync.Mutex
	// digests maps a tag to the digest returned for it. A missing tag yields
	// empty with no error, which is how a registry reports an unknown tag.
	digests map[string]string
	// errs maps a tag to the error returned for it.
	errs map[string]error
	// calls records every tag requested, in order.
	calls []string
}

func (f *spyFetcher) FetchDigestForTag(
	_ *zerolog.Logger,
	_ context.Context,
	_ types.Container,
	_ string,
	tag string,
	_ ...string,
) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, tag)

	if err, ok := f.errs[tag]; ok {
		return "", err
	}

	return f.digests[tag], nil
}

// newTestLogger returns a logger that writes each event to the test's log, so a
// failing test shows what it logged.
//
// Parameters:
//   - t: Test that receives the log lines.
//
// Returns:
//   - *zerolog.Logger: Logger writing to t.
func newTestLogger(t *testing.T) *zerolog.Logger {
	t.Helper()

	// Frame 4 skips zerolog's write path, so each line names the log call.
	w := zerolog.TestWriter{T: t, Frame: 4}

	return new(zerolog.New(w))
}

// transientThenOKFetcher fails its first sweep, then behaves like a registry
// that published the prefixed tag.
type transientThenOKFetcher struct {
	err     error
	sweptUp bool
	calls   int
}

func (f *transientThenOKFetcher) FetchDigestForTag(
	_ *zerolog.Logger,
	_ context.Context,
	_ types.Container,
	_ string,
	tag string,
	_ ...string,
) (string, error) {
	f.calls++

	if !f.sweptUp {
		f.sweptUp = true

		return "", f.err
	}

	if tag == "v1.2.3" {
		return testDigest, nil
	}

	return "", nil
}

// gatedFetcher holds its first request open until gate closes, so tests can
// start other lookups while a sweep is in flight. Later requests answer at once.
type gatedFetcher struct {
	// gate releases the first request when closed.
	gate chan struct{}
	// firstErr, when set, is the error the first request returns.
	firstErr error
	// digests maps a tag to the digest returned for it.
	digests map[string]string
	// mu guards calls.
	mu sync.Mutex
	// calls counts the requests made.
	calls int
}

func (f *gatedFetcher) FetchDigestForTag(
	_ *zerolog.Logger,
	_ context.Context,
	_ types.Container,
	_ string,
	tag string,
	_ ...string,
) (string, error) {
	f.mu.Lock()
	f.calls++
	first := f.calls == 1
	f.mu.Unlock()

	if first {
		<-f.gate

		if f.firstErr != nil {
			return "", f.firstErr
		}
	}

	return f.digests[tag], nil
}

// callCount returns how many requests the fetcher has received.
func (f *gatedFetcher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.calls
}

func TestTagCandidates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		want    []string
	}{
		{name: "unprefixed prefers the v form", version: "1.2.3", want: []string{"v1.2.3", "1.2.3"}},
		{name: "prefixed prefers itself", version: "v1.2.3", want: []string{"v1.2.3", "1.2.3"}},
		{name: "prerelease", version: "1.2.3-rc.1", want: []string{"v1.2.3-rc.1", "1.2.3-rc.1"}},
		{name: "calendar version", version: "2026.09.1", want: []string{"v2026.09.1", "2026.09.1"}},
		{name: "non semver is still probed", version: "nightly", want: []string{"vnightly", "nightly"}},
		{name: "whitespace is trimmed", version: "  1.2.3  ", want: []string{"v1.2.3", "1.2.3"}},
		{name: "empty", version: ""},
		{name: "whitespace only", version: "   "},
		{name: "lone v yields one candidate", version: "v", want: []string{"v"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tagCandidates(tt.version))
		})
	}
}

// The v-prefixed spelling must win when both match, since that is the common case.
func TestResolveTagPrefersFirstCandidate(t *testing.T) {
	t.Parallel()

	fetcher := &spyFetcher{digests: map[string]string{
		"v1.2.3": testDigest,
		"1.2.3":  testDigest,
	}}

	r := NewResolver(fetcher)
	got := r.ResolveTag(newTestLogger(t), context.Background(), testContainer(), "", "1.2.3", testDigest)

	assert.Equal(t, "v1.2.3", got)
	assert.Equal(t, []string{"v1.2.3"}, fetcher.calls, "the first match must end the probe")
}

// A maintainer who tags without a "v" costs one extra request, and only that.
func TestResolveTagFallsBackToSecondCandidate(t *testing.T) {
	t.Parallel()

	fetcher := &spyFetcher{digests: map[string]string{"1.2.3": testDigest}}

	r := NewResolver(fetcher)
	got := r.ResolveTag(newTestLogger(t), context.Background(), testContainer(), "", "1.2.3", testDigest)

	assert.Equal(t, "1.2.3", got)
	assert.Equal(t, []string{"v1.2.3", "1.2.3"}, fetcher.calls)
}

func TestResolveTagNoMatch(t *testing.T) {
	t.Parallel()

	fetcher := &spyFetcher{digests: map[string]string{
		"v1.2.3": "sha256:2222222222222222222222222222222222222222222222222222222222222222",
		"1.2.3":  "sha256:3333333333333333333333333333333333333333333333333333333333333333",
	}}

	r := NewResolver(fetcher)
	got := r.ResolveTag(newTestLogger(t), context.Background(), testContainer(), "", "1.2.3", testDigest)

	assert.Empty(t, got)
	assert.Equal(t, []string{"v1.2.3", "1.2.3"}, fetcher.calls)
}

// A registry that publishes no versioned tags is the latest-only mirror case.
func TestResolveTagEmptyRegistry(t *testing.T) {
	t.Parallel()

	fetcher := &spyFetcher{digests: map[string]string{}}

	r := NewResolver(fetcher)
	got := r.ResolveTag(newTestLogger(t), context.Background(), testContainer(), "", "1.2.3", testDigest)

	assert.Empty(t, got)
	assert.Equal(t, []string{"v1.2.3", "1.2.3"}, fetcher.calls)
}

// A clean sweep that found nothing is a final answer, so it is cached.
func TestResolveTagCachesDefinitiveMiss(t *testing.T) {
	t.Parallel()

	fetcher := &spyFetcher{digests: map[string]string{}}

	r := NewResolver(fetcher)
	ctx := context.Background()

	assert.Empty(t, r.ResolveTag(newTestLogger(t), ctx, testContainer(), "", "1.2.3", testDigest))
	assert.Empty(t, r.ResolveTag(newTestLogger(t), ctx, testContainer(), "", "1.2.3", testDigest))
	assert.Len(t, fetcher.calls, 2, "only the first attempt should reach the registry")
}

// A transient failure must not deny every sibling container its own attempt.
func TestResolveTagDoesNotCacheTransientFailure(t *testing.T) {
	t.Parallel()

	transient := errors.New("connection reset")
	fetcher := &transientThenOKFetcher{err: transient}

	r := NewResolver(fetcher)
	ctx := context.Background()

	// The first sweep is inconclusive, so it returns empty and caches nothing.
	assert.Empty(t, r.ResolveTag(newTestLogger(t), ctx, testContainer(), "", "1.2.3", testDigest))

	// A sibling container must still get a real attempt, which now succeeds.
	assert.Equal(t, "v1.2.3", r.ResolveTag(newTestLogger(t), ctx, testContainer(), "", "1.2.3", testDigest))
}

// A rate limit is a statement about the whole host, so it is remembered briefly
// rather than costing every sibling container its own doomed request.
func TestResolveTagRateLimitIsNegativelyCachedForTheTTL(t *testing.T) {
	t.Parallel()

	fetcher := &transientThenOKFetcher{err: fmt.Errorf("429: %w", ratelimit.ErrRateLimited)}

	r := NewResolver(fetcher)
	now := time.Now()
	r.now = func() time.Time { return now }

	ctx := context.Background()

	assert.Empty(t, r.ResolveTag(newTestLogger(t), ctx, testContainer(), "", "1.2.3", testDigest))

	// Siblings inside the TTL must not re-probe a host that just refused.
	for range 3 {
		assert.Empty(t, r.ResolveTag(newTestLogger(t), ctx, testContainer(), "", "1.2.3", testDigest))
	}

	assert.Equal(t, 1, fetcher.calls, "the refusal must be cached, not retried per sibling")

	// Once the TTL lapses the next container gets a real attempt, which succeeds.
	now = now.Add(rateLimitCacheTTL + time.Second)

	assert.Equal(t, "v1.2.3", r.ResolveTag(newTestLogger(t), ctx, testContainer(), "", "1.2.3", testDigest))
}

// A rate limit must abandon the probe rather than spend a second refused request.
func TestResolveTagAbandonsSecondCandidateAfterRateLimit(t *testing.T) {
	t.Parallel()

	fetcher := &spyFetcher{
		digests: map[string]string{"1.2.3": testDigest},
		errs:    map[string]error{"v1.2.3": fmt.Errorf("429: %w", ratelimit.ErrRateLimited)},
	}

	r := NewResolver(fetcher)
	got := r.ResolveTag(newTestLogger(t), context.Background(), testContainer(), "", "1.2.3", testDigest)

	assert.Empty(t, got)
	assert.Equal(t, []string{"v1.2.3"}, fetcher.calls)
}

// A non rate-limit failure on the first candidate still allows the second.
func TestResolveTagContinuesAfterOrdinaryError(t *testing.T) {
	t.Parallel()

	fetcher := &spyFetcher{
		digests: map[string]string{"1.2.3": testDigest},
		errs:    map[string]error{"v1.2.3": errors.New("connection reset")},
	}

	r := NewResolver(fetcher)
	got := r.ResolveTag(newTestLogger(t), context.Background(), testContainer(), "", "1.2.3", testDigest)

	assert.Equal(t, "1.2.3", got)
	assert.Equal(t, []string{"v1.2.3", "1.2.3"}, fetcher.calls)
}

func TestResolveTagSkipsWhenNotWorthProbing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		container types.Container
		version   string
		digest    string
	}{
		{name: "no version", container: testContainer(), digest: testDigest},
		{name: "no new digest", container: testContainer(), version: "1.2.3"},
		{name: "nil container", version: "1.2.3", digest: testDigest},
		{name: "digest pinned", container: pinnedContainer(), version: "1.2.3", digest: testDigest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fetcher := &spyFetcher{digests: map[string]string{"v1.2.3": testDigest}}

			r := NewResolver(fetcher)
			got := r.ResolveTag(newTestLogger(t), context.Background(), tt.container, "", tt.version, tt.digest)

			assert.Empty(t, got)
			assert.Empty(t, fetcher.calls, "no candidate may be requested")
		})
	}
}

// Sibling containers on one image must not each pay for the lookup.
func TestResolveTagCachesAcrossContainers(t *testing.T) {
	t.Parallel()

	fetcher := &spyFetcher{digests: map[string]string{"v1.2.3": testDigest}}

	r := NewResolver(fetcher)
	ctx := context.Background()

	first := r.ResolveTag(newTestLogger(t), ctx, testContainer(), "", "1.2.3", testDigest)
	second := r.ResolveTag(newTestLogger(t), ctx, testContainer(), "", "1.2.3", testDigest)

	assert.Equal(t, "v1.2.3", first)
	assert.Equal(t, "v1.2.3", second)
	assert.Equal(t, []string{"v1.2.3"}, fetcher.calls)
}

// A different pulled image invalidates the previous answer.
func TestResolveTagCacheKeysOnDigest(t *testing.T) {
	t.Parallel()

	fetcher := &spyFetcher{digests: map[string]string{
		"v1.2.3": testDigest,
		"v1.3.0": "sha256:4444444444444444444444444444444444444444444444444444444444444444",
	}}

	r := NewResolver(fetcher)
	ctx := context.Background()

	assert.Equal(t, "v1.2.3", r.ResolveTag(newTestLogger(t), ctx, testContainer(), "", "1.2.3", testDigest))
	assert.Equal(
		t,
		"v1.3.0",
		r.ResolveTag(
			newTestLogger(t),
			ctx,
			testContainer(),
			"",
			"1.3.0",
			"sha256:4444444444444444444444444444444444444444444444444444444444444444",
		),
	)
}

// TestResolveTagNilFetcher verifies that a Resolver built without a fetcher
// makes no lookups and returns empty instead of panicking.
func TestResolveTagNilFetcher(t *testing.T) {
	t.Parallel()

	r := NewResolver(nil)

	assert.NotPanics(t, func() {
		assert.Empty(t, r.ResolveTag(newTestLogger(t), context.Background(), testContainer(), "", "1.2.3", testDigest))
	})
}

func TestResolveTagNilResolver(t *testing.T) {
	t.Parallel()

	var r *Resolver

	assert.Empty(t, r.ResolveTag(newTestLogger(t), context.Background(), testContainer(), "", "1.2.3", testDigest))
}

// One resolver is shared by every parallel staleness worker, so concurrent
// lookups for one image must be safe and must share a single sweep. The first
// request is held open until every other lookup is waiting on it, so they join
// the sweep in flight rather than finding its answer in the cache. Run with
// -race to make the safety check meaningful.
func TestResolveTagIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		fetcher := &gatedFetcher{gate: make(chan struct{}), digests: map[string]string{"v1.2.3": testDigest}}
		r := NewResolver(fetcher)

		var wg sync.WaitGroup

		resolve := func() {
			wg.Go(func() {
				assert.Equal(t, "v1.2.3", r.ResolveTag(newTestLogger(t), t.Context(), testContainer(), "", "1.2.3", testDigest))
			})
		}

		resolve()
		synctest.Wait()

		for range 31 {
			resolve()
		}

		synctest.Wait()
		assert.Equal(t, 1, fetcher.callCount(), "the other lookups must wait for the sweep in flight")

		close(fetcher.gate)
		wg.Wait()

		assert.Equal(t, 1, fetcher.callCount(), "concurrent lookups must share one sweep")
	})
}

// A lookup that joined another caller's sweep makes its own attempt when that
// sweep fails, since the failure may come from the other caller. The caller
// that ran the failed sweep does not retry, and nothing is cached.
func TestResolveTagRetriesAfterJoiningFailedSweep(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		fetcher := &gatedFetcher{
			gate:     make(chan struct{}),
			firstErr: errors.New("connection reset"),
			digests:  map[string]string{"v1.2.3": testDigest},
		}
		r := NewResolver(fetcher)

		var (
			wg             sync.WaitGroup
			leader, joined string
		)

		wg.Go(func() {
			leader = r.ResolveTag(newTestLogger(t), t.Context(), testContainer(), "", "1.2.3", testDigest)
		})
		synctest.Wait()

		wg.Go(func() {
			joined = r.ResolveTag(newTestLogger(t), t.Context(), testContainer(), "", "1.2.3", testDigest)
		})
		synctest.Wait()

		close(fetcher.gate)
		wg.Wait()

		assert.Empty(t, leader, "the caller that ran the failed sweep gets no tag")
		assert.Equal(t, "v1.2.3", joined, "the joined lookup retries on its own")
		assert.Equal(t, 3, fetcher.callCount(), "two candidates for the failed sweep and one for the retry")
	})
}

// A lookup waiting on another caller's sweep returns as soon as its own context
// ends, while the sweep carries on for the caller that started it.
func TestResolveTagStopsWaitingWhenItsContextEnds(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		fetcher := &gatedFetcher{gate: make(chan struct{}), digests: map[string]string{"v1.2.3": testDigest}}
		r := NewResolver(fetcher)

		var (
			wg     sync.WaitGroup
			leader string
		)

		wg.Go(func() {
			leader = r.ResolveTag(newTestLogger(t), t.Context(), testContainer(), "", "1.2.3", testDigest)
		})
		synctest.Wait()

		ctx, cancel := context.WithCancel(t.Context())
		waiterDone := make(chan string)

		go func() {
			waiterDone <- r.ResolveTag(newTestLogger(t), ctx, testContainer(), "", "1.2.3", testDigest)
		}()

		synctest.Wait()

		cancel()
		assert.Empty(t, <-waiterDone, "the canceled lookup stops waiting with no tag")

		close(fetcher.gate)
		wg.Wait()

		assert.Equal(t, "v1.2.3", leader)
		assert.Equal(t, 1, fetcher.callCount())
	})
}

// Concurrent lookups spanning different digests must not cross-contaminate the
// per-digest cache entries.
func TestResolveTagConcurrentDistinctDigests(t *testing.T) {
	t.Parallel()

	second := "sha256:2222222222222222222222222222222222222222222222222222222222222222"

	fetcher := &spyFetcher{digests: map[string]string{
		"v1.2.3": testDigest,
		"v2.0.0": second,
	}}

	r := NewResolver(fetcher)
	ctx := context.Background()

	var wg sync.WaitGroup

	for range 16 {
		wg.Go(func() {
			assert.Equal(t, "v1.2.3", r.ResolveTag(newTestLogger(t), ctx, testContainer(), "", "1.2.3", testDigest))
			assert.Equal(t, "v2.0.0", r.ResolveTag(newTestLogger(t), ctx, testContainer(), "", "2.0.0", second))
		})
	}

	wg.Wait()
}

// TestDigestFetcherUsesItsSettings verifies that a DigestFetcher makes its
// registry requests with the settings it was built with, so a registry served
// over plain HTTP is reachable only when the settings skip TLS.
func TestDigestFetcherUsesItsSettings(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodHead && r.URL.Path == "/v2/org/app/manifests/v1.2.3":
			w.Header().Set("Docker-Content-Digest", testDigest)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	container := stubContainer{imageName: strings.TrimPrefix(server.URL, "http://") + "/org/app:1.2.3"}

	got, err := NewDigestFetcher(registryConfig.Registry{TLSSkip: true}, ratelimit.New()).
		FetchDigestForTag(newTestLogger(t), t.Context(), container, "", "v1.2.3")
	require.NoError(t, err)
	assert.Equal(t, strings.TrimPrefix(testDigest, "sha256:"), got)

	_, err = NewDigestFetcher(registryConfig.Registry{}, ratelimit.New()).
		FetchDigestForTag(newTestLogger(t), t.Context(), container, "", "v1.2.3")
	require.Error(t, err, "an HTTPS request reached the plain HTTP registry")
}
