// Package network builds the network settings for a replacement container.
//
// It copies the source container's network endpoints. Values that Docker
// assigned itself, such as the short ID alias and generated MAC addresses, are
// left out so the replacement gets its own.
package network
