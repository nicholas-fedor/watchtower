// Package registry implements fake Hub, GHCR, LSCR, and private registry dialects.
//
// The persona proxy terminates those hostnames, speaks auth and 429 the way
// Watchtower already parses, and reverse-proxies blob/manifest traffic to a
// local distribution registry. The e2e module does not import Watchtower
// packages. Wire formats are fixtures copied from public docs and Watchtower
// tests.
package registry
