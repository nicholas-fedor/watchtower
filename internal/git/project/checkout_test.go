package project

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestCheckoutStashCanceledAfterRestoreRollsBack(t *testing.T) {
	t.Parallel()

	dir, first, second := initTwoCommitRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=local\n"), 0o600))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	err := Checkout(ctx, dir, first, CheckoutOptions{
		Stash: true,
		restore: func(root string, saved []savedFile) error {
			cancel()

			return restoreSaved(root, saved)
		},
	})
	require.ErrorIs(t, err, context.Canceled)

	repo, err := goGit.PlainOpen(dir)
	require.NoError(t, err)
	head, err := repo.Head()
	require.NoError(t, err)
	assert.Equal(t, second, head.Hash().String())

	got, err := os.ReadFile(filepath.Join(dir, ".env"))
	require.NoError(t, err)
	assert.Equal(t, "SECRET=local\n", string(got))
}

func TestRestoreHead(t *testing.T) {
	t.Parallel()

	dir, first, second := initTwoCommitRepo(t)
	require.NoError(t, Checkout(t.Context(), dir, first, CheckoutOptions{}))
	require.NoError(t, RestoreHead(dir, second, false))

	repo, err := goGit.PlainOpen(dir)
	require.NoError(t, err)
	head, err := repo.Head()
	require.NoError(t, err)
	assert.Equal(t, second, head.Hash().String())
	require.Error(t, RestoreHead(dir, "", false))
}

