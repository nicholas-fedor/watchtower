package docker

import (
	"context"
	"fmt"
	"sync"

	"github.com/nicholas-fedor/watchtower/testing/e2e/engine"
)

// Pool is a fixed-size set of DinD workers.
type Pool struct {
	mu sync.Mutex
	// idle are workers not currently running a case.
	idle []*Daemon
	// all is every worker started by NewPool.
	all []*Daemon
	// waiters are blocked Acquire callers.
	waiters []chan *Daemon
}

// NewPool starts n DinD workers.
//
// Parameters:
//   - ctx: Cancellation.
//   - n: Worker count. Values below 1 become 1.
//   - envelope: Host envelope applied to every DinD.
//
// Returns:
//   - *Pool: Ready pool.
//   - error: If any worker fails to start.
func NewPool(ctx context.Context, size int, envelope engine.Envelope) (*Pool, error) {
	if size < 1 {
		size = 1
	}

	pool := &Pool{
		idle: make([]*Daemon, 0, size),
		all:  make([]*Daemon, 0, size),
	}

	for idx := range size {
		daemon, err := StartDaemon(ctx, envelope)
		if err != nil {
			_ = pool.Close(ctx)

			return nil, fmt.Errorf("worker %d: %w", idx, err)
		}

		pool.idle = append(pool.idle, daemon)
		pool.all = append(pool.all, daemon)
	}

	return pool, nil
}

// Acquire blocks until a worker is free.
//
// Parameters:
//   - ctx: Cancellation.
//
// Returns:
//   - *Daemon: Exclusive worker.
//   - error: Context canceled.
func (p *Pool) Acquire(ctx context.Context) (*Daemon, error) {
	p.mu.Lock()
	if len(p.idle) > 0 {
		daemon := p.idle[len(p.idle)-1]
		p.idle = p.idle[:len(p.idle)-1]
		p.mu.Unlock()

		return daemon, nil
	}

	waiter := make(chan *Daemon, 1)
	p.waiters = append(p.waiters, waiter)
	p.mu.Unlock()

	select {
	case daemon := <-waiter:
		return daemon, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("acquire dind worker: %w", ctx.Err())
	}
}

// Release returns a worker to the pool.
//
// Parameters:
//   - daemon: Previously acquired worker.
func (p *Pool) Release(daemon *Daemon) {
	if daemon == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.waiters) > 0 {
		waiter := p.waiters[0]
		p.waiters = p.waiters[1:]

		waiter <- daemon

		return
	}

	p.idle = append(p.idle, daemon)
}

// Close terminates every worker.
//
// Parameters:
//   - ctx: Cancellation.
//
// Returns:
//   - error: First close error.
func (p *Pool) Close(ctx context.Context) error {
	p.mu.Lock()
	all := append([]*Daemon{}, p.all...)
	p.idle = nil
	p.all = nil
	p.mu.Unlock()

	var firstErr error

	for _, daemon := range all {
		err := daemon.Close(ctx)
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}
