// Package logging builds and configures Watchtower's zerolog loggers and picks
// the level for a problem that is reported repeatedly. It also declares the log
// messages that notification templates match by their text.
//
// It holds no logger state. Callers create a logger and pass it to the
// subsystems that use it.
package logging
