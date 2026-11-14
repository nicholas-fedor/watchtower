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

var (
	errRestoreHeadMissing = errors.New("restore project head: missing directory or commit")
	errRestoreHeadInvalid = errors.New("restore project head: invalid commit")
)

type remoteOperationError struct {
	cause error
}

// Error returns a sanitized message for a remote operation failure.
//
// Returns:
//   - string: Generic remote operation failure message.
func (e *remoteOperationError) Error() string {
	return "remote operation failed"
}

// Unwrap returns the underlying remote operation error.
//
// Returns:
//   - error: Original error, if present.
func (e *remoteOperationError) Unwrap() error {
	return e.cause
}

// sanitizeRemoteError wraps err so its message cannot expose remote details.
//
// Parameters:
//   - err: Error returned by a remote operation.
//
// Returns:
//   - error: Sanitized wrapper, or nil when err is nil.
func sanitizeRemoteError(err error) error {
	if err == nil {
		return nil
	}

	return &remoteOperationError{cause: err}
}

// CheckoutOptions controls how a project worktree is moved to a commit.
type CheckoutOptions struct {
	// AuthFor returns fetch credentials for the origin URL. Nil is anonymous fetch.
	AuthFor func(repoURL string) (transport.AuthMethod, error)
	// restore replaces restoreSaved. Tests use it to fail a restore. Nil uses restoreSaved.
	restore func(root string, saved []savedFile) error
	// Prior, when non-nil, receives the HEAD hash from before checkout.
	// The caller uses it to restore the worktree when it will not apply.
	Prior *string
	// CABundle is extra PEM CAs for HTTPS fetch.
	CABundle []byte
	// Stash saves local changes, checks out the commit, then writes those files back.
	Stash bool
	// InsecureSkipTLS skips TLS verification for HTTPS fetch.
	InsecureSkipTLS bool
}

// savedFile is a local file restored after checkout when Stash is set.
type savedFile struct {
	rel  string
	data []byte
	mode fs.FileMode
}

// localStash holds the local files saved before a checkout, with a copy
// outside the worktree.
//
// The copy is removed when the operation ends, unless the last write-back
// failed. Then it is the only intact copy of those files.
type localStash struct {
	// restore writes the files back. Nil writes them under root.
	restore func(root string, saved []savedFile) error
	// root is the worktree root.
	root string
	// copyDir is the temp copy of the files, or empty when none were saved.
	copyDir string
	// files are the saved local files.
	files []savedFile
	// kept is true while the last write-back failed, so the copy is kept.
	kept bool
}

// projectCheckout is a checkout in progress of an existing project worktree.
//
// It keeps what an undo needs: the HEAD before checkout and the saved local
// files.
type projectCheckout struct {
	// repo is the open project repository.
	repo *goGit.Repository
	// worktree is the repository's worktree.
	worktree *goGit.Worktree
	// stash holds the local files to write back after checkout.
	stash localStash
	// opts are the caller's checkout options.
	opts CheckoutOptions
	// prior is the HEAD before checkout, or zero when it could not be read.
	prior plumbing.Hash
}

// Checkout fetches origin and checks out commit in an existing worktree.
//
// The worktree and its HEAD are read after the fetch, just before the
// checkout, so a change made during the fetch counts. When Stash is false
// and the worktree is dirty, the call fails. When Stash is true, modified and
// untracked files are saved, the commit is checked out, and those files are
// written back.
// Fetch uses AuthFor, CABundle, and InsecureSkipTLS when set. If fetch fails
// but commit is already in the local object store, checkout continues.
// A checkout that fails or is canceled after the worktree changed puts the
// previous HEAD and the saved files back.
//
// Parameters:
//   - ctx: Cancellation and timeout for the fetch and checkout.
//   - dir: Existing Git checkout (Compose project directory).
//   - commit: Commit SHA from monitoring.
//   - opts: Stash, auth, and TLS settings.
//
// Returns:
//   - error: Non-nil on dirty-tree, missing commit, checkout, or restore failure.
func Checkout(ctx context.Context, dir, commit string, opts CheckoutOptions) error {
	checkout, err := openCheckout(ctx, dir, opts)
	if err != nil {
		return err
	}

	defer checkout.stash.discard()

	hash, err := checkout.resolve(ctx, commit)
	if err != nil {
		return err
	}

	err = checkout.captureLocal()
	if err != nil {
		return err
	}

	err = checkout.switchTo(ctx, hash)
	if err != nil {
		return err
	}

	return checkout.restoreStash(ctx)
}

