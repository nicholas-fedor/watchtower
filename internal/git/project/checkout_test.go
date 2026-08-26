package project

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	goGit "github.com/go-git/go-git/v5"
	gitHTTP "github.com/go-git/go-git/v5/plumbing/transport/http"
)

func TestCheckoutStashReappliesLocalEnv(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	repo, err := goGit.PlainInit(dir, false)
	require.NoError(t, err)

	worktree, err := repo.Worktree()
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services: {}\n"), 0o600))

	_, err = worktree.Add("compose.yaml")
	require.NoError(t, err)

	first, err := worktree.Commit("init", &goGit.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@example.com", When: time.Now()},
	})
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services: {api: {}}\n"), 0o600))

	_, err = worktree.Add("compose.yaml")
	require.NoError(t, err)

	second, err := worktree.Commit("api", &goGit.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@example.com", When: time.Now()},
	})
	require.NoError(t, err)
	require.NotEqual(t, first, second)

	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=local\n"), 0o600))

	err = Checkout(t.Context(), dir, first.String(), CheckoutOptions{})
	require.ErrorIs(t, err, ErrDirty)

	err = Checkout(t.Context(), dir, first.String(), CheckoutOptions{Stash: true})
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(dir, ".env"))
	require.NoError(t, err)
	assert.Equal(t, "SECRET=local\n", string(got))

	compose, err := os.ReadFile(filepath.Join(dir, "compose.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "services: {}\n", string(compose))
}

func TestCheckoutCleanWorktree(t *testing.T) {
	t.Parallel()

	dir, first, second := initTwoCommitRepo(t)
	require.NoError(t, Checkout(t.Context(), dir, first, CheckoutOptions{}))

	got, err := os.ReadFile(filepath.Join(dir, "compose.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "services: {}\n", string(got))
	require.NoError(t, Checkout(t.Context(), dir, second, CheckoutOptions{}))
}

func TestLocalizeWorktreePath(t *testing.T) {
	t.Parallel()

	_, ok := localizeWorktreePath("")
	assert.False(t, ok)
	_, ok = localizeWorktreePath("/etc/passwd")
	assert.False(t, ok)
	_, ok = localizeWorktreePath("../outside")
	assert.False(t, ok)
	_, ok = localizeWorktreePath("ok/../../../etc")
	assert.False(t, ok)

	got, ok := localizeWorktreePath(".env")
	assert.True(t, ok)
	assert.Equal(t, ".env", got)

	got, ok = localizeWorktreePath("nested/file.env")
	assert.True(t, ok)
	assert.Equal(t, filepath.Join("nested", "file.env"), got)
}

func TestCheckoutErrors(t *testing.T) {
	t.Parallel()

	t.Run("not a repo", func(t *testing.T) {
		t.Parallel()

		err := Checkout(t.Context(), t.TempDir(), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CheckoutOptions{})
		require.Error(t, err)
		assert.ErrorContains(t, err, "open git project:")
	})

	t.Run("unknown commit", func(t *testing.T) {
		t.Parallel()

		dir, _, _ := initTwoCommitRepo(t)
		err := Checkout(t.Context(), dir, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CheckoutOptions{})
		require.Error(t, err)
		assert.ErrorContains(t, err, "resolve")
	})
}

func TestCheckoutFetchFailUsesLocalCommit(t *testing.T) {
	t.Parallel()

	dir, first, _ := initTwoCommitRepo(t)
	addBrokenOrigin(t, dir)

	require.NoError(t, Checkout(t.Context(), dir, first, CheckoutOptions{}))

	got, err := os.ReadFile(filepath.Join(dir, "compose.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "services: {}\n", string(got))
}

func TestCheckoutAuthForUsesOriginURL(t *testing.T) {
	t.Parallel()

	dir, first, _ := initTwoCommitRepo(t)

	const origin = "https://127.0.0.1:1/org/app.git"

	repo, err := goGit.PlainOpen(dir)
	require.NoError(t, err)
	_, err = repo.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{origin}})
	require.NoError(t, err)

	var seen string

	err = Checkout(t.Context(), dir, first, CheckoutOptions{
		AuthFor: func(repoURL string) (transport.AuthMethod, error) {
			seen = repoURL

			//nolint:nilnil // Anonymous Git access is a nil AuthMethod, not an error.
			return nil, nil
		},
	})
	require.NoError(t, err)
	assert.Equal(t, origin, seen)
}

func TestCheckoutAuthErrorWithoutLocalCommit(t *testing.T) {
	t.Parallel()

	dir, _, _ := initTwoCommitRepo(t)
	addBrokenOrigin(t, dir)

	err := Checkout(t.Context(), dir, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CheckoutOptions{
		AuthFor: func(string) (transport.AuthMethod, error) {
			return nil, os.ErrPermission
		},
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "project auth:")
}

func TestOriginFetchAuth(t *testing.T) {
	t.Parallel()

	basic := &gitHTTP.BasicAuth{Username: "git", Password: "tok"}

	assert.Nil(t, originFetchAuth("git@github.com:org/app.git", basic))
	assert.Nil(t, originFetchAuth("ssh://git@github.com/org/app.git", basic))
	assert.Equal(t, basic, originFetchAuth("https://github.com/org/app.git", basic))
	assert.Equal(t, basic, originFetchAuth("http://git.example.com/org/app.git", basic))
	assert.Nil(t, originFetchAuth("git@github.com:org/app.git", nil))
}

func TestOriginFetchOptionsDropsHTTPAuthOnSSHOrigin(t *testing.T) {
	t.Parallel()

	basic := &gitHTTP.BasicAuth{Username: "git", Password: "tok"}
	opts := CheckoutOptions{
		AuthFor: func(string) (transport.AuthMethod, error) {
			return basic, nil
		},
	}

	got, err := originFetchOptions("git@github.com:org/app.git", opts)
	require.NoError(t, err)
	assert.Nil(t, got.Auth)

	got, err = originFetchOptions("ssh://git@github.com/org/app.git", opts)
	require.NoError(t, err)
	assert.Nil(t, got.Auth)

	got, err = originFetchOptions("https://github.com/org/app.git", opts)
	require.NoError(t, err)
	assert.Equal(t, basic, got.Auth)
}

func TestCheckoutShortSHA(t *testing.T) {
	t.Parallel()

	dir, first, _ := initTwoCommitRepo(t)
	require.GreaterOrEqual(t, len(first), 12)
	require.NoError(t, Checkout(t.Context(), dir, first[:12], CheckoutOptions{}))

	got, err := os.ReadFile(filepath.Join(dir, "compose.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "services: {}\n", string(got))
}

// initTwoCommitRepo creates a repo with two commits of compose.yaml.
//
// Parameters:
//   - t: Test handle.
//
// Returns:
//   - string: Repository directory.
//   - string: First commit SHA.
//   - string: Second commit SHA.
func initTwoCommitRepo(t *testing.T) (string, string, string) {
	t.Helper()

	dir := t.TempDir()
	repo, err := goGit.PlainInit(dir, false)
	require.NoError(t, err)

	worktree, err := repo.Worktree()
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services: {}\n"), 0o600))

	_, err = worktree.Add("compose.yaml")
	require.NoError(t, err)

	first, err := worktree.Commit("init", &goGit.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@example.com", When: time.Now()},
	})
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services: {api: {}}\n"), 0o600))

	_, err = worktree.Add("compose.yaml")
	require.NoError(t, err)

	second, err := worktree.Commit("api", &goGit.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@example.com", When: time.Now()},
	})
	require.NoError(t, err)

	return dir, first.String(), second.String()
}

// addBrokenOrigin adds an origin remote that cannot be fetched.
func addBrokenOrigin(t *testing.T, dir string) {
	t.Helper()

	repo, err := goGit.PlainOpen(dir)
	require.NoError(t, err)

	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{"https://127.0.0.1:1/nope.git"},
	})
	require.NoError(t, err)
}
