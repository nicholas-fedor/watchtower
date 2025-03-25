// Package helpers provides utility functions for registry-related operations in Watchtower.
// It includes methods for parsing registry addresses and normalizing digests.
package helpers

import (
	"fmt"
	"strings"

	"github.com/distribution/reference"
)

// Domains for Docker Hub, the default registry.
const (
	DefaultRegistryDomain       = "docker.io"
	DefaultRegistryHost         = "index.docker.io"
	LegacyDefaultRegistryDomain = "index.docker.io"
)

// GetRegistryAddress extracts the registry address from an image reference.
// It returns the domain part of the reference, mapping Docker Hub’s default domain
// to its canonical host address if applicable.
func GetRegistryAddress(imageRef string) (string, error) {
	normalizedRef, err := reference.ParseNormalizedNamed(imageRef)
	if err != nil {
		return "", fmt.Errorf("failed to parse image reference: %w", err)
	}

	address := reference.Domain(normalizedRef)
	if address == DefaultRegistryDomain {
		address = DefaultRegistryHost
	}

	return address, nil
}

// NormalizeDigest standardizes a digest string for consistent comparison.
// It trims common prefixes (e.g., "sha256:") to return the raw digest value,
// ensuring compatibility across different registry formats.
func NormalizeDigest(digest string) string {
	prefixes := []string{"sha256:"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(digest, prefix) {
			return strings.TrimPrefix(digest, prefix)
		}
	}

	return digest
}
