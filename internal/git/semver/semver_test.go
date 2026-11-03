package semver

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestValidPolicy verifies that only the four policy names are accepted.
func TestValidPolicy(t *testing.T) {
	t.Parallel()

	assert.True(t, ValidPolicy(PolicyNone))
	assert.True(t, ValidPolicy(PolicyPatch))
	assert.True(t, ValidPolicy(PolicyMinor))
	assert.True(t, ValidPolicy(PolicyMajor))
	assert.False(t, ValidPolicy(""))
	assert.False(t, ValidPolicy("nightly"))
}
