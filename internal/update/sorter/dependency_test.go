package sorter

import (
	"fmt"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"

	composeLabels "github.com/nicholas-fedor/watchtower/internal/compose/labels"
	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/testutil/logcapture"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

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

// newNopLogger returns a logger that discards every event, for benchmarks and
// fuzz targets.
//
// Returns:
//   - *zerolog.Logger: Logger that discards all events.
func newNopLogger() *zerolog.Logger {
	return new(zerolog.Nop())
}

// Test_dependencySorter verifies that containers are sorted after the containers
// they depend on, through volumes-from, network_mode, Compose depends_on, and
// Watchtower depends-on links, with Watchtower containers last, and that a
// circular dependency is an error.
func Test_dependencySorter(t *testing.T) {
	t.Parallel()

	t.Run("Sort", func(t *testing.T) {
		t.Parallel()

		t.Run("should sort a volumes-from dependent after a source referenced by Docker ID", func(t *testing.T) {
			t.Parallel()

			const appID = "9b738d6a78250d731816cc5020d8cfaf55e605cd510f4d4662688bd9a6c4e0c7"

			app := deployed{name: "nextcloud-app", id: appID, project: "nextcloud", service: "app"}.container()
			nginx := deployed{
				name:        "nextcloud-nginx",
				id:          "nginx-id",
				project:     "nextcloud",
				service:     "web",
				volumesFrom: []string{appID + ":rw"},
			}.container()

			containers := []types.Container{nginx, app}
			err := dependencySorter{}.Sort(newTestLogger(t), containers, false)
			require.NoError(t, err)
			assert.Equal(t, "nextcloud-app", containers[0].Name())
			assert.Equal(t, "nextcloud-nginx", containers[1].Name())
		})

		t.Run("should sort a volumes-from dependent after a source in another Compose project", func(t *testing.T) {
			t.Parallel()

			const appID = "9b738d6a78250d731816cc5020d8cfaf55e605cd510f4d4662688bd9a6c4e0c7"

			app := deployed{name: "shared-app", id: appID, project: "data", service: "app"}.container()
			nginx := deployed{
				name:        "nextcloud-nginx",
				id:          "nginx-id",
				project:     "nextcloud",
				service:     "web",
				volumesFrom: []string{appID + ":rw"},
			}.container()

			containers := []types.Container{nginx, app}
			err := dependencySorter{}.Sort(newTestLogger(t), containers, false)
			require.NoError(t, err)
			assert.Equal(t, "shared-app", containers[0].Name())
			assert.Equal(t, "nextcloud-nginx", containers[1].Name())
		})

		t.Run("should sort a volumes-from chain after compose depends_on", func(t *testing.T) {
			t.Parallel()

			const appID = "9b738d6a78250d731816cc5020d8cfaf55e605cd510f4d4662688bd9a6c4e0c7"

			postgres := deployed{name: "nextcloud-db", project: "nextcloud", service: "db"}.container()
			app := deployed{
				name:             "nextcloud-app",
				id:               appID,
				project:          "nextcloud",
				service:          "app",
				composeDependsOn: "db",
			}.container()
			nginx := deployed{
				name:        "nextcloud-nginx",
				id:          "nginx-id",
				project:     "nextcloud",
				service:     "web",
				volumesFrom: []string{appID + ":rw"},
			}.container()

			containers := []types.Container{nginx, app, postgres}
			err := dependencySorter{}.Sort(newTestLogger(t), containers, true)
			require.NoError(t, err)
			assert.Equal(t, "nextcloud-db", containers[0].Name())
			assert.Equal(t, "nextcloud-app", containers[1].Name())
			assert.Equal(t, "nextcloud-nginx", containers[2].Name())
		})

		t.Run("should sort a volumes-from chain after watchtower depends-on", func(t *testing.T) {
			t.Parallel()

			const appID = "9b738d6a78250d731816cc5020d8cfaf55e605cd510f4d4662688bd9a6c4e0c7"

			postgres := deployed{name: "nextcloud-db", project: "nextcloud", service: "db"}.container()
			app := deployed{
				name:      "nextcloud-app",
				id:        appID,
				project:   "nextcloud",
				service:   "app",
				dependsOn: "nextcloud-db",
			}.container()
			nginx := deployed{
				name:        "nextcloud-nginx",
				id:          "nginx-id",
				project:     "nextcloud",
				service:     "web",
				volumesFrom: []string{appID + ":rw"},
			}.container()

			containers := []types.Container{nginx, app, postgres}
			err := dependencySorter{}.Sort(newTestLogger(t), containers, false)
			require.NoError(t, err)
			assert.Equal(t, "nextcloud-db", containers[0].Name())
			assert.Equal(t, "nextcloud-app", containers[1].Name())
			assert.Equal(t, "nextcloud-nginx", containers[2].Name())
		})

		t.Run("should sort a network_mode dependent after a provider referenced by Docker ID", func(t *testing.T) {
			t.Parallel()

			const providerID = "25e75393800b5c450a6841212a3b92ed28fa35414a586dec9f2c8a520d4910c2"

			gluetun := deployed{name: "gluetun", id: providerID, project: "gluetun", service: "vpn"}.container()
			qbittorrent := deployed{name: "qbittorrent", project: "qbittorrent", service: "app", networkMode: "container:" + providerID}.container()

			containers := []types.Container{qbittorrent, gluetun}
			err := dependencySorter{}.Sort(newTestLogger(t), containers, false)
			require.NoError(t, err)
			assert.Equal(t, "gluetun", containers[0].Name())
			assert.Equal(t, "qbittorrent", containers[1].Name())
		})

		t.Run("should resolve container ID network_mode to the ID owner when a peer reuses that ID as its name", func(t *testing.T) {
			t.Parallel()

			const providerID = "25e75393800b5c450a6841212a3b92ed28fa35414a586dec9f2c8a520d4910c2"

			gluetun := deployed{name: "gluetun", id: providerID, project: "gluetun", service: "vpn"}.container()
			decoy := deployed{name: providerID, project: "other", service: "svc"}.container()
			qbittorrent := deployed{name: "qbittorrent", project: "qbittorrent", service: "app", networkMode: "container:" + providerID}.container()

			_, indegree, adjacency, _, err := buildDependencyGraph(
				newTestLogger(t),
				[]types.Container{qbittorrent, decoy, gluetun},
				false,
			)
			require.NoError(t, err)
			assert.Equal(t, 1, indegree["qbittorrent-app"])
			assert.Contains(t, adjacency["gluetun-vpn"], "qbittorrent-app")
			assert.NotContains(t, adjacency["other-svc"], "qbittorrent-app")
		})

		t.Run("should sort containers with no dependencies", func(t *testing.T) {
			t.Parallel()

			c1 := deployed{name: "c1"}.container()
			c2 := deployed{name: "c2"}.container()
			c3 := deployed{name: "c3"}.container()
			containers := []types.Container{c1, c2, c3}
			ds := dependencySorter{}
			err := ds.Sort(newTestLogger(t), containers, true)
			require.NoError(t, err)
			require.Len(t, containers, 3)
			// Order may vary since no dependencies
		})

		t.Run("should sort containers with simple dependencies", func(t *testing.T) {
			t.Parallel()

			c1 := deployed{name: "c1", dependsOn: "c2"}.container()
			c2 := deployed{name: "c2"}.container()
			containers := []types.Container{c1, c2}
			ds := dependencySorter{}
			err := ds.Sort(newTestLogger(t), containers, true)
			require.NoError(t, err)
			require.Len(t, containers, 2)
			assert.Equal(t, "c2", containers[0].Name())
			assert.Equal(t, "c1", containers[1].Name())
		})

		t.Run("should sort containers with complex dependency chains", func(t *testing.T) {
			t.Parallel()

			c1 := deployed{name: "c1", dependsOn: "c2,c3"}.container()
			c2 := deployed{name: "c2", dependsOn: "c3"}.container()
			c3 := deployed{name: "c3"}.container()
			containers := []types.Container{c1, c2, c3}
			ds := dependencySorter{}
			err := ds.Sort(newTestLogger(t), containers, true)
			require.NoError(t, err)
			require.Len(t, containers, 3)
			assert.Equal(t, "c3", containers[0].Name())
			assert.Equal(t, "c2", containers[1].Name())
			assert.Equal(t, "c1", containers[2].Name())
		})

		t.Run("should detect circular dependencies", func(t *testing.T) {
			t.Parallel()

			c1 := deployed{name: "c1", dependsOn: "c2"}.container()
			c2 := deployed{name: "c2", dependsOn: "c1"}.container()
			containers := []types.Container{c1, c2}
			ds := dependencySorter{}
			err := ds.Sort(newTestLogger(t), containers, true)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "circular reference detected")
			assert.Contains(t, err.Error(), "c1 -> c2 -> c1")
		})

		t.Run("should ignore Compose depends_on but honor Watchtower labels when useComposeDependsOn is false", func(t *testing.T) {
			t.Parallel()

			// c1 names c2 in its Watchtower depends-on label. c2's Compose
			// depends_on label names c1, which would make a cycle if it were read.
			c1 := deployed{name: "c1", project: "stack", service: "c1", dependsOn: "c2"}.container()
			c2 := deployed{name: "c2", project: "stack", service: "c2", composeDependsOn: "c1"}.container()

			containers := []types.Container{c1, c2}
			ds := dependencySorter{}
			err := ds.Sort(newTestLogger(t), containers, false)
			require.NoError(t, err)
			require.Len(t, containers, 2)
			// c1 depends on c2, so c2 must come before c1
			assert.Equal(t, "c2", containers[0].Name())
			assert.Equal(t, "c1", containers[1].Name())
		})

		t.Run("should place Watchtower containers last", func(t *testing.T) {
			t.Parallel()

			watchtower := deployed{name: "watchtower", watchtower: true}.container()
			c1 := deployed{name: "c1", dependsOn: "c2"}.container()
			c2 := deployed{name: "c2"}.container()
			containers := []types.Container{watchtower, c1, c2}
			ds := dependencySorter{}
			err := ds.Sort(newTestLogger(t), containers, true)
			require.NoError(t, err)
			require.Len(t, containers, 3)
			assert.Equal(t, "c2", containers[0].Name())
			assert.Equal(t, "c1", containers[1].Name())
			assert.Equal(t, "watchtower", containers[2].Name())
		})

		t.Run("should handle empty container list", func(t *testing.T) {
			t.Parallel()

			containers := []types.Container{}
			ds := dependencySorter{}
			err := ds.Sort(newTestLogger(t), containers, true)
			require.NoError(t, err)
			assert.Empty(t, containers)
		})

		t.Run("should handle single container", func(t *testing.T) {
			t.Parallel()

			c1 := deployed{name: "c1"}.container()
			containers := []types.Container{c1}
			ds := dependencySorter{}
			err := ds.Sort(newTestLogger(t), containers, true)
			require.NoError(t, err)
			require.Len(t, containers, 1)
			assert.Equal(t, "c1", containers[0].Name())
		})
	})

	t.Run("sortByDependencies", func(t *testing.T) {
		t.Parallel()

		t.Run("should sort containers with no dependencies", func(t *testing.T) {
			t.Parallel()

			c1 := deployed{name: "c1"}.container()
			c2 := deployed{name: "c2"}.container()
			containers := []types.Container{c1, c2}
			result, err := sortByDependencies(newTestLogger(t), containers, true)
			require.NoError(t, err)
			require.Len(t, result, 2)
		})

		t.Run("should sort containers with simple dependencies", func(t *testing.T) {
			t.Parallel()

			c1 := deployed{name: "c1", dependsOn: "c2"}.container()
			c2 := deployed{name: "c2"}.container()
			containers := []types.Container{c1, c2}
			result, err := sortByDependencies(newTestLogger(t), containers, true)
			require.NoError(t, err)
			require.Len(t, result, 2)
			assert.Equal(t, "c2", result[0].Name())
			assert.Equal(t, "c1", result[1].Name())
		})

		t.Run("should sort containers with complex chains", func(t *testing.T) {
			t.Parallel()

			a := deployed{name: "a", dependsOn: "b"}.container()
			b := deployed{name: "b", dependsOn: "c"}.container()
			c := deployed{name: "c"}.container()
			containers := []types.Container{a, b, c}
			result, err := sortByDependencies(newTestLogger(t), containers, true)
			require.NoError(t, err)
			require.Len(t, result, 3)
			assert.Equal(t, "c", result[0].Name())
			assert.Equal(t, "b", result[1].Name())
			assert.Equal(t, "a", result[2].Name())
		})

		t.Run("should detect circular dependencies", func(t *testing.T) {
			t.Parallel()

			c1 := deployed{name: "c1", dependsOn: "c2"}.container()
			c2 := deployed{name: "c2", dependsOn: "c1"}.container()
			containers := []types.Container{c1, c2}
			_, err := sortByDependencies(newTestLogger(t), containers, true)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "circular reference detected")
			assert.Contains(t, err.Error(), "c1 -> c2 -> c1")
		})

		t.Run("should handle empty list", func(t *testing.T) {
			t.Parallel()

			containers := []types.Container{}
			result, err := sortByDependencies(newTestLogger(t), containers, true)
			require.NoError(t, err)
			assert.Empty(t, result)
		})

		t.Run("should handle single container", func(t *testing.T) {
			t.Parallel()

			c1 := deployed{name: "c1"}.container()
			containers := []types.Container{c1}
			result, err := sortByDependencies(newTestLogger(t), containers, true)
			require.NoError(t, err)
			require.Len(t, result, 1)
			assert.Equal(t, "c1", result[0].Name())
		})

		t.Run("should handle disconnected components", func(t *testing.T) {
			t.Parallel()

			// Component 1: A -> B
			a := deployed{name: "a", dependsOn: "b"}.container()
			b := deployed{name: "b"}.container()

			// Component 2: C -> D
			c := deployed{name: "c", dependsOn: "d"}.container()
			d := deployed{name: "d"}.container()

			containers := []types.Container{a, b, c, d}
			result, err := sortByDependencies(newTestLogger(t), containers, true)
			require.NoError(t, err)
			require.Len(t, result, 4)

			resultNames := make([]string, len(result))
			for i, c := range result {
				resultNames[i] = c.Name()
			}

			assertOrderBefore(t, resultNames, "b", "a")
			assertOrderBefore(t, resultNames, "d", "c")
		})

		t.Run("should sort a container whose depends-on label names itself without a cycle", func(t *testing.T) {
			t.Parallel()

			// The container model drops the link to the container itself. The
			// replica prefix cases below test the sorter's own guard.
			c1 := deployed{name: "c1", dependsOn: "c1"}.container()

			containers := []types.Container{c1}
			result, err := sortByDependencies(newTestLogger(t), containers, true)
			require.NoError(t, err)
			// Self-reference is filtered, not a cycle
			require.Len(t, result, 1)
			assert.Equal(t, "c1", result[0].Name())
		})

		t.Run("should skip self-reference via prefix matching for replica-named containers", func(t *testing.T) {
			t.Parallel()

			// Container named "myapp-1" linking to "myapp" should NOT create a self-dependency
			// The prefix matching would match "myapp-1" when looking for "myapp" replicas,
			// but the self-reference guard should skip it
			myapp := deployed{name: "myapp-1", dependsOn: "myapp"}.container()

			containers := []types.Container{myapp}
			containerMap, indegree, adjacency, _, err := buildDependencyGraph(newTestLogger(t), containers, true)
			require.NoError(t, err)
			require.Len(t, containerMap, 1)

			// Self-reference via prefix matching should be skipped
			// indegree["myapp-1"] should be 0 (not incremented)
			assert.Equal(t, 0, indegree["myapp-1"])
			// adjacency["myapp-1"] should NOT contain "myapp-1"
			assert.NotContains(t, adjacency["myapp-1"], "myapp-1")
		})

		t.Run("should skip self-reference via prefix matching for db replica-named containers", func(t *testing.T) {
			t.Parallel()

			// Container named "db-1" linking to "db" should NOT create a self-dependency
			// Similar to the myapp case, this tests another common naming pattern
			db := deployed{name: "db-1", dependsOn: "db"}.container()

			containers := []types.Container{db}
			containerMap, indegree, adjacency, _, err := buildDependencyGraph(newTestLogger(t), containers, true)
			require.NoError(t, err)
			require.Len(t, containerMap, 1)

			// Self-reference via prefix matching should be skipped
			// indegree["db-1"] should be 0 (not incremented)
			assert.Equal(t, 0, indegree["db-1"])
			// adjacency["db-1"] should NOT contain "db-1"
			assert.NotContains(t, adjacency["db-1"], "db-1")
		})

		t.Run("should handle containers with project-prefixed names and service links", func(t *testing.T) {
			t.Parallel()

			// test-db-1 (no dependencies)
			db := deployed{name: "test-db-1"}.container()

			// app-1 depends on "db"
			app := deployed{name: "app-1", dependsOn: "db"}.container()

			// test-web-1 depends on "app"
			web := deployed{name: "test-web-1", dependsOn: "app"}.container()

			containers := []types.Container{web, app, db} // Unsorted order
			result, err := sortByDependencies(newTestLogger(t), containers, true)
			require.NoError(t, err)
			require.Len(t, result, 3)
			// Dependencies first: db, then app (depends on db), then web (depends on app)
			assert.Equal(t, "test-db-1", result[0].Name())
			assert.Equal(t, "app-1", result[1].Name())
			assert.Equal(t, "test-web-1", result[2].Name())
		})

		t.Run("should handle containers with service names", func(t *testing.T) {
			t.Parallel()

			c1 := deployed{name: "container1", dependsOn: "web"}.container()
			c2 := deployed{name: "container2", service: "web"}.container()

			containers := []types.Container{c1, c2}
			result, err := sortByDependencies(newTestLogger(t), containers, true)
			require.NoError(t, err)
			require.Len(t, result, 2)
			assert.Equal(t, "container2", result[0].Name()) // web service
			assert.Equal(t, "container1", result[1].Name()) // depends on web
		})

		t.Run("should handle containers with replicas without collision", func(t *testing.T) {
			t.Parallel()

			c1 := deployed{name: "myproject-web-1", project: "myproject", service: "web"}.container()
			c2 := deployed{name: "myproject-web-2", project: "myproject", service: "web"}.container()

			containers := []types.Container{c1, c2}
			result, err := sortByDependencies(newTestLogger(t), containers, true)
			require.NoError(t, err)
			require.Len(t, result, 2)
			// Order may vary since no dependencies
		})

		t.Run("should handle prefix matching linking service name to multiple replicas deterministically", func(t *testing.T) {
			t.Parallel()

			// App container that depends on "db" service
			app := deployed{name: "app", dependsOn: "db"}.container()

			// Multiple db replicas
			db1 := deployed{name: "db-1"}.container()
			db2 := deployed{name: "db-2"}.container()
			db3 := deployed{name: "db-3"}.container()

			containers := []types.Container{app, db1, db2, db3}
			containerMap, indegree, adjacency, _, err := buildDependencyGraph(newTestLogger(t), containers, true)
			require.NoError(t, err)
			require.Len(t, containerMap, 4)

			// Verify app depends on all db replicas
			assert.Equal(t, 3, indegree["app"])
			assert.Contains(t, adjacency["db-1"], "app")
			assert.Contains(t, adjacency["db-2"], "app")
			assert.Contains(t, adjacency["db-3"], "app")

			// Verify db replicas have no dependencies
			assert.Equal(t, 0, indegree["db-1"])
			assert.Equal(t, 0, indegree["db-2"])
			assert.Equal(t, 0, indegree["db-3"])
		})

		t.Run("should handle containers with service names containing leading slashes", func(t *testing.T) {
			t.Parallel()

			// Test that containers with service names containing leading slashes are handled correctly
			c1 := deployed{name: "container1", dependsOn: "web-service"}.container()
			c2 := deployed{name: "container2", service: "/web-service"}.container()

			containers := []types.Container{c1, c2}
			result, err := sortByDependencies(newTestLogger(t), containers, true)
			require.NoError(t, err)
			require.Len(t, result, 2)
			assert.Equal(t, "container2", result[0].Name())
			// web-service container first
			assert.Equal(t, "container1", result[1].Name()) // depends on web-service
		})

		t.Run("should handle containers with container names containing leading slashes", func(t *testing.T) {
			t.Parallel()

			// Test containers with container names containing leading slashes
			c1 := deployed{name: "container1", dependsOn: "/web"}.container()
			c2 := deployed{name: "web"}.container()

			containers := []types.Container{c1, c2}
			result, err := sortByDependencies(newTestLogger(t), containers, true)
			require.NoError(t, err)
			require.Len(t, result, 2)
			assert.Equal(t, "web", result[0].Name())
			// web container first
			assert.Equal(t, "container1", result[1].Name()) // depends on web
		})

		t.Run("should handle containers with empty names falling back to IDs", func(t *testing.T) {
			t.Parallel()

			// Test containers with empty names that fall back to container IDs
			c1 := deployed{name: "", id: "id-c1", dependsOn: "id-c2"}.container()
			c2 := deployed{name: "", id: "id-c2"}.container()

			containers := []types.Container{c1, c2}
			result, err := sortByDependencies(newTestLogger(t), containers, true)
			require.NoError(t, err)
			require.Len(t, result, 2)
			assert.Equal(t, types.ContainerID("id-c2"), result[0].ID())
			// dependency first
			assert.Equal(t, types.ContainerID("id-c1"), result[1].ID())
			// dependent second
		})

		t.Run("should handle containers with malformed link targets", func(t *testing.T) {
			t.Parallel()

			// Test containers with links to non-existent containers
			c1 := deployed{name: "container1", dependsOn: "non-existent,/also-non-existent"}.container()
			c2 := deployed{name: "container2"}.container()

			containers := []types.Container{c1, c2}
			result, err := sortByDependencies(newTestLogger(t), containers, true)
			require.NoError(t, err)
			require.Len(t, result, 2)
			// Order may vary since no valid dependencies
		})

		t.Run("should handle containers with network mode dependencies", func(t *testing.T) {
			t.Parallel()

			// container1 shares web's network namespace.
			c1 := deployed{name: "container1", networkMode: "container:web"}.container()
			c2 := deployed{name: "web"}.container()

			containers := []types.Container{c1, c2}
			result, err := sortByDependencies(newTestLogger(t), containers, true)
			require.NoError(t, err)
			require.Len(t, result, 2)
			assert.Equal(t, "web", result[0].Name())        // web container first
			assert.Equal(t, "container1", result[1].Name()) // depends on web
		})

		t.Run("should handle containers with mixed dependency sources", func(t *testing.T) {
			t.Parallel()

			// app names db in its depends-on label and links to cache. It
			// shares redis's network namespace and mounts proxy's volumes.
			c1 := deployed{
				name:        "app",
				dependsOn:   "db",
				links:       []string{"/cache:/app/cache"},
				networkMode: "container:redis",
				volumesFrom: []string{"proxy"},
			}.container()

			c2 := deployed{name: "db"}.container()
			c3 := deployed{name: "cache"}.container()
			c4 := deployed{name: "redis"}.container()
			c5 := deployed{name: "proxy"}.container()

			containers := []types.Container{c1, c2, c3, c4, c5}

			// The sort places app last even when one source is dropped, so
			// each source must count toward app's dependencies.
			_, indegree, _, _, err := buildDependencyGraph(newTestLogger(t), containers, true)
			require.NoError(t, err)
			assert.Equal(t, 4, indegree["app"])

			result, err := sortByDependencies(newTestLogger(t), containers, true)
			require.NoError(t, err)
			require.Len(t, result, 5)
			// Dependencies should come before dependents
			resultNames := make([]string, len(result))
			for i, c := range result {
				resultNames[i] = c.Name()
			}
			// app should be last
			assert.Equal(t, "app", resultNames[len(resultNames)-1])
		})

		t.Run("should handle large number of containers with no dependencies", func(t *testing.T) {
			t.Parallel()

			// Test performance and correctness with many containers
			containers := make([]types.Container, 100)

			for i := range 100 {
				containers[i] = deployed{name: fmt.Sprintf("container%d", i)}.container()
			}

			result, err := sortByDependencies(newTestLogger(t), containers, true)
			require.NoError(t, err)
			require.Len(t, result, 100)
		})

		t.Run("should handle containers with empty identifiers", func(t *testing.T) {
			t.Parallel()

			// This tests that containers with empty names fall back to using container ID as identifier
			c1 := deployed{name: "", id: "id-c1"}.container()
			c2 := deployed{name: "", id: "id-c2"}.container()

			containers := []types.Container{c1, c2}
			result, err := sortByDependencies(newTestLogger(t), containers, true)
			require.NoError(t, err) // Should not detect false cycle
			require.Len(t, result, 2)
		})

		t.Run("should resolve a service name shared by two projects to both", func(t *testing.T) {
			t.Parallel()

			// app depending on "db"
			app := deployed{name: "app", dependsOn: "db"}.container()

			// db1 from project1
			db1 := deployed{name: "project1_db_1", project: "project1", service: "db"}.container()

			// db2 from project2
			db2 := deployed{name: "project2_db_1", project: "project2", service: "db"}.container()

			containers := []types.Container{app, db1, db2}

			containerMap, indegree, adjacency, _, err := buildDependencyGraph(newTestLogger(t), containers, true)
			require.NoError(t, err)

			// "db" names the db service of both projects, so app depends on each.
			require.Len(t, containerMap, 3)
			assert.Equal(t, 2, indegree["app"])
			assert.Equal(t, map[string][]string{
				"project1-db": {"app"},
				"project2-db": {"app"},
			}, adjacency)
		})

		t.Run("should detect circular dependencies via watchtower labels", func(t *testing.T) {
			t.Parallel()

			c1 := deployed{name: "c1", dependsOn: "c2"}.container()
			c2 := deployed{name: "c2", dependsOn: "c1"}.container()

			containers := []types.Container{c1, c2}

			_, err := sortByDependencies(newTestLogger(t), containers, true)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "circular reference detected")
			assert.Contains(t, err.Error(), "c1 -> c2 -> c1")
		})

		t.Run("should handle dependencies to filtered containers", func(t *testing.T) {
			t.Parallel()

			app := deployed{name: "app", dependsOn: "db"}.container()

			// Only app in containers list, db not included
			containers := []types.Container{app}

			containerMap, indegree, _, _, err := buildDependencyGraph(newTestLogger(t), containers, true)
			require.NoError(t, err)
			require.Len(t, containerMap, 1)
			assert.Equal(t, 0, indegree["app"])
		})
	})
}

