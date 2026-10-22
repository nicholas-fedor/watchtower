// Package auth authenticates requests to container registries through the
// registry HTTP API V2 challenge protocol.
//
// It answers a registry's challenge with basic credentials or with a bearer
// token fetched from the challenge's realm, and caches bearer tokens for reuse.
// Anonymous GitHub Container Registry tokens are shared across public images,
// and lscr.io images authenticate against ghcr.io, which hosts them.
//
// It also finds the credentials for an image's registry in the Docker config
// file or the REPO_USER and REPO_PASS environment variables.
package auth
