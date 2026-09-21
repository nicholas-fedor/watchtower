package git

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/rs/zerolog"

	gitPkg "github.com/nicholas-fedor/watchtower/pkg/container/git"
	"github.com/nicholas-fedor/watchtower/pkg/types"
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

// Options configures a Git watcher client. Credentials stay here, not on UpdateParams.
type Options struct {
	Token           string
	Username        string
	Password        string
	SSHKeyPath      string
	SSHKnownHosts   string
	Timeout         time.Duration
	CABundle        []byte
	InsecureSkipTLS bool
	Hosts           map[string]string
}

// CheckRequest is a remote staleness query for one associated container.
type CheckRequest struct {
	Repo       string
	Ref        string
	Policy     string
	Host       string
	LastCommit string
	LastTag    string
}

// CheckResult is the outcome of a remote Git check.
type CheckResult struct {
	Stale  bool
	Commit string
	Tag    string
	Kind   string
	// Ref is the branch or tag name used to clone (empty when unknown).
	Ref string
}

// Client inspects remotes and clones repositories for Git-sourced updates.
type Client struct {
	log     *zerolog.Logger
	opts    Options
	http    *http.Client
	lister  RefLister
	origins map[string]url.URL
	mu      sync.Mutex
	seen    map[string]seenStamp
}

// seenStamp is a remote tip observed without rebuilding the running container.
type seenStamp struct {
	commit string
	tag    string
}

// RefLister lists remote refs for staleness checks.
type RefLister interface {
	List(ctx context.Context, repo string) ([]RemoteRef, error)
}

// RemoteRef is a named ref from ls-remote or a provider tag list.
type RemoteRef struct {
	Name string
	Hash string
}

const (
	kindBranch        = "branch"
	kindTag           = "tag"
	defaultGitTimeout = 30 * time.Second
)

// New constructs a Git watcher client.
//
// Parameters:
//   - log: Process logger. A nop logger is used when nil.
//   - opts: Auth, timeout, and optional extra host→provider mappings.
//
// Returns:
//   - *Client: Ready client. Safe to hold when no container is watched.
func New(log *zerolog.Logger, opts Options) *Client {
	if log == nil {
		log = new(zerolog.Nop())
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultGitTimeout
	}

	client := &Client{
		log:     log,
		opts:    opts,
		http:    newGitHTTPClient(timeout, opts.InsecureSkipTLS, opts.CABundle),
		origins: make(map[string]url.URL),
	}
	client.lister = gitLister{client: client}

	return client
}

// Check reports whether the remote has advanced past the known running revision.
//
// An empty last-commit or tag is not stale. There is nothing to compare, so
// Watchtower must not rebuild only to write a stamp. Missing refs return an
// error so the container can be skipped without failing the session.
//
// Parameters:
//   - ctx: Cancellation and deadline.
//   - req: Repo, ref, policy, and last observed stamp.
//
// Returns:
//   - CheckResult: Stale flag and the remote commit/tag to build.
//   - error: Non-nil on auth, network, or missing-ref failures.
func (c *Client) Check(ctx context.Context, req CheckRequest) (CheckResult, error) {
	if c == nil {
		return CheckResult{}, nil
	}

	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	req.Ref = cmpOr(req.Ref, "main")
	req.Policy = cmpOr(req.Policy, types.GitPolicyNone)

	err := validateRef(req.Ref)
	if err != nil {
		return CheckResult{}, err
	}

	if req.Policy != types.GitPolicyNone {
		return c.checkTagPolicy(ctx, req)
	}

	return c.checkExactRef(ctx, req)
}

