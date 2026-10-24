// Package provenance describes where a container's image comes from.
//
// It reads the Watchtower Git association labels and the Git stamps on a
// container. It also builds the changelog links and report metadata that
// notifications show.
//
// OCI image annotations are handled by package oci. This package never treats
// org.opencontainers.image.source as a watcher association.
package provenance