// TestPrefixMatchingIssues verifies that a link matches Compose replicas by their
// numeric suffix, and not containers whose names merely share a prefix.
func TestPrefixMatchingIssues(t *testing.T) {
	t.Parallel()

	t.Run("should not match containers with similar names that are not Docker Compose replicas (issue-1161)", func(t *testing.T) {
		t.Parallel()

		// App container that depends on "watchtower-test-database"
		app := deployed{name: "watchtower-test-app1", dependsOn: "watchtower-test-database"}.container()

		// First database container (exact match)
		db1 := deployed{name: "watchtower-test-database"}.container()

		// Second database container with similar name (should NOT be matched)
		db2 := deployed{name: "watchtower-test-database2"}.container()

		containers := []types.Container{app, db1, db2}
		containerMap, indegree, adjacency, _, err := buildDependencyGraph(newTestLogger(t), containers, true)
		require.NoError(t, err)
		require.Len(t, containerMap, 3)

		// Verify app depends ONLY on watchtower-test-database, not on watchtower-test-database2
		assert.Equal(t, 1, indegree["watchtower-test-app1"])
		assert.Contains(t, adjacency["watchtower-test-database"], "watchtower-test-app1")
		assert.Empty(t, adjacency["watchtower-test-database2"])

		// Verify db1 and db2 have no dependencies
		assert.Equal(t, 0, indegree["watchtower-test-database"])
		assert.Equal(t, 0, indegree["watchtower-test-database2"])
	})

	t.Run("should match Docker Compose-style numeric replica suffixes", func(t *testing.T) {
		t.Parallel()

		// App container that depends on "myapp-db"
		app := deployed{name: "myapp-app1", dependsOn: "myapp-db"}.container()

		// Database replicas with numeric suffixes (should be matched)
		db1 := deployed{name: "myapp-db-1"}.container()
		db2 := deployed{name: "myapp-db-2"}.container()

		containers := []types.Container{app, db1, db2}
		containerMap, indegree, adjacency, _, err := buildDependencyGraph(newTestLogger(t), containers, true)
		require.NoError(t, err)
		require.Len(t, containerMap, 3)

		// Verify app depends on BOTH db replicas (since they have numeric suffixes)
		assert.Equal(t, 2, indegree["myapp-app1"])
		assert.Contains(t, adjacency["myapp-db-1"], "myapp-app1")
		assert.Contains(t, adjacency["myapp-db-2"], "myapp-app1")
	})
}

