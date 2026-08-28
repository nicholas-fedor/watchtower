package docker

import (
	"context"
	"fmt"

	"github.com/nicholas-fedor/watchtower/testing/e2e/engine"
)

// Pair is two DinD workers used as Watchtower plus a remote Docker host.
type Pair struct {
	Watchtower *Daemon
	Remote     *Daemon
}

// StartRemotePair starts two DinD workers for class K remote-host cases.
//
// Parameters:
//   - ctx: Cancellation.
//
// Returns:
//   - *Pair: Watchtower worker and remote daemon worker.
//   - error: Startup failure.
func StartRemotePair(ctx context.Context) (*Pair, error) {
	var unset engine.Envelope

	watchtower, err := StartDaemon(ctx, unset)
	if err != nil {
		return nil, fmt.Errorf("watchtower dind: %w", err)
	}

	remote, remoteErr := StartDaemon(ctx, unset)
	if remoteErr != nil {
		_ = watchtower.Close(ctx)

		return nil, fmt.Errorf("remote dind: %w", remoteErr)
	}

	return &Pair{Watchtower: watchtower, Remote: remote}, nil
}

// Close terminates both workers.
//
// Parameters:
//   - ctx: Cancellation.
//
// Returns:
//   - error: First close error.
func (p *Pair) Close(ctx context.Context) error {
	var first error
	if p.Remote != nil {
		first = p.Remote.Close(ctx)
	}

	if p.Watchtower != nil {
		err := p.Watchtower.Close(ctx)
		if first == nil {
			first = err
		}
	}

	return first
}
