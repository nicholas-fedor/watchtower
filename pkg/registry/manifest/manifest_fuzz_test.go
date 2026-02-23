package manifest_test

import (
	"strings"
	"testing"
	"time"

	dockerImageType "github.com/docker/docker/api/types/image"

	mockActions "github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/registry/manifest"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// FuzzBuildManifestURL fuzzes the BuildManifestURL function with various image reference strings
// to ensure robust parsing. It tests valid references, malformed strings, and edge cases to prevent
// crashes and unexpected behavior during manifest URL construction.
func FuzzBuildManifestURL(f *testing.F) {
	// Seed with valid image references
	f.Add("ghcr.io/nicholas-fedor/watchtower:mytag")
	f.Add("nickfedor/watchtower:latest")
	f.Add("nginx:latest")
	f.Add("localhost:5000/repo/image:tag")
	f.Add("docker.io/library/alpine:v3.14")
	f.Add("registry.example.com/user/repo:1.0.0")

	// Seed with malformed strings
	f.Add("invalid:image:ref:!!")
	f.Add(
		"docker-registry.domain/imagename@sha256:daf7034c5c89775afe3008393ae033529913548243b84926931d7c84398ecda7",
	)
	f.Add("")
	f.Add(":::")
	f.Add("image@digest")
	f.Add("registry.com:port/image")

	// Seed with edge cases
	f.Add("image:tag")              // No registry
	f.Add("REGISTRY.COM/IMAGE:TAG") // Uppercase
	f.Add("very-long-registry-name.with.many.subdomains.com/repo/image:tag")
	f.Add("localhost/image")                                    // No tag, assumes latest
	f.Add("image")                                              // Just image name
	f.Add("registry.com:5000/image:tag")                        // Port number
	f.Add("192.168.1.1:5000/repo/image:v1.0")                   // IP address
	f.Add("registry.com/image:tag:with:colons")                 // Multiple colons in tag
	f.Add("registry.com/image@sha256:abc123")                   // Digest instead of tag
	f.Add(strings.Repeat("a", 1000) + ":tag")                   // Very long image name
	f.Add("registry.com/" + strings.Repeat("a", 1000) + ":tag") // Very long repo path

	f.Fuzz(func(_ *testing.T, imageRef string) {
		// Create a mock container with the fuzzed image reference
		mock := createMockContainerForFuzz(imageRef)
		// Call BuildManifestURL with HTTPS scheme; we don't care about the result, just that it doesn't panic
		_, _ = manifest.BuildManifestURL(mock, "https")
	})
}

// createMockContainerForFuzz creates a minimal mock container for fuzz testing BuildManifestURL.
func createMockContainerForFuzz(imageRef string) types.Container {
	imageInfo := dockerImageType.InspectResponse{
		RepoTags: []string{imageRef},
	}
	mockID := "fuzz-mock-id"
	mockName := "fuzz-mock-container"
	mockCreated := time.Now()

	return mockActions.CreateMockContainerWithImageInfo(
		mockID,
		mockName,
		imageRef,
		mockCreated,
		imageInfo,
	)
}
