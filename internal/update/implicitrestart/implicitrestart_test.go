package implicitrestart

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/modelfixture"
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
	"github.com/nicholas-fedor/watchtower/internal/update/sorter"
)

// testGraph returns the dependency graph of the monitored containers, with the
// unmonitored ones outside it.
func testGraph(t *testing.T, monitored, unmonitored []*container.Container) *sorter.DependencyGraph {
	t.Helper()

	graph, err := sorter.NewDependencyGraph(newTestLogger(t), monitored, unmonitored, false)
	require.NoError(t, err)

	return graph
}

// newTestLogger returns a logger that writes each event to the test's log, so a
// failing test shows what it logged.
//
// Parameters:
//   - t: Test that receives the log lines.
//
// Returns:
//   - *zerolog.Logger: Logger writing to t.
func newTestLogger(t *testing.T) *zerolog.Logger {
	t.Helper()

	// Frame 4 skips zerolog's write path, so each line names the log call.
	w := zerolog.TestWriter{T: t, Frame: 4}

	return new(zerolog.New(w))
}

// restartSpec describes a container for the implicit restart tests.
type restartSpec struct {
	// name is the container name, without the leading slash.
	name string
	// id is the container ID. Empty means an ID derived from the name.
	id string
	// project, service, and number are the Compose labels. Empty means unset.
	project, service, number string
	// dependsOn is the Watchtower depends-on label.
	dependsOn string
	// networkMode is the network mode, such as container:vpn.
	networkMode string
	// watchtower marks the container as a Watchtower instance.
	watchtower bool
	// stale marks the container as updating.
	stale bool
	// unmonitored leaves the container out of the monitored containers.
	unmonitored bool
	// excluded keeps the container from being marked or marking others.
	excluded bool
}

// restartSpecID returns a stable 64-character hex container ID for a name.
func restartSpecID(name string) string {
	sum := sha256.Sum256([]byte(name))

	return hex.EncodeToString(sum[:])
}

// build builds the container the description stands for.
func (s restartSpec) build() *container.Container {
	labels := map[string]string{}

	for key, value := range map[string]string{
		"com.docker.compose.project":                s.project,
		"com.docker.compose.service":                s.service,
		"com.docker.compose.container-number":       s.number,
		"com.centurylinklabs.watchtower.depends-on": s.dependsOn,
	} {
		if value != "" {
			labels[key] = value
		}
	}

	if s.watchtower {
		labels["com.centurylinklabs.watchtower"] = "true"
	}

	c := container.NewContainer(nil, &dockerContainer.InspectResponse{
		ID:         cmp.Or(s.id, restartSpecID(s.name)),
		Name:       "/" + s.name,
		HostConfig: &dockerContainer.HostConfig{NetworkMode: dockerContainer.NetworkMode(s.networkMode)},
		Config:     &dockerContainer.Config{Labels: labels},
	}, nil)
	c.SetStale(s.stale)

	return c
}