// TestIdentifierCollisionIssues verifies that containers resolving to the same
// identifier are reported as an identifier collision when the graph is built
// and when containers are sorted, and that distinct identifiers sort normally.
func TestIdentifierCollisionIssues(t *testing.T) {
	t.Parallel()

	t.Run("ResolveContainerIdentifier", func(t *testing.T) {
		t.Parallel()

		t.Run("should return different identifiers for containers from different projects with same service name", func(t *testing.T) {
			t.Parallel()

			// Container from project "app1" with service "web"
			first := deployed{
				name:    "app1_web_1",
				project: "app1",
				service: "web",
				labels:  map[string]string{"com.docker.compose.version": "3.8", "com.docker.compose.config-hash": "abc123"},
			}.container()

			// Container from project "app2" with same service "web"
			second := deployed{
				name:    "app2_web_1",
				project: "app2",
				service: "web",
				labels:  map[string]string{"com.docker.compose.version": "3.8", "com.docker.compose.config-hash": "def456"},
			}.container()

			result1 := container.ResolveContainerIdentifier(first)
			result2 := container.ResolveContainerIdentifier(second)

			// Containers from different projects should return different identifiers
			assert.Equal(t, "app1-web", result1)
			assert.Equal(t, "app2-web", result2)
			assert.NotEqual(t, result2, result1)
		})
	})

	t.Run("buildDependencyGraph", func(t *testing.T) {
		t.Parallel()

		t.Run("should treat containers from different projects as separate entities in dependency graph", func(t *testing.T) {
			t.Parallel()

			// Create two containers from different projects with same service name
			c1 := deployed{name: "app1_web_1", project: "app1", service: "web"}.container()
			c2 := deployed{name: "app2_web_1", project: "app2", service: "web"}.container()

			containers := []types.Container{c1, c2}

			containerMap, indegree, _, normalizedMap, err := buildDependencyGraph(newTestLogger(t), containers, true)
			require.NoError(t, err)

			// Containers from different projects should have different identifiers
			ident1 := container.ResolveContainerIdentifier(c1)
			ident2 := container.ResolveContainerIdentifier(c2)

			assert.Equal(t, "app1-web", ident1)
			assert.Equal(t, "app2-web", ident2)

			// Both containers should be in the map as separate entities
			require.Len(t, containerMap, 2)
			assert.Equal(t, c1, containerMap["app1-web"])
			assert.Equal(t, c2, containerMap["app2-web"])

			// Verify indegree reflects separate entities
			require.Len(t, indegree, 2)
			assert.Equal(t, 0, indegree["app1-web"])
			assert.Equal(t, 0, indegree["app2-web"])

			// The normalizedMap should show containers map to their respective identifiers
			require.Len(t, normalizedMap, 2)
			assert.Equal(t, "app1-web", normalizedMap[c1])
			assert.Equal(t, "app2-web", normalizedMap[c2])
		})

		t.Run("should maintain backward compatibility for containers without project labels", func(t *testing.T) {
			t.Parallel()

			// Container with service label but no project label
			c := deployed{name: "", service: "web"}.container()

			result := container.ResolveContainerIdentifier(c)

			// Should return just the service name, same as before
			assert.Equal(t, "web", result)
		})

		t.Run("should support exact matching for cross-project dependencies", func(t *testing.T) {
			t.Parallel()

			// Container that links to exact container name
			app := deployed{name: "app", dependsOn: "project1-db"}.container()

			// DB container from project1
			db1 := deployed{name: "project1_db_1", project: "project1", service: "db"}.container()

			containers := []types.Container{app, db1}

			containerMap, indegree, adjacency, normalizedMap, err := buildDependencyGraph(newTestLogger(t), containers,
				true,
			)
			require.NoError(t, err)

			// Verify identifiers
			assert.Equal(t, "app", container.ResolveContainerIdentifier(app))
			assert.Equal(t, "project1-db", container.ResolveContainerIdentifier(db1))

			// All containers should be in the map
			require.Len(t, containerMap, 2)

			// app should have indegree 1 (depends on db1)
			assert.Equal(t, 1, indegree["app"])

			// db1 should have app as dependent
			assert.Contains(t, adjacency["project1-db"], "app")

			// Verify normalizedMap
			require.Len(t, normalizedMap, 2)
		})
	})

	t.Run("identifierCollisionError", func(t *testing.T) {
		t.Parallel()

		t.Run("should format error message correctly", func(t *testing.T) {
			t.Parallel()

			c1 := deployed{name: "container1", id: "id1"}.container()
			c2 := deployed{name: "container2", id: "id2"}.container()

			containers := []types.Container{c1, c2}

			err := identifierCollisionError{
				DuplicateIdentifier: "test-id",
				AffectedContainers:  containers,
			}

			expected := "identifier collision detected: 'test-id' used by containers: container1 (id1), container2 (id2)"
			assert.Equal(t, expected, err.Error())
		})
	})

	t.Run("buildDependencyGraph with collisions", func(t *testing.T) {
		t.Parallel()

		t.Run("should return identifierCollisionError when containers have same normalized identifier", func(t *testing.T) {
			t.Parallel()

			c1 := deployed{name: "c1", id: "id1"}.container()
			c2 := deployed{name: "c1", id: "id2"}.container()

			containers := []types.Container{c1, c2}

			_, _, _, _, err := buildDependencyGraph(newTestLogger(t), containers, true)

			var collisionErr identifierCollisionError
			require.ErrorAs(t, err, &collisionErr)
			assert.Equal(t, "c1", collisionErr.DuplicateIdentifier)
			assert.Len(t, collisionErr.AffectedContainers, 2)
		})

		t.Run("should succeed when containers have different identifiers", func(t *testing.T) {
			t.Parallel()

			c1 := deployed{name: "c1"}.container()
			c2 := deployed{name: "c2"}.container()

			containers := []types.Container{c1, c2}

			containerMap, indegree, adjacency, normalizedMap, err := buildDependencyGraph(newTestLogger(t), containers,
				true,
			)

			require.NoError(t, err)
			require.Len(t, containerMap, 2)
			require.Len(t, indegree, 2)
			assert.Empty(t, adjacency) // No links set up
			require.Len(t, normalizedMap, 2)
		})
	})

	t.Run("sortByDependencies with collisions", func(t *testing.T) {
		t.Parallel()

		t.Run("should propagate identifierCollisionError from buildDependencyGraph", func(t *testing.T) {
			t.Parallel()

			c1 := deployed{name: "c1", id: "id1"}.container()
			c2 := deployed{name: "c1", id: "id2"}.container()

			containers := []types.Container{c1, c2}

			_, err := sortByDependencies(newTestLogger(t), containers, true)

			var collisionErr identifierCollisionError
			require.ErrorAs(t, err, &collisionErr)
		})
	})
}

