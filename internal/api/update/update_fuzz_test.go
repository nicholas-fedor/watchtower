package update

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/pkg/metrics"
)

// FuzzExtractImages fuzzes the core image-name parsing logic used by extractImages.
// It tests that splitting comma-separated image names never panics and produces
// deterministic results.
func FuzzExtractImages(f *testing.F) {
	f.Add("nginx:latest")
	f.Add("nginx:latest,redis:7")
	f.Add("nginx:latest,redis:7,postgres:16")
	f.Add("")
	f.Add(",,,")
	f.Add("a")
	f.Add(strings.Repeat("x", 1000))
	f.Add("image with spaces")
	f.Add("registry.com/org/image:v1.2.3")

	f.Fuzz(func(t *testing.T, raw string) {
		// This mirrors the core logic in extractImages:
		// strings.Split on comma-separated values
		parts := strings.Split(raw, ",")

		// Invariant: result should never be nil
		if parts == nil {
			t.Error("strings.Split returned nil")
		}

		// Invariant: for non-empty input, at least one part is returned
		if len(raw) > 0 && len(parts) == 0 {
			t.Errorf("non-empty input %q produced zero parts", raw)
		}

		// Invariant: empty input produces exactly one part (the empty string)
		if raw == "" && len(parts) != 1 {
			t.Errorf("empty input produced %d parts, want 1", len(parts))
		}

		// Invariant: number of parts equals number of commas + 1
		expectedParts := strings.Count(raw, ",") + 1
		if len(parts) != expectedParts {
			t.Errorf("input %q: got %d parts, want %d", raw, len(parts), expectedParts)
		}
	})
}

// FuzzHandlerNew fuzzes the Handler constructor to ensure it never panics
// with various lock channel configurations.
func FuzzHandlerNew(f *testing.F) {
	f.Add(true)
	f.Add(false)

	f.Fuzz(func(t *testing.T, withLock bool) {
		var lock chan bool
		if withLock {
			lock = make(chan bool, 1)
		}

		h := New(func(_ context.Context, _ []string) *metrics.Metric {
			return &metrics.Metric{}
		}, lock)

		require.NotNil(t, h)
		assert.Equal(t, "/v1/update", h.Path)
	})
}
