package containers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
