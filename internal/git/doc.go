// Package git monitors associated containers against hosted Git remotes.
//
// A Client is the process-wide watcher. Check compares a branch SHA or a
// semver tag policy (patch, minor, or major) to the known running revision
// (stamp labels, then image OCI revision / git-<shortsha> tag, then a tip
// observed earlier in this process) and reports whether the remote has
// advanced. Missing refs are errors so a single container can be skipped
// without aborting the session. No known running revision is not stale.
// Watchtower records the current remote tip and does not rebuild only to
// write a stamp.
//
// Refs are resolved through the GitHub, GitLab, or Gitea HTTP API when the
// host is known, then ls-remote. A container git-host label supplies a
// self-hosted HTTP API origin. Unclassified hosts stay on ls-remote.
// Credentials live on Options and are never copied onto types.UpdateParams.
//
// CheckContainer builds a CheckRequest from container association labels and
// --git-image mappings. Clone materializes a worktree at the resolved
// revision. The caller deletes that directory.
//
// Association labels, stamps, and changelog URLs are defined in
// pkg/container/git. OCI image annotations in pkg/container/oci never
// associate a container. After Check reports a stale ref, a Git URL context
// is built by package apply. A local path context is a Compose project
// directory checked out by package project and applied by internal/compose.
package git
