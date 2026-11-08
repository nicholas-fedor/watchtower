package image

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// repoDigestPart returns the digest part of a RepoDigest, after its first @.
//
// Parameters:
//   - repoDigest: RepoDigest in the format "name@digest".
//
// Returns:
//   - string: The part after the first @, empty when there is none.
func repoDigestPart(repoDigest string) string {
	_, digest, _ := strings.Cut(repoDigest, "@")

	return digest
}

// FuzzExtractImageDigest verifies that ExtractImageDigest never panics and
// returns either nothing or the non-empty digest part of the RepoDigest. Docker
// reports RepoDigests as "name@sha256:...", so the digest is passed on as it is.
//
// Well-formed seeds use short, registry-qualified and port-qualified names.
// Malformed seeds lack the @ or leave the name or digest part empty.
func FuzzExtractImageDigest(f *testing.F) {
	f.Add("nginx@sha256:abc123")
	f.Add("my-registry.com/nginx@sha256:abc123def456")
	f.Add("")
	f.Add("no-digest-here")
	f.Add("@sha256:abc")
	f.Add("nginx@")
	f.Add("nginx@sha256:")
	f.Add("sha256:abc123")
	f.Add("registry.example.com:5000/org/image@sha256:abcdef1234567890")

	f.Fuzz(func(t *testing.T, repoDigest string) {
		digest := ExtractImageDigest([]string{repoDigest}, "nginx:latest")

		if repoDigest == "" {
			assert.Empty(t, digest, "empty input should return empty digest")

			return
		}

		assert.Equal(t, repoDigestPart(repoDigest), digest)
	})
}

// FuzzExtractImageDigestMultiple verifies that ExtractImageDigest selects the
// digest of one of the comma-separated RepoDigests, or nothing when none has a
// digest part.
//
// One seed puts a malformed entry before a valid one. Another has no valid
// entry at all.
func FuzzExtractImageDigestMultiple(f *testing.F) {
	f.Add("nginx@sha256:abc123")
	f.Add("sha256:invalid,nginx@sha256:valid")
	f.Add("no-digest,also-no-digest")

	f.Fuzz(func(t *testing.T, digestsStr string) {
		var digests []string
		if digestsStr != "" {
			digests = strings.Split(digestsStr, ",")
		}

		result := ExtractImageDigest(digests, "nginx:latest")

		candidates := make([]string, 0, len(digests))
		for _, d := range digests {
			if part := repoDigestPart(d); part != "" {
				candidates = append(candidates, part)
			}
		}

		if len(candidates) == 0 {
			assert.Empty(t, result)

			return
		}

		assert.Contains(t, candidates, result)
	})
}
