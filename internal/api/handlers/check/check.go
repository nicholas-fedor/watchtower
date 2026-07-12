package check

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// ContainerCheck holds the update availability result for a single container.
type ContainerCheck struct {
	Name            string    `json:"name"`
	Image           string    `json:"image"`
	ImageID         string    `json:"image_id"`
	Digest          string    `json:"digest"`
	UpdateAvailable bool      `json:"update_available"`
	LatestImageID   string    `json:"latest_image_id"`
	LatestDigest    string    `json:"latest_digest"`
	Error           string    `json:"error,omitempty"`
	Timestamp       time.Time `json:"timestamp"`
}

// CheckFunc performs the update availability check for all watched containers.
type CheckFunc func(ctx context.Context, images, names []string) ([]ContainerCheck, error)

// extractFilterParams parses comma-separated query parameters into a slice.
// Supports both repeated params (?name=a&name=b) and comma-separated values (?name=a,b).
func extractFilterParams(c fiber.Ctx, key string) []string {
	var results []string

	queryArgs := c.Request().URI().QueryArgs()
	values := queryArgs.PeekMulti(key)

	for _, v := range values {
		parts := strings.SplitSeq(string(v), ",")
		for p := range parts {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				results = append(results, trimmed)
			}
		}
	}

	return results
}

// CheckForUpdates checks all watched containers for available image updates.
//
// It queries the registry for the latest digest (HEAD with GET fallback) unless
// no-pull is active for the container. The provided filter determines which
// containers are included. nil or a pass-through filter includes all.
//
// Parameters:
//   - ctx: Context for the Docker API call.
//   - client: Docker client.
//   - filter: Combined container filter (general + image + name constraints).
//   - params: Update parameters (MonitorOnly, NoPull, LabelPrecedence, CooldownDelay).
//
// Returns:
//   - []ContainerCheck: Update availability for each matching container.
//   - error: Non-nil if listing containers fails.
func CheckForUpdates(
	ctx context.Context,
	client container.Client,
	filter types.Filter,
	params types.UpdateParams,
) ([]ContainerCheck, error) {
	containers, err := client.ListContainers(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	results := make([]ContainerCheck, 0, len(containers))
	now := time.Now().UTC()

	for _, c := range containers {
		if filter != nil && !filter(c) {
			continue
		}

		result := ContainerCheck{
			Name:      c.Name(),
			Image:     c.ImageName(),
			ImageID:   string(c.ImageID()),
			Timestamp: now,
		}

		info := c.ImageInfo()
		if info != nil {
			result.Digest = container.ExtractImageDigest(info.RepoDigests, c.ImageName())
		}

		stale, latestID, latestDigest, err := client.IsContainerStale(
			ctx,
			c,
			params,
		)
		if err != nil {
			result.Error = err.Error()

			logrus.WithError(err).WithFields(logrus.Fields{
				"container": c.Name(),
				"image":     c.ImageName(),
			}).Debug("Failed to check container for updates")
		} else {
			result.UpdateAvailable = stale

			if latestDigest != "" {
				result.LatestDigest = latestDigest
			}

			if latestID != "" {
				result.LatestImageID = string(latestID)
			}
		}

		results = append(results, result)
	}

	return results, nil
}