// Clone checks out repo at the resolved revision into a new temp directory.
//
// The caller must remove the returned directory.
//
// Parameters:
//   - ctx: Cancellation and deadline.
//   - repo: Clone URL.
//   - rev: Commit, tag, kind, and ref from Check.
//
// Returns:
//   - string: Temporary directory containing the worktree.
//   - error: Non-nil on clone or checkout failure. The directory is removed on error.
func (c *Client) Clone(ctx context.Context, repo string, rev CheckResult) (string, error) {
	if c == nil {
		return "", fmt.Errorf("%w: client is nil", ErrCloneFailed)
	}

	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	dir, err := os.MkdirTemp("", "watchtower-git-*")
	if err != nil {
		return "", fmt.Errorf("%w: mkdir: %w", ErrCloneFailed, err)
	}

	err = cloneCheckout(ctx, dir, repo, rev, c)
	if err != nil {
		_ = os.RemoveAll(dir)

		return "", err
	}

	return dir, nil
}

// checkExactRef compares one branch or tag to the last-commit stamp.
//
// Parameters:
//   - ctx: Cancellation and timeout.
//   - req: Repo, ref, and last commit.
//
// Returns:
//   - CheckResult: Stale when a known revision differs from the remote tip.
//   - error: Non-nil when the ref cannot be resolved.
func (c *Client) checkExactRef(ctx context.Context, req CheckRequest) (CheckResult, error) {
	resolved, err := c.resolveRef(ctx, req.Repo, req.Ref, req.Host)
	if err != nil {
		return CheckResult{}, err
	}

	stale := req.LastCommit != "" && !sameRevision(req.LastCommit, resolved.Hash)

	return CheckResult{
		Stale:  stale,
		Commit: resolved.Hash,
		Tag:    tagName(resolved),
		Kind:   resolved.Kind,
		Ref:    resolved.Name,
	}, nil
}

// checkTagPolicy selects a newer semver tag allowed by req.Policy.
//
// Parameters:
//   - ctx: Cancellation and timeout.
//   - req: Repo, policy, and last tag stamp.
//
// Returns:
//   - CheckResult: Stale when a newer allowed tag exists.
//   - error: Non-nil when tags cannot be listed.
func (c *Client) checkTagPolicy(ctx context.Context, req CheckRequest) (CheckResult, error) {
	tags, err := c.listTags(ctx, req.Repo, req.Host)
	if err != nil {
		return CheckResult{}, err
	}

	names := make([]string, 0, len(tags))
	byName := make(map[string]string, len(tags))

	for _, tag := range tags {
		names = append(names, tag.Name)
		byName[tag.Name] = tag.Hash
	}

	if req.LastTag == "" && req.LastCommit == "" {
		selected, ok := SelectTag("", names, req.Policy)
		if !ok {
			// No usable semver tags. Observe the configured ref without
			// treating the missing baseline as a reason to rebuild.
			return c.checkExactRef(ctx, CheckRequest{
				Repo:       req.Repo,
				Ref:        req.Ref,
				Policy:     types.GitPolicyNone,
				Host:       req.Host,
				LastCommit: req.LastCommit,
			})
		}

		return CheckResult{
			Stale:  false,
			Commit: byName[selected],
			Tag:    selected,
			Kind:   kindTag,
			Ref:    selected,
		}, nil
	}

	selected, ok := SelectTag(req.LastTag, names, req.Policy)
	if !ok {
		return CheckResult{Stale: false}, nil
	}

	return CheckResult{
		Stale:  true,
		Commit: byName[selected],
		Tag:    selected,
		Kind:   kindTag,
		Ref:    selected,
	}, nil
}

type resolvedRef struct {
	Name string
	Hash string
	Kind string
}

// resolveRef resolves ref via the product API, then go-git ls-remote.
//
// Parameters:
//   - ctx: Cancellation and timeout.
//   - repo: Clone URL.
//   - ref: Branch or tag name.
//   - apiOrigin: Optional HTTP API base URL from the git-host label.
//
// Returns:
//   - resolvedRef: Name, hash, and kind.
//   - error: Non-nil when the ref is missing or listing fails.
func (c *Client) resolveRef(ctx context.Context, repo, ref, apiOrigin string) (resolvedRef, error) {
	resolved, ok, err := c.resolveViaAPI(ctx, repo, ref, apiOrigin)
	if err != nil {
		if !errors.Is(err, errAPINotFound) {
			c.log.Debug().Err(err).Str("repo", repo).Str("ref", ref).Msg("Git API resolve failed, using ls-remote")
		}
	} else if ok {
		return resolved, nil
	}

	refs, err := c.lister.List(ctx, repo)
	if err != nil {
		return resolvedRef{}, fmt.Errorf("ls-remote %s: %w", repo, err)
	}

	return resolveListedRef(refs, ref)
}

