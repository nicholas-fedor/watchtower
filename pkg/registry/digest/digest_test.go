package digest

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCanonicalizeImageName validates the canonicalizeImageName function using
// table-driven tests covering bare names, tagged images, fully-qualified
// references, custom registries, and invalid/empty inputs.
func TestCanonicalizeImageName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "bare name gets default registry and library namespace",
			input:    "nginx",
			expected: "docker.io/library/nginx",
		},
		{
			name:     "bare name with tag preserved",
			input:    "nginx:latest",
			expected: "docker.io/library/nginx:latest",
		},
		{
			name:     "already canonical with tag preserved",
			input:    "docker.io/library/nginx:latest",
			expected: "docker.io/library/nginx:latest",
		},
		{
			name:     "custom registry with tag preserved",
			input:    "ghcr.io/owner/repo:tag",
			expected: "ghcr.io/owner/repo:tag",
		},
		{
			name:     "custom registry without tag",
			input:    "ghcr.io/owner/repo",
			expected: "ghcr.io/owner/repo",
		},
		{
			name:     "private registry without tag",
			input:    "myregistry.io/myimage",
			expected: "myregistry.io/myimage",
		},
		{
			name:     "different tags resolve to different keys",
			input:    "nginx:stable",
			expected: "docker.io/library/nginx:stable",
		},
		{
			name:     "empty string falls back to original",
			input:    "",
			expected: "",
		},
		{
			name:     "invalid uppercase falls back to original",
			input:    "INVALID UPPER",
			expected: "INVALID UPPER",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := canonicalizeImageName(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// loadLockEntry is a test helper that loads the *imageLockEntry from the
// imageInspectLocks map for the given key.
func loadLockEntry(t *testing.T, key string) *imageLockEntry {
	t.Helper()

	val, ok := imageInspectLocks.Load(key)
	require.True(t, ok, "expected lock entry for key %q", key)

	entry, ok := val.(*imageLockEntry)
	require.True(t, ok, "expected *imageLockEntry, got %T", val)

	return entry
}

// TestGetImageInspectLock verifies basic lock acquisition: first call creates a
// new entry with refs=1, second call with the same canonical name returns the
// same lock and increments refs, and different image names return distinct locks.
func TestGetImageInspectLock(t *testing.T) {
	imageName := "test-get-lock.io/repo/image:v1"
	canonicalKey := canonicalizeImageName(imageName)

	defer imageInspectLocks.Delete(canonicalKey)

	lock1, release1 := getImageInspectLock(imageName)
	defer release1()

	require.NotNil(t, lock1)

	entry := loadLockEntry(t, canonicalKey)
	assert.Equal(t, 1, entry.refs)

	lock2, release2 := getImageInspectLock(imageName)
	defer release2()

	assert.Same(t, lock1, lock2, "same image name must return the same mutex pointer")

	entry = loadLockEntry(t, canonicalKey)
	assert.Equal(t, 2, entry.refs)

	// Different image name should produce a different lock.
	otherImage := "test-get-lock.io/repo/other:v1"
	otherKey := canonicalizeImageName(otherImage)

	defer imageInspectLocks.Delete(otherKey)

	lock3, release3 := getImageInspectLock(otherImage)
	defer release3()

	assert.NotSame(t, lock1, lock3, "different image names must return different mutexes")
}

// TestGetImageInspectLock_CanonicalEquivalence ensures that short and
// fully-qualified representations of the same image resolve to the same lock.
func TestGetImageInspectLock_CanonicalEquivalence(t *testing.T) {
	shortName := "nginx:latest"
	fullName := "docker.io/library/nginx:latest"
	canonicalKey := canonicalizeImageName(shortName)

	defer imageInspectLocks.Delete(canonicalKey)

	lock1, release1 := getImageInspectLock(shortName)
	defer release1()

	lock2, release2 := getImageInspectLock(fullName)
	defer release2()

	assert.Same(t, lock1, lock2, "short and full image names must resolve to the same lock")

	// Verify pointer equality by checking the map only contains one entry
	// for the canonical key.
	entry := loadLockEntry(t, canonicalKey)
	assert.Equal(t, 2, entry.refs)
}

// TestReleaseImageInspectLock exercises the reference-counting behavior of
// releaseImageInspectLock including nested acquire/release, release of a
// non-existent key, and release of a map entry with the wrong type.
func TestReleaseImageInspectLock(t *testing.T) {
	t.Run("acquire then release removes entry from map", func(t *testing.T) {
		imageName := "test-release.io/refcount:v1"
		canonicalKey := canonicalizeImageName(imageName)

		defer imageInspectLocks.Delete(canonicalKey)

		_, release := getImageInspectLock(imageName)

		entry := loadLockEntry(t, canonicalKey)
		assert.Equal(t, 1, entry.refs)
		assert.False(t, entry.dead)

		release()

		// Entry is deleted from the map when refs reach zero.
		_, exists := imageInspectLocks.Load(canonicalKey)
		assert.False(t, exists, "entry must be removed from map when refs reach zero")
	})

	t.Run("nested acquire then single release leaves refs=1 and dead=false", func(t *testing.T) {
		imageName := "test-release.io/nested:v1"
		canonicalKey := canonicalizeImageName(imageName)

		defer imageInspectLocks.Delete(canonicalKey)

		_, release1 := getImageInspectLock(imageName)
		_, release2 := getImageInspectLock(imageName)

		// Release the second (nested) acquisition.
		release2()

		entry := loadLockEntry(t, canonicalKey)
		assert.Equal(t, 1, entry.refs)
		assert.False(t, entry.dead)

		// Release the first acquisition — entry is removed from map.
		release1()

		_, exists := imageInspectLocks.Load(canonicalKey)
		assert.False(t, exists, "entry must be removed from map when refs reach zero")
	})

	t.Run("release non-existent key does not panic", func(t *testing.T) {
		assert.NotPanics(t, func() {
			releaseImageInspectLock("non-existent-key-for-release-test")
		})
	})

	t.Run("release key with wrong type in map does not panic", func(t *testing.T) {
		badKey := "test-release.io/wrong-type:v1"
		imageInspectLocks.Store(badKey, "not-an-imageLockEntry")

		defer imageInspectLocks.Delete(badKey)

		assert.NotPanics(t, func() {
			releaseImageInspectLock(badKey)
		})
	})
}

// TestImageInspectLock_EntryCleanupOnRelease verifies that after a lock entry is
// released (refs=0), it is removed from the map via CompareAndDelete. A subsequent
// acquire creates a fresh entry.
func TestImageInspectLock_EntryCleanupOnRelease(t *testing.T) {
	imageName := "test-cleanup.io/image:v1"
	canonicalKey := canonicalizeImageName(imageName)

	defer imageInspectLocks.Delete(canonicalKey)

	// First acquire-release cycle.
	lock1, release1 := getImageInspectLock(imageName)
	release1()

	// Entry should be removed from the map after release.
	_, exists := imageInspectLocks.Load(canonicalKey)
	assert.False(t, exists, "entry must be removed from map when refs reach zero")

	// Second acquire creates a fresh entry.
	lock2, release2 := getImageInspectLock(imageName)
	defer release2()

	assert.NotNil(t, lock2, "re-acquire must return a valid lock")

	entry := loadLockEntry(t, canonicalKey)
	assert.Equal(t, 1, entry.refs)
	assert.False(t, entry.dead)

	// Fresh entry has a different pointer than the deleted one.
	assert.NotSame(t, lock1, lock2, "re-acquire after cleanup must create a new mutex")
}

// TestImageInspectLock_ConcurrentSerialization verifies that goroutines using
// the same lock entry serialize access to a shared counter. It pre-creates the
// entry so all goroutines are guaranteed to receive the same mutex pointer.
func TestImageInspectLock_ConcurrentSerialization(t *testing.T) {
	imageName := "test-concurrent.io/same-image:v1"
	canonicalKey := canonicalizeImageName(imageName)

	defer imageInspectLocks.Delete(canonicalKey)

	// Pre-create the entry so all goroutines find it in the map and get the
	// same lock pointer, avoiding the transient LoadOrStore race where
	// concurrent first-callers can create separate entries.
	_, release := getImageInspectLock(imageName)
	release()

	// Verify the entry was cleaned up, then re-create it so goroutines
	// below all see the same pointer. We re-acquire here and hold the
	// release until the end.
	lock, release := getImageInspectLock(imageName)
	defer release()

	const goroutines = 50

	var (
		counter int
		wg      sync.WaitGroup
	)

	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()

			lock.Lock()

			// Read-modify-write under the lock.
			val := counter
			counter = val + 1

			lock.Unlock()
		}()
	}

	wg.Wait()

	assert.Equal(t, goroutines, counter, "counter must equal the number of goroutines")
}

