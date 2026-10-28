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
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	"github.com/nicholas-fedor/watchtower/internal/update/sorter"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// testGraph returns the dependency graph of the given containers, all of them
// monitored.
func testGraph(t *testing.T, containers []types.Container) *sorter.DependencyGraph {
	t.Helper()

	graph, err := sorter.NewDependencyGraph(newTestLogger(t), containers, nil, false)
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
func (s restartSpec) build() types.Container {
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

			var all, monitored, unmonitored []types.Container

			excluded := map[types.ContainerID]struct{}{}

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
				if c.(*container.Container).IsLinkedToRestarting() {
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
		containers := []types.Container{app, db}
		markImplicitRestarts(log, testGraph(t, containers), containers, containers, nil)
		assert.True(t, db.IsLinkedToRestarting())

		app.SetStale(false)

		got := Reconcile(log, testGraph(t, containers), containers, containers, types.UpdateParams{}, nil)

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
		containers := []types.Container{app, db}
		got := Reconcile(log, testGraph(t, containers), containers, containers, types.UpdateParams{}, nil)

		assert.True(t, db.IsLinkedToRestarting())
		assert.ElementsMatch(t, []types.Container{app, db}, got)
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
		containers := []types.Container{app, dependent}

		graph, err := sorter.NewDependencyGraph(log, containers, nil, false)
		require.NoError(t, err)

		got := Reconcile(
			log,
			graph,
			containers,
			containers,
			types.UpdateParams{NoRestart: true},
			// The update excludes the containers Git built without a restart.
			map[types.ContainerID]struct{}{app.ID(): {}},
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
		containers := []types.Container{excluded, serviceMatch, dependent}

		graph, err := sorter.NewDependencyGraph(log, containers, nil, false)
		require.NoError(t, err)

		got := Reconcile(
			log,
			graph,
			containers,
			containers,
			types.UpdateParams{},
			map[types.ContainerID]struct{}{excluded.ID(): {}},
		)

		assert.False(t, dependent.IsLinkedToRestarting())
		assert.ElementsMatch(t, []types.Container{serviceMatch}, got)
	})
}