// listTags lists remote tags via the product API, then go-git ls-remote.
//
// Parameters:
//   - ctx: Cancellation and timeout.
//   - repo: Clone URL.
//   - apiOrigin: Optional HTTP API base URL from the git-host label.
//
// Returns:
//   - []RemoteRef: Short tag names and commit hashes.
//   - error: Non-nil when listing fails.
func (c *Client) listTags(ctx context.Context, repo, apiOrigin string) ([]RemoteRef, error) {
	tags, ok, err := c.listTagsViaAPI(ctx, repo, apiOrigin)
	if err != nil {
		c.log.Debug().Err(err).Str("repo", repo).Msg("Git API list tags failed, using ls-remote")
	} else if ok {
		return tags, nil
	}

	refs, err := c.lister.List(ctx, repo)
	if err != nil {
		return nil, fmt.Errorf("ls-remote tags %s: %w", repo, err)
	}

	byName := collectTagHashes(refs)

	listed := make([]RemoteRef, 0, len(byName))
	for name, hash := range byName {
		listed = append(listed, RemoteRef{Name: name, Hash: hash})
	}

	return listed, nil
}

// tagName returns the resolved name when the ref is a tag.
//
// Parameters:
//   - resolved: Resolved branch or tag.
//
// Returns:
//   - string: Tag name, or empty for a branch.
func tagName(resolved resolvedRef) string {
	if resolved.Kind == kindTag {
		return resolved.Name
	}

	return ""
}

// withTimeout returns ctx with the Git timeout unless a tighter deadline exists.
//
// Parameters:
//   - ctx: Parent context.
//
// Returns:
//   - context.Context: Context that expires at the Git timeout.
//   - context.CancelFunc: Cancel function. A no-op when ctx already expires sooner.
func (c *Client) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := c.opts.Timeout
	if timeout <= 0 {
		timeout = defaultGitTimeout
	}

	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= timeout {
		return ctx, func() {}
	}

	return context.WithTimeout(ctx, timeout)
}

// cmpOr returns value, or fallback when value is empty.
//
// Parameters:
//   - value: Preferred string.
//   - fallback: Used when value is empty.
//
// Returns:
//   - string: Non-empty preference.
func cmpOr(value, fallback string) string {
	if value == "" {
		return fallback
	}

	return value
}

// CheckContainer runs Check using association and stamp labels from the container.
//
// Parameters:
//   - ctx: Cancellation and timeout.
//   - client: Git monitor client.
//   - c: Container to inspect.
//   - params: Update parameters with mappings and defaults.
//
// Returns:
//   - CheckResult: Empty result when the container is not associated.
//   - error: Non-nil on monitor failure.
func CheckContainer(
	ctx context.Context,
	client *Client,
	c types.Container,
	params types.UpdateParams,
) (CheckResult, error) {
	assoc, ok := gitPkg.ResolveAssociation(c, params)
	if !ok {
		return CheckResult{}, nil
	}

	if raw := strings.TrimSpace(policyLabelValue(c)); raw != "" && !types.ValidGitPolicy(strings.ToLower(raw)) {
		return CheckResult{}, fmt.Errorf("%w: %s", ErrInvalidPolicy, raw)
	}

	if raw := strings.TrimSpace(assoc.Host); raw != "" {
		_, err := gitPkg.ParseAPIOrigin(raw)
		if err != nil {
			return CheckResult{}, fmt.Errorf("%w: %s", ErrInvalidHost, raw)
		}
	}

	lastCommit, lastTag := gitPkg.Baseline(c)
	if lastCommit == "" && lastTag == "" {
		lastCommit, lastTag = client.recall(assoc.Repo, assoc.Ref, assoc.Policy)
	}

	result, err := client.Check(ctx, CheckRequest{
		Repo:       assoc.Repo,
		Ref:        assoc.Ref,
		Policy:     assoc.Policy,
		Host:       assoc.Host,
		LastCommit: lastCommit,
		LastTag:    lastTag,
	})
	if err != nil {
		return result, err
	}

	// Record an observed tip only when this session did not rebuild. A stale
	// result must keep the previous baseline so a failed apply can retry.
	if !result.Stale {
		client.remember(assoc.Repo, assoc.Ref, assoc.Policy, result.Commit, result.Tag)
	}

	return result, nil
}

