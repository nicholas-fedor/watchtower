// Package hosts names the container registry domains Watchtower treats specially.
//
// Keep vanity-to-canonical mappings here so auth, digest, and rate limiting
// share one source of truth. [LSCRRegistryDomain] is hosted on
// [GitHubRegistryDomain]. [DockerRegistryDomain] is served at
// [DockerRegistryHost].
package hosts
