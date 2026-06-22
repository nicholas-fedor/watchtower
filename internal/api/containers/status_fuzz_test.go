package containers

import (
	"strings"
	"testing"

	"github.com/moby/moby/api/types/image"

	"github.com/nicholas-fedor/watchtower/pkg/types"
	typemocks "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

// FuzzExtractDigest fuzzes the extractDigest function which parses registry
// manifest digests from RepoDigests strings. It tests that the function never
// panics and correctly handles various digest formats.
func FuzzExtractDigest(f *testing.F) {
	f.Add("nginx@sha256:abc123def456")
	f.Add("myregistry.com/myimage@sha256:deadbeef")
	f.Add("sha256:abc123")
	f.Add("")
	f.Add("@")
	f.Add("nginx@")
	f.Add("@sha256:abc")
	f.Add("nginx@sha256:abc@sha256:def")
	f.Add("nginx@sha256:" + strings.Repeat("a", 1000))
	f.Add("nginx@sha256:abc\x00def")
	f.Add("unicode@sha256:\xe6\x97\xa5\xe6\x9c\xac\xe8\xaa\x9e")

	f.Fuzz(func(t *testing.T, repoDigest string) {
		result := extractDigest([]string{repoDigest}, "test-container")

		// Invariant: if input contains @, result should be the part after @
		if _, after, ok := strings.Cut(repoDigest, "@"); ok {
			if result != after {
				t.Errorf("extractDigest(%q) = %q, want %q", repoDigest, result, after)
			}
		} else if result != "" {
			t.Errorf("extractDigest(%q) = %q, want empty string", repoDigest, result)
		}

		// Invariant 2: result is everything after the first @ (may contain more @)
		if _, after, ok := strings.Cut(repoDigest, "@"); ok {
			expected := after
			if result != expected {
				t.Errorf("extractDigest(%q) = %q, want %q", repoDigest, result, expected)
			}
		}
	})
}

// FuzzExtractDigestSlice fuzzes extractDigest with varying slice lengths.
func FuzzExtractDigestSlice(f *testing.F) {
	f.Add(0, 0)
	f.Add(1, 0)
	f.Add(2, 0)
	f.Add(1, 1)
	f.Add(3, 2)
	f.Add(5, 4)

	f.Fuzz(func(t *testing.T, sliceLen, seed int) {
		if sliceLen < 0 || sliceLen > 100 {
			return
		}

		digests := make([]string, sliceLen)
		for i := range digests {
			digests[i] = "image@sha256:digest" + string(rune('a'+i%26))
		}

		result := extractDigest(digests, "test-container")

		if sliceLen > 0 {
			firstDigest := digests[0]
			if _, after, ok := strings.Cut(firstDigest, "@"); ok {
				if result != after {
					t.Errorf("extractDigest with sliceLen=%d: got %q, want %q", sliceLen, result, after)
				}
			}
		} else if result != "" {
			t.Errorf("extractDigest with empty slice: got %q, want empty", result)
		}

		_ = seed
	})
}

// FuzzContainerToStatus fuzzes the containerToStatus function which transforms
// a types.Container into a Status struct. It tests that the function never
// panics regardless of what the container's methods return.
func FuzzContainerToStatus(f *testing.F) {
	f.Add("container-name", "image:tag", "sha256:abc123", true, "nginx@sha256:digest123")
	f.Add("", "", "", false, "")
	f.Add("name with spaces", "registry.com/org/image:v1.2.3", "sha256:"+strings.Repeat("a", 64), true, "image@sha256:"+strings.Repeat("b", 64))
	f.Add("unicode-\xe5\x90\x8d\xe5\x89\x8d", "\xe3\x82\xa4\xe3\x83\xa1\xe3\x83\xbc\xe3\x82\xb8:\xe3\x82\xbf\xe3\x82\xb0", "sha256:abc", true, "")

	f.Fuzz(func(t *testing.T, name, imageName, imageID string, hasInfo bool, repoDigest string) {
		container := typemocks.NewMockContainer(t)
		container.EXPECT().Name().Return(name)
		container.EXPECT().ImageName().Return(imageName)
		container.EXPECT().ImageID().Return(types.ImageID(imageID))

		if hasInfo {
			info := &image.InspectResponse{
				RepoDigests: []string{repoDigest},
			}
			container.EXPECT().ImageInfo().Return(info)
		} else {
			container.EXPECT().ImageInfo().Return(nil)
		}

		status := containerToStatus(container)

		if status.Name != name {
			t.Errorf("Name = %q, want %q", status.Name, name)
		}

		if status.Image != imageName {
			t.Errorf("Image = %q, want %q", status.Image, imageName)
		}

		if status.ImageID != imageID {
			t.Errorf("ImageID = %q, want %q", status.ImageID, imageID)
		}

		if hasInfo && repoDigest != "" {
			if _, after, ok := strings.Cut(repoDigest, "@"); ok {
				if status.Digest != after {
					t.Errorf("Digest = %q, want %q", status.Digest, after)
				}
			} else if status.Digest != "" {
				t.Errorf("Digest = %q, want empty (no @ in repoDigest)", status.Digest)
			}
		}
	})
}
