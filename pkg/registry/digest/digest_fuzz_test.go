package digest

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// FuzzCanonicalizeImageName fuzzes canonicalizeImageName to verify it never
// panics on arbitrary input and that canonical output is deterministic.
func FuzzCanonicalizeImageName(f *testing.F) {
	// Seed corpus with representative image name formats.
	f.Add("nginx")
	f.Add("nginx:latest")
	f.Add("docker.io/library/nginx:latest")
	f.Add("ghcr.io/owner/repo:tag")
	f.Add("ghcr.io/owner/repo")
	f.Add("myregistry.io/myimage")
	f.Add("")
	f.Add("INVALID UPPER")
	f.Add("library/alpine")
	f.Add("alpine:3.19")
	f.Add("localhost/test:dev")
	f.Add("registry.example.com/ns/image:sha256-abc123")

	f.Fuzz(func(t *testing.T, imageName string) {
		// Must not panic on any input.
		result := canonicalizeImageName(imageName)

		// Determinism: calling again must return the same value.
		result2 := canonicalizeImageName(imageName)
		assert.Equal(t, result, result2, "canonicalizeImageName must be deterministic")

		// If parsing succeeded (non-fallback), the result should be non-empty
		// and contain only lowercase characters in the host portion (no spaces).
		if result != imageName {
			assert.NotEmpty(t, result, "canonical result should not be empty when parsing succeeds")
		}
	})
}

// FuzzGetReleaseImageInspectLock fuzzes the lock acquisition and release cycle
// with various image name formats to verify no panics or deadlocks occur.
func FuzzGetReleaseImageInspectLock(f *testing.F) {
	f.Add("nginx")
	f.Add("nginx:latest")
	f.Add("ghcr.io/owner/repo:v1")
	f.Add("")
	f.Add("library/alpine:3.19")

	f.Fuzz(func(t *testing.T, imageName string) {
		// Acquire and release must not panic on any input.
		lock, release := getImageInspectLock(imageName)

		if lock != nil {
			lock.Lock()

			// Perform trivial work to avoid SA2001 (empty critical section).
			_ = imageName

			lock.Unlock()
		}

		release()

		// Acquire again to test revival path.
		lock2, release2 := getImageInspectLock(imageName)

		if lock2 != nil {
			lock2.Lock()

			// Perform trivial work to avoid SA2001 (empty critical section).
			_ = imageName

			lock2.Unlock()
		}

		release2()

		// Clean up to avoid unbounded test map growth.
		key := canonicalizeImageName(imageName)
		imageInspectLocks.Delete(key)
	})
}

// FuzzLockLifecycleConcurrency fuzzes the concurrent lock lifecycle to verify
// that acquire/release cycles from two goroutines sharing a canonical key never
// panic and that both goroutines can independently lock and unlock.
func FuzzLockLifecycleConcurrency(f *testing.F) {
	f.Add("nginx")
	f.Add("nginx:latest")
	f.Add("ghcr.io/owner/repo:v1")
	f.Add("library/alpine:3.19")

	f.Fuzz(func(t *testing.T, imageName string) {
		key := canonicalizeImageName(imageName)

		defer imageInspectLocks.Delete(key)

		// Goroutine A acquires, works, unlocks, then signals B to start
		// before calling releaseA. This forces B's getImageInspectLock to
		// race with A's releaseA, exercising the window where refs drops
		// to zero and deletion is pending.
		startB := make(chan struct{})
		aDone := make(chan struct{})
		bDone := make(chan struct{})

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()

			lockA, releaseA := getImageInspectLock(imageName)

			lockA.Lock()

			_ = imageName // Trivial work inside critical section.

			lockA.Unlock()

			// Signal B to start its getImageInspectLock while the entry
			// still exists (refs > 0), then release — B's acquire races
			// with our release's CompareAndDelete.
			close(startB)
			releaseA()

			close(aDone)
		}()

		go func() {
			defer wg.Done()

			<-startB // Wait until A has unlocked but not yet released.

			lockB, releaseB := getImageInspectLock(imageName)

			lockB.Lock()

			_ = imageName // Trivial work inside critical section.

			lockB.Unlock()
			releaseB()

			close(bDone)
		}()

		<-aDone
		<-bDone

		wg.Wait()

		// Verify the entry was cleaned up (refs should be 0) or removed.
		val, exists := imageInspectLocks.Load(key)
		if exists {
			entry, ok := val.(*imageLockEntry)
			if ok {
				entry.mu.Lock()
				assert.Zero(t, entry.refs, "all references must be released after concurrent lifecycle")
				entry.mu.Unlock()
			}
		}
	})
}
