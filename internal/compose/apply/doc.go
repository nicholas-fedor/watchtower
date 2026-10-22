// Package apply applies updated Docker Compose services after a Git checkout.
//
// Services are built and started through the Docker Compose SDK rather than a
// compose binary, and only the stale associated services are applied.
package apply
