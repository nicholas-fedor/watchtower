package run

import (
	"time"

	"github.com/nicholas-fedor/watchtower/testing/e2e/docker"
	"github.com/nicholas-fedor/watchtower/testing/e2e/watchtower"
)

const (
	// decoyName is the unenlisted container suffix when Topology.Decoy is set.
	decoyName = "e2e-decoy"
	// subjectRepo is the local image name before it is pushed to the inner registry.
	subjectRepo = "e2e/app"
	// subjectTag is the dummy subject tag Watchtower pulls.
	subjectTag = "latest"
	// imageGeneration1 is TAG/REV baked into the first published image.
	imageGeneration1 = "r1"
	// imageGeneration2 is TAG/REV baked into the replacement image.
	imageGeneration2 = "r2"
	// permFile is the mode for inspect and porcelain artifacts.
	permFile = 0o600
	// prefixIDLen is how many case-id characters go into the Docker name prefix.
	prefixIDLen = 12
	// apiReadyWait is how long HTTP-API cases wait before probing /v1/containers/details.
	apiReadyWait = 2 * time.Second
	// containerStopSeconds is the Docker stop timeout after an HTTP-API probe.
	containerStopSeconds = 5
)

// Options controls one sitting's execution environment.
type Options struct {
	// Daemon is the acquired DinD worker.
	Daemon *docker.Daemon
	// Artifacts holds the built Watchtower, subject, and persona binaries.
	Artifacts watchtower.Artifacts
	// RunDir is artifacts/<run-id>.
	RunDir string
	// Keep retains passing case directories.
	Keep bool
}
