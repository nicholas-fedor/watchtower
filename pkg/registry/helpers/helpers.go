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
	DefaultRegistryDomain       = "docker.io"
	DefaultRegistryHost         = "index.docker.io"
	LegacyDefaultRegistryDomain = "index.docker.io"
)

// Errors for helper operations.
var (
	// errFailedParseImageReference indicates a failure to parse an image reference into a normalized form.
	errFailedParseImageReference = errors.New("failed to parse image reference")
)

// GetRegistryAddress extracts the registry address from an image reference.
// It returns the domain part of the reference, mapping Docker Hub’s default domain
// to its canonical host address if applicable.
func GetRegistryAddress(imageRef string) (string, error) {
	normalizedRef, err := reference.ParseNormalizedNamed(imageRef)
	if err != nil {
		logrus.WithError(err).
			WithField("image_ref", imageRef).
			Debug("Failed to parse image reference")

		return "", fmt.Errorf("%w: %w", errFailedParseImageReference, err)
	}

	address := reference.Domain(normalizedRef)
	if address == DefaultRegistryDomain {
		logrus.WithFields(logrus.Fields{
			"image_ref": imageRef,
			"address":   address,
		}).Debug("Mapped Docker Hub domain to canonical host")

		address = DefaultRegistryHost
	}

	logrus.WithFields(logrus.Fields{
		"image_ref": imageRef,
		"address":   address,
	}).Debug("Extracted registry address")

	return address, nil
}

// NormalizeDigest standardizes a digest string for consistent comparison.
// It trims common prefixes (e.g., "sha256:") to return the raw digest value,
// ensuring compatibility across different registry formats.
func NormalizeDigest(digest string) string {
	prefixes := []string{"sha256:"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(digest, prefix) {
			normalized := strings.TrimPrefix(digest, prefix)
			logrus.WithFields(logrus.Fields{
				"original":   digest,
				"normalized": normalized,
			}).Debug("Normalized digest by trimming prefix")

			return normalized
		}
	}

	return digest
}