// TestMarkImplicitRestarts verifies which containers are marked as linked to a
// restarting container: those whose links name it, directly or through other
// marked containers, by container name, Compose service, replica, or Docker
// ID. A Watchtower container is marked through its own links and marks the
// containers that name it exactly, or as the only replica of its Compose
// service. A link that names an unmonitored container, or a service more than
// one service could be, marks nothing.
func TestMarkImplicitRestarts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		specs []restartSpec
		want  []string
	}{
		{
			name:  "service name of a single container",
			specs: []restartSpec{{name: "app", dependsOn: "db"}, {name: "project1-db", stale: true}},
			want:  []string{"app"},
		},
		{
			name: "exact name before a service name",
			specs: []restartSpec{
				{name: "app", dependsOn: "db"},
				{name: "db"},
				{name: "project1-db", stale: true},
			},
			want: nil,
		},
		{
			name:  "project-qualified service",
			specs: []restartSpec{{name: "app", dependsOn: "myproject-db"}, {name: "myproject-db", stale: true}},
			want:  []string{"app"},
		},
		{
			name: "multi-segment container name",
			specs: []restartSpec{
				{name: "app", dependsOn: "production-api-gateway"},
				{name: "production-api-gateway", stale: true},
			},
			want: []string{"app"},
		},
		{
			name: "replica of a project-qualified service",
			specs: []restartSpec{
				{name: "app", dependsOn: "myapp-db"},
				{name: "myapp-db-1", project: "myapp", service: "db", number: "1", stale: true},
			},
			want: []string{"app"},
		},
		{
			name: "hyphenated service in another project",
			specs: []restartSpec{
				{name: "app1-foo-1", project: "app1", service: "foo", number: "1", dependsOn: "watchtower-test-database"},
				{
					name: "database1-watchtower-test-database-1", project: "database1",
					service: "watchtower-test-database", number: "1", stale: true,
				},
			},
			want: []string{"app1-foo-1"},
		},
		{
			name: "one replica of a service with two",
			specs: []restartSpec{
				{name: "app", dependsOn: "db"},
				{name: "stack-db-1", project: "stack", service: "db", number: "1"},
				{name: "stack-db-2", project: "stack", service: "db", number: "2", stale: true},
			},
			want: []string{"app"},
		},
		{
			name: "same service in two projects",
			specs: []restartSpec{
				{name: "app", dependsOn: "db"},
				{name: "project1-db-1", project: "project1", service: "db", number: "1", stale: true},
				{name: "project2-db-1", project: "project2", service: "db", number: "1"},
			},
			want: []string{"app"},
		},
		{
			name: "hyphenated service in two other projects, one by container name",
			specs: []restartSpec{
				{
					name: "app-web-1", project: "app", service: "web", number: "1",
					dependsOn: "watchtower-test-database",
				},
				{
					name: "app-web-2", project: "app", service: "web", number: "2",
					dependsOn: "watchtower-test-database",
				},
				{
					name: "database1-watchtower-test-database-1", project: "database1",
					service: "watchtower-test-database", number: "1",
				},
				{
					name: "watchtower-test-database2", project: "database2",
					service: "watchtower-test-database", number: "1", stale: true,
				},
			},
			want: []string{"app-web-1", "app-web-2"},
		},
		{
			name: "service of the dependent's project unmonitored",
			specs: []restartSpec{
				{name: "a-web-1", project: "a", service: "web", number: "1", dependsOn: "db"},
				{name: "a-db-1", project: "a", service: "db", number: "1", unmonitored: true},
				{name: "b-db-1", project: "b", service: "db", number: "1", stale: true},
			},
			want: nil,
		},
		{
			name: "service in two projects, one of them the dependent's",
			specs: []restartSpec{
				{name: "a-web-1", project: "a", service: "web", number: "1", dependsOn: "db"},
				{name: "a-db-1", project: "a", service: "db", number: "1"},
				{name: "b-db-1", project: "b", service: "db", number: "1", stale: true},
			},
			want: nil,
		},
		{
			name: "through a marked container",
			specs: []restartSpec{
				{name: "web", dependsOn: "api"},
				{name: "api", dependsOn: "db"},
				{name: "db", stale: true},
			},
			want: []string{"api", "web"},
		},
		{
			name: "through an excluded container",
			specs: []restartSpec{
				{name: "web", dependsOn: "api"},
				{name: "api", dependsOn: "db", excluded: true},
				{name: "db", stale: true},
			},
			want: nil,
		},
		{
			name: "Watchtower routed through a VPN container",
			specs: []restartSpec{
				{name: "watchtower", watchtower: true, networkMode: "container:" + restartSpecID("vpn")},
				{name: "vpn", stale: true},
			},
			want: []string{"watchtower"},
		},
		{
			name: "container naming a restarting Watchtower container",
			specs: []restartSpec{
				{name: "watchtower", watchtower: true, stale: true},
				{name: "app", dependsOn: "watchtower"},
			},
			want: []string{"app"},
		},
		{
			name: "container naming a restarting Watchtower container's Compose service",
			specs: []restartSpec{
				{name: "infra-watchtower-1", project: "infra", service: "watchtower", number: "1", watchtower: true, stale: true},
				{name: "app", dependsOn: "infra-watchtower"},
			},
			want: []string{"app"},
		},
		{
			name: "unmonitored container named exactly",
			specs: []restartSpec{
				{name: "app", dependsOn: "db"},
				{name: "db", unmonitored: true},
				{name: "db-1", stale: true},
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var all, monitored, unmonitored []*container.Container

			excluded := map[container.ContainerID]struct{}{}

			for _, spec := range tt.specs {
				c := spec.build()
				all = append(all, c)

				switch {
				case spec.unmonitored:
					unmonitored = append(unmonitored, c)
				case spec.excluded:
					monitored = append(monitored, c)
					excluded[c.ID()] = struct{}{}
				default:
					monitored = append(monitored, c)
				}
			}

			graph, err := sorter.NewDependencyGraph(newTestLogger(t), monitored, unmonitored, false)
			require.NoError(t, err)

			markImplicitRestarts(newTestLogger(t), graph, all, monitored, excluded)

			var marked []string

			for _, c := range all {
				if c.IsLinkedToRestarting() {
					marked = append(marked, c.Name())
				}
			}

			sort.Strings(marked)
			assert.Equal(t, tt.want, marked)
		})
	}
}

