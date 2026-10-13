// Package settings holds the small set of Watchtower settings that can be
// changed while the process is running, for example from the web UI.
//
// Flags and environment variables still decide the defaults (see
// internal/config). A Store layers optional overrides on top of them:
//
//   - Effective returns the values in force, defaults with overrides applied.
//   - Update changes or clears overrides after validating the result.
//   - ApplyTo projects the effective values onto a types.UpdateParams, so
//     every update path picks them up at run time.
//
// Overrides are written to settings.json in the data directory when one is
// available. Without a writable data directory the store still works, but
// changes last only until the process exits.
//
// Only behavior that is safe to change mid-flight belongs here. Credentials,
// the HTTP API token, the listen address, and the Docker connection stay in
// flags and environment variables.
package settings