// Test_identifierIndex_CollisionLogsAtDebug verifies that an identifier
// collision returns an identifierCollisionError and logs only at debug, since
// every caller reports the returned error itself. The debug line carries the
// identifier and the error naming both containers.
func Test_identifierIndex_CollisionLogsAtDebug(t *testing.T) {
	t.Parallel()

	containers := []types.Container{
		deployed{name: "app", id: "a000000000000000000000000000000000000000000000000000000000000000"}.container(),
		deployed{name: "app", id: "b000000000000000000000000000000000000000000000000000000000000000"}.container(),
	}

	log, output := logcapture.New(zerolog.DebugLevel)

	index, err := identifierIndex(log, containers)
	require.Nil(t, index)

	var collisionErr identifierCollisionError
	require.ErrorAs(t, err, &collisionErr)

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	require.Len(t, lines, 1)
	assert.Contains(t, lines[0], "level=debug")
	assert.Contains(t, lines[0], `msg="Identifier collision in the dependency graph"`)
	assert.Contains(t, lines[0], "identifier=app")
	assert.Contains(t, lines[0], "app (a00000000000)")
	assert.Contains(t, lines[0], "app (b00000000000)")
}

// Test_isPositiveInteger verifies which replica suffixes count as positive
// integers.
func Test_isPositiveInteger(t *testing.T) {
	t.Parallel()

	t.Run("validates positive integers correctly", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name     string
			input    string
			expected bool
		}{
			{name: "should return true for single digit positive integer", input: "1", expected: true},
			{name: "should return true for multi-digit positive integer", input: "123", expected: true},
			{name: "should return true for large positive integer", input: "999", expected: true},
			{name: "should return true for zero-prefixed positive integer", input: "01", expected: true},
			{name: "should return false for zero", input: "0", expected: false},
			{name: "should return false for negative number", input: "-1", expected: false},
			{name: "should return false for alphabetic string", input: "abc", expected: false},
			{name: "should return false for alphanumeric string", input: "1a", expected: false},
			{name: "should return false for empty string", input: "", expected: false},
			{name: "should return false for decimal number", input: "1.5", expected: false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				input, expected := tt.input, tt.expected

				result := isPositiveInteger(input)
				assert.Equal(t, expected, result)
			})
		}
	})
}

