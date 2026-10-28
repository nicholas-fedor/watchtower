package rebuild

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Test_noticeFormatting verifies how rebuild notices shorten commits and format
// repository paths and image names.
func Test_noticeFormatting(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "7ab67c773bf4", shortGitSHA("7ab67c773bf4bf670a97bc7b03a032a7fbd80892"))
	assert.Equal(t, "git.example.com/org/watchtower-git-support-test", gitNoticePath("https://git.example.com/org/watchtower-git-support-test.git"))
	assert.Equal(t, "git.example.com/org/watchtower-git-support-test", gitNoticePath("git@git.example.com:org/watchtower-git-support-test.git"))
	assert.Equal(t, "wtgit-semver:git-7ab67c773bf4", familiarImage("docker.io/library/wtgit-semver:git-7ab67c773bf4"))
}

// Test_withGitMeta verifies that a log event a disabled level discards, which
// zerolog represents as nil, stays nil instead of gaining Git fields.
func Test_withGitMeta(t *testing.T) {
	t.Parallel()

	c := gitAssociatedContainer("id", "/app", "myapp:latest", nil)

	assert.Nil(t, withGitMeta(nil, c, types.UpdateParams{EnableGitMonitoring: true}, "v1.0.0", "0123456789abcdef"))
}
