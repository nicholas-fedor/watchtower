// Package rebuild updates containers whose source is a Git repository.
//
// It checks the repository of each Git-associated container for a newer commit
// or tag. A stale container gets a new image built from its Git source, or its
// Compose project is checked out from Git and applied. The package records
// which containers it rebuilt or recreated, so the update session leaves them
// out of its own recreation.
package rebuild