// Test_extractServiceName verifies the service name taken from a container
// identifier.
func Test_extractServiceName(t *testing.T) {
	t.Parallel()

	t.Run("extracts service name from container identifier", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name     string
			input    string
			expected string
		}{
			{name: "should return simple identifier without project prefix as-is", input: "postgres", expected: "postgres"},
			{name: "should extract service name from project-service identifier", input: "postgresql-postgres", expected: "postgres"},
			{name: "should extract service name from project-service-replica identifier", input: "project-service-1", expected: "service"},
			{name: "should extract service name from complex project-service-replica identifier", input: "myapp-database-2", expected: "database"},
			{name: "should return empty string for empty input", input: "", expected: ""},
			{name: "should handle single character identifier", input: "a", expected: "a"},
			{name: "should handle identifier with multiple dashes", input: "my-complex-app-name-service-3", expected: "service"},
			{name: "should handle two-part identifier without replica", input: "myapp-web", expected: "web"},
			{name: "should handle two-part identifier with replica", input: "web-1", expected: "web"},
			{name: "should return last part when no replica suffix", input: "project-web", expected: "web"},
			{name: "should handle replica number 0 (not positive, so treat as name)", input: "service-0", expected: "0"},
			{name: "should handle large replica number", input: "project-service-999", expected: "service"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				input, expected := tt.input, tt.expected

				result := extractServiceName(input)
				assert.Equal(t, expected, result)
			})
		}
	})
}

// TestServiceOnlyMatching verifies that a bare service name links to the
// replicas of exactly one Compose service, and to nothing when several services
// share the name.
func TestServiceOnlyMatching(t *testing.T) {
	t.Parallel()

	t.Run("buildDependencyGraph", func(t *testing.T) {
		t.Parallel()

		t.Run("should match single unambiguous cross-project service by service name", func(t *testing.T) {
			t.Parallel()

			// App container from project1 that depends on "db" service
			app := deployed{name: "project1_app_1", dependsOn: "db", project: "project1", service: "app"}.container()

			// DB container from project2 (different project, but only one "db" service)
			db := deployed{name: "project2_db_1", project: "project2", service: "db"}.container()

			containers := []types.Container{app, db}

			containerMap, indegree, adjacency, _, err := buildDependencyGraph(newTestLogger(t), containers, true)
			require.NoError(t, err)

			// Verify containers are in the map
			require.Len(t, containerMap, 2)

			// App should have indegree 1 (depends on db via service-only match)
			assert.Equal(t, 1, indegree["project1-app"])

			// DB should have app as dependent
			assert.Contains(t, adjacency["project2-db"], "project1-app")
		})

		t.Run("should match watchtower label referencing service name without project prefix", func(t *testing.T) {
			t.Parallel()

			// App container with watchtower depends-on label referencing just "postgres"
			app := deployed{name: "myapp", dependsOn: "postgres"}.container()

			// Postgres container with project prefix in identifier
			postgres := deployed{name: "postgresql_postgres_1", project: "postgresql", service: "postgres"}.container()

			containers := []types.Container{app, postgres}

			containerMap, indegree, adjacency, _, err := buildDependencyGraph(newTestLogger(t), containers, true)
			require.NoError(t, err)

			// Verify containers are in the map
			require.Len(t, containerMap, 2)

			// App should have indegree 1 (depends on postgres via service-only match)
			assert.Equal(t, 1, indegree["myapp"])

			// Postgres should have app as dependent
			assert.Contains(t, adjacency["postgresql-postgres"], "myapp")
		})

		t.Run("should match every project's service when several share the service name", func(t *testing.T) {
			t.Parallel()

			// App container that depends on "db" service
			app := deployed{name: "app", dependsOn: "db"}.container()

			// DB container from project1
			db1 := deployed{name: "project1_db_1", project: "project1", service: "db"}.container()

			// DB container from project2 (same service name, different project)
			db2 := deployed{name: "project2_db_1", project: "project2", service: "db"}.container()

			containers := []types.Container{app, db1, db2}

			containerMap, indegree, _, _, err := buildDependencyGraph(newTestLogger(t), containers, true)
			require.NoError(t, err)

			// Verify all containers are in the map
			require.Len(t, containerMap, 3)

			// App depends on the db service of both projects
			assert.Equal(t, 2, indegree["app"])

			// Both db containers should have indegree 0
			assert.Equal(t, 0, indegree["project1-db"])
			assert.Equal(t, 0, indegree["project2-db"])
		})

		t.Run("should prefer exact match over service-only match", func(t *testing.T) {
			t.Parallel()

			// App container that links to exact identifier "project1-db"
			app := deployed{name: "app", dependsOn: "project1-db"}.container()

			// DB container from project1
			db1 := deployed{name: "project1_db_1", project: "project1", service: "db"}.container()

			// DB container from project2 (should NOT be matched)
			db2 := deployed{name: "project2_db_1", project: "project2", service: "db"}.container()

			containers := []types.Container{app, db1, db2}

			containerMap, indegree, adjacency, _, err := buildDependencyGraph(newTestLogger(t), containers, true)
			require.NoError(t, err)

			// Verify all containers are in the map
			require.Len(t, containerMap, 3)

			// App should have indegree 1 (exact match to project1-db only)
			assert.Equal(t, 1, indegree["app"])

			// Only project1-db should have app as dependent
			assert.Contains(t, adjacency["project1-db"], "app")
			assert.NotContains(t, adjacency["project2-db"], "app")
		})

		t.Run("should prefer replica match over service-only match", func(t *testing.T) {
			t.Parallel()

			// App container that links to "db" (should match db-1 replica, not project1-db)
			app := deployed{name: "app", dependsOn: "db"}.container()

			// DB replica container (db-1 pattern)
			dbReplica := deployed{name: "db-1"}.container()

			// DB container with project prefix (service name also "db")
			dbProject := deployed{name: "project_db_1", project: "project", service: "db"}.container()

			containers := []types.Container{app, dbReplica, dbProject}

			containerMap, indegree, adjacency, _, err := buildDependencyGraph(newTestLogger(t), containers, true)
			require.NoError(t, err)

			// Verify all containers are in the map
			require.Len(t, containerMap, 3)

			// App should have indegree 1 (replica match to db-1)
			assert.Equal(t, 1, indegree["app"])

			// db-1 should have app as dependent (replica match)
			assert.Contains(t, adjacency["db-1"], "app")
		})
	})
}

// assertOrderBefore checks that first comes before second in names.
//
// Parameters:
//   - t: Test that receives the failure.
//   - names: Container names in sorted order.
//   - first: Name that must come first.
//   - second: Name that must come after first.
func assertOrderBefore(t *testing.T, names []string, first, second string) {
	t.Helper()

	assert.Less(t, indexOf(names, first), indexOf(names, second), "%s must sort before %s", first, second)
}

// indexOf returns the position of target in names.
//
// Parameters:
//   - names: Names to search.
//   - target: Name to find.
//
// Returns:
//   - int: The position of target, or -1 when names lacks it.
func indexOf(names []string, target string) int {
	for i, name := range names {
		if name == target {
			return i
		}
	}

	return -1
}

