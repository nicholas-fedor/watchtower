// Package provider classifies Git repository hosts by the API they serve, so
// Watchtower knows how to look up their releases and changelogs. A host it
// cannot classify is reached through go-git alone. It also decides when two
// spellings of a hostname name the same host.
package provider
