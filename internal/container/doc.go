// Package container models the Docker containers that Watchtower watches.
//
// A Container holds the inspect data of a container and its image. It reads
// the Watchtower labels that control how the container is updated, and it finds
// the containers it depends on. It also builds the configuration that recreates
// the container.
package container
