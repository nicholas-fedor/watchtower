// Package stream stores Watchtower stdout/stderr as labeled log streams.
//
// Loki is the durable backend. Memory is the unit-test adapter. The harness
// is the collector: it follows the inner Engine logs API and pushes. Host
// Docker logging drivers are never configured.
package stream
