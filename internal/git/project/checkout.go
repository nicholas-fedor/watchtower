// Package project checks out an existing Compose project worktree to a monitored commit.
package project

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"

	goGit "github.com/go-git/go-git/v5"
	gitHTTP "github.com/go-git/go-git/v5/plumbing/transport/http"
)

const projectDirPerm = 0o700

// ErrDirty indicates the worktree has local changes and stash is off.
var ErrDirty = errors.New("git project worktree is dirty")

// CheckoutOptions controls how a project worktree is moved to a commit.
type CheckoutOptions struct {
	// Stash saves local changes, checks out the commit, then writes those files back.
	Stash bool
	// AuthFor returns fetch credentials for the origin URL. Nil is anonymous fetch.
	AuthFor func(repoURL string) (transport.AuthMethod, error)
	// restore replaces restoreSaved. Tests use it to fail a restore. Nil uses restoreSaved.
	restore func(root string, saved []savedFile) error
	// CABundle is extra PEM CAs for HTTPS fetch.
	CABundle []byte
	// InsecureSkipTLS skips TLS verification for HTTPS fetch.
	InsecureSkipTLS bool
}

// savedFile is a local file restored after checkout when Stash is set.
type savedFile struct {
	rel  string
	data []byte
	mode fs.FileMode
}

// Checkout fetches origin and checks out commit in an existing worktree.
//
// When Stash is false and the worktree is dirty, the call fails.
// When Stash is true, modified and untracked files are saved, the commit is
// checked out, and those files are written back.
// Fetch uses AuthFor, CABundle, and InsecureSkipTLS when set. If fetch fails
// but commit is already in the local object store, checkout continues.
//
// Parameters:
//   - ctx: Cancellation and timeout for fetch.
//   - dir: Existing Git checkout (Compose project directory).
//   - commit: Commit SHA from monitoring.
//   - opts: Stash, auth, and TLS settings.
//
// Returns:
//   - error: Non-nil on dirty-tree, missing commit, checkout, or restore failure.
func Checkout(ctx context.Context, dir, commit string, opts CheckoutOptions) error {
	repo, err := goGit.PlainOpen(dir)
	if err != nil {
		return fmt.Errorf("open git project: %w", err)
	}

	worktree, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("project worktree: %w", err)
	}

	status, err := worktree.Status()
	if err != nil {
		return fmt.Errorf("project status: %w", err)
	}

	dirty := !status.IsClean()
	if dirty && !opts.Stash {
		return ErrDirty
	}

	// Snapshot before fetch/checkout so local .env-style files can be restored.
	saved, err := snapshotDirty(dir, status)
	if err != nil {
		return err
	}

	// Keep a copy outside the worktree. A failed restore after checkout
	// must not be the only copy of those bytes.
	snapshotDir, err := writeSnapshot(opts.Stash, saved)
	if err != nil {
		return err
	}

	head, headErr := repo.Head()

	// Fetch may fail on a private origin or a dead remote. A commit already
	// in the local object store is enough to check out.
	fetchErr := fetchOrigin(ctx, repo, opts)

	resolved, err := repo.ResolveRevision(plumbing.Revision(commit))
	if err != nil {
		discardSnapshot(snapshotDir)

		if fetchErr != nil {
			return fetchErr
		}

		return fmt.Errorf("resolve %s: %w", commit, err)
	}

	err = worktree.Checkout(&goGit.CheckoutOptions{
		Hash:  *resolved,
		Force: true,
	})
	if err != nil {
		return finishCheckout(dir, snapshotDir, saved, opts, fmt.Errorf("checkout %s: %w", commit, err))
	}

	if !opts.Stash {
		discardSnapshot(snapshotDir)

		return nil
	}

	err = restoreSnapshot(dir, saved, opts)
	if err == nil {
		discardSnapshot(snapshotDir)

		return nil
	}

	if headErr == nil && head != nil {
		rbErr := worktree.Checkout(&goGit.CheckoutOptions{
			Hash:  head.Hash(),
			Force: true,
		})
		if rbErr != nil {
			return fmt.Errorf("%w (rollback checkout: %w; snapshot %s)", err, rbErr, snapshotDir)
		}
	}

	rbErr := restoreSnapshot(dir, saved, opts)
	if rbErr != nil {
		return fmt.Errorf("%w (rollback restore: %w; snapshot %s)", err, rbErr, snapshotDir)
	}

	discardSnapshot(snapshotDir)

	return err
}

