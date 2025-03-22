// Package util provides utility functions for Watchtower operations.
package util

import (
	"bytes"
	"crypto/rand"
	"fmt"
)

// sha256ByteLength defines the number of bytes in a SHA-256 hash.
// It ensures a 32-byte hash, resulting in a 64-character hex string.
const sha256ByteLength = 32

// sha256HexLength defines the length of a SHA-256 hash in hexadecimal.
// It accounts for 64 characters (32 bytes * 2 hex digits per byte).
const sha256HexLength = 64

// GenerateRandomSHA256 generates a random 64-character SHA-256 hash string.
// It produces a hexadecimal representation without a prefix.
func GenerateRandomSHA256() string {
	return GenerateRandomPrefixedSHA256()[7:]
}

// GenerateRandomPrefixedSHA256 generates a random 64-character SHA-256 hash string, prefixed with "sha256:".
// It uses a cryptographically secure random source for the hash bytes.
func GenerateRandomPrefixedSHA256() string {
	hash := make([]byte, sha256ByteLength)
	_, _ = rand.Read(hash)
	hashBuilder := bytes.NewBufferString("sha256:")
	hashBuilder.Grow(sha256HexLength)

	for _, h := range hash {
		_, _ = fmt.Fprintf(hashBuilder, "%02x", h)
	}

	return hashBuilder.String()
}
