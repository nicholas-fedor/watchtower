package git

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/nicholas-fedor/watchtower/internal/git/semver"
)

// FuzzSelectTag verifies tag-policy selection never panics and only returns
// a tag from the advertised list.
func FuzzSelectTag(f *testing.F) {
	f.Add("v1.2.3", "v1.2.3\nv1.2.4\nv1.3.0", semver.PolicyPatch)
	f.Add("v1.2.3", "v1.2.4\nv2.0.0", semver.PolicyMinor)
	f.Add("", "v1.0.0\nv2.0.0", semver.PolicyMajor)
	f.Add("nightly", "v1.0.0", semver.PolicyMajor)
	f.Add("v1.0.0", "latest\nnightly", semver.PolicyPatch)
	f.Add("v1.2.3", "v1.2.4", "")
	f.Add("v1.2.3", "v1.2.4", semver.PolicyNone)
	f.Add("1.2.3", "1.2.4", semver.PolicyPatch)

	f.Fuzz(func(t *testing.T, lastTag, rawTags, policy string) {
		tags := strings.Split(rawTags, "\n")

		got, ok := selectTag(lastTag, tags, policy)
		if !ok {
			assert.Empty(t, got)

			return
		}

		assert.Contains(t, tags, got)
	})
}
