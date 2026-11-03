package git

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/git/semver"
)

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
		{name: "none policy", lastTag: "v1.2.3", tags: tags, policy: semver.PolicyNone},
		{name: "patch", lastTag: "v1.2.3", tags: tags, policy: semver.PolicyPatch, want: "v1.2.4", ok: true},
		{name: "minor", lastTag: "v1.2.3", tags: tags, policy: semver.PolicyMinor, want: "v1.3.0", ok: true},
		{name: "major", lastTag: "v1.2.3", tags: tags, policy: semver.PolicyMajor, want: "v2.0.0", ok: true},
		{name: "already latest major", lastTag: "v2.0.0", tags: tags, policy: semver.PolicyMajor},
		{name: "only non-semver", lastTag: "v1.0.0", tags: []string{"nightly", "foo"}, policy: semver.PolicyPatch},
		{name: "no last tag picks highest", lastTag: "", tags: tags, policy: semver.PolicyMajor, want: "v2.0.0", ok: true},
		{name: "invalid last tag treated as empty", lastTag: "nightly", tags: tags, policy: semver.PolicyMajor, want: "v2.0.0", ok: true},
		{name: "unprefixed last tag", lastTag: "1.2.3", tags: tags, policy: semver.PolicyPatch, want: "v1.2.4", ok: true},
		{name: "unprefixed remote tags", lastTag: "v1.2.3", tags: []string{"1.2.4", "1.3.0"}, policy: semver.PolicyPatch, want: "1.2.4", ok: true},
		{name: "whitespace last tag", lastTag: "  ", tags: []string{"v1.0.1"}, policy: semver.PolicyMajor, want: "v1.0.1", ok: true},
		{name: "older candidates ignored", lastTag: "v1.2.4", tags: []string{"v1.2.3", "v1.2.4"}, policy: semver.PolicyPatch},
		{name: "patch skips pre-release", lastTag: "v1.2.3", tags: []string{"v1.2.4-rc.1", "v1.2.4"}, policy: semver.PolicyPatch, want: "v1.2.4", ok: true},
		{name: "patch ignores only pre-release", lastTag: "v1.2.3", tags: []string{"v1.2.4-rc.1"}, policy: semver.PolicyPatch},
		{name: "minor skips pre-release", lastTag: "v1.2.3", tags: []string{"v1.3.0-rc.1", "v1.3.0"}, policy: semver.PolicyMinor, want: "v1.3.0", ok: true},
		{name: "pre-release baseline moves to release", lastTag: "v1.2.3-rc.1", tags: []string{"v1.2.3", "v1.2.4-rc.1"}, policy: semver.PolicyPatch, want: "v1.2.3", ok: true},
		{name: "build metadata does not hide a release", lastTag: "v1.2.3", tags: []string{"v1.2.4+sha.1"}, policy: semver.PolicyPatch, want: "v1.2.4+sha.1", ok: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := selectTag(tt.lastTag, tt.tags, tt.policy)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAllowedBump(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		current   string
		candidate string
		policy    string
		want      bool
	}{
		{name: "not newer", current: "v1.2.3", candidate: "v1.2.3", policy: semver.PolicyMajor},
		{name: "older", current: "v1.2.3", candidate: "v1.2.2", policy: semver.PolicyMajor},
		{name: "patch same minor", current: "v1.2.3", candidate: "v1.2.4", policy: semver.PolicyPatch, want: true},
		{name: "patch different minor", current: "v1.2.3", candidate: "v1.3.0", policy: semver.PolicyPatch},
		{name: "minor same major", current: "v1.2.3", candidate: "v1.4.0", policy: semver.PolicyMinor, want: true},
		{name: "minor different major", current: "v1.2.3", candidate: "v2.0.0", policy: semver.PolicyMinor},
		{name: "major any newer", current: "v1.2.3", candidate: "v2.0.0", policy: semver.PolicyMajor, want: true},
		{name: "unknown policy", current: "v1.2.3", candidate: "v1.2.4", policy: "nightly"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, allowedBump(tt.current, tt.candidate, tt.policy))
		})
	}
}

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

func TestSelectTagKeepsRemoteName(t *testing.T) {
	t.Parallel()

	got, ok := selectTag("v1.0.0", []string{"1.0.1"}, semver.PolicyPatch)
	require.True(t, ok)
	assert.Equal(t, "1.0.1", got)
}