// openCheckout opens the project repository and its worktree.
//
// Parameters:
//   - ctx: Cancellation for the checkout.
//   - dir: Existing Git checkout.
//   - opts: The caller's checkout options.
//
// Returns:
//   - *projectCheckout: The opened checkout.
//   - error: Non-nil when ctx is done or the repository or its worktree cannot
//     be opened.
func openCheckout(ctx context.Context, dir string, opts CheckoutOptions) (*projectCheckout, error) {
	err := checkoutCanceled(ctx)
	if err != nil {
		return nil, err
	}

	repo, err := goGit.PlainOpen(dir)
	if err != nil {
		return nil, fmt.Errorf("open git project: %w", err)
	}

	err = checkoutCanceled(ctx)
	if err != nil {
		return nil, err
	}

	worktree, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("project worktree: %w", err)
	}

	return &projectCheckout{
		repo:     repo,
		worktree: worktree,
		stash:    localStash{restore: opts.restore, root: dir, copyDir: "", files: nil, kept: false},
		opts:     opts,
		prior:    plumbing.ZeroHash,
	}, nil
}

// resolve fetches origin and resolves commit.
//
// A commit already in the local object store is enough to check out, so a
// fetch that fails on a private origin or a dead remote does not end the
// checkout. A fetch that ends because ctx is done ends the checkout.
//
// Parameters:
//   - ctx: Cancellation and timeout for the fetch.
//   - commit: Commit SHA, full or abbreviated.
//
// Returns:
//   - plumbing.Hash: The resolved commit.
//   - error: The fetch error when ctx ended it or the commit is missing, or the
//     resolve or cancellation error.
func (c *projectCheckout) resolve(ctx context.Context, commit string) (plumbing.Hash, error) {
	fetchErr := fetchOrigin(ctx, c.repo, c.opts)

	if ctx.Err() != nil ||
		errors.Is(fetchErr, context.Canceled) ||
		errors.Is(fetchErr, context.DeadlineExceeded) {
		if fetchErr != nil {
			return plumbing.ZeroHash, fetchErr
		}

		return plumbing.ZeroHash, checkoutCanceled(ctx)
	}

	resolved, err := c.repo.ResolveRevision(plumbing.Revision(commit))
	if err != nil {
		canceled := checkoutCanceled(ctx)
		if canceled != nil {
			return plumbing.ZeroHash, canceled
		}

		if fetchErr != nil {
			return plumbing.ZeroHash, fetchErr
		}

		return plumbing.ZeroHash, fmt.Errorf("resolve %s: %w", commit, err)
	}

	err = checkoutCanceled(ctx)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	return *resolved, nil
}

// captureLocal saves the worktree's local files and records its HEAD, which
// an undo puts back.
//
// A dirty worktree fails with [ErrDirty] unless Stash is set. With Stash set,
// modified and untracked files are saved. Once they are saved, the HEAD is
// recorded in Prior when it is set.
//
// Returns:
//   - error: [ErrDirty], or the error from reading the worktree or saving its
//     files.
func (c *projectCheckout) captureLocal() error {
	status, err := c.worktree.Status()
	if err != nil {
		return fmt.Errorf("project status: %w", err)
	}

	if !status.IsClean() && !c.opts.Stash {
		return ErrDirty
	}

	err = c.stash.save(status)
	if err != nil {
		return err
	}

	head, err := c.repo.Head()
	if err == nil && head != nil {
		c.prior = head.Hash()

		if c.opts.Prior != nil {
			*c.opts.Prior = c.prior.String()
		}
	}

	return nil
}

// switchTo checks the worktree out at hash.
//
// A done ctx leaves the worktree untouched. A checkout that fails or that
// ctx ends is undone. The previous HEAD is checked out again and the saved
// local files are written back.
//
// Parameters:
//   - ctx: Cancellation for the checkout.
//   - hash: The commit to check out.
//
// Returns:
//   - error: The checkout or cancellation error, joined with any undo failure.
func (c *projectCheckout) switchTo(ctx context.Context, hash plumbing.Hash) error {
	// Saving the local files can take a while, so ctx may have ended since
	// the fetch.
	err := checkoutCanceled(ctx)
	if err != nil {
		return err
	}

	err = c.worktree.Checkout(&goGit.CheckoutOptions{
		Hash:  hash,
		Force: true,
	})
	if err != nil {
		return c.abort(err)
	}

	err = checkoutCanceled(ctx)
	if err != nil {
		return c.abort(err)
	}

	return nil
}

// restoreStash writes the saved local files back onto the checked-out commit.
//
// It does nothing without Stash. When the files cannot be written back, or
// ctx ends after they are, the checkout is undone. The previous HEAD is
// checked out again and the saved local files are written back.
//
// Parameters:
//   - ctx: Cancellation for the checkout.
//
// Returns:
//   - error: The restore or cancellation error, joined with any undo failure.
func (c *projectCheckout) restoreStash(ctx context.Context) error {
	if !c.opts.Stash {
		return nil
	}

	err := c.stash.putBack()
	if err == nil {
		err = checkoutCanceled(ctx)
	}

	if err != nil {
		return c.abort(err)
	}

	return nil
}