// TestReconcileImplicitRestarts verifies that implicit restarts follow the
// anchors that still restart. A dependent loses its restart when its anchor is
// no longer stale or is excluded, and an excluded exact link does not fall
// through to a service-name match.
func TestReconcileImplicitRestarts(t *testing.T) {
	t.Parallel()

	t.Run("drops dependents when the anchor apply failed", func(t *testing.T) {
		t.Parallel()

		log := newTestLogger(t)
		app := mockUpdate.CreateMockContainerWithLinks(
			"app",
			"/app",
			"app:latest",
			time.Now(),
			nil,
			nil,
		)
		db := mockUpdate.CreateMockContainerWithLinks(
			"db",
			"/db",
			"db:latest",
			time.Now(),
			[]string{"/app:app"},
			nil,
		)

		app.SetStale(true)
		containers := []*container.Container{app, db}
		markImplicitRestarts(log, testGraph(t, containers, nil), containers, containers, nil)
		assert.True(t, db.IsLinkedToRestarting())

		app.SetStale(false)

		got := Reconcile(log, testGraph(t, containers, nil), containers, containers, updateParams.Params{}, nil)

		assert.False(t, db.IsLinkedToRestarting())
		assert.Empty(t, got)
	})

	t.Run("keeps dependents when the anchor is still stale", func(t *testing.T) {
		t.Parallel()

		log := newTestLogger(t)
		app := mockUpdate.CreateMockContainerWithLinks(
			"app",
			"/app",
			"app:latest",
			time.Now(),
			nil,
			nil,
		)
		db := mockUpdate.CreateMockContainerWithLinks(
			"db",
			"/db",
			"db:latest",
			time.Now(),
			[]string{"/app:app"},
			nil,
		)

		app.SetStale(true)
		containers := []*container.Container{app, db}
		got := Reconcile(log, testGraph(t, containers, nil), containers, containers, updateParams.Params{}, nil)

		assert.True(t, db.IsLinkedToRestarting())
		assert.ElementsMatch(t, []*container.Container{app, db}, got)
	})

	t.Run("does not propagate restarts from a no-restart build anchor", func(t *testing.T) {
		t.Parallel()

		log := newTestLogger(t)
		app := mockUpdate.CreateMockContainerWithLinks(
			"app",
			"/app",
			"app:latest",
			time.Now(),
			nil,
			nil,
		)
		dependent := mockUpdate.CreateMockContainerWithLinks(
			"dependent",
			"/dependent",
			"dependent:latest",
			time.Now(),
			[]string{"/app:app"},
			nil,
		)

		app.SetStale(true)
		containers := []*container.Container{app, dependent}

		graph, err := sorter.NewDependencyGraph(log, containers, nil, false)
		require.NoError(t, err)

		got := Reconcile(
			log,
			graph,
			containers,
			containers,
			updateParams.Params{NoRestart: true},
			// The update excludes the containers Git built without a restart.
			map[container.ContainerID]struct{}{app.ID(): {}},
		)

		assert.False(t, dependent.IsLinkedToRestarting())
		assert.Empty(t, got)
	})
	t.Run("does not fall through from an excluded exact link to a service-name match", func(t *testing.T) {
		t.Parallel()

		log := newTestLogger(t)
		excluded := mockUpdate.CreateMockContainerWithLinks(
			"app",
			"/app",
			"app:latest",
			time.Now(),
			nil,
			nil,
		)
		serviceMatch := mockUpdate.CreateMockContainerWithLinks(
			"app-1",
			"/app-1",
			"app:latest",
			time.Now(),
			nil,
			nil,
		)
		dependent := mockUpdate.CreateMockContainerWithLinks(
			"dependent",
			"/dependent",
			"dependent:latest",
			time.Now(),
			[]string{"/app:app"},
			nil,
		)

		excluded.SetStale(true)
		serviceMatch.SetStale(true)
		containers := []*container.Container{excluded, serviceMatch, dependent}

		graph, err := sorter.NewDependencyGraph(log, containers, nil, false)
		require.NoError(t, err)

		got := Reconcile(
			log,
			graph,
			containers,
			containers,
			updateParams.Params{},
			map[container.ContainerID]struct{}{excluded.ID(): {}},
		)

		assert.False(t, dependent.IsLinkedToRestarting())
		assert.ElementsMatch(t, []*container.Container{serviceMatch}, got)
	})
}

