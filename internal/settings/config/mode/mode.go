package mode

// Mode holds how the process enters its main loop.
type Mode struct {
	// Porcelain is the porcelain output version, or empty when disabled.
	Porcelain string
	// RunOnce performs one update cycle and exits.
	RunOnce bool
	// HealthCheck runs a health check and exits.
	HealthCheck bool
	// SelfUpdateOrchestrator runs the ephemeral self-update orchestrator path.
	SelfUpdateOrchestrator bool
	// NoStartupMessage suppresses the startup notification.
	NoStartupMessage bool
}
