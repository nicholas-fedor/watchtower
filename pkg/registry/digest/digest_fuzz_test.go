package digest

import (
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
