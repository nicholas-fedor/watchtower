package routes

import (
	"context"
	"fmt"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/timeout"

	"github.com/nicholas-fedor/watchtower/internal/api/config"
	"github.com/nicholas-fedor/watchtower/internal/api/handlers/check"
	"github.com/nicholas-fedor/watchtower/internal/api/handlers/events"
	"github.com/nicholas-fedor/watchtower/internal/api/handlers/update"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

func registerCheckRoute(app *fiber.App, auth fiber.Handler, opts config.Options) {
	if opts.Client == nil {
		return
	}

	checkTimeout := opts.CheckTimeout
	if checkTimeout <= 0 {
		checkTimeout = config.DefaultCheckTimeout
	}

	params := config.BuildUpdateParams(opts)
	scanStartedData := events.NewScanStartedData(params)

	handler := check.New(opts.Logger,
		func(ctx context.Context, images, names []string) ([]check.ContainerCheck, error) {
			imageFilter := opts.FilterByImage(images, opts.Filter)
			containerFilter := update.ContainerFilter(names)
			combinedFilter := func(c types.FilterableContainer) bool {
				return imageFilter(c) && containerFilter(c.Name(), true)
			}

			results, err := check.CheckForUpdates(
				opts.Logger,
				ctx,
				opts.Client,
				combinedFilter,
				params,
				opts.GitClient,
			)
			if err != nil {
				return results, fmt.Errorf("check for updates: %w", err)
			}

			recordCheck(opts, results)

			return results, nil
		},
		checkTimeout,
		opts.Notifier,
		opts.NotificationSplitByContainer,
		opts.EventBroadcaster,
		scanStartedData,
	)

	app.Post(
		handler.Path,
		auth,
		timeout.New(
			handler.Handle,
			timeout.Config{
				Timeout: checkTimeout,
			},
		),
	)
}

// recordCheck adds a completed check to the scan history so that checks
// started from the API or the web UI are visible alongside update scans.
func recordCheck(opts config.Options, results []check.ContainerCheck) {
	if opts.DefaultMetrics == nil {
		return
	}

	store := opts.DefaultMetrics()
	if store == nil {
		return
	}

	available, failed := 0, 0

	for _, result := range results {
		switch {
		case result.Error != "":
			failed++
		case result.UpdateAvailable:
			available++
		}
	}

	store.RecordCheck(len(results), available, failed)
}
