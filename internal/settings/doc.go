// Package settings declares Watchtower's command-line flags and resolves them
// into a single immutable Config.
//
// Each settings domain declares its flags in its own subpackage, and values
// resolve as flag, then environment variable, then default. Before the
// configuration loads, the package applies flag aliases and reads secrets from
// files. It also configures logging and exports the Docker client settings to
// the environment.
//
// The resolved settings are grouped by domain in subpackages. Config projects
// them into the update parameters and Docker client options of the run-once,
// scheduled, and HTTP API update paths, so every path works from the same
// resolved settings.
package settings
