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

// TestSelectTag verifies which tag each policy selects from a remote list,
// including unprefixed, pre-release and non-semver tags.
func TestSelectTag(t *testing.T) {
	t.Parallel()

	tags := []string{"v1.2.3", "v1.2.4", "v1.3.0", "v2.0.0", "latest"}

	tests := []struct {
		name    string
		lastTag string
		tags    []string
		policy  string
		want    string
		ok      bool
	}{
		{name: "empty policy", lastTag: "v1.2.3", tags: tags, policy: ""},
		{name: "none policy", lastTag: "v1.2.3", tags: tags, policy: PolicyNone},
		{name: "patch", lastTag: "v1.2.3", tags: tags, policy: PolicyPatch, want: "v1.2.4", ok: true},
		{name: "minor", lastTag: "v1.2.3", tags: tags, policy: PolicyMinor, want: "v1.3.0", ok: true},
		{name: "major", lastTag: "v1.2.3", tags: tags, policy: PolicyMajor, want: "v2.0.0", ok: true},
		{name: "already latest major", lastTag: "v2.0.0", tags: tags, policy: PolicyMajor},
		{name: "only non-semver", lastTag: "v1.0.0", tags: []string{"nightly", "foo"}, policy: PolicyPatch},
		{name: "unknown policy without last tag", lastTag: "", tags: tags, policy: "nightly"},
		{name: "no last tag picks highest", lastTag: "", tags: tags, policy: PolicyMajor, want: "v2.0.0", ok: true},
		{name: "invalid last tag treated as empty", lastTag: "nightly", tags: tags, policy: PolicyMajor, want: "v2.0.0", ok: true},
		{name: "unprefixed last tag", lastTag: "1.2.3", tags: tags, policy: PolicyPatch, want: "v1.2.4", ok: true},
		{name: "unprefixed remote tags", lastTag: "v1.2.3", tags: []string{"1.2.4", "1.3.0"}, policy: PolicyPatch, want: "1.2.4", ok: true},
		{name: "whitespace last tag", lastTag: "  ", tags: []string{"v1.0.1"}, policy: PolicyMajor, want: "v1.0.1", ok: true},
		{name: "older candidates ignored", lastTag: "v1.2.4", tags: []string{"v1.2.3", "v1.2.4"}, policy: PolicyPatch},
		{name: "patch skips pre-release", lastTag: "v1.2.3", tags: []string{"v1.2.4-rc.1", "v1.2.4"}, policy: PolicyPatch, want: "v1.2.4", ok: true},
		{name: "patch ignores only pre-release", lastTag: "v1.2.3", tags: []string{"v1.2.4-rc.1"}, policy: PolicyPatch},
		{name: "minor skips pre-release", lastTag: "v1.2.3", tags: []string{"v1.3.0-rc.1", "v1.3.0"}, policy: PolicyMinor, want: "v1.3.0", ok: true},
		{name: "pre-release baseline moves to release", lastTag: "v1.2.3-rc.1", tags: []string{"v1.2.3", "v1.2.4-rc.1"}, policy: PolicyPatch, want: "v1.2.3", ok: true},
		{name: "build metadata does not hide a release", lastTag: "v1.2.3", tags: []string{"v1.2.4+sha.1"}, policy: PolicyPatch, want: "v1.2.4+sha.1", ok: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := SelectTag(tt.lastTag, tt.tags, tt.policy)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestHighestRelease verifies which tag wins. Non-semver tags and
// pre-releases are skipped. Of equal tags, the name that sorts first is kept
// whatever the input order, because ls-remote lists tags in map order.
func TestHighestRelease(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tags []string
		want string
	}{
		{name: "no tags"},
		{name: "highest release", tags: []string{"v1.2.3", "v2.0.0", "v1.10.0"}, want: "v2.0.0"},
		{name: "unprefixed tags", tags: []string{"1.2.3", "1.10.0"}, want: "1.10.0"},
		{name: "non-semver tags skipped", tags: []string{"latest", "nightly", "v1.0.0"}, want: "v1.0.0"},
		{name: "pre-release skipped", tags: []string{"v1.0.0", "v2.0.0-rc.1"}, want: "v1.0.0"},
		{name: "only pre-release and non-semver", tags: []string{"v2.0.0-rc.1", "latest"}},
		{name: "equal tags keep lowest name", tags: []string{"1.2.3", "v1.2.3"}, want: "1.2.3"},
		{name: "equal tags keep lowest name reversed", tags: []string{"v1.2.3", "1.2.3"}, want: "1.2.3"},
		{name: "build metadata compares equal", tags: []string{"v1.2.3+a", "v1.2.3+b"}, want: "v1.2.3+a"},
		{name: "build metadata compares equal reversed", tags: []string{"v1.2.3+b", "v1.2.3+a"}, want: "v1.2.3+a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, HighestRelease(tt.tags))
		})
	}
}

// TestParts verifies the major, minor, and patch parts of prefixed,
// unprefixed, pre-release, and short tags.
func TestParts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		tag   string
		major string
		minor string
		patch string
	}{
		{name: "prefixed", tag: "v1.2.3", major: "1", minor: "2", patch: "3"},
		{name: "unprefixed", tag: "1.2.3", major: "1", minor: "2", patch: "3"},
		{name: "prerelease", tag: "v1.2.3-rc.1", major: "1", minor: "2", patch: "3"},
		{name: "major only", tag: "v2", major: "2", minor: "0", patch: "0"},
		{name: "invalid", tag: "nightly"},
		{name: "empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			major, minor, patch := Parts(tt.tag)
			assert.Equal(t, tt.major, major)
			assert.Equal(t, tt.minor, minor)
			assert.Equal(t, tt.patch, patch)
		})
	}
}

