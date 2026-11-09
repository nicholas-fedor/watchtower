package percontainer

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/filters"
)

// Client is the part of Watchtower's container client container schedules
// use: listing the containers. A client that also offers Docker's event
// stream ([EventStream]) can be followed.
type Client interface {
	ListContainers(ctx context.Context, filter ...filters.Filter) ([]*container.Container, error)
}

// FromClient returns the source of containers' own schedules when enabled
// (--container-schedules): Docker's container events, through the client. It
// returns nil (every container on the global schedule) when the option is off
// or the client cannot stream events.
func FromClient(enabled bool, client Client, filter filters.Filter, log *zerolog.Logger) Source {
	if !enabled {
		return nil
	}

	stream, ok := client.(EventStream)
	if !ok {
		log.Warn().Msg("Container schedules need Docker's event stream, which this client lacks: " +
			"every container follows the global schedule")

		return nil
	}

	log.Info().Msg("Containers with a schedule label follow their own schedule")

	return &DockerSource{
		Events: stream,
		List: func(ctx context.Context) ([]filters.FilterableContainer, error) {
			containers, err := client.ListContainers(ctx)
			if err != nil {
				return nil, fmt.Errorf("list containers: %w", err)
			}

			found := make([]filters.FilterableContainer, 0, len(containers))
			for _, c := range containers {
				found = append(found, c)
			}

			return found, nil
		},
		Filter: filter,
		Log:    log,
	}
}
