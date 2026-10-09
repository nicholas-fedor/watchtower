package sorter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"

	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// cycleContainer returns a container with the given name and labels.
func cycleContainer(name string, labels map[string]string) types.Container {
	return container.NewContainer(nil, &dockerContainer.InspectResponse{
		ID:         name + "-id",
		Name:       "/" + name,
		HostConfig: &dockerContainer.HostConfig{},
		Config:     &dockerContainer.Config{Labels: labels},
	}, nil)
}

// dependsOn returns a container whose Watchtower depends-on label lists links.
func dependsOn(name, links string) types.Container {
	labels := map[string]string{}
	if links != "" {
		labels["com.centurylinklabs.watchtower.depends-on"] = links
	}

	return cycleContainer(name, labels)
}

// composeService returns a Compose container of the stack project named
// <service>-server, whose depends_on label lists dependsOnServices.
func composeService(service, dependsOnServices string) types.Container {
	labels := map[string]string{
		"com.docker.compose.project":          "stack",
		"com.docker.compose.service":          service,
		"com.docker.compose.container-number": "1",
	}
	if dependsOnServices != "" {
		labels["com.docker.compose.depends_on"] = dependsOnServices
	}

	return cycleContainer(service+"-server", labels)
}

// TestDependencyGraph_CycleMembers verifies that every container in a circular dependency is
// reported, in the order given, and that containers outside a cycle are not,
// including those that only depend on a cycle member and Watchtower containers.
func TestDependencyGraph_CycleMembers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		containers []types.Container
		// useComposeDependsOn includes Compose depends_on labels as links.
		useComposeDependsOn bool
		want                []string
	}{
		{
			name:       "no containers",
			containers: nil,
			want:       []string{},
		},
		{
			name:       "acyclic",
			containers: []types.Container{dependsOn("c1", ""), dependsOn("c2", "c1")},
			want:       []string{},
		},
		{
			name:       "two containers",
			containers: []types.Container{dependsOn("c1", "c2"), dependsOn("c2", "c1")},
			want:       []string{"c1", "c2"},
		},
		{
			name: "three containers",
			containers: []types.Container{
				dependsOn("c1", "c2"),
				dependsOn("c2", "c3"),
				dependsOn("c3", "c1"),
			},
			want: []string{"c1", "c2", "c3"},
		},
		{
			// A container listing itself is a self-dependency, which the
			// update skips separately, not a cycle between containers.
			name:       "self-reference",
			containers: []types.Container{dependsOn("c1", "c1")},
			want:       []string{},
		},
		{
			name: "disconnected components",
			containers: []types.Container{
				dependsOn("c1", ""),
				dependsOn("c2", "c3"),
				dependsOn("c3", "c2"),
			},
			want: []string{"c2", "c3"},
		},
		{
			name: "dependents of a cycle member",
			containers: []types.Container{
				dependsOn("worker", "c1"),
				dependsOn("c1", "c2"),
				dependsOn("c2", "c1"),
			},
			want: []string{"c1", "c2"},
		},
		{
			name:       "unknown dependencies",
			containers: []types.Container{dependsOn("c1", "c2,unknown"), dependsOn("c2", "c1")},
			want:       []string{"c1", "c2"},
		},
		{
			// The sort orders Watchtower containers last without reading
			// their links, so they cannot be part of a cycle.
			name: "Watchtower containers set aside",
			containers: []types.Container{
				cycleContainer("watchtower", map[string]string{
					"com.centurylinklabs.watchtower":            "true",
					"com.centurylinklabs.watchtower.depends-on": "c1",
				}),
				dependsOn("c1", "watchtower"),
			},
			want: []string{},
		},
		{
			name: "Compose services with other container names",
			containers: []types.Container{
				composeService("web", "api:service_started:false"),
				composeService("api", "web:service_started:false"),
				composeService("app", ""),
			},
			useComposeDependsOn: true,
			want:                []string{"web-server", "api-server"},
		},
		{
			name: "Compose depends_on ignored",
			containers: []types.Container{
				composeService("web", "api:service_started:false"),
				composeService("api", "web:service_started:false"),
			},
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			graph, err := NewDependencyGraph(testLog(), tt.containers, tt.useComposeDependsOn)
			require.NoError(t, err)
			assert.Equal(t, tt.want, containerNames(graph.CycleMembers()))
		})
	}
}