// Test_matchSet_match verifies which identifiers each link form matches: exact
// keys, Compose replicas, unique service names, and multi-segment services,
// with ambiguous and partial matches left out.
func Test_matchSet_match(t *testing.T) {
	t.Parallel()

	// Exhaustive link x identifier permutations across Compose stacks, multi-segment
	// services, replicas, bare container names, and ambiguous cross-project cases.
	t.Run("matches identifiers for each link form", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name        string
			link        string
			identifiers []string
			expected    []string
		}{
			{name: "exact project-service key", link: "project-db", identifiers: []string{"project-db", "other-db"}, expected: []string{"project-db"}},
			{name: "exact bare container name", link: "net-proxy", identifiers: []string{"net-proxy", "web-app"}, expected: []string{"net-proxy"}},
			{
				name:        "replica suffixes from project-service link",
				link:        "project-db",
				identifiers: []string{"project-db-1", "project-db-2", "other"},
				expected:    []string{"project-db-1", "project-db-2"},
			},
			{name: "non-numeric hyphen suffix is not a replica", link: "project-db", identifiers: []string{"project-db-backup"}, expected: nil},
			{name: "numeric-looking middle segment is not a replica of shorter prefix", link: "project", identifiers: []string{"project-db-1"}, expected: nil},
			{name: "unhyphenated service against single project-service key", link: "db", identifiers: []string{"project1-db"}, expected: []string{"project1-db"}},
			{name: "unhyphenated service against multi-segment project-service key", link: "proxy", identifiers: []string{"myproject-net-proxy"}, expected: []string{"myproject-net-proxy"}},
			{name: "unhyphenated service ambiguous across two projects", link: "db", identifiers: []string{"project1-db", "project2-db"}, expected: nil},
			{name: "unhyphenated service with three projects still ambiguous", link: "cache", identifiers: []string{"a-cache", "b-cache", "c-cache"}, expected: nil},
			{name: "multi-segment link against project-service key", link: "net-proxy", identifiers: []string{"myproject-net-proxy", "myproject-web"}, expected: []string{"myproject-net-proxy"}},
			{name: "multi-segment link sole candidate", link: "net-proxy", identifiers: []string{"myproject-net-proxy"}, expected: []string{"myproject-net-proxy"}},
			{name: "multi-segment link must not match trailing-token-only peer", link: "net-proxy", identifiers: []string{"myproject-other-proxy"}, expected: nil},
			{name: "multi-segment link must not match substring service", link: "net-proxy", identifiers: []string{"myproject-net"}, expected: nil},
			{name: "multi-segment link must not match longer multi-segment sibling", link: "net-proxy", identifiers: []string{"myproject-net-proxy-extra"}, expected: nil},
			{name: "multi-segment link ambiguous across two projects", link: "net-proxy", identifiers: []string{"stack-a-net-proxy", "stack-b-net-proxy"}, expected: nil},
			{name: "multi-segment link prefers exact over suffix when both present", link: "net-proxy", identifiers: []string{"net-proxy", "myproject-net-proxy"}, expected: []string{"net-proxy"}},
			{name: "unhyphenated db among multi-stack unrelated services", link: "db", identifiers: []string{"frontend-web", "backend-api", "data-db"}, expected: []string{"data-db"}},
			{
				name:        "hyphenated link among multi-stack noise",
				link:        "net-proxy",
				identifiers: []string{"frontend-web", "backend-api", "infra-net-proxy", "infra-other-proxy"},
				expected:    []string{"infra-net-proxy"},
			},
			{
				name:        "hyphenated link with same trailing token in another stack",
				link:        "auth-gateway",
				identifiers: []string{"stack-a-auth-gateway", "stack-b-edge-gateway"},
				expected:    []string{"stack-a-auth-gateway"},
			},
			{name: "empty link", link: "", identifiers: []string{"db"}, expected: nil},
			{name: "empty identifiers", link: "db", identifiers: []string{}, expected: nil},
			{name: "no candidates at all", link: "missing", identifiers: []string{"a", "b"}, expected: nil},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				link, identifiers, expected := tt.link, tt.identifiers, tt.expected

				matches := newMatchSet(identifierSet(identifiers...)).match(link)
				if expected == nil {
					assert.Empty(t, matches)

					return
				}

				assert.Equal(t, expected, matches)
			})
		}
	})
}

// identifierSet builds a bool set from identifier strings for resolveLinkToCanonicalKeys tests.
func identifierSet(ids ...string) map[string]bool {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}

	return set
}

// Test_buildLinkMatchIndexes verifies the aliases a link can use for each
// container: its canonical key, a bare name no other container shares, and its
// Docker ID, without one container's alias replacing another's key.
func Test_buildLinkMatchIndexes(t *testing.T) {
	t.Parallel()

	t.Run("should map unique bare names to canonical identifiers", func(t *testing.T) {
		t.Parallel()

		dep := deployed{name: "net-proxy", project: "myproject", service: "net-proxy"}.container()
		app := deployed{name: "web-app", project: "myproject", service: "web"}.container()

		containerMap := map[string]types.Container{
			"myproject-net-proxy": dep,
			"myproject-web":       app,
		}

		idSet, aliasToCanonical := buildLinkMatchIndexes(newTestLogger(t), containerMap)
		assert.Equal(t, "myproject-net-proxy", aliasToCanonical["myproject-net-proxy"])
		assert.Equal(t, "myproject-net-proxy", aliasToCanonical["net-proxy"])
		assert.Equal(t, "myproject-web", aliasToCanonical["web-app"])
		assert.Contains(t, idSet, "myproject-net-proxy")
		assert.Contains(t, idSet, "myproject-web")
		assert.Contains(t, idSet, "net-proxy")
		assert.Contains(t, idSet, "web-app")
	})

	t.Run("should omit bare name alias when it collides across containers", func(t *testing.T) {
		t.Parallel()

		c1 := deployed{name: "shared", id: "id-first"}.container()
		c2 := deployed{name: "shared", id: "id-second"}.container()

		containerMap := map[string]types.Container{
			"project1-service": c1,
			"project2-service": c2,
		}

		_, aliasToCanonical := buildLinkMatchIndexes(newTestLogger(t), containerMap)
		_, hasBare := aliasToCanonical["shared"]
		assert.False(t, hasBare)
		assert.Equal(t, "project1-service", aliasToCanonical["project1-service"])
		assert.Equal(t, "project2-service", aliasToCanonical["project2-service"])
	})

	t.Run("should preserve canonical self-mapping when bare name equals another canonical key", func(t *testing.T) {
		t.Parallel()

		// Container A is keyed by "shared".
		// Container B's bare name is also "shared".
		// B must not overwrite or delete A's canonical self-mapping.
		canonicalOwner := deployed{name: "shared", id: "id-owner"}.container()
		bareClaimant := deployed{name: "shared", id: "id-claimant"}.container()

		containerMap := map[string]types.Container{
			"shared":          canonicalOwner,
			"project-service": bareClaimant,
		}

		// The index also holds each container's Docker ID.
		idSet, aliasToCanonical := buildLinkMatchIndexes(newTestLogger(t), containerMap)
		assert.Equal(t, "shared", aliasToCanonical["shared"])
		assert.Equal(t, "project-service", aliasToCanonical["project-service"])
		assert.Equal(t, "shared", aliasToCanonical["id-owner"])
		assert.Equal(t, "project-service", aliasToCanonical["id-claimant"])
		assert.Equal(t, identifierSet("shared", "project-service", "id-owner", "id-claimant"), idSet)
	})

	t.Run("should map a Docker ID alias for concrete containers", func(t *testing.T) {
		t.Parallel()

		const providerID = "25e75393800b5c450a6841212a3b92ed28fa35414a586dec9f2c8a520d4910c2"

		gluetun := deployed{name: "gluetun", id: providerID, project: "gluetun", service: "vpn"}.container()

		containerMap := map[string]types.Container{
			"gluetun-vpn": gluetun,
		}

		idSet, aliasToCanonical := buildLinkMatchIndexes(newTestLogger(t), containerMap)
		assert.Equal(t, "gluetun-vpn", aliasToCanonical[providerID])
		assert.Contains(t, idSet, providerID)
	})

	t.Run("should skip an empty Docker ID", func(t *testing.T) {
		t.Parallel()

		// The deployment helper derives an ID when none is given. This
		// container is built from inspect data without one.
		gluetun := container.NewContainer(nil, &dockerContainer.InspectResponse{
			Name: "/gluetun",
			Config: &dockerContainer.Config{Labels: map[string]string{
				composeLabels.ComposeProjectLabel: "gluetun",
				composeLabels.ComposeServiceLabel: "vpn",
			}},
		}, nil)

		containerMap := map[string]types.Container{
			"gluetun-vpn": gluetun,
		}

		idSet, aliasToCanonical := buildLinkMatchIndexes(newTestLogger(t), containerMap)
		assert.Equal(t, "gluetun-vpn", aliasToCanonical["gluetun-vpn"])
		assert.Equal(t, "gluetun-vpn", aliasToCanonical["gluetun"])
		require.Len(t, idSet, 2)
	})

	t.Run("should skip a Docker ID that equals the canonical identifier", func(t *testing.T) {
		t.Parallel()

		const providerID = "25e75393800b5c450a6841212a3b92ed28fa35414a586dec9f2c8a520d4910c2"

		gluetun := deployed{name: providerID, id: providerID}.container()

		containerMap := map[string]types.Container{
			providerID: gluetun,
		}

		_, aliasToCanonical := buildLinkMatchIndexes(newTestLogger(t), containerMap)
		assert.Equal(t, providerID, aliasToCanonical[providerID])
	})

	t.Run("should not overwrite an existing canonical key with a Docker ID", func(t *testing.T) {
		t.Parallel()

		const providerID = "25e75393800b5c450a6841212a3b92ed28fa35414a586dec9f2c8a520d4910c2"

		owner := deployed{name: "owner", project: "owner", service: "svc"}.container()
		claimant := deployed{name: "gluetun", id: providerID, project: "gluetun", service: "vpn"}.container()

		containerMap := map[string]types.Container{
			providerID:    owner,
			"gluetun-vpn": claimant,
		}

		_, aliasToCanonical := buildLinkMatchIndexes(newTestLogger(t), containerMap)
		assert.Equal(t, providerID, aliasToCanonical[providerID])
	})

	t.Run("should retain a Docker ID alias when another container uses that ID as its name", func(t *testing.T) {
		t.Parallel()

		const providerID = "25e75393800b5c450a6841212a3b92ed28fa35414a586dec9f2c8a520d4910c2"

		gluetun := deployed{name: "gluetun", id: providerID, project: "gluetun", service: "vpn"}.container()
		decoy := deployed{name: providerID, project: "other", service: "svc"}.container()

		containerMap := map[string]types.Container{
			"gluetun-vpn": gluetun,
			"other-svc":   decoy,
		}

		_, aliasToCanonical := buildLinkMatchIndexes(newTestLogger(t), containerMap)
		assert.Equal(t, "gluetun-vpn", aliasToCanonical[providerID])
	})
}

