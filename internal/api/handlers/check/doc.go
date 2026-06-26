// Package check provides the /v1/check endpoint for read-only update
// availability checks.
//
// It exposes POST /v1/check which iterates watched containers and reports
// whether updates are available, without pulling images or restarting
// containers.
//
// Endpoints:
//
//	POST /v1/check    Check for available container updates (requires auth)
package check
