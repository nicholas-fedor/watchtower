// Package helpers provides utility functions for registry-related operations in Watchtower.
// It includes methods for parsing registry addresses and normalizing digests.
package helpers

import (
	"errors"
	"fmt"
	"strings"

	"github.com/distribution/reference"
	"github.com/sirupsen/logrus"
)

// Domains for Docker Hub, the default registry.
const (
	// Canonical domain for Docker Hub.
	DockerRegistryDomain = "docker.io"
	// Canonical host address for Docker Hub.
	DockerRegistryHost = "index.docker.io"
)

// Errors for helper operations.
var (
	// errFailedParseImageReference indicates a failure to parse an image reference into a normalized form.
	errFailedParseImageReference = errors.New("failed to parse image reference")
)

// GetRegistryAddress extracts the registry address from an image reference.
//
// It returns the domain part of the reference, mapping Docker Hub’s default domain to its canonical host if needed.
//
// Parameters:
//   - imageRef: Image reference string (e.g., "docker.io/library/alpine").
//
// Returns:
//   - string: Registry address (e.g., "index.docker.io") if successful.
//   - error: Non-nil if parsing fails, nil on success.
func GetRegistryAddress(imageRef string) (string, error) {
	// Parse the image reference into a normalized form for consistent domain extraction.
	normalizedRef, err := reference.ParseNormalizedNamed(imageRef)
	if err != nil {
		logrus.WithError(err).
			WithField("image_ref", imageRef).
			Debug("Failed to parse image reference")

		return "", fmt.Errorf("%w: %w", errFailedParseImageReference, err)
	}

	// Extract the domain from the normalized reference.
	address := reference.Domain(normalizedRef)

	// Map Docker Hub’s default domain to its canonical host for registry requests.
	if address == DockerRegistryDomain {
		logrus.WithFields(logrus.Fields{
			"image_ref": imageRef,
			"address":   address,
		}).Debug("Mapped Docker Hub domain to canonical host")

		address = DockerRegistryHost
	}

	logrus.WithFields(logrus.Fields{
		"image_ref": imageRef,
		"address":   address,
	}).Debug("Extracted registry address")

	return address, nil
}

// NormalizeDigest standardizes a digest string for consistent comparison.
//
// It trims common prefixes (e.g., "sha256:") to return the raw digest value.
//
// Parameters:
//   - digest: Digest string (e.g., "sha256:abc123").
//
// Returns:
//   - string: Normalized digest (e.g., "abc123").
func NormalizeDigest(digest string) string {
	// List of prefixes to strip from the digest.
	prefixes := []string{"sha256:"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(digest, prefix) {
			// Trim the prefix to get the raw digest value.
			normalized := strings.TrimPrefix(digest, prefix)
			logrus.WithFields(logrus.Fields{
				"original":   digest,
				"normalized": normalized,
			}).Debug("Normalized digest by trimming prefix")

			return normalized
		}
	}

	// Return unchanged if no prefix matches.
	return digest
}
