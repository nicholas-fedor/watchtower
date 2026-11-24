package schedule

// Schedule holds when periodic updates run.
type Schedule struct {
	// Spec is the cron schedule expression (may be derived from IntervalSeconds).
	Spec string
	// IntervalSeconds is the poll interval in seconds when not using a cron expression.
	IntervalSeconds int
	// UpdateOnStart runs an update check immediately on startup.
	UpdateOnStart bool
	// WatchEvents follows Docker's container events to check new containers as they start.
	WatchEvents bool
}