// abort undoes a checkout that failed or was canceled after the worktree
// changed.
//
// It checks the previous HEAD out again when it is known, then writes the
// saved local files back.
//
// Parameters:
//   - cause: The error that ended the checkout.
//
// Returns:
//   - error: cause, joined with a failed HEAD rollback, and wrapped with the
//     snapshot path when the local files cannot be written back.
func (c *projectCheckout) abort(cause error) error {
	if !c.prior.IsZero() {
		err := c.worktree.Checkout(&goGit.CheckoutOptions{
			Hash:  c.prior,
			Force: true,
		})
		if err != nil {
			cause = errors.Join(cause, fmt.Errorf("rollback checkout: %w", err))
		}
	}

	err := c.stash.putBack()
	if err != nil {
		return fmt.Errorf("%w (local files: %w; snapshot %s)", cause, err, c.stash.copyDir)
	}

	return cause
}

// save saves the worktree's modified and untracked files and copies them to
// a temp directory outside the worktree.
//
// The copy means a failed write-back is never the only copy of those bytes.
// A clean worktree saves nothing.
//
// Parameters:
//   - status: The worktree's status.
//
// Returns:
//   - error: Non-nil when a file cannot be read or the copy cannot be written.
func (s *localStash) save(status goGit.Status) error {
	files, err := snapshotDirty(s.root, status)
	if err != nil {
		return err
	}

	copyDir, err := writeSnapshot(files)
	if err != nil {
		return err
	}

	s.files = files
	s.copyDir = copyDir

	return nil
}

// putBack writes the saved files back onto the worktree.
//
// It does nothing when no files were saved.
//
// Returns:
//   - error: Non-nil when a file cannot be written.
func (s *localStash) putBack() error {
	if len(s.files) == 0 {
		return nil
	}

	write := s.restore
	if write == nil {
		write = restoreSaved
	}

	err := write(s.root, s.files)
	s.kept = err != nil

	return err
}

// discard removes the temp copy unless the last write-back failed.
func (s *localStash) discard() {
	if s.kept || s.copyDir == "" {
		return
	}

	_ = os.RemoveAll(s.copyDir)
}

// checkoutCanceled reports a done ctx as a canceled checkout.
//
// Parameters:
//   - ctx: Cancellation for the checkout.
//
// Returns:
//   - error: ctx's error wrapped as a canceled checkout, or nil while ctx is live.
func checkoutCanceled(ctx context.Context) error {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("checkout canceled: %w", err)
	}

	return nil
}

// RestoreHead checks the worktree out at hash.
//
// Call this after Checkout returned nil when the caller will not apply that
// commit. A local checkout does not use the caller's context, which may
// already be canceled. When stash is set, dirty files are saved before the
// forced checkout and written back afterward. When stash is unset, the forced
// checkout is unchanged.
//
// Parameters:
//   - dir: Existing Git checkout.
//   - hash: Commit recorded in CheckoutOptions.Prior.
//   - stash: Whether to save and restore local files around the checkout.
//
// Returns:
//   - error: Non-nil when dir or hash is missing, or the checkout fails.
func RestoreHead(dir, hash string, stash bool) error {
	if dir == "" || hash == "" {
		return errRestoreHeadMissing
	}

	parsed := plumbing.NewHash(hash)
	if parsed.IsZero() {
		return fmt.Errorf("%w: %s", errRestoreHeadInvalid, hash)
	}

	repo, err := goGit.PlainOpen(dir)
	if err != nil {
		return fmt.Errorf("restore project head: %w", err)
	}

	worktree, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("restore project head: %w", err)
	}

	local := localStash{restore: nil, root: dir, copyDir: "", files: nil, kept: false}

	if stash {
		status, err := worktree.Status()
		if err != nil {
			return fmt.Errorf("restore project head: %w", err)
		}

		err = local.save(status)
		if err != nil {
			return err
		}
	}

	defer local.discard()

	err = worktree.Checkout(&goGit.CheckoutOptions{
		Hash:  parsed,
		Force: true,
	})

	restoreErr := local.putBack()
	if restoreErr != nil {
		restoreErr = fmt.Errorf("%w; snapshot %s", restoreErr, local.copyDir)
	}

	if err != nil {
		if restoreErr != nil {
			return fmt.Errorf("restore project head: %w", errors.Join(err, restoreErr))
		}

		return fmt.Errorf("restore project head: %w", err)
	}

	if restoreErr != nil {
		return fmt.Errorf("restore project head: %w", restoreErr)
	}

	return nil
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

	return fmt.Errorf("fetch project: %w", sanitizeRemoteError(err))
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
		return nil, fmt.Errorf("project auth: %w", sanitizeRemoteError(err))
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
//   - saved: Files captured by snapshotDirty.
//
// Returns:
//   - string: Temp directory, or empty when there is nothing to keep.
//   - error: Non-nil when the copy cannot be written.
func writeSnapshot(saved []savedFile) (string, error) {
	if len(saved) == 0 {
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
