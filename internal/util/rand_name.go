package util

import (
	"crypto/rand"
	"math/big"
)

// randomNameLength sets the length of random container names (32).
const randomNameLength = 32

// letters defines the character set for random names.
var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

// RandName generates a 32-character random container name.
//
// Returns:
//   - string: Random Docker-compatible name.
func RandName() string {
	nameBuffer := make([]rune, randomNameLength)
	for i := range nameBuffer {
		// Use crypto/rand for secure randomness.
		index, _ := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		nameBuffer[i] = letters[index.Int64()]
	}

	return string(nameBuffer)
}