// finishCheckout puts local files back when checkout fails part way through.
//
// Parameters:
//   - dir: Worktree root.
//   - snapshotDir: Temp copy of local files. Kept when restore fails.
//   - saved: In-memory snapshot.
//   - opts: Stash and test restore hook.
//   - checkoutErr: Checkout failure to return.
//
// Returns:
//   - error: checkoutErr, wrapped when the snapshot cannot be restored.
func finishCheckout(dir, snapshotDir string, saved []savedFile, opts CheckoutOptions, checkoutErr error) error {
	if !opts.Stash || len(saved) == 0 {
		discardSnapshot(snapshotDir)

		return checkoutErr
	}

	err := restoreSnapshot(dir, saved, opts)
	if err != nil {
		return fmt.Errorf("%w (local files: %w; snapshot %s)", checkoutErr, err, snapshotDir)
	}

	discardSnapshot(snapshotDir)

	return checkoutErr
}

// fetchOrigin updates refs from origin using process-wide Git auth and TLS.
//
// Parameters:
//   - ctx: Cancellation and timeout.
//   - repo: Open project repository.
//   - opts: Auth and TLS settings.
//
// Returns:
//   - error: Non-nil when origin exists and cannot be fetched.
func fetchOrigin(ctx context.Context, repo *goGit.Repository, opts CheckoutOptions) error {
	fetchOpts, err := originFetchOptions(originURL(repo), opts)
	if err != nil {
		return err
	}

	err = repo.FetchContext(ctx, fetchOpts)
	if err == nil || errors.Is(err, goGit.NoErrAlreadyUpToDate) || errors.Is(err, goGit.ErrRemoteNotFound) {
		return nil
	}

	return fmt.Errorf("fetch project: %w", err)
}

// originFetchOptions builds fetch options for origin, dropping HTTP BasicAuth on SSH remotes.
//
// Parameters:
//   - origin: Origin remote URL, or empty.
//   - opts: Auth and TLS settings.
//
// Returns:
//   - *goGit.FetchOptions: Options for FetchContext.
//   - error: Non-nil when AuthFor fails.
func originFetchOptions(origin string, opts CheckoutOptions) (*goGit.FetchOptions, error) {
	fetchOpts := &goGit.FetchOptions{
		RemoteName:      "origin",
		InsecureSkipTLS: opts.InsecureSkipTLS,
		CABundle:        opts.CABundle,
	}

	if origin == "" || opts.AuthFor == nil {
		return fetchOpts, nil
	}

	auth, err := opts.AuthFor(origin)
	if err != nil {
		return nil, fmt.Errorf("project auth: %w", err)
	}

	fetchOpts.Auth = originFetchAuth(origin, auth)

	return fetchOpts, nil
}

// originURL returns the first URL configured on the origin remote.
//
// Parameters:
//   - repo: Open repository.
//
// Returns:
//   - string: Origin URL, or empty when origin is missing.
func originURL(repo *goGit.Repository) string {
	if repo == nil {
		return ""
	}

	remote, err := repo.Remote("origin")
	if err != nil {
		return ""
	}

	cfg := remote.Config()
	if cfg == nil || len(cfg.URLs) == 0 {
		return ""
	}

	return cfg.URLs[0]
}

// originFetchAuth drops HTTP BasicAuth on SSH origins.
//
// The SSH transport rejects http.BasicAuth. A process-wide token must not
// be attached when origin is git@host:path or ssh://.
//
// Parameters:
//   - origin: Origin remote URL.
//   - auth: Auth returned by AuthFor.
//
// Returns:
//   - transport.AuthMethod: auth, or nil when BasicAuth cannot be used.
func originFetchAuth(origin string, auth transport.AuthMethod) transport.AuthMethod {
	if auth == nil || origin == "" {
		return auth
	}

	if _, ok := auth.(*gitHTTP.BasicAuth); !ok {
		return auth
	}

	endpoint, err := transport.NewEndpoint(origin)
	if err != nil {
		return auth
	}

	if endpoint.Protocol == "ssh" {
		return nil
	}

	return auth
}

