package container

import (
	"testing"

	"github.com/stretchr/testify/assert"

	dockerContainer "github.com/moby/moby/api/types/container"

	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
)

// TestContainer_Links_NoConfig verifies that a container without inspect
// config data still reports its host-config dependencies instead of panicking.
func TestContainer_Links_NoConfig(t *testing.T) {
	t.Parallel()

	c := &Container{
		normalizedName: "app",
		containerInfo: &dockerContainer.InspectResponse{
			Name: "/app",
			HostConfig: &dockerContainer.HostConfig{
				NetworkMode: "container:vpn",
			},
		},
	}

	links, namesItself := c.Links(false)

	assert.Equal(t, []string{"vpn"}, links)
	assert.False(t, namesItself)
}

// TestContainer_Links_NamesItself verifies that Links leaves out a depends-on
// entry naming the container by its own name and reports that it did.
func TestContainer_Links_NamesItself(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		dependsOn string
		want      bool
		wantLinks []string
	}{
		{name: "no entry", dependsOn: "", want: false, wantLinks: []string{}},
		{name: "another container", dependsOn: "db", want: false, wantLinks: []string{"db"}},
		{name: "its own name", dependsOn: "db,app", want: true, wantLinks: []string{"db"}},
		{name: "its own name with spaces and a leading slash", dependsOn: " /app ", want: true, wantLinks: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			containerLabels := map[string]string{}
			if tt.dependsOn != "" {
				containerLabels[labels.DependsOn] = tt.dependsOn
			}

			c := MockContainer(containerfixture.WithName("/app"), containerfixture.WithLabels(containerLabels))

			links, namesItself := c.Links(false)

			assert.Equal(t, tt.wantLinks, links)
			assert.Equal(t, tt.want, namesItself)
		})
	}
}
