// Package watchtower builds and runs Watchtower as a black-box binary or container.
//
// It never imports github.com/nicholas-fedor/watchtower. Source is located via
// WATCHTOWER_SOURCE (default ../..) or WATCHTOWER_IMAGE. HTTP API probes go
// through the inner daemon so container packaging stays first-class.
package watchtower