// snapshotDirty copies modified and untracked regular files from the worktree.
//
// Deleted paths are skipped. They cannot be restored as file contents.
//
// Parameters:
//   - root: Worktree root.
//   - status: go-git status map.
//
// Returns:
//   - []savedFile: Files to restore after checkout.
//   - error: Non-nil when a dirty path cannot be read.
func snapshotDirty(root string, status goGit.Status) ([]savedFile, error) {
	opened, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("open project root: %w", err)
	}
	defer opened.Close()

	var saved []savedFile

	for rel, fileStatus := range status {
		if fileStatus == nil {
			continue
		}

		if fileStatus.Worktree == goGit.Unmodified && fileStatus.Staging == goGit.Unmodified {
			continue
		}

		// Deletions have no content to reapply after checkout.
		if fileStatus.Worktree == goGit.Deleted || fileStatus.Staging == goGit.Deleted {
			continue
		}

		name, ok := localizeWorktreePath(rel)
		if !ok {
			continue
		}

		info, err := opened.Lstat(name)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}

			return nil, fmt.Errorf("stat local change %s: %w", rel, err)
		}

		if !info.Mode().IsRegular() {
			continue
		}

		data, err := opened.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("read local change %s: %w", rel, err)
		}

		saved = append(saved, savedFile{rel: name, data: data, mode: info.Mode()})
	}

	return saved, nil
}

// writeSnapshot copies saved files to a temp directory outside the worktree.
//
// Parameters:
//   - stash: When false, no directory is created.
//   - saved: Files captured by snapshotDirty.
//
// Returns:
//   - string: Temp directory, or empty when there is nothing to keep.
//   - error: Non-nil when the copy cannot be written.
func writeSnapshot(stash bool, saved []savedFile) (string, error) {
	if !stash || len(saved) == 0 {
		return "", nil
	}

	dir, err := os.MkdirTemp("", "watchtower-git-stash-*")
	if err != nil {
		return "", fmt.Errorf("stash snapshot: %w", err)
	}

	err = restoreSaved(dir, saved)
	if err != nil {
		_ = os.RemoveAll(dir)

		return "", err
	}

	return dir, nil
}

// restoreSnapshot writes saved files back using the test hook when set.
//
// Parameters:
//   - dir: Worktree root.
//   - saved: Files captured by snapshotDirty.
//   - opts: Optional restore hook.
//
// Returns:
//   - error: Non-nil when a file cannot be written.
func restoreSnapshot(dir string, saved []savedFile, opts CheckoutOptions) error {
	if opts.restore != nil {
		return opts.restore(dir, saved)
	}

	return restoreSaved(dir, saved)
}

// discardSnapshot removes a temp snapshot. An empty path is a no-op.
//
// Parameters:
//   - dir: Temp directory from writeSnapshot.
//
// Returns:
//   - none.
func discardSnapshot(dir string) {
	if dir == "" {
		return
	}

	_ = os.RemoveAll(dir)
}

// restoreSaved writes snapshotted files back onto the worktree.
//
// Parameters:
//   - root: Worktree root.
//   - saved: Files captured by snapshotDirty.
//
// Returns:
//   - error: Non-nil when a file cannot be written.
func restoreSaved(root string, saved []savedFile) error {
	opened, err := os.OpenRoot(root)
	if err != nil {
		return fmt.Errorf("open project root: %w", err)
	}
	defer opened.Close()

	for _, file := range saved {
		dir := filepath.Dir(file.rel)
		if dir != "." {
			err := opened.MkdirAll(dir, projectDirPerm)
			if err != nil {
				return fmt.Errorf("restore %s: %w", file.rel, err)
			}
		}

		err := opened.WriteFile(file.rel, file.data, file.mode.Perm())
		if err != nil {
			return fmt.Errorf("restore %s: %w", file.rel, err)
		}
	}

	return nil
}

// localizeWorktreePath converts a go-git status path into a local path under the root.
//
// Parameters:
//   - rel: Slash-separated path from go-git status.
//
// Returns:
//   - string: Operating-system path that filepath.IsLocal accepts.
//   - bool: False when the path is empty, absolute, or escapes the root.
func localizeWorktreePath(rel string) (string, bool) {
	local, err := filepath.Localize(filepath.ToSlash(rel))
	if err != nil {
		return "", false
	}

	return local, true
}