// Test_resolveLinkToCanonicalKeys verifies that a link resolves to the canonical
// keys of the containers it names, without duplicates, and to nothing when it
// is empty or names no container.
func Test_resolveLinkToCanonicalKeys(t *testing.T) {
	t.Parallel()

	t.Run("should resolve bare multi-segment container name to project-service key", func(t *testing.T) {
		t.Parallel()

		idSet := identifierSet("myproject-net-proxy", "myproject-web", "net-proxy", "web-app")
		alias := map[string]string{
			"myproject-net-proxy": "myproject-net-proxy",
			"myproject-web":       "myproject-web",
			"net-proxy":           "myproject-net-proxy",
			"web-app":             "myproject-web",
		}

		keys := resolveLinkToCanonicalKeys("net-proxy", newMatchSet(idSet), alias)
		assert.Equal(t, []string{"myproject-net-proxy"}, keys)
	})

	t.Run("should resolve project-service suffix match without bare-name alias", func(t *testing.T) {
		t.Parallel()

		idSet := identifierSet("myproject-net-proxy")
		alias := map[string]string{"myproject-net-proxy": "myproject-net-proxy"}

		keys := resolveLinkToCanonicalKeys("net-proxy", newMatchSet(idSet), alias)
		assert.Equal(t, []string{"myproject-net-proxy"}, keys)
	})

	t.Run("should return nil for empty or unmatched links", func(t *testing.T) {
		t.Parallel()

		idSet := identifierSet("myproject-db")
		alias := map[string]string{"myproject-db": "myproject-db"}

		assert.Nil(t, resolveLinkToCanonicalKeys("", newMatchSet(idSet), alias))
		assert.Nil(t, resolveLinkToCanonicalKeys("missing", newMatchSet(idSet), alias))
	})

	t.Run("should dedupe when bare alias and canonical both match", func(t *testing.T) {
		t.Parallel()

		idSet := identifierSet("myproject-db", "db")
		alias := map[string]string{
			"myproject-db": "myproject-db",
			"db":           "myproject-db",
		}

		keys := resolveLinkToCanonicalKeys("db", newMatchSet(idSet), alias)
		assert.Equal(t, []string{"myproject-db"}, keys)
	})
}