// TestBaseline verifies that a semver tag, pre-releases included, is kept as a
// baseline and that any other stamp is not.
func TestBaseline(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tag  string
		want string
	}{
		{name: "empty"},
		{name: "whitespace only", tag: "  "},
		{name: "prefixed", tag: "v1.2.3", want: "v1.2.3"},
		{name: "unprefixed", tag: "1.2.3", want: "1.2.3"},
		{name: "pre-release", tag: "v1.2.3-rc.1", want: "v1.2.3-rc.1"},
		{name: "surrounding whitespace trimmed", tag: "  v1.2.3  ", want: "v1.2.3"},
		{name: "non-semver", tag: "nightly"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, Baseline(tt.tag))
		})
	}
}

// TestAllowedBump verifies the version window each policy allows.
func TestAllowedBump(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		current   string
		candidate string
		policy    string
		want      bool
	}{
		{name: "not newer", current: "v1.2.3", candidate: "v1.2.3", policy: PolicyMajor},
		{name: "older", current: "v1.2.3", candidate: "v1.2.2", policy: PolicyMajor},
		{name: "patch same minor", current: "v1.2.3", candidate: "v1.2.4", policy: PolicyPatch, want: true},
		{name: "patch different minor", current: "v1.2.3", candidate: "v1.3.0", policy: PolicyPatch},
		{name: "minor same major", current: "v1.2.3", candidate: "v1.4.0", policy: PolicyMinor, want: true},
		{name: "minor different major", current: "v1.2.3", candidate: "v2.0.0", policy: PolicyMinor},
		{name: "major any newer", current: "v1.2.3", candidate: "v2.0.0", policy: PolicyMajor, want: true},
		{name: "unknown policy", current: "v1.2.3", candidate: "v1.2.4", policy: "nightly"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, allowedBump(tt.current, tt.candidate, tt.policy))
		})
	}
}

// TestCanonicalize verifies that tags gain a leading v only when that makes
// them valid semantic versions.
func TestCanonicalize(t *testing.T) {
	t.Parallel()

	assert.Empty(t, canonicalize(""))
	assert.Empty(t, canonicalize("  "))
	assert.Equal(t, "v1.2.3", canonicalize("v1.2.3"))
	assert.Equal(t, "v1.2.3", canonicalize("1.2.3"))
	assert.Equal(t, "v1.2.3", canonicalize("  1.2.3  "))
	assert.Equal(t, "nightly", canonicalize("nightly"))
	assert.Equal(t, "v1.2.3-rc.1", canonicalize("1.2.3-rc.1"))
}
