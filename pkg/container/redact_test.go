package container

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_redactedRemote(t *testing.T) {
	t.Parallel()

	t.Run("replaces a password and leaves other URLs unchanged", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, "https://xxxxx:xxxxx@github.com/org/app.git#abc", redactedRemote("https://user:secret@github.com/org/app.git#abc"))
		assert.Equal(t, "https://github.com/org/app.git", redactedRemote("https://github.com/org/app.git"))
		assert.Equal(t, "://bad", redactedRemote("://bad"))
	})
}

// TestRedactGitURL verifies that an SCP-style Git URL whose user looks like a token
// is dropped, while ordinary SSH users are kept.
func TestRedactGitURL(t *testing.T) {
	t.Parallel()

	assert.Empty(t, redactGitURL("token@github.com:org/app.git"))
	assert.Empty(t, redactGitURL("ghp_abcdefghijklmnopqrstuvwxyz@github.com:org/app.git"))
	assert.Equal(t, "git@github.com:org/app.git", redactGitURL("git@github.com:org/app.git"))
	assert.Equal(t, "deploy@git.example.com:org/app.git", redactGitURL("deploy@git.example.com:org/app.git"))
}
