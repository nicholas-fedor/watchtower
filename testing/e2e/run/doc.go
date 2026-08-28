// Package run executes one e2e Case against a DinD worker.
//
// The pipeline publishes dummy images, creates topology, starts Watchtower,
// waits, then evaluates Expect plus always-on invariants (fidelity, secrets,
// decoy prefix, artifacts).
package run
