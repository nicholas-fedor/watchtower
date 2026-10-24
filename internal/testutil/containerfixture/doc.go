// Package containerfixture builds Docker inspect data for container tests.
//
// It builds the data only, so the tests of any package can turn it into that
// package's container value without an import cycle.
package containerfixture