// TestImageInspectLock_DifferentImagesConcurrent verifies that goroutines
// holding locks for different image names can execute their critical sections
// concurrently. It uses channels to confirm both goroutines are inside the
// critical section at the same time.
func TestImageInspectLock_DifferentImagesConcurrent(t *testing.T) {
	imageA := "test-diff-concurrent.io/image-a:v1"
	imageB := "test-diff-concurrent.io/image-b:v1"
	keyA := canonicalizeImageName(imageA)
	keyB := canonicalizeImageName(imageB)

	defer imageInspectLocks.Delete(keyA)
	defer imageInspectLocks.Delete(keyB)

	enteredA := make(chan struct{})
	enteredB := make(chan struct{})
	proceedA := make(chan struct{})
	proceedB := make(chan struct{})

	// Goroutine for image A: signals when it has entered the critical section,
	// then waits for permission to exit.
	go func() {
		lock, release := getImageInspectLock(imageA)
		defer release()

		lock.Lock()
		defer lock.Unlock()

		close(enteredA) // Signal: A is inside.
		<-proceedA      // Wait: permission to leave.
	}()

	// Goroutine for image B: same pattern.
	go func() {
		lock, release := getImageInspectLock(imageB)
		defer release()

		lock.Lock()
		defer lock.Unlock()

		close(enteredB) // Signal: B is inside.
		<-proceedB      // Wait: permission to leave.
	}()

	// Wait until both goroutines are inside their critical sections.
	<-enteredA
	<-enteredB

	// Both goroutines are simultaneously in their critical sections,
	// proving different image locks do not block each other.

	close(proceedA)
	close(proceedB)
}