// TestRebind verifies which containers are rebound to a recreated network
// namespace and where they are placed in the restart order.
func TestRebind(t *testing.T) {
	t.Parallel()

	// named builds a container with a name, an ID derived from it and an
	// optional network mode.
	named := func(name, networkMode string, updates ...containerfixture.Update) *container.Container {
		all := make([]containerfixture.Update, 0, 3+len(updates))
		all = append(all,
			containerfixture.WithName("/"+name),
			containerfixture.WithID(name+"-id"),
			containerfixture.WithNetworkMode(networkMode),
		)

		return modelfixture.Container(append(all, updates...)...)
	}

	names := func(containers []*container.Container) []string {
		names := make([]string, 0, len(containers))
		for _, c := range containers {
			names = append(names, c.Name())
		}

		return names
	}

	t.Run("places dependents after the container they join", func(t *testing.T) {
		t.Parallel()

		vpn := named("vpn", "bridge")
		db := named("db", "bridge")
		app := named("app", "container:vpn")
		stopped := named("stopped", "container:vpn-id")
		plan := []*container.Container{vpn, db}

		got := Rebind(newTestLogger(t), testGraph(t, plan, []*container.Container{app}),
			plan, []*container.Container{app, stopped, db, vpn}, nil)

		assert.Equal(t, []string{"vpn", "app", "stopped", "db"}, names(got))
		assert.True(t, app.IsRebinding())
		assert.True(t, stopped.IsRebinding())
		assert.False(t, vpn.IsRebinding())
		assert.False(t, db.IsRebinding())
		assert.Equal(t, "vpn", stopped.NetworkContainer(),
			"the removed container's ID is replaced by the name its replacement takes")
	})

	t.Run("rebinds a chain of namespaces", func(t *testing.T) {
		t.Parallel()

		vpn := named("vpn", "bridge")
		proxy := named("proxy", "container:vpn")
		app := named("app", "container:proxy")
		plan := []*container.Container{vpn}

		got := Rebind(newTestLogger(t), testGraph(t, plan, []*container.Container{app, proxy}),
			plan, []*container.Container{app, proxy}, nil)

		assert.Equal(t, []string{"vpn", "proxy", "app"}, names(got))
		assert.True(t, app.IsRebinding())
	})

	// Docker resolves a legacy link when it creates the container, so the
	// linked container must have been recreated first.
	t.Run("places a dependent after the containers its host config names", func(t *testing.T) {
		t.Parallel()

		vpn := named("vpn", "bridge")
		db := named("db", "bridge")
		app := named("app", "container:vpn", containerfixture.WithLinks([]string{"/db:/app/db"}))
		plan := []*container.Container{vpn, db}

		got := Rebind(newTestLogger(t), testGraph(t, plan, []*container.Container{app}),
			plan, []*container.Container{app}, nil)

		assert.Equal(t, []string{"vpn", "db", "app"}, names(got))
	})

	// A container is placed only once every container it needs is placed,
	// also when one of them was still waiting for its own dependencies.
	t.Run("places a dependent after a dependency that waited", func(t *testing.T) {
		t.Parallel()

		vpn := named("vpn", "bridge")
		db := named("db", "bridge")
		app := named("app", "container:vpn", containerfixture.WithLinks([]string{"/metrics:/app/metrics"}))
		metrics := named("metrics", "container:db")
		agent := named("agent", "container:db", containerfixture.WithLinks([]string{"/app:/agent/app"}))
		plan := []*container.Container{vpn, db}
		candidates := []*container.Container{app, metrics, agent}

		got := Rebind(newTestLogger(t), testGraph(t, plan, candidates), plan, candidates, nil)

		assert.Equal(t, []string{"vpn", "db", "metrics", "app", "agent"}, names(got))
	})

	// A monitor-only container is in the graph, so its labeled dependencies
	// order it too.
	t.Run("places a dependent after its dependencies in the graph", func(t *testing.T) {
		t.Parallel()

		vpn := named("vpn", "bridge")
		db := named("db", "bridge")
		app := named("app", "container:vpn",
			containerfixture.WithLabels(map[string]string{labels.DependsOn: "db"}))
		plan := []*container.Container{vpn, db}

		got := Rebind(newTestLogger(t), testGraph(t, []*container.Container{vpn, db, app}, nil),
			plan, []*container.Container{app}, nil)

		assert.Equal(t, []string{"vpn", "db", "app"}, names(got))
	})

	// The plan's sort cannot see a link to a container outside it, so a
	// container of the plan that joins a rebound container moves after it.
	t.Run("places a container of the plan after the rebound container it joins", func(t *testing.T) {
		t.Parallel()

		gateway := named("gateway", "bridge")
		proxy := named("proxy", "container:gateway")
		app := named("app", "container:proxy")
		plan := []*container.Container{app, gateway}

		got := Rebind(newTestLogger(t), testGraph(t, plan, []*container.Container{proxy}),
			plan, []*container.Container{proxy}, nil)

		assert.Equal(t, []string{"gateway", "proxy", "app"}, names(got))
		assert.True(t, proxy.IsRebinding())
		assert.False(t, app.IsRebinding())
	})

	// The scan keeps a network container's ID when it cannot inspect the
	// container, an ID that is gone once the container is recreated.
	t.Run("names the recreated container in the network mode", func(t *testing.T) {
		t.Parallel()

		vpn := named("vpn", "bridge")
		planned := named("planned", "container:vpn-id")
		rebound := named("rebound", "container:vpn-id")
		plan := []*container.Container{vpn, planned}

		Rebind(newTestLogger(t), testGraph(t, plan, []*container.Container{rebound}),
			plan, []*container.Container{rebound}, nil)

		assert.Equal(t, "vpn", planned.NetworkContainer())
		assert.Equal(t, "vpn", rebound.NetworkContainer())
	})

	t.Run("leaves out other containers", func(t *testing.T) {
		t.Parallel()

		vpn := named("vpn", "bridge")
		other := named("other", "container:db")
		watchtower := named("watchtower", "container:vpn",
			containerfixture.WithLabels(map[string]string{labels.Watchtower: "true"}))
		skipped := named("skipped", "container:vpn")
		planned := named("planned", "container:vpn")
		plan := []*container.Container{vpn, planned}

		got := Rebind(newTestLogger(t), testGraph(t, plan, nil),
			plan,
			[]*container.Container{other, watchtower, skipped, planned},
			map[container.ContainerID]struct{}{skipped.ID(): {}},
		)

		assert.Equal(t, []string{"vpn", "planned"}, names(got))
		assert.False(t, other.IsRebinding())
		assert.False(t, watchtower.IsRebinding())
		assert.False(t, skipped.IsRebinding())
		assert.False(t, planned.IsRebinding())
	})

	// The self-update replaces Watchtower's container after this process
	// hands over, too late to recreate a container against the replacement.
	t.Run("leaves the containers that join a Watchtower container", func(t *testing.T) {
		t.Parallel()

		watchtower := named("watchtower", "bridge",
			containerfixture.WithLabels(map[string]string{labels.Watchtower: "true"}))
		app := named("app", "container:watchtower")
		plan := []*container.Container{watchtower}

		got := Rebind(newTestLogger(t), testGraph(t, plan, nil), plan, []*container.Container{app}, nil)

		assert.Equal(t, []string{"watchtower"}, names(got))
		assert.False(t, app.IsRebinding())
	})
}
