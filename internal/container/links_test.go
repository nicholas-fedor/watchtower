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

// TestContainer_NetworkContainer verifies that only a network mode naming
// another container reports that container.
func TestContainer_NetworkContainer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mode string
		want string
	}{
		{name: "container by name", mode: "container:vpn", want: "vpn"},
		{name: "container by ID", mode: "container:0e12a4efaf2c", want: "0e12a4efaf2c"},
		{name: "name with a leading slash", mode: "container:/vpn", want: "vpn"},
		{name: "bridge", mode: "bridge", want: ""},
		{name: "host", mode: "host", want: ""},
		{name: "unset", mode: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := MockContainer(containerfixture.WithNetworkMode(tt.mode))
			assert.Equal(t, tt.want, c.NetworkContainer())
		})
	}
}

// TestContainer_SetNetworkContainer verifies that the network mode of a
// container joining another container's namespace is set to name that
// container, without changing the inspect data read earlier. A container with
// a network of its own keeps its network mode.
func TestContainer_SetNetworkContainer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mode string
		want string
	}{
		{name: "container by ID", mode: "container:0e12a4efaf2c", want: "container:vpn"},
		{name: "container by name", mode: "container:vpn", want: "container:vpn"},
		{name: "bridge", mode: "bridge", want: "bridge"},
		{name: "unset", mode: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := MockContainer(containerfixture.WithNetworkMode(tt.mode))
			before := c.ContainerInfo()

			c.SetNetworkContainer("vpn")

			assert.Equal(t, tt.want, string(c.ContainerInfo().HostConfig.NetworkMode))
			assert.Equal(t, tt.mode, string(before.HostConfig.NetworkMode), "the inspect data read earlier is not changed")
		})
	}
}

// TestContainer_JoinsNetworkOf verifies that a container joins the network
// namespace of the container its network mode names by name or ID and of no
// other.
func TestContainer_JoinsNetworkOf(t *testing.T) {
	t.Parallel()

	vpn := MockContainer(containerfixture.WithName("/vpn"), containerfixture.WithID("vpn-id"))

	tests := []struct {
		name  string
		mode  string
		other *Container
		want  bool
	}{
		{name: "by name", mode: "container:vpn", other: vpn, want: true},
		{name: "by name with slash", mode: "container:/vpn", other: vpn, want: true},
		{name: "by ID", mode: "container:vpn-id", other: vpn, want: true},
		{name: "another container", mode: "container:db", other: vpn, want: false},
		{name: "own network", mode: "bridge", other: vpn, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := MockContainer(containerfixture.WithName("/app"), containerfixture.WithNetworkMode(tt.mode))
			assert.Equal(t, tt.want, c.JoinsNetworkOf(tt.other))
		})
	}
}

// TestContainer_CreateConfigTargets verifies that the network container,
// legacy links and volumes-from each name another container by name or ID,
// and that a label dependency does not. A legacy link of a Compose container
// names the container Docker links, without the project prefix that
// dependency ordering adds.
func TestContainer_CreateConfigTargets(t *testing.T) {
	t.Parallel()

	db := MockContainer(containerfixture.WithName("/db"), containerfixture.WithID("db-id"))
	others := []*Container{MockContainer(containerfixture.WithName("/cache")), db}

	tests := []struct {
		name string
		c    *Container
		want []*Container
	}{
		{name: "network container by name", c: MockContainer(containerfixture.WithNetworkMode("container:db")), want: []*Container{db}},
		{name: "network container by ID", c: MockContainer(containerfixture.WithNetworkMode("container:db-id")), want: []*Container{db}},
		{name: "legacy link", c: MockContainer(containerfixture.WithLinks([]string{"/db:/app/db"})), want: []*Container{db}},
		{
			name: "legacy link of a Compose container",
			c: MockContainer(
				containerfixture.WithLinks([]string{"/db:/app/db"}),
				containerfixture.WithLabels(map[string]string{"com.docker.compose.project": "stack"}),
			),
			want: []*Container{db},
		},
		{name: "volumes-from", c: MockContainer(containerfixture.WithVolumesFrom([]string{"db:ro"})), want: []*Container{db}},
		{
			name: "label dependency",
			c:    MockContainer(containerfixture.WithLabels(map[string]string{labels.DependsOn: "db"})),
			want: nil,
		},
		{name: "another container", c: MockContainer(containerfixture.WithNetworkMode("container:vpn")), want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.c.CreateConfigTargets(others))
		})
	}

	assert.Empty(t, MockContainer(containerfixture.WithNetworkMode("container:db")).CreateConfigTargets(nil))

	self := MockContainer(containerfixture.WithName("/loop"), containerfixture.WithNetworkMode("container:loop"))
	assert.Empty(t, self.CreateConfigTargets([]*Container{self}), "a container never targets itself")
}
