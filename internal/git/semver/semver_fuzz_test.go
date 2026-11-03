package semver

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/semver"
)

// FuzzSelectTag verifies tag-policy selection never panics and only returns
// a tag from the advertised list. Seeds cover every policy and an empty one.
// They also cover empty, unprefixed and non-semver baselines and a list of
// only non-semver tags.
func FuzzSelectTag(f *testing.F) {
	f.Add("v1.2.3", "v1.2.3\nv1.2.4\nv1.3.0", PolicyPatch)
	f.Add("v1.2.3", "v1.2.4\nv2.0.0", PolicyMinor)
	f.Add("", "v1.0.0\nv2.0.0", PolicyMajor)
	f.Add("nightly", "v1.0.0", PolicyMajor)
	f.Add("v1.0.0", "latest\nnightly", PolicyPatch)
	f.Add("v1.2.3", "v1.2.4", "")
	f.Add("v1.2.3", "v1.2.4", PolicyNone)
	f.Add("1.2.3", "1.2.4", PolicyPatch)

	f.Fuzz(func(t *testing.T, lastTag, rawTags, policy string) {
		tags := strings.Split(rawTags, "\n")

		got, ok := SelectTag(lastTag, tags, policy)
		if !ok {
			assert.Empty(t, got)

			return
		}

		assert.Contains(t, tags, got)
	})
}

// FuzzHighestRelease verifies that the result is empty or a release tag from
// the list and that no release tag in the list is higher. Seeds cover
// prefixed, unprefixed, pre-release, build metadata and non-semver tags.
func FuzzHighestRelease(f *testing.F) {
	f.Add("v1.2.3\nv2.0.0\nv1.10.0")
	f.Add("1.2.3\nv1.2.3")
	f.Add("v2.0.0-rc.1\nv1.0.0")
	f.Add("v1.2.3+a\nv1.2.3+b")
	f.Add("latest\nnightly")
	f.Add("")

	f.Fuzz(func(t *testing.T, rawTags string) {
		tags := strings.Split(rawTags, "\n")

		got := HighestRelease(tags)
		best := canonicalize(got)

		if got != "" {
			assert.Contains(t, tags, got)
			assert.True(t, semver.IsValid(best) && semver.Prerelease(best) == "", "result %q is not a release tag", got)
		}

		for _, tag := range tags {
			canon := canonicalize(tag)
			if semver.IsValid(canon) && semver.Prerelease(canon) == "" {
				require.NotEmpty(t, got, "release tag %q was skipped", tag)
				assert.LessOrEqual(t, semver.Compare(canon, best), 0, "tag %q is higher than %q", tag, got)
			}
		}
	})
}