// recall returns a remote tip observed earlier in this process.
//
// Parameters:
//   - repo: Associated clone URL.
//   - ref: Watched branch or tag.
//   - policy: Update policy used for the watch key.
//
// Returns:
//   - string: Last observed commit SHA, or empty.
//   - string: Last observed tag, or empty.
func (c *Client) recall(repo, ref, policy string) (string, string) {
	if c == nil {
		return "", ""
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	seen, ok := c.seen[seenKey(repo, ref, policy)]
	if !ok {
		return "", ""
	}

	return seen.commit, seen.tag
}

// remember stores a remote tip observed without rebuilding.
//
// Parameters:
//   - repo: Associated clone URL.
//   - ref: Watched branch or tag.
//   - policy: Update policy used for the watch key.
//   - commit: Remote commit SHA to remember.
//   - tag: Remote tag to remember, or empty.
func (c *Client) remember(repo, ref, policy, commit, tag string) {
	if c == nil || (commit == "" && tag == "") {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.seen == nil {
		c.seen = make(map[string]seenStamp)
	}

	c.seen[seenKey(repo, ref, policy)] = seenStamp{commit: commit, tag: tag}
}

// seenKey identifies one watched repo, ref, and policy in process memory.
//
// Parameters:
//   - repo: Associated clone URL.
//   - ref: Watched branch or tag.
//   - policy: Update policy.
//
// Returns:
//   - string: Map key unique to that watch target.
func seenKey(repo, ref, policy string) string {
	return repo + "\n" + ref + "\n" + policy
}

// sameRevision reports whether observed names the same Git object as remote.
//
// A hex prefix of at least 7 characters matches a longer SHA so git-<shortsha>
// image tags compare equal to the full remote tip.
//
// Parameters:
//   - observed: Known running revision.
//   - remote: Resolved remote tip.
//
// Returns:
//   - bool: True when both name the same object.
func sameRevision(observed, remote string) bool {
	observed = strings.ToLower(strings.TrimSpace(observed))

	remote = strings.ToLower(strings.TrimSpace(remote))
	if observed == "" || remote == "" {
		return false
	}

	if observed == remote {
		return true
	}

	if !isHexSHA(observed) || !isHexSHA(remote) {
		return false
	}

	const minRevisionPrefix = 7

	if len(observed) < minRevisionPrefix || len(remote) < minRevisionPrefix {
		return false
	}

	if len(observed) < len(remote) {
		return strings.HasPrefix(remote, observed)
	}

	return strings.HasPrefix(observed, remote)
}

// isHexSHA reports whether s is a non-empty hexadecimal string.
//
// Parameters:
//   - s: Candidate SHA or prefix.
//
// Returns:
//   - bool: True when s is only hexadecimal digits.
func isHexSHA(s string) bool {
	if s == "" {
		return false
	}

	for _, r := range s {
		if !unicode.Is(unicode.ASCII_Hex_Digit, r) {
			return false
		}
	}

	return true
}

// policyLabelValue returns the raw git-semver-policy label, or empty.
//
// Parameters:
//   - c: Container to inspect.
//
// Returns:
//   - string: Raw label value, or empty when unset.
func policyLabelValue(c types.Container) string {
	if c == nil {
		return ""
	}

	val, ok := c.GetLabel(gitPkg.SemverPolicyLabel)
	if !ok {
		return ""
	}

	return val
}
