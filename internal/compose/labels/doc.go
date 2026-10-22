// Package labels reads the Docker Compose labels Watchtower relies on.
//
// Compose labels supply the service identities used for dependency ordering.
// The image label is mapped to the new image when a container is recreated, in
// the form Compose recorded it.
package labels