// TestNewDependencyGraph_IdentifierCollision verifies that containers sharing
// a dependency identifier are reported as an error, as the sort reports them.
func TestNewDependencyGraph_IdentifierCollision(t *testing.T) {
	t.Parallel()

	labels := map[string]string{
		"com.docker.compose.project": "stack",
		"com.docker.compose.service": "web",
	}

	_, err := NewDependencyGraph(testLog(), []types.Container{
		cycleContainer("first", labels),
		cycleContainer("second", labels),
	}, false)
	require.ErrorIs(t, err, ErrIdentifierCollision)
}

// TestDependencyGraph_CycleMembersLinkSources verifies that a cycle is found whichever source
// of dependency links closes it, and however the link names its target: by
// container name, Compose service, Docker ID, or with a volumes-from mode.
func TestDependencyGraph_CycleMembersLinkSources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		containers []types.Container
		// useComposeDependsOn includes Compose depends_on labels as links.
		useComposeDependsOn bool
		want                []string
	}{
		{
			name: "network mode by name",
			containers: deploy(
				deployed{name: "app", networkMode: "container:vpn"},
				deployed{name: "vpn", dependsOn: "app"},
			),
			want: []string{"app", "vpn"},
		},
		{
			name: "network mode by Docker ID",
			containers: deploy(
				deployed{name: "app", networkMode: "container:" + dockerID("vpn")},
				deployed{name: "vpn", dependsOn: "app"},
			),
			want: []string{"app", "vpn"},
		},
		{
			name: "volumes-from by name with a mode",
			containers: deploy(
				deployed{name: "app", volumesFrom: []string{"data:ro"}},
				deployed{name: "data", dependsOn: "app"},
			),
			want: []string{"app", "data"},
		},
		{
			name: "volumes-from by Docker ID",
			containers: deploy(
				deployed{name: "app", volumesFrom: []string{dockerID("data") + ":rw"}},
				deployed{name: "data", dependsOn: "app"},
			),
			want: []string{"app", "data"},
		},
		{
			// A depends-on label replaces the other link sources, except
			// volumes-from, which is always a dependency.
			name: "volumes-from beside a depends-on label",
			containers: deploy(
				deployed{name: "app", dependsOn: "db", volumesFrom: []string{"data"}},
				deployed{name: "data", dependsOn: "app"},
				deployed{name: "db"},
			),
			want: []string{"app", "data"},
		},
		{
			name: "legacy links",
			containers: deploy(
				deployed{name: "app", links: []string{"db:database"}},
				deployed{name: "db", links: []string{"/app:web"}},
			),
			want: []string{"app", "db"},
		},
		{
			name: "legacy links in a Compose project",
			containers: deploy(
				deployed{name: "stack-app-1", project: "stack", service: "app", number: "1", links: []string{"db:db"}},
				deployed{name: "stack-db-1", project: "stack", service: "db", number: "1", links: []string{"app:app"}},
			),
			want: []string{"stack-app-1", "stack-db-1"},
		},
		{
			name: "Compose depends_on across replicas",
			containers: deploy(
				deployed{name: "stack-web-1", project: "stack", service: "web", number: "1", composeDependsOn: "api:service_started:false"},
				deployed{name: "stack-api-1", project: "stack", service: "api", number: "1", composeDependsOn: "web:service_started:false"},
				deployed{name: "stack-api-2", project: "stack", service: "api", number: "2", composeDependsOn: "web:service_started:false"},
			),
			useComposeDependsOn: true,
			want:                []string{"stack-web-1", "stack-api-1", "stack-api-2"},
		},
		{
			name: "Compose depends_on in JSON form",
			containers: deploy(
				deployed{name: "stack-web-1", project: "stack", service: "web", number: "1", composeDependsOn: `{"api":{"condition":"service_started"}}`},
				deployed{name: "stack-api-1", project: "stack", service: "api", number: "1", composeDependsOn: `{"web":{"condition":"service_started"}}`},
			),
			useComposeDependsOn: true,
			want:                []string{"stack-web-1", "stack-api-1"},
		},
		{
			name: "Compose v1 container names",
			containers: deploy(
				deployed{name: "stack_web_1", project: "stack", service: "web", number: "1", composeDependsOn: "api"},
				deployed{name: "stack_api_1", project: "stack", service: "api", number: "1", composeDependsOn: "web"},
			),
			useComposeDependsOn: true,
			want:                []string{"stack_web_1", "stack_api_1"},
		},
		{
			name: "same services in two projects",
			containers: deploy(
				deployed{name: "a-web-1", project: "a", service: "web", number: "1", composeDependsOn: "db"},
				deployed{name: "b-db-1", project: "b", service: "db", number: "1", composeDependsOn: "web"},
			),
			useComposeDependsOn: true,
			want:                []string{},
		},
		{
			name: "network mode on a Watchtower container",
			containers: deploy(
				deployed{name: "watchtower", watchtower: true, dependsOn: "app"},
				deployed{name: "app", networkMode: "container:" + dockerID("watchtower")},
			),
			want: []string{},
		},
		{
			name: "network-mode dependent of a cycle",
			containers: deploy(
				deployed{name: "app", networkMode: "container:vpn"},
				deployed{name: "vpn", dependsOn: "proxy"},
				deployed{name: "proxy", dependsOn: "vpn"},
			),
			want: []string{"vpn", "proxy"},
		},
		{
			name: "three link sources in one cycle",
			containers: deploy(
				deployed{name: "app", networkMode: "container:" + dockerID("vpn")},
				deployed{name: "vpn", volumesFrom: []string{"data:ro"}},
				deployed{name: "data", links: []string{"app:app"}},
			),
			want: []string{"app", "vpn", "data"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			graph, err := NewDependencyGraph(testLog(), tt.containers, tt.useComposeDependsOn)
			require.NoError(t, err)
			assert.Equal(t, tt.want, containerNames(graph.CycleMembers()))
		})
	}
}

