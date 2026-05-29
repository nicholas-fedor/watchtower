// Package containers provides the /v1/containers HTTP API endpoint, exposing the
// current image identity (name, local ID, and registry manifest digest) of each
// container Watchtower watches.
//
// This lets an external orchestrator compare what each container is actually
// running against a registry's current digest without pulling any image layers.
//
// Key components:
//   - Handler: Serves the /v1/containers endpoint with container status information.
//   - Status: Data model for container image identity (name, image, image ID, digest).
//   - New: Creates a handler with a list function for fetching container statuses.
//
// Usage example:
//
//	handler := containers.New(listFunc)
//	http.HandleFunc("GET "+handler.Path, handler.Handle)
//
// The package returns JSON responses with container array, count, timestamp,
// and API version.
package containers