func TestRestoreHeadStashKeepsLocalFiles(t *testing.T) {
	t.Parallel()

	dir, first, _ := initTwoCommitRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=local\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services: {local: {}}\n"), 0o600))

	require.NoError(t, RestoreHead(dir, first, true))

	repo, err := goGit.PlainOpen(dir)
	require.NoError(t, err)
	head, err := repo.Head()
	require.NoError(t, err)
	assert.Equal(t, first, head.Hash().String())

	got, err := os.ReadFile(filepath.Join(dir, ".env"))
	require.NoError(t, err)
	assert.Equal(t, "SECRET=local\n", string(got))

	composeFile, err := os.ReadFile(filepath.Join(dir, "compose.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "services: {local: {}}\n", string(composeFile))
}

func TestCheckoutRestoreFailureRollsBack(t *testing.T) {
	t.Parallel()

	dir, first, second := initTwoCommitRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=local\n"), 0o600))

	calls := 0
	err := Checkout(t.Context(), dir, first, CheckoutOptions{
		Stash: true,
		restore: func(root string, saved []savedFile) error {
			calls++
			if calls == 1 {
				return os.ErrPermission
			}

			return restoreSaved(root, saved)
		},
	})
	require.ErrorIs(t, err, os.ErrPermission)
	assert.Equal(t, 2, calls)

	repo, err := goGit.PlainOpen(dir)
	require.NoError(t, err)
	head, err := repo.Head()
	require.NoError(t, err)
	assert.Equal(t, second, head.Hash().String())

	got, err := os.ReadFile(filepath.Join(dir, ".env"))
	require.NoError(t, err)
	assert.Equal(t, "SECRET=local\n", string(got))

	compose, err := os.ReadFile(filepath.Join(dir, "compose.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "services: {api: {}}\n", string(compose))
}

// TestCheckoutRestoreAndRollbackFailureKeepsLocalFiles verifies that when the
// stashed files cannot be written back and HEAD cannot be rolled back either,
// the checkout still tries to write the local files back and reports both
// failures.
//
// The first write-back fails and leaves the worktree read-only, so the HEAD
// rollback cannot rewrite compose.yaml. The second write-back makes the
// worktree writable again and succeeds. The test is skipped for root and on
// Windows because file modes stop neither.
func TestCheckoutRestoreAndRollbackFailureKeepsLocalFiles(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("file permissions do not block writes here")
	}

	dir, first, _ := initTwoCommitRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=local\n"), 0o600))

	compose := filepath.Join(dir, "compose.yaml")
	writable := func() {
		_ = os.Chmod(dir, 0o700)
		_ = os.Chmod(compose, 0o600)
	}
	t.Cleanup(writable)

	calls := 0
	err := Checkout(t.Context(), dir, first, CheckoutOptions{
		Stash: true,
		restore: func(root string, saved []savedFile) error {
			calls++
			if calls == 1 {
				require.NoError(t, os.Chmod(compose, 0o400))
				require.NoError(t, os.Chmod(dir, 0o500))

				return os.ErrPermission
			}

			writable()

			return restoreSaved(root, saved)
		},
	})
	require.ErrorIs(t, err, os.ErrPermission)
	require.ErrorContains(t, err, "rollback checkout")
	assert.Equal(t, 2, calls)

	got, err := os.ReadFile(filepath.Join(dir, ".env"))
	require.NoError(t, err)
	assert.Equal(t, "SECRET=local\n", string(got))
}

// TestCheckoutRestoreFailureKeepsSnapshot verifies that when the stashed files
// can never be written back, the checkout keeps their temp copy and names it in
// the error, since that copy is then the only intact copy of the local files.
func TestCheckoutRestoreFailureKeepsSnapshot(t *testing.T) {
	t.Parallel()

	dir, first, _ := initTwoCommitRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=local\n"), 0o600))

	err := Checkout(t.Context(), dir, first, CheckoutOptions{
		Stash: true,
		restore: func(string, []savedFile) error {
			return os.ErrPermission
		},
	})
	require.ErrorIs(t, err, os.ErrPermission)

	_, snapshot, found := strings.Cut(err.Error(), "snapshot ")
	require.True(t, found, "error names no snapshot: %v", err)

	snapshot = strings.TrimSuffix(snapshot, ")")

	t.Cleanup(func() { _ = os.RemoveAll(snapshot) })

	got, err := os.ReadFile(filepath.Join(snapshot, ".env"))
	require.NoError(t, err)
	assert.Equal(t, "SECRET=local\n", string(got))
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

// TestCheckoutCanceledFetchDoesNotMutate verifies that a canceled fetch leaves the project at its original commit.
func TestCheckoutCanceledFetchDoesNotMutate(t *testing.T) {
	t.Parallel()

	dir, first, second := initTwoCommitRepo(t)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancel()
		<-r.Context().Done()
	}))
	defer server.Close()

	repo, err := goGit.PlainOpen(dir)
	require.NoError(t, err)
	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{server.URL + "/repo.git"},
	})
	require.NoError(t, err)

	err = Checkout(ctx, dir, first, CheckoutOptions{})
	require.ErrorIs(t, err, context.Canceled)

	repo, err = goGit.PlainOpen(dir)
	require.NoError(t, err)
	head, err := repo.Head()
	require.NoError(t, err)
	assert.Equal(t, second, head.Hash().String())

	got, err := os.ReadFile(filepath.Join(dir, "compose.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "services: {api: {}}\n", string(got))
}

// TestCheckoutFetchErrorDoesNotExposeRemoteCredentials verifies that project fetch errors redact remote credentials.
func TestCheckoutFetchErrorDoesNotExposeRemoteCredentials(t *testing.T) {
	t.Parallel()

	const remote = "https://audit-user:fetch-password@%zz/repo.git?token=fetch-query-secret"

	dir, _, _ := initTwoCommitRepo(t)
	repo, err := goGit.PlainOpen(dir)
	require.NoError(t, err)
	_, err = repo.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{remote}})
	require.NoError(t, err)

	err = Checkout(
		t.Context(),
		dir,
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		CheckoutOptions{},
	)
	require.Error(t, err)
	require.ErrorContains(t, err, "fetch project: remote operation failed")
	assert.NotContains(t, err.Error(), remote)
	assert.NotContains(t, err.Error(), "audit-user")
	assert.NotContains(t, err.Error(), "fetch-password")
	assert.NotContains(t, err.Error(), "fetch-query-secret")
}

