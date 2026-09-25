package git

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChangelog(t *testing.T) {
	t.Parallel()

	t.Run("explicit template", func(t *testing.T) {
		t.Parallel()

		c := testContainer(t, map[string]string{
			ChangelogLabel: "https://example.com/notes/{tag}#{commit}",
		}, "app:latest")
		got := Changelog(c, "https://github.com/org/app.git", ChangelogVars{Tag: "v1.2.3", Commit: "abc"})
		assert.Equal(t, "https://example.com/notes/v1.2.3#abc", got)
	})

	t.Run("derived github url", func(t *testing.T) {
		t.Parallel()

		got := Changelog(testContainer(t, nil, "app:latest"), "https://github.com/org/app.git", ChangelogVars{})
		assert.Equal(t, "https://github.com/org/app/releases", got)
	})

	t.Run("empty when unknown host", func(t *testing.T) {
		t.Parallel()

		got := Changelog(testContainer(t, nil, "app:latest"), "https://git.unknown.example/org/app.git", ChangelogVars{})
		assert.Empty(t, got)
	})
}

func TestApplyVars(t *testing.T) {
	t.Parallel()

	got := applyVars("https://ex/{major}.{minor}.{patch}/{tag}/{commit}", ChangelogVars{
		Tag:    "v1.2.3",
		Commit: "deadbeef",
	})
	assert.Equal(t, "https://ex/1.2.3/v1.2.3/deadbeef", got)

	got = applyVars("https://ex/{tag}/{commit}/{major}", ChangelogVars{})
	assert.Equal(t, "https://ex/{tag}/{commit}/{major}", got)
}

func TestOrUnchanged(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "{tag}", orUnchanged("", "{tag}"))
	assert.Equal(t, "v1", orUnchanged("v1", "{tag}"))
}

func TestSplitSemverParts(t *testing.T) {
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

			major, minor, patch := splitSemverParts(tt.tag)
			assert.Equal(t, tt.major, major)
			assert.Equal(t, tt.minor, minor)
			assert.Equal(t, tt.patch, patch)
		})
	}
}

func TestCanonicalizeSemver(t *testing.T) {
	t.Parallel()

	assert.Empty(t, canonicalizeSemver(""))
	assert.Empty(t, canonicalizeSemver("  "))
	assert.Equal(t, "v1.2.3", canonicalizeSemver("v1.2.3"))
	assert.Equal(t, "v1.2.3", canonicalizeSemver("1.2.3"))
	assert.Equal(t, "nightly", canonicalizeSemver("nightly"))
}

func TestDerivedReleasesURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		repo      string
		apiOrigin string
		want      string
	}{
		{name: "github", repo: "https://github.com/org/app.git", want: "https://github.com/org/app/releases"},
		{name: "gitlab", repo: "https://gitlab.com/org/app.git", want: "https://gitlab.com/org/app/-/releases"},
		{name: "codeberg", repo: "https://codeberg.org/org/app.git", want: "https://codeberg.org/org/app/releases"},
		{
			name:      "git-host label origin",
			repo:      "git@git.example.com:org/app.git",
			apiOrigin: "https://git.example.com:3000/gitea",
			want:      "https://git.example.com:3000/gitea/org/app/releases",
		},
		{
			name:      "self-hosted GitLab path",
			repo:      "https://gitlab.example.com/group/app.git",
			apiOrigin: "https://gitlab.example.com/gitlab",
			want:      "https://gitlab.example.com/gitlab/group/app/-/releases",
		},
		{
			name:      "known GitHub API path is not in releases URL",
			repo:      "https://github.com/org/app.git",
			apiOrigin: "https://github.com/api/v3",
			want:      "https://github.com/org/app/releases",
		},
		{
			name:      "mismatched git-host",
			repo:      "https://github.com/org/app.git",
			apiOrigin: "https://git.example.com/gitlab",
		},
		{
			name:      "pathless unknown git-host",
			repo:      "git@git.example.com:org/app.git",
			apiOrigin: "https://git.example.com:3000",
		},
		{
			name:      "unknown git-host path",
			repo:      "https://git.example.com/org/app.git",
			apiOrigin: "https://git.example.com/internal",
		},
		{name: "unknown", repo: "https://git.unknown.example/org/app.git"},
		{name: "empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, derivedReleasesURL(tt.repo, tt.apiOrigin))
		})
	}
}

func TestParseRepo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		raw   string
		host  string
		owner string
		repo  string
		ok    bool
	}{
		{name: "https", raw: "https://github.com/org/app.git", host: "github.com", owner: "org", repo: "app", ok: true},
		{name: "https with port", raw: "https://git.example.com:8443/org/app.git", host: "git.example.com:8443", owner: "org", repo: "app", ok: true},
		{name: "scp", raw: "git@github.com:org/app.git", host: "github.com", owner: "org", repo: "app", ok: true},
		{name: "ssh url", raw: "ssh://git@gitlab.com/group/app.git", host: "gitlab.com", owner: "group", repo: "app", ok: true},
		{name: "no scheme", raw: "github.com/org/app", host: "github.com", owner: "org", repo: "app", ok: true},
		{name: "nested group", raw: "https://gitlab.com/a/b/c.git", host: "gitlab.com", owner: "a/b", repo: "c", ok: true},
		{name: "scp empty host", raw: "git@:org/app.git"},
		{name: "scp missing colon", raw: "git@github.com"},
		{name: "host only", raw: "https://github.com"},
		{name: "scheme without host", raw: "https://"},
		{name: "empty"},
		{name: "whitespace", raw: "  "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			host, owner, name, ok := parseRepo(tt.raw)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.host, host)
			assert.Equal(t, tt.owner, owner)
			assert.Equal(t, tt.repo, name)
		})
	}
}

func TestSplitOwnerRepo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		host  string
		path  string
		owner string
		repo  string
		ok    bool
	}{
		{name: "simple", host: "github.com", path: "org/app.git", owner: "org", repo: "app", ok: true},
		{name: "leading slash", host: "github.com", path: "/org/app", owner: "org", repo: "app", ok: true},
		{name: "nested", host: "gitlab.com", path: "group/sub/repo", owner: "group/sub", repo: "repo", ok: true},
		{name: "no slash", host: "github.com", path: "app"},
		{name: "trailing slash", host: "github.com", path: "org/"},
		{name: "empty path", host: "github.com", path: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			host, owner, name, ok := splitOwnerRepo(tt.host, tt.path)
			assert.Equal(t, tt.ok, ok)

			if !tt.ok {
				assert.Empty(t, host)
				assert.Empty(t, owner)
				assert.Empty(t, name)

				return
			}

			assert.Equal(t, tt.host, host)
			assert.Equal(t, tt.owner, owner)
			assert.Equal(t, tt.repo, name)
		})
	}
}
