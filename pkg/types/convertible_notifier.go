package types

import (
	"time"

	"github.com/spf13/cobra"
)

// ConvertibleNotifier defines a notifier that generates a shoutrrr URL.
type ConvertibleNotifier interface {
	// GetURL creates a shoutrrr URL from configuration.
	//
	// Parameters:
	//   - c: Cobra command with flags.
	//
	// Returns:
	//   - string: Generated URL.
	//   - error: Non-nil if URL creation fails, nil on success.
	GetURL(c *cobra.Command) (string, error)
}

// DelayNotifier defines a notifier with a delay before sending.
type DelayNotifier interface {
	// GetDelay returns the delay duration for notifications.
	//
	// Returns:
	//   - time.Duration: Delay before sending.
	GetDelay() time.Duration
}
