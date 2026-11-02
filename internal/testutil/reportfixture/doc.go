// Package reportfixture builds session reports for tests.
//
// Each report holds containers with stable IDs, image IDs and names derived
// from their state, so tests can assert rendered notification output. The
// package imports the container model and the update state, so tests of those
// packages build their reports locally.
package reportfixture
