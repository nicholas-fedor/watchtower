// Package assert evaluates black-box Watchtower outcomes from inspect, logs, and HTTP.
//
// Fidelity diffs inspect-before versus inspect-after after a successful update.
// Porcelain checks the session JSON field set. Secrets fail the case when tokens
// leak into logs, porcelain, config, or events.
package assert
