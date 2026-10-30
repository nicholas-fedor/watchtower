package update

import (
	"context"
	"errors"
	"fmt"

	"github.com/rs/zerolog"

	containerClient "github.com/nicholas-fedor/watchtower/internal/container/client"
	"github.com/nicholas-fedor/watchtower/internal/observability/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

const (
	// msgImageDiskSpaceExceeded is logged and sampled when image usage reaches the maximum.
	msgImageDiskSpaceExceeded = "Docker image usage exceeds configured maximum"

	// msgImageDiskSpaceWarn is logged and sampled when image usage reaches the warning threshold.
	msgImageDiskSpaceWarn = "Docker image usage exceeds configured warning threshold"

	// msgImageDiskSpaceOK is logged when image usage is below all configured thresholds.
	msgImageDiskSpaceOK = "Docker image usage within configured budget"
)

// Errors for Docker image-usage budget checks.
var (
	// errImageDiskUsageFailed indicates GET /system/df for images failed.
	errImageDiskUsageFailed = errors.New("failed to query Docker image disk usage")
	// errImageDiskSpaceExceeded indicates image usage reached the configured maximum.
	errImageDiskSpaceExceeded = errors.New("docker image usage exceeds configured maximum")
)

// checkImageDiskSpace enforces the optional Docker image-usage budget.
//
// When both thresholds are unset the check is skipped. Otherwise it queries
// image usage once, updates Prometheus gauges, warns when usage reaches the
// warning threshold, and returns errImageDiskSpaceExceeded when usage reaches
// the maximum. A daemon query failure is fail-closed. Both failures are logged
// at error here, so the session's own failure line for the returned error is
// kept out of notifications.
//
// Parameters:
//   - log: Session logger.
//   - ctx: Context for cancellation and timeout control.
//   - client: Docker client used to query image usage.
//   - config: Update policy containing disk-space thresholds in bytes.
//
// Returns:
//   - error: Non-nil when the query fails or usage reaches the maximum.
func checkImageDiskSpace(
	log *zerolog.Logger,
	ctx context.Context,
	client containerClient.Client,
	config types.UpdateParams,
) error {
	if config.DiskSpaceMax == 0 && config.DiskSpaceWarn == 0 {
		return nil
	}

	usage, err := client.GetImageDiskUsage(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to query Docker image disk usage")

		return reportedError{err: fmt.Errorf("%w: %w", errImageDiskUsageFailed, err)}
	}

	metrics.RecordImageDiskUsage(
		usage.TotalSize,
		usage.Reclaimable,
		config.DiskSpaceMax,
		config.DiskSpaceWarn,
	)

	clogVal := log.With().
		Int64("usage", usage.TotalSize).
		Int64("max", config.DiskSpaceMax).
		Int64("warn", config.DiskSpaceWarn).
		Int64("reclaimable", usage.Reclaimable).
		Int64("image_count", usage.TotalCount).
		Logger()
	clog := &clogVal

	if config.DiskSpaceMax > 0 && usage.TotalSize >= config.DiskSpaceMax {
		clog.Error().Msg(msgImageDiskSpaceExceeded)

		return reportedError{err: fmt.Errorf(
			"%w: usage %d bytes, max %d bytes",
			errImageDiskSpaceExceeded,
			usage.TotalSize,
			config.DiskSpaceMax,
		)}
	}

	if config.DiskSpaceWarn > 0 && usage.TotalSize >= config.DiskSpaceWarn {
		clog.Warn().Msg(msgImageDiskSpaceWarn)

		return nil
	}

	clog.Debug().Msg(msgImageDiskSpaceOK)

	return nil
}
