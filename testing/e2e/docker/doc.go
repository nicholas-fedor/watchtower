// Package docker manages Testcontainers DinD workers and inner-daemon fixtures.
//
// Isolation is the DinD container (plus Ryuk on outer resources). Watchtower
// --scope is never injected as leak prevention. Inner daemons have no live
// registry egress. Dummy images are FROM scratch or host-loaded tarballs.
package docker
