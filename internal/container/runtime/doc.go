// Package runtime detects whether Watchtower runs on Docker or Podman.
//
// It checks the runtime marker files and the environment before it asks the
// daemon. Podman needs different handling of some container settings when a
// container is recreated.
package runtime
