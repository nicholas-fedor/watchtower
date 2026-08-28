package docker

import (
	"context"
	"fmt"

	"github.com/nicholas-fedor/watchtower/testing/e2e/engine"
)

// DaemonInfo is a live DinD ping result for e2e doctor.
type DaemonInfo struct {
	// Version is the inner Docker engine version.
	Version string
	// Host is the inner API URL.
	Host string
}

// ProbeDaemon starts one DinD worker, pings it, and tears it down.
//
// Parameters:
//   - ctx: Cancellation.
//
// Returns:
//   - DaemonInfo: Inner version and host URL.
//   - error: Start or ping failure.
func ProbeDaemon(ctx context.Context) (DaemonInfo, error) {
	var unset engine.Envelope

	daemon, err := StartDaemon(ctx, unset)
	if err != nil {
		return DaemonInfo{}, fmt.Errorf("start dind: %w", err)
	}
	defer daemon.Close(ctx)

	version, verErr := daemon.Version(ctx)
	if verErr != nil {
		return DaemonInfo{}, fmt.Errorf("docker version: %w", verErr)
	}

	return DaemonInfo{Version: version, Host: daemon.Host()}, nil
}