// TestDependencyGraph_Subgraph verifies that a subset's graph keeps exactly
// the links among its containers that the full set's graph has. A link whose
// target is outside the subset is dropped rather than matched to another
// container in the subset, whatever source and reference form the link uses.
// The alone column records the edges that resolving links within the subset
// alone would give instead.
func TestDependencyGraph_Subgraph(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		all  []types.Container
		// subset names the containers that become graph nodes.
		subset []string
		// useComposeDependsOn includes Compose depends_on labels as links.
		useComposeDependsOn bool
		// want lists the edges as dependent->dependency.
		want []string
		// alone lists the edges when links resolve within the subset alone.
		alone []string
	}{
		{
			name: "exact target outside the subset",
			all: deploy(
				deployed{name: "db"},
				deployed{name: "db-1", dependsOn: "c"},
				deployed{name: "c", dependsOn: "db"},
			),
			subset: []string{"c", "db-1"},
			want:   []string{"db-1->c"},
			alone:  []string{"c->db-1", "db-1->c"},
		},
		{
			name: "ambiguous service name",
			all: deploy(
				deployed{name: "app", dependsOn: "db"},
				deployed{name: "xdb", project: "x", service: "db"},
				deployed{name: "ydb", project: "y", service: "db"},
			),
			subset: []string{"app", "xdb"},
			want:   []string{},
			alone:  []string{"app->xdb"},
		},
		{
			name: "network mode by Docker ID inside the subset",
			all: deploy(
				deployed{name: "app", networkMode: "container:" + dockerID("vpn")},
				deployed{name: "vpn"},
				deployed{name: "other"},
			),
			subset: []string{"app", "vpn"},
			want:   []string{"app->vpn"},
			alone:  []string{"app->vpn"},
		},
		{
			name: "volumes-from by name outside the subset",
			all: deploy(
				deployed{name: "app", volumesFrom: []string{"data:ro"}},
				deployed{name: "data"},
				deployed{name: "data-1"},
			),
			subset: []string{"app", "data-1"},
			want:   []string{},
			alone:  []string{"app->data-1"},
		},
		{
			name: "volumes-from by Docker ID outside the subset",
			all: deploy(
				deployed{name: "app", volumesFrom: []string{dockerID("data") + ":ro"}},
				deployed{name: "data"},
				deployed{name: "data-1"},
			),
			subset: []string{"app", "data-1"},
			want:   []string{},
			alone:  []string{},
		},
		{
			name: "Compose replicas in and out of the subset",
			all: deploy(
				deployed{name: "stack-web-1", project: "stack", service: "web", number: "1", composeDependsOn: "api"},
				deployed{name: "stack-api-1", project: "stack", service: "api", number: "1"},
				deployed{name: "stack-api-2", project: "stack", service: "api", number: "2"},
			),
			subset:              []string{"stack-web-1", "stack-api-2"},
			useComposeDependsOn: true,
			want:                []string{"stack-web-1->stack-api-2"},
			alone:               []string{"stack-web-1->stack-api-2"},
		},
		{
			name: "legacy link outside the subset in a Compose project",
			all: deploy(
				deployed{name: "stack-app-1", project: "stack", service: "app", number: "1", links: []string{"db:db"}},
				deployed{name: "stack-db", project: "stack", service: "db"},
				deployed{name: "stack-db-1", project: "stack", service: "db", number: "1"},
			),
			subset: []string{"stack-app-1", "stack-db-1"},
			want:   []string{},
			alone:  []string{"stack-app-1->stack-db-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			subset := pick(tt.all, tt.subset...)
			require.Len(t, subset, len(tt.subset))

			assert.ElementsMatch(t, tt.want, graphEdges(t, subset, tt.all, tt.useComposeDependsOn),
				"links resolved against all")
			assert.ElementsMatch(t, tt.alone, graphEdges(t, subset, nil, tt.useComposeDependsOn),
				"links resolved within the subset")
		})
	}
}