// TestOriginFetchOptionsErrorDoesNotExposeRemoteCredentials verifies that project authentication errors redact remote credentials.
func TestOriginFetchOptionsErrorDoesNotExposeRemoteCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		remote  string
		secrets []string
	}{
		{
			name:    "HTTPS",
			remote:  "https://audit-user:fetch-password@git.example.com/org/app.git?token=fetch-query-secret",
			secrets: []string{"audit-user", "fetch-password", "fetch-query-secret"},
		},
		{
			name:    "SCP",
			remote:  "audit-user:scp-password@git.example.com:org/app.git?token=scp-query-secret",
			secrets: []string{"audit-user", "scp-password", "scp-query-secret"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sentinel := errors.New("auth unavailable")
			_, err := originFetchOptions(tt.remote, CheckoutOptions{
				AuthFor: func(string) (transport.AuthMethod, error) {
					return nil, errors.Join(sentinel, errors.New(tt.remote))
				},
			})
			require.ErrorIs(t, err, sentinel)
			assert.Equal(t, "project auth: remote operation failed", err.Error())
			assert.NotContains(t, err.Error(), tt.remote)

			for _, secret := range tt.secrets {
				assert.NotContains(t, err.Error(), secret)
			}
		})
	}
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

// TestCheckoutRecordsPriorHead verifies that Checkout reports the HEAD from
// before the checkout through CheckoutOptions.Prior, which a caller uses to
// put the worktree back when it does not apply the commit.
func TestCheckoutRecordsPriorHead(t *testing.T) {
	t.Parallel()

	dir, first, second := initTwoCommitRepo(t)

	var prior string

	require.NoError(t, Checkout(t.Context(), dir, first, CheckoutOptions{Prior: &prior}))
	assert.Equal(t, second, prior)
}

// TestCheckoutFailureRollsBack verifies that a checkout that fails part way
// puts HEAD back and writes the stashed local files back.
//
// The target commit adds a file under a directory the worktree has made
// read-only, so go-git cannot write it. The test is skipped for root and on
// Windows because the directory mode stops neither.
func TestCheckoutFailureRollsBack(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permissions do not block writes here")
	}

	dir, _, second := initTwoCommitRepo(t)

	repo, err := goGit.PlainOpen(dir)
	require.NoError(t, err)
	worktree, err := repo.Worktree()
	require.NoError(t, err)

	// The third commit changes a tracked file as well as adding one, so the
	// test also shows that the rollback restores file contents.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services: {web: {}}\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "config"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config", "app.yaml"), []byte("app: {}\n"), 0o600))

	_, err = worktree.Add("compose.yaml")
	require.NoError(t, err)
	_, err = worktree.Add("config/app.yaml")
	require.NoError(t, err)

	third, err := worktree.Commit("config", &goGit.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@example.com", When: time.Now()},
	})
	require.NoError(t, err)

	// Go back to the second commit and leave an empty read-only config
	// directory that the third commit cannot write into. Git does not track
	// empty directories, so the worktree stays clean apart from the local .env
	// file.
	require.NoError(t, RestoreHead(dir, second, false))
	require.NoError(t, os.RemoveAll(filepath.Join(dir, "config")))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "config"), 0o500))
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "config"), 0o700) })
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=local\n"), 0o600))

	err = Checkout(t.Context(), dir, third.String(), CheckoutOptions{Stash: true})
	require.Error(t, err)

	head, err := repo.Head()
	require.NoError(t, err)
	assert.Equal(t, second, head.Hash().String())

	compose, err := os.ReadFile(filepath.Join(dir, "compose.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "services: {api: {}}\n", string(compose))

	got, err := os.ReadFile(filepath.Join(dir, ".env"))
	require.NoError(t, err)
	assert.Equal(t, "SECRET=local\n", string(got))
}
