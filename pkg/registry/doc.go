// Package registry provides functionality for interacting with container registries in Watchtower.
// It handles authentication, digest retrieval, and image pull options for registry operations.
//
// Key components:
//   - auth: Manages registry authentication (token fetching, challenge handling).
//   - digest: Retrieves and compares image digests via HTTP requests.
//   - helpers: Utilities for registry address parsing and digest normalization.
//   - manifest: Constructs manifest URLs for digest fetching.
//   - registry: Configures pull options and API consumption checks.
//
// Usage example:
//
//	opts, err := registry.GetPullOptions("docker.io/library/alpine")
//	if err != nil {
//	    logrus.WithError(err).Error("Failed to get pull options")
//	}
//	digest, err := digest.FetchDigest(ctx, container, opts.RegistryAuth)
//
// The package integrates with Docker’s registry API, supports credential fetching from config files
// or environment variables, and uses logrus for logging operations.
package registry
