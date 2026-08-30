package infra

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/nicholas-fedor/watchtower/testing/e2e/store"
	"github.com/nicholas-fedor/watchtower/testing/e2e/stream"
)

const (
	// DefaultDatabaseURL is the compose Postgres DSN bound to loopback.
	DefaultDatabaseURL = "postgres://e2e:e2e@127.0.0.1:5432/e2e?sslmode=disable"
	// DefaultLokiURL is the compose Loki HTTP origin bound to loopback.
	DefaultLokiURL = "http://127.0.0.1:3100"
	// DefaultListen is the control-plane bind address.
	DefaultListen = "127.0.0.1:9472"
	// readyWait is how long Ensure waits for health.
	readyWait = 45 * time.Second
	// readyTick is the health poll interval while compose is starting.
	readyTick = 500 * time.Millisecond
)

// ErrSidecarsUnhealthy means compose came up but health checks did not.
var ErrSidecarsUnhealthy = errors.New("control-plane sidecars not healthy")

// Env holds connection strings for the control plane.
type Env struct {
	// DatabaseURL is the Postgres DSN.
	DatabaseURL string
	// LokiURL is the Loki HTTP origin.
	LokiURL string
	// Listen is host:port for Fiber.
	Listen string
	// Token is an optional bearer token. Empty means unauthenticated loopback.
	Token string
}

// FromEnv reads WATCHTOWER_E2E_* variables with compose defaults.
//
// Returns:
//   - Env: Connection settings.
func FromEnv() Env {
	return Env{
		DatabaseURL: cmp.Or(os.Getenv("WATCHTOWER_E2E_DATABASE_URL"), DefaultDatabaseURL),
		LokiURL:     cmp.Or(os.Getenv("WATCHTOWER_E2E_LOKI_URL"), DefaultLokiURL),
		Listen:      cmp.Or(os.Getenv("WATCHTOWER_E2E_LISTEN"), DefaultListen),
		Token:       os.Getenv("WATCHTOWER_E2E_TOKEN"),
	}
}

// EnsureCompose runs `docker compose up -d` if Postgres or Loki are not ready.
//
// Parameters:
//   - ctx: Cancellation.
//   - moduleRoot: testing/e2e directory containing compose.yaml.
//   - env: Target URLs.
//
// Returns:
//   - error: Compose or health failure.
func EnsureCompose(ctx context.Context, moduleRoot string, env Env) error {
	if pingPostgres(ctx, env.DatabaseURL) == nil && pingLoki(ctx, env.LokiURL) == nil {
		return nil
	}

	compose := filepath.Join(moduleRoot, "compose.yaml")

	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", compose, "up", "-d")
	cmd.Dir = moduleRoot

	raw, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose up: %w: %s", err, raw)
	}

	deadline := time.Now().Add(readyWait)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if pingPostgres(ctx, env.DatabaseURL) == nil && pingLoki(ctx, env.LokiURL) == nil {
			return nil
		}

		timer := time.NewTimer(readyTick)
		select {
		case <-ctx.Done():
			timer.Stop()

			return ctx.Err()
		case <-timer.C:
		}
	}

	return fmt.Errorf("%w after %s", ErrSidecarsUnhealthy, readyWait)
}

// pingPostgres opens and closes a Postgres pool to prove readiness.
//
// Parameters:
//   - ctx: Cancellation.
//   - dsn: Postgres URL.
//
// Returns:
//   - error: Connect or ping failure.
func pingPostgres(ctx context.Context, dsn string) error {
	return store.Ping(ctx, dsn)
}

// pingLoki hits Loki /ready.
//
// Parameters:
//   - ctx: Cancellation.
//   - base: Loki HTTP origin.
//
// Returns:
//   - error: Ready-check failure.
func pingLoki(ctx context.Context, base string) error {
	return stream.OpenLoki(base).Ready(ctx)
}
