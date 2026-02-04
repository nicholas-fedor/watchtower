package digest

import (
	"strings"
	"sync"
	"testing"
)

const (
	// testDigest is a sample SHA256 digest used in benchmarks.
	testDigest = "d68e1e532088964195ad3a0a71526bc2f11a78de0def85629beb75e2265f0547"
	// dummyWork is a placeholder value used in mutex critical sections.
	dummyWork = "dummy"
)

// BenchmarkDigestsMatch benchmarks the DigestsMatch function which compares local digests
// with a remote digest to check for matches.
// This measures the CPU overhead of digest comparison without network operations.
func BenchmarkDigestsMatch(b *testing.B) {
	// Pre-defined test data representing typical Docker image digests
	localDigests := []string{
		"ghcr.io/example/app@sha256:" + testDigest,
		"docker.io/library/nginx@sha256:a5967740c5c9a1c4c6f5a5c5c5c5c5c5c5c5c5c5c5c5c5c5c5c5c5c5c",
	}
	remoteDigest := testDigest

	for b.Loop() {
		_ = DigestsMatch(localDigests, remoteDigest)
	}
}

// BenchmarkDigestsMatchNoMatch benchmarks DigestsMatch when there are no matches.
// This represents the worst case where the loop must iterate through all digests.
func BenchmarkDigestsMatchNoMatch(b *testing.B) {
	localDigests := []string{
		"ghcr.io/example/app@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"docker.io/library/nginx@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"docker.io/library/postgres@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		"quay.io/redhat/ubi8@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		"gcr.io/distroless/static@sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
	}
	remoteDigest := testDigest

	for b.Loop() {
		_ = DigestsMatch(localDigests, remoteDigest)
	}
}

// BenchmarkDigestsMatchManyDigests benchmarks DigestsMatch with a large number of local digests.
// This simulates containers with multiple image tags or mirrored registries.
func BenchmarkDigestsMatchManyDigests(b *testing.B) {
	// Generate 20 local digests to simulate multi-mirror scenario
	localDigests := make([]string, 20)
	for i := range 20 {
		localDigests[i] = "ghcr.io/mirror/registry-" + string(rune('a'+i)) +
			"/app@sha256:" + testDigest
	}

	remoteDigest := testDigest

	for b.Loop() {
		_ = DigestsMatch(localDigests, remoteDigest)
	}
}

// BenchmarkNormalizeDigest benchmarks the NormalizeDigest function which strips
// common prefixes like "sha256:" from digest strings.
func BenchmarkNormalizeDigest(b *testing.B) {
	testDigests := []string{
		"sha256:" + testDigest,
		"sha256:abc123",
		testDigest,
		"sha256:" + strings.Repeat("a", 64),
	}

	for b.Loop() {
		for _, d := range testDigests {
			_ = NormalizeDigest(d)
		}
	}
}

// BenchmarkInspectMutexContention benchmarks the inspectMutex lock contention
// when multiple goroutines attempt to acquire the lock simultaneously.
// This measures the overhead of the global mutex in concurrent scenarios.
func BenchmarkInspectMutexContention(b *testing.B) {
	// Simulate mutex lock/unlock with a no-op critical section
	// This measures only the mutex contention overhead
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			inspectMutex.Lock()
			// Simulate minimal work inside the critical section
			_ = dummyWork

			inspectMutex.Unlock()
		}
	})
}

// BenchmarkMutexLockUnlock benchmarks the raw lock/unlock operations
// to isolate mutex overhead from any work done in the critical section.
func BenchmarkMutexLockUnlock(b *testing.B) {
	var mu sync.Mutex

	for b.Loop() {
		mu.Lock()

		_ = dummyWork

		mu.Unlock()
	}
}

// BenchmarkMutexLockUnlockParallel benchmarks lock/unlock under concurrent contention.
func BenchmarkMutexLockUnlockParallel(b *testing.B) {
	var mu sync.Mutex

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			mu.Lock()

			_ = dummyWork

			mu.Unlock()
		}
	})
}

// BenchmarkStringOperations benchmarks string operations used in digest processing.
// This includes strings.CutPrefix, strings.Split, and strings.Contains.
func BenchmarkStringOperations(b *testing.B) {
	testCases := []struct {
		name   string
		input  string
		prefix string
	}{
		{
			"with sha256 prefix",
			"sha256:" + testDigest,
			"sha256:",
		},
		{
			"without prefix",
			testDigest,
			"sha256:",
		},
		{
			"docker digest format",
			"ghcr.io/example/app@sha256:" + testDigest,
			"@",
		},
		{
			"repo digest split",
			"ghcr.io/example/app@sha256:" + testDigest,
			"@",
		},
	}

	for b.Loop() {
		for _, tc := range testCases {
			// Simulate strings.CutPrefix
			if after, ok := strings.CutPrefix(tc.input, tc.prefix); ok {
				_ = after
			}
			// Simulate strings.Split
			_ = strings.Split(tc.input, "@")
		}
	}
}

// BenchmarkConcurrentDigestChecks benchmarks concurrent digest checks
// to simulate multiple containers being checked simultaneously.
func BenchmarkConcurrentDigestChecks(b *testing.B) {
	numGoroutines := 10

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for i := range numGoroutines {
				// Simulate the digest fetch pattern: lock, work, unlock
				inspectMutex.Lock()

				_ = i // Simulate minimal work

				inspectMutex.Unlock()
			}
		}
	})
}

// BenchmarkDigestsMatchEarlyMatch benchmarks DigestsMatch when the first digest matches.
// This is the best case scenario where the loop exits early.
func BenchmarkDigestsMatchEarlyMatch(b *testing.B) {
	localDigests := []string{
		"ghcr.io/example/app@sha256:" + testDigest,
		"docker.io/library/nginx@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"docker.io/library/postgres@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
	}
	remoteDigest := testDigest

	for b.Loop() {
		_ = DigestsMatch(localDigests, remoteDigest)
	}
}

// BenchmarkDigestsMatchLateMatch benchmarks DigestsMatch when the last digest matches.
// This is the worst case for early exit optimization.
func BenchmarkDigestsMatchLateMatch(b *testing.B) {
	localDigests := []string{
		"ghcr.io/example/app@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"docker.io/library/nginx@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"docker.io/library/postgres@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		"ghcr.io/example/app@sha256:" + testDigest, // Matches last
	}
	remoteDigest := testDigest

	for b.Loop() {
		_ = DigestsMatch(localDigests, remoteDigest)
	}
}
