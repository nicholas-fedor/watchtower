package git

import (
	"errors"
	"fmt"
)

// Errors returned by the Git watcher.
var (
	// ErrRefNotFound indicates the requested branch or tag does not exist.
	ErrRefNotFound = errors.New("git ref not found")
	// ErrInvalidRef indicates a ref name is not a safe Git reference.
	ErrInvalidRef = errors.New("invalid git ref")
	// ErrCloneFailed indicates clone or checkout failed.
	ErrCloneFailed = errors.New("git clone failed")
	// ErrAuthRequired indicates the remote rejected anonymous access.
	ErrAuthRequired = errors.New("git authentication required")
	// ErrAuthFailed indicates the configured credentials were rejected.
	ErrAuthFailed = errors.New("git authorization failed")
	// ErrRepoNotFound indicates the remote repository does not exist.
	ErrRepoNotFound = errors.New("git repository not found")
	// ErrInvalidPolicy indicates a git-semver-policy label is not none/patch/minor/major.
	ErrInvalidPolicy = errors.New("invalid git semver policy")
	// ErrInvalidHost indicates a git-host label is not an HTTP base URL.
	ErrInvalidHost = errors.New("invalid git-host")
)

// Errors for HTTP provider probes.
var (
	// errCrossHostRedirect indicates a Location header pointed at another origin.
	errCrossHostRedirect = errors.New("git http redirect left the original host")
	// errEmptyProbeURL indicates a fingerprint request had no host.
	errEmptyProbeURL = errors.New("empty probe url")
	// errAPINotFound indicates the provider has no such ref.
	errAPINotFound = fmt.Errorf("%w", ErrRefNotFound)
	// errAPIStatus indicates a non-success HTTP status from a Git provider.
	errAPIStatus = errors.New("git provider http error")
)
