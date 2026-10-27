// Package cleanup removes the Watchtower containers and images that updates
// leave behind.
//
// At startup it stops and removes the other Watchtower instances in the same
// scope, and the orchestrator containers of interrupted ephemeral
// self-updates. During each update session it removes the predecessors that a
// self-update renamed, and it removes the images that updates replaced, once
// per image.
package cleanup
