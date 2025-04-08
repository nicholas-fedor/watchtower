// Package util provides utility functions for Watchtower operations.
// It includes tools for slice and map manipulation, random name generation, and SHA-256 hashing.
//
// Key components:
//   - SliceEqual: Compares string slices for equality.
//   - SliceSubtract: Removes elements from string slices.
//   - StringMapSubtract: Removes matching key-value pairs from string maps.
//   - StructMapSubtract: Removes matching keys from struct maps.
//   - RandName: Generates random 32-character container names.
//   - GenerateRandomSHA256: Creates random 64-character SHA-256 hashes.
//
// Usage example:
//
//	equal := util.SliceEqual(slice1, slice2)
//	name := util.RandName()
//	hash := util.GenerateRandomPrefixedSHA256()
//
// The package uses crypto/rand for secure random generation and integrates with
// container and actions packages for configuration and naming tasks.
package util
