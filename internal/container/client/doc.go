// Package client manages the Docker containers that Watchtower watches through
// the Docker API.
//
// It lists and inspects containers and recreates them with their
// configuration. It also runs the commands that lifecycle hooks define inside
// containers.
package client
