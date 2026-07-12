package containers

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/nicholas-fedor/watchtower/pkg/container"
)

// FuzzExtractDigest verifies that ExtractImageDigest never panics and correctly
// extracts digests from RepoDigests strings in various formats.
func FuzzExtractDigest(f *testing.F) {
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
		digest := container.ExtractImageDigest([]string{repoDigest}, "nginx:latest")

		if repoDigest == "" {
			assert.Empty(t, digest, "empty input should return empty digest")

			return
		}

		assert.True(t, digest == "" || strings.HasPrefix(digest, "sha256:"),
			"digest should be empty or start with sha256:, got %q", digest)
	})
}

// FuzzExtractDigestMultiple verifies that ExtractImageDigest correctly selects
// a valid digest from a list of RepoDigests.
func FuzzExtractDigestMultiple(f *testing.F) {
	f.Add("nginx@sha256:abc123")
	f.Add("sha256:invalid,nginx@sha256:valid")
	f.Add("no-digest,also-no-digest")

	f.Fuzz(func(t *testing.T, digestsStr string) {
		var digests []string
		if digestsStr != "" {
			digests = strings.Split(digestsStr, ",")
		}

		result := container.ExtractImageDigest(digests, "nginx:latest")

		assert.True(t, result == "" || strings.HasPrefix(result, "sha256:"),
			"result should be empty or start with sha256:, got %q", result)
	})
}

// FuzzFilterStatuses verifies that filterStatuses never panics and correctly
// filters container statuses by name and image.
func FuzzFilterStatuses(f *testing.F) {
	f.Add("nginx", "nginx:latest")
	f.Add("", "")
	f.Add("nginx", "")
	f.Add("", "nginx:latest")
	f.Add("nonexistent", "nonexistent:latest")

	f.Fuzz(func(t *testing.T, nameFilter, imageFilter string) {
		statuses := []Status{
			{
				Name:    "nginx-proxy",
				Image:   "nginx:latest",
				ImageID: "sha256:abc",
				Digest:  "sha256:def",
			},
			{
				Name:    "redis-cache",
				Image:   "redis:7",
				ImageID: "sha256:123",
				Digest:  "sha256:456",
			},
			{
				Name:    "mysql-db",
				Image:   "mysql:8.0",
				ImageID: "sha256:789",
				Digest:  "sha256:012",
			},
		}

		filtered := filterStatuses(statuses, nameFilter, imageFilter)

		for _, s := range filtered {
			if nameFilter != "" {
				assert.Equal(t, nameFilter, s.Name,
					"filtered status should match name filter")
			}

			if imageFilter != "" {
				assert.Equal(t, imageFilter, s.Image,
					"filtered status should match image filter")
			}
		}

		if nameFilter == "" && imageFilter == "" {
			assert.Len(t, filtered, len(statuses),
				"no filter should return all statuses")
		}
	})
}
