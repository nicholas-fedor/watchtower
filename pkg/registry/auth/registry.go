package auth

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/distribution/reference"
	"github.com/sirupsen/logrus"
)

// ChallengeHeader is the HTTP Header containing challenge instructions.
const ChallengeHeader = "WWW-Authenticate"

const (
	DockerRegistryDomain = "docker.io"       // Primary domain for Docker Hub image references.
	DockerRegistryHost   = "index.docker.io" // Current Docker Hub registry API endpoint.
	GitHubRegistryDomain = "ghcr.io"         // Canonical domain for GitHub Container Registry.
	LSCRRegistryDomain   = "lscr.io"         // LinuxServer's vanity domain - images are hosted on ghcr.io.
)

// Errors for registry operations.
var (
	errFailedParseImageReference = errors.New("failed to parse image reference")
	errInvalidChallengeHeader    = errors.New("challenge header did not include all values needed to construct an auth url")
)

// challengeValues holds the parsed components of a WWW-Authenticate Bearer challenge.
type challengeValues struct {
	realm   string
	service string
	scope   string
}

// parseChallenge parses a Bearer challenge header into its components.
//
// It splits the header into key-value pairs and removes quotes from values.
// The header is expected to start with "bearer " (case-insensitive), where the
// scheme must be followed by whitespace or end of string. Parameter keys are
// matched case-insensitively, but value casing is preserved. Commas inside
// quoted values are not treated as delimiters.
//
// Parameters:
//   - header: The raw WWW-Authenticate header value.
//
// Returns:
//   - challengeValues: Parsed realm, service, and scope values.
func parseChallenge(header string) challengeValues {
	trimmed := strings.TrimSpace(header)
	lowered := strings.ToLower(trimmed)

	var raw string

	switch {
	case lowered == "bearer":
		raw = ""
	case strings.HasPrefix(lowered, "bearer "), strings.HasPrefix(lowered, "bearer\t"):
		raw = trimmed[6:]
	}

	parts := splitQuoted(raw, ',')

	var values challengeValues

	for _, part := range parts {
		trimmedPart := strings.TrimSpace(part)

		key, val, ok := strings.Cut(trimmedPart, "=")
		if ok {
			key = strings.TrimSpace(key)
			val = strings.TrimSpace(val)

			switch strings.ToLower(key) {
			case "realm":
				values.realm = strings.Trim(val, `"`)
			case "service":
				values.service = strings.Trim(val, `"`)
			case "scope":
				values.scope = strings.Trim(val, `"`)
			}
		}
	}

	return values
}

// splitQuoted splits a string by a delimiter rune without splitting on
// delimiters that appear inside double-quoted sections.
//
// Parameters:
//   - input: The string to split.
//   - delim: The delimiter rune to split on.
//
// Returns:
//   - []string: The split segments, with quoted commas preserved.
func splitQuoted(input string, delim rune) []string {
	var (
		parts   []string
		current []rune
	)

	inQuotes := false

	for _, char := range input {
		switch {
		case char == '"':
			inQuotes = !inQuotes

			current = append(current, char)
		case char == delim && !inQuotes:
			parts = append(parts, string(current))

			current = current[:0]
		default:
			current = append(current, char)
		}
	}

	// Preserve trailing empty segment when the string ends with a delimiter.
	if len(current) > 0 || (len(input) > 0 && rune(input[len(input)-1]) == delim) {
		parts = append(parts, string(current))
	}

	return parts
}

// extractChallengeHost extracts the host from a realm URL (e.g., "https://ghcr.io/token" -> "ghcr.io").
//
// It parses the trimmed realm with the URL parser and returns the parsed URL's
// Host for valid http/https URLs. Realms containing queries, fragments, or other
// URL components are handled correctly. Invalid or unsupported realms return an
// empty string and log the failure.
//
// Parameters:
//   - realm: The realm URL from the WWW-Authenticate header.
//   - fields: Logging fields for context.
//
// Returns:
//   - string: The extracted host, or empty if extraction fails.
func extractChallengeHost(realm string, fields logrus.Fields) string {
	realm = strings.TrimSpace(realm)
	logrus.WithFields(fields).
		WithField("trimmed_realm", realm).
		Debug("Trimmed realm for host extraction")

	parsed, err := url.Parse(realm)
	if err != nil || parsed.Host == "" {
		logrus.WithFields(fields).
			WithField("realm", realm).
			Debug("Failed to extract challenge host from realm")

		return ""
	}

	switch parsed.Scheme {
	case "http", "https":
		return parsed.Host
	}

	logrus.WithFields(fields).
		WithField("realm", realm).
		Debug("Failed to extract challenge host from realm")

	return ""
}

// GetRegistryAddress extracts the registry address from an image reference.
//
// It returns the domain part of the reference, mapping Docker Hub's default
// domain to its canonical host if needed.
// lscr.io is mapped to ghcr.io since lscr.io images are hosted on GitHub
// Container Registry.
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
	domain := reference.Domain(normalizedRef)

	// Map Docker Hub's default domain to its canonical host for registry requests.
	if domain == DockerRegistryDomain {
		logrus.WithFields(logrus.Fields{
			"image_ref": imageRef,
			"address":   domain,
		}).Debug("Mapped Docker Hub domain to canonical host")

		domain = DockerRegistryHost
	}

	// lscr.io images are hosted on GitHub Container Registry (ghcr.io).
	// Map here so all callers benefit, including GetChallengeURL and GetAuthURL.
	if domain == LSCRRegistryDomain {
		logrus.WithFields(logrus.Fields{
			"image_ref": imageRef,
			"address":   domain,
		}).Debug("Mapped lscr.io to ghcr.io")

		domain = GitHubRegistryDomain
	}

	logrus.WithFields(logrus.Fields{
		"image_ref": imageRef,
		"address":   domain,
	}).Debug("Extracted registry address")

	return domain, nil
}
