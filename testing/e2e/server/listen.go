package server

import (
	"cmp"
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/nicholas-fedor/watchtower/testing/e2e/api"
	"github.com/nicholas-fedor/watchtower/testing/e2e/control"
	"github.com/nicholas-fedor/watchtower/testing/e2e/docker"
	"github.com/nicholas-fedor/watchtower/testing/e2e/infra"
	"github.com/nicholas-fedor/watchtower/testing/e2e/run"
	"github.com/nicholas-fedor/watchtower/testing/e2e/store"
	"github.com/nicholas-fedor/watchtower/testing/e2e/stream"
	"github.com/nicholas-fedor/watchtower/testing/e2e/web"
)

// Listen starts Postgres, Loki, the JSON API, dashboard, and dispatcher, then blocks.
//
// The TCP bind happens first so a second process cannot InterruptRunning the
// sitting owned by the process that already holds the port.
//
// Parameters:
//   - ctx: Cancellation. Listen returns nil on cancel after shutdown.
//   - listen: Bind address. Empty uses env/default.
//   - token: Optional bearer token.
//
// Returns:
//   - error: Startup, listen, or shutdown failure.
func Listen(ctx context.Context, listen, token string) error {
	env := infra.FromEnv()
	env.Listen = cmp.Or(listen, env.Listen)
	env.Token = cmp.Or(token, env.Token)

	ln, listenErr := net.Listen("tcp", env.Listen)
	if listenErr != nil {
		return fmt.Errorf("listen %s: %w", env.Listen, listenErr)
	}
	defer ln.Close()

	moduleRoot, rootErr := docker.ModuleRoot()
	if rootErr != nil {
		return rootErr
	}

	ensureErr := infra.EnsureCompose(ctx, moduleRoot, env)
	if ensureErr != nil {
		return ensureErr
	}

	records, storeErr := store.OpenPostgres(ctx, env.DatabaseURL)
	if storeErr != nil {
		return storeErr
	}
	defer records.Close()

	logs := stream.OpenLoki(env.LokiURL)
	defer logs.Close()

	readyErr := logs.Ready(ctx)
	if readyErr != nil {
		return readyErr
	}

	svc := control.New(records, logs, func(ctx context.Context, svc *control.Service, sitting store.Run) error {
		return run.ExecuteStored(ctx, svc, sitting)
	})
	if interruptErr := svc.InterruptRunning(ctx); interruptErr != nil {
		return interruptErr
	}

	app, _ := api.NewApp(svc, env.Token)
	web.Mount(app, svc)

	loopCtx, loopCancel := context.WithCancel(ctx)
	defer loopCancel()

	var wg sync.WaitGroup
	wg.Go(func() {
		svc.Loop(loopCtx)
	})
	defer wg.Wait()

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Listener(ln)
	}()

	select {
	case <-ctx.Done():
		return finishListen(svc, app, loopCancel, errCh)
	case err := <-errCh:
		loopCancel()
		svc.RequestStop()
		_ = shutdownApp(app, shutdownHTTP)

		return err
	}
}

// finishListen stops the dispatcher, ends SSE, and drains HTTP.
//
// Parameters:
//   - svc: Control plane.
//   - app: Fiber application.
//   - loopCancel: Stops Loop.
//   - errCh: Listener result.
//
// Returns:
//   - error: Always nil. HTTP shutdown errors are ignored so SIGINT exits 0.
func finishListen(svc *control.Service, app *fiber.App, loopCancel context.CancelFunc, errCh <-chan error) error {
	loopCancel()
	svc.RequestStop()
	_ = shutdownApp(app, shutdownHTTP)
	select {
	case <-errCh:
	case <-time.After(time.Second):
	}

	return nil
}