// TestDependencyLinkFormPermutations verifies that each form a dependency link
// can take sorts the dependency before its dependent, across Compose stacks,
// replicas, and shared or ambiguous names.
//
// Containers with no dependency between them sort in reverse alphabetical
// order of their graph keys. Each dependent's key sorts after its
// dependency's. A dropped dependency then puts the dependent first.
func TestDependencyLinkFormPermutations(t *testing.T) {
	t.Parallel()

	// Each dependency source and link form must resolve against the
	// ResolveContainerIdentifier graph keys.
	t.Run("sorts dependency before dependent for each link form", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name string
			// dependency and dependent are the two containers. The dependent
			// names the dependency through the source the case describes.
			dependency, dependent deployed
			// useCompose is whether Compose depends_on labels are read.
			useCompose bool
		}{
			{
				name:       "depends-on bare container_name against project-service key",
				dependency: deployed{name: "net-proxy", project: "myproject", service: "net-proxy"},
				dependent:  deployed{name: "web-app", project: "myproject", service: "web", dependsOn: "net-proxy"},
				useCompose: true,
			},
			{
				name:       "depends-on bare name without Compose labels",
				dependency: deployed{name: "database"},
				dependent:  deployed{name: "web", dependsOn: "database"},
				useCompose: true,
			},
			{
				name:       "depends-on with Compose depends_on not read",
				dependency: deployed{name: "postgres", project: "stack", service: "postgres", composeDependsOn: "web"},
				dependent:  deployed{name: "web", project: "stack", service: "web", dependsOn: "postgres"},
				useCompose: false,
			},
			{
				name:       "Compose depends_on service in the same project",
				dependency: deployed{name: "myproject-database", project: "myproject", service: "database"},
				dependent: deployed{
					name: "myproject-web", project: "myproject", service: "web", composeDependsOn: "database",
				},
				useCompose: true,
			},
			{
				name:       "Compose depends_on multi-segment service",
				dependency: deployed{name: "myproject-net-proxy", project: "myproject", service: "net-proxy"},
				dependent: deployed{
					name: "myproject-web", project: "myproject", service: "web", composeDependsOn: "net-proxy",
				},
				useCompose: true,
			},
			{
				name:       "depends-on bare service name against project-service key",
				dependency: deployed{name: "myproject-database", project: "myproject", service: "database"},
				dependent:  deployed{name: "myproject-web", project: "myproject", service: "web", dependsOn: "database"},
				useCompose: true,
			},
			{
				name:       "depends-on bare multi-segment service name",
				dependency: deployed{name: "myproject-net-proxy", project: "myproject", service: "net-proxy"},
				dependent:  deployed{name: "myproject-web", project: "myproject", service: "web", dependsOn: "net-proxy"},
				useCompose: true,
			},
			{
				name:       "network_mode of another container",
				dependency: deployed{name: "vpn", project: "stack", service: "vpn"},
				dependent: deployed{
					name: "web", project: "stack", service: "web", networkMode: "container:vpn",
				},
				useCompose: true,
			},
			{
				name:       "network_mode with Compose depends_on not read",
				dependency: deployed{name: "vpn", project: "stack", service: "vpn"},
				dependent: deployed{
					name: "web", project: "stack", service: "web", networkMode: "container:vpn",
				},
				useCompose: false,
			},
			{
				name:       "legacy Docker link",
				dependency: deployed{name: "db"},
				dependent:  deployed{name: "web", links: []string{"/db:/web/db"}},
				useCompose: true,
			},
			{
				name:       "cross-project depends-on by container name",
				dependency: deployed{name: "app1-database", project: "app1", service: "database"},
				dependent:  deployed{name: "app2-web", project: "app2", service: "web", dependsOn: "app1-database"},
				useCompose: true,
			},
			{
				name:       "depends-on service name without container_name override",
				dependency: deployed{name: "stack-cache", project: "stack", service: "cache"},
				dependent:  deployed{name: "stack-worker", project: "stack", service: "worker", dependsOn: "cache"},
				useCompose: true,
			},
			{
				name:       "depends-on bare service name of a single-container project",
				dependency: deployed{name: "redis", project: "cache", service: "redis"},
				dependent:  deployed{name: "worker", project: "cache", service: "worker", dependsOn: "redis"},
				useCompose: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				containers := deploy(tt.dependent, tt.dependency)
				err := dependencySorter{}.Sort(newTestLogger(t), containers, tt.useCompose)
				require.NoError(t, err)
				assert.Equal(t, tt.dependency.name, containers[0].Name(), "dependency must sort before dependent")
				assert.Equal(t, tt.dependent.name, containers[1].Name())
			})
		}
	})

	t.Run("should order multiple dependents after a shared network provider", func(t *testing.T) {
		t.Parallel()

		vpn := deployed{name: "vpn", project: "stack", service: "vpn"}.container()
		webA := deployed{name: "web-a", project: "stack", service: "web-a", networkMode: "container:vpn"}.container()
		webB := deployed{name: "web-b", project: "stack", service: "web-b", networkMode: "container:vpn"}.container()

		containers := []types.Container{webA, webB, vpn}
		err := dependencySorter{}.Sort(newTestLogger(t), containers, true)
		require.NoError(t, err)
		assert.Equal(t, "vpn", containers[0].Name())
		names := []string{containers[1].Name(), containers[2].Name()}
		assert.ElementsMatch(t, []string{"web-a", "web-b"}, names)
	})

	t.Run("should order when dependent has multiple dependencies", func(t *testing.T) {
		t.Parallel()

		db := deployed{name: "db", project: "app", service: "db"}.container()
		cache := deployed{name: "cache", project: "app", service: "cache"}.container()
		web := deployed{name: "web", project: "app", service: "web", dependsOn: "db,cache"}.container()

		containers := []types.Container{web, db, cache}
		err := dependencySorter{}.Sort(newTestLogger(t), containers, true)
		require.NoError(t, err)
		assert.Equal(t, "web", containers[2].Name())

		ordered := []string{containers[0].Name(), containers[1].Name(), containers[2].Name()}
		assertOrderBefore(t, ordered, "db", "web")
		assertOrderBefore(t, ordered, "cache", "web")
	})

	t.Run("should match Compose replica identifiers from a service link", func(t *testing.T) {
		t.Parallel()

		db1 := deployed{name: "myproject-db-1", project: "myproject", service: "db"}.container()
		db2 := deployed{name: "myproject-db-2", project: "myproject", service: "db"}.container()
		web := deployed{name: "myproject-web", project: "myproject", service: "web", dependsOn: "myproject-db"}.container()

		containers := []types.Container{web, db1, db2}
		err := dependencySorter{}.Sort(newTestLogger(t), containers, true)
		require.NoError(t, err)
		assert.Equal(t, "myproject-web", containers[2].Name())
		assert.ElementsMatch(t, []string{"myproject-db-1", "myproject-db-2"}, []string{containers[0].Name(), containers[1].Name()})
	})

	t.Run("should link a bare service name to the service in every project", func(t *testing.T) {
		t.Parallel()

		db1 := deployed{name: "project1-db", project: "project1", service: "db"}.container()
		db2 := deployed{name: "project2-db", project: "project2", service: "db"}.container()
		app := deployed{name: "project3-app", project: "project3", service: "app", dependsOn: "db"}.container()

		_, indegree, adjacency, _, err := buildDependencyGraph(newTestLogger(t), []types.Container{app, db1, db2}, true)
		require.NoError(t, err)
		assert.Equal(t, 2, indegree["project3-app"])
		assert.Contains(t, adjacency["project1-db"], "project3-app")
		assert.Contains(t, adjacency["project2-db"], "project3-app")
	})

	t.Run("should ignore missing dependency targets without failing sort", func(t *testing.T) {
		t.Parallel()

		app := deployed{name: "app", project: "myproject", service: "app", dependsOn: "missing-dep"}.container()
		standalone := deployed{name: "other", project: "myproject", service: "other"}.container()

		containers := []types.Container{app, standalone}
		err := dependencySorter{}.Sort(newTestLogger(t), containers, true)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"app", "other"}, []string{containers[0].Name(), containers[1].Name()})
	})

	t.Run("should keep Watchtower last when linked non-Watchtower containers sort first", func(t *testing.T) {
		t.Parallel()

		db := deployed{name: "db", project: "myproject", service: "db"}.container()
		web := deployed{name: "web", project: "myproject", service: "web", dependsOn: "db"}.container()
		wt := deployed{name: "watchtower", watchtower: true}.container()

		containers := []types.Container{wt, web, db}
		err := dependencySorter{}.Sort(newTestLogger(t), containers, true)
		require.NoError(t, err)
		assert.Equal(t, "db", containers[0].Name())
		assert.Equal(t, "web", containers[1].Name())
		assert.Equal(t, "watchtower", containers[2].Name())
	})

	t.Run("should order independent stacks without cross-linking same service names", func(t *testing.T) {
		t.Parallel()

		// Two Compose projects each have db→web.
		// Bare link "db" is ambiguous and must not couple stacks.
		// Each web only orders relative to its own db when links use project-qualified or unique names.
		dbA := deployed{name: "stack-a-db", project: "stack-a", service: "db"}.container()
		webA := deployed{name: "stack-a-web", project: "stack-a", service: "web", dependsOn: "stack-a-db"}.container()
		dbB := deployed{name: "stack-b-db", project: "stack-b", service: "db"}.container()
		webB := deployed{name: "stack-b-web", project: "stack-b", service: "web", dependsOn: "stack-b-db"}.container()

		containers := []types.Container{webA, webB, dbA, dbB}
		err := dependencySorter{}.Sort(newTestLogger(t), containers, true)
		require.NoError(t, err)

		ordered := make([]string, len(containers))
		for i, c := range containers {
			ordered[i] = c.Name()
		}

		assertOrderBefore(t, ordered, "stack-a-db", "stack-a-web")
		assertOrderBefore(t, ordered, "stack-b-db", "stack-b-web")
	})

	t.Run("should order cross-stack Watchtower depends-on by foreign container_name", func(t *testing.T) {
		t.Parallel()

		// Provider stack owns the named network peer.
		// Consumer stack names it by container name in a Watchtower depends-on label.
		provider := deployed{name: "shared-net-proxy", project: "infra", service: "net-proxy"}.container()
		consumer := deployed{name: "web-app", project: "web", service: "app", dependsOn: "shared-net-proxy"}.container()
		unrelated := deployed{name: "other-db", project: "other", service: "db"}.container()

		containers := []types.Container{consumer, unrelated, provider}
		err := dependencySorter{}.Sort(newTestLogger(t), containers, true)
		require.NoError(t, err)

		ordered := make([]string, len(containers))
		for i, c := range containers {
			ordered[i] = c.Name()
		}

		assertOrderBefore(t, ordered, "shared-net-proxy", "web-app")
		assert.ElementsMatch(t, []string{"shared-net-proxy", "web-app", "other-db"}, ordered)
	})

	t.Run("should not couple stacks when multi-segment services share a trailing token", func(t *testing.T) {
		t.Parallel()

		// stack-a has service net-proxy
		// stack-b has service other-proxy.
		// A dependent linking to net-proxy must only wait on stack-a's peer, not other-proxy.
		proxyA := deployed{name: "stack-a-net-proxy", project: "stack-a", service: "net-proxy"}.container()
		proxyB := deployed{name: "stack-b-other-proxy", project: "stack-b", service: "other-proxy"}.container()
		client := deployed{name: "stack-a-client", project: "stack-a", service: "client", dependsOn: "net-proxy"}.container()

		_, indegree, adjacency, _, err := buildDependencyGraph(newTestLogger(t), []types.Container{client, proxyB, proxyA}, true)
		require.NoError(t, err)
		assert.Equal(t, 1, indegree["stack-a-client"])
		assert.Contains(t, adjacency["stack-a-net-proxy"], "stack-a-client")
		assert.NotContains(t, adjacency["stack-b-other-proxy"], "stack-a-client")

		containers := []types.Container{client, proxyB, proxyA}
		err = dependencySorter{}.Sort(newTestLogger(t), containers, true)
		require.NoError(t, err)

		ordered := make([]string, len(containers))
		for i, c := range containers {
			ordered[i] = c.Name()
		}

		assertOrderBefore(t, ordered, "stack-a-net-proxy", "stack-a-client")
	})

	t.Run("should order multi-segment container_name dependency among multi-stack peers", func(t *testing.T) {
		t.Parallel()

		// Explicit container_name equals multi-segment service and Compose keys differ.
		provider := deployed{name: "net-proxy", project: "media", service: "net-proxy"}.container()
		dependent := deployed{name: "web-app", project: "media", service: "web", dependsOn: "net-proxy"}.container()
		foreign := deployed{name: "net-proxy-other", project: "other", service: "net-proxy-other"}.container()
		// Same trailing token in another stack must not steal the edge.
		decoy := deployed{name: "edge-proxy", project: "edge", service: "edge-proxy"}.container()

		containers := []types.Container{dependent, foreign, decoy, provider}
		err := dependencySorter{}.Sort(newTestLogger(t), containers, true)
		require.NoError(t, err)

		ordered := make([]string, len(containers))
		for i, c := range containers {
			ordered[i] = c.Name()
		}

		assertOrderBefore(t, ordered, "net-proxy", "web-app")
	})

	t.Run("should order chain across three stacks with mixed link forms", func(t *testing.T) {
		t.Parallel()

		// data.db ← (compose service) app.worker ← (watchtower container_name) edge.client
		db := deployed{name: "data-db", project: "data", service: "db"}.container()
		worker := deployed{name: "app-worker", project: "app", service: "worker", dependsOn: "db"}.container()
		// Watchtower depends-on uses worker's container_name.
		client := deployed{name: "edge-client", project: "edge", service: "client", dependsOn: "app-worker"}.container()

		containers := []types.Container{client, worker, db}
		err := dependencySorter{}.Sort(newTestLogger(t), containers, true)
		require.NoError(t, err)

		ordered := make([]string, len(containers))
		for i, c := range containers {
			ordered[i] = c.Name()
		}

		assertOrderBefore(t, ordered, "data-db", "app-worker")
		assertOrderBefore(t, ordered, "app-worker", "edge-client")
		assertOrderBefore(t, ordered, "data-db", "edge-client")
	})

	t.Run("should not create edge for hyphenated link against trailing-token-only peer", func(t *testing.T) {
		t.Parallel()

		// Regression: extractServiceName("net-proxy") and extractServiceName("other-proxy")
		// both yield "proxy".
		// Matching must not treat them as the same dependency.
		decoy := deployed{name: "other-proxy", project: "other", service: "other-proxy"}.container()
		dependent := deployed{name: "web", project: "app", service: "web", dependsOn: "net-proxy"}.container()

		containers := []types.Container{dependent, decoy}
		err := dependencySorter{}.Sort(newTestLogger(t), containers, true)
		require.NoError(t, err)
		// No dependency edge: both indegree 0. Order is reverse-alpha among keys.
		assert.ElementsMatch(t, []string{"web", "other-proxy"}, []string{containers[0].Name(), containers[1].Name()})
	})
}
