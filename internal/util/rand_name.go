// Package util provides utility functions for Watchtower operations.
package util

import (
	"crypto/rand"
	"math/big"
)

// randomNameLength defines the length of randomly generated container names.
// It ensures a consistent, Docker-compatible 32-character name.
const randomNameLength = 32

// letters defines the character set for random container names.
// It includes all alphabetic characters, both uppercase and lowercase.
var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

// RandName generates a random, 32-character, Docker-compatible container name.
// It uses a cryptographically secure random number generator for enhanced security.
func RandName() string {
	nameBuffer := make([]rune, randomNameLength)
	for i := range nameBuffer {
		// Use crypto/rand for secure random index selection.
		index, _ := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		nameBuffer[i] = letters[index.Int64()]
	}

	return string(nameBuffer)
}