// graphEdges returns the edges between containers, as dependent->dependency
// container names, in the dependency graph of all. With all nil, the graph is
// built from containers alone.
func graphEdges(t *testing.T, containers, all []types.Container, useComposeDependsOn bool) []string {
	t.Helper()

	if all == nil {
		all = containers
	}

	graph, err := NewDependencyGraph(testLog(), all, useComposeDependsOn)
	require.NoError(t, err)

	containerMap, _, adjacency, _, err := graph.subgraph(containers)
	require.NoError(t, err)

	edges := []string{}

	for dependency, dependents := range adjacency {
		for _, dependent := range dependents {
			edges = append(edges, containerMap[dependent].Name()+"->"+containerMap[dependency].Name())
		}
	}

	return edges
}

// TestDependencyGraph_Sort verifies that sorting orders dependencies first
// across link sources, puts Watchtower containers last, sorts a subset by the
// links between its containers only, and rejects a container that is not in
// the graph.
func TestDependencyGraph_Sort(t *testing.T) {
	t.Parallel()

	all := deploy(
		deployed{name: "app", networkMode: "container:" + dockerID("vpn"), volumesFrom: []string{"data:ro"}},
		deployed{name: "vpn"},
		deployed{name: "data", dependsOn: "vpn"},
		deployed{name: "watchtower", watchtower: true, dependsOn: "app"},
		deployed{name: "db"},
		deployed{name: "db-1", dependsOn: "c"},
		deployed{name: "c", dependsOn: "db"},
	)

	graph, err := NewDependencyGraph(testLog(), all, false)
	require.NoError(t, err)

	tests := []struct {
		name string
		// sort names the containers to sort, in their order before sorting.
		sort []string
		want []string
	}{
		{name: "link sources", sort: []string{"app", "data", "vpn"}, want: []string{"vpn", "data", "app"}},
		{name: "Watchtower last", sort: []string{"watchtower", "app", "vpn"}, want: []string{"vpn", "app", "watchtower"}},
		{name: "link target outside the subset", sort: []string{"db-1", "c"}, want: []string{"c", "db-1"}},
		{name: "nothing to sort", sort: []string{}, want: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			containers := pick(all, tt.sort...)
			require.NoError(t, graph.Sort(testLog(), containers))
			assert.Equal(t, tt.want, containerNames(containers))
		})
	}

	t.Run("container not in the graph", func(t *testing.T) {
		t.Parallel()

		containers := append(pick(all, "app"), deployed{name: "other"}.container())
		require.ErrorIs(t, graph.Sort(testLog(), containers), ErrNotInGraph)
	})
}
