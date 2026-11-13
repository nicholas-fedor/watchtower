// Package recreate creates the replacement for a container that Watchtower
// updates.
//
// It creates the replacement from the source container's configuration and
// then attaches its networks. It also resolves volumes-from references to
// container names.
package recreate
