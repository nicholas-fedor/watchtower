package util

import (
	"bytes"
	"crypto/rand"
	"fmt"
)

// sha256ByteLength sets the byte length of a SHA-256 hash (32).
const sha256ByteLength = 32

// sha256HexLength sets the hex length of a SHA-256 hash (64).
const sha256HexLength = 64

// GenerateRandomSHA256 generates a 64-character SHA-256 hash.
//
// Returns:
//   - string: Random hash without prefix.
func GenerateRandomSHA256() string {
	return GenerateRandomPrefixedSHA256()[7:]
}

// GenerateRandomPrefixedSHA256 generates a prefixed SHA-256 hash.
//
// Returns:
//   - string: Random hash with "sha256:" prefix.
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
