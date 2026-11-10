package update

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"
	dockerNetwork "github.com/moby/moby/api/types/network"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/git"
	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
	"github.com/nicholas-fedor/watchtower/internal/registry/ratelimit"
	gitConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/git"
	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/logcapture"
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
	"github.com/nicholas-fedor/watchtower/internal/update/rebuild"
	"github.com/nicholas-fedor/watchtower/internal/update/sorter"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
)

// planTestRestarts builds the dependency graph of the monitored containers, as
// the update session does, and plans their restarts with a client mock that
// expects no calls.
//
// Parameters:
//   - t: Test that owns the plan.
//   - session: Git session that has checked the containers.
//   - config: Update parameters.
//   - progress: Session progress.
//   - filtered: The monitored containers, which are also every listed container.
//   - inCycle: IDs of the containers treated as cycle members.
//
// Returns:
//   - []*container.Container: The containers to restart.
func planTestRestarts(
	t *testing.T,
	session *rebuild.Session,
	config updateParams.Params,
	progress *state.Progress,
	filtered []*container.Container,
	inCycle map[container.ContainerID]struct{},
) []*container.Container {
	t.Helper()

	log := newTestLogger(t)
	graph, err := sorter.NewDependencyGraph(log, filtered, nil, config.UseComposeDependsOn)
	require.NoError(t, err)

	restarts, err := planRestarts(log, t.Context(), newMockClient(t), config, session, progress,
		graph, filtered, filtered, inCycle)
	require.NoError(t, err)

	return restarts
}

// staleGitTestContainer returns a Git-watched container whose remote tip is
// newer than its stamp, after the session's staleness phase has checked and
// recorded it.
//
// Parameters:
//   - t: Test that owns the repository.
//   - config: Update parameters for the check.
//   - progress: Session progress that receives the check's result.
//
// Returns:
//   - *container.Container: The stale container, marked for update.
//   - *rebuild.Session: The session holding its check result.
//   - *git.Client: The session's Git client.
//   - string: The repository's tip commit.
func staleGitTestContainer(
	t *testing.T,
	config updateParams.Params,
	progress *state.Progress,
) (*container.Container, *rebuild.Session, *git.Client, string) {
	t.Helper()

	app, gitClient, commit := gitWatchedTestContainer(t, nil)
	session := rebuild.NewSession(gitClient)

	_, err := checkStaleness(newTestLogger(t), t.Context(), newMockClient(t), config, ratelimit.New(),
		session, progress, []*container.Container{app})
	require.NoError(t, err)
	require.True(t, app.IsStale())

	return app, session, gitClient, commit
}

// Test_hasSelfDependency verifies that a container whose depends-on label
// names itself is detected.
func Test_hasSelfDependency(t *testing.T) {
	t.Parallel()

	t.Run("should return false when no depends-on label is present", func(t *testing.T) {
		t.Parallel()

		container := mockUpdate.CreateMockContainerWithConfig(
			"test-container",
			"/test-container",
			"test:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels:       map[string]string{},
				ExposedPorts: dockerNetwork.PortSet{},
			},
		)
		result := hasSelfDependency(container)
		assert.False(t, result)
	})

	t.Run("should return false when depends-on label is empty", func(t *testing.T) {
		t.Parallel()

		container := mockUpdate.CreateMockContainerWithConfig(
			"test-container",
			"/test-container",
			"test:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels: map[string]string{
					"com.centurylinklabs.watchtower.depends-on": "",
				},
				ExposedPorts: dockerNetwork.PortSet{},
			},
		)
		result := hasSelfDependency(container)
		assert.False(t, result)
	})

	t.Run("should return false when depends-on contains other containers", func(t *testing.T) {
		t.Parallel()

		container := mockUpdate.CreateMockContainerWithConfig(
			"test-container",
			"/test-container",
			"test:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels: map[string]string{
					"com.centurylinklabs.watchtower.depends-on": "other-container",
				},
				ExposedPorts: dockerNetwork.PortSet{},
			},
		)
		result := hasSelfDependency(container)
		assert.False(t, result)
	})

	t.Run("should return true when depends-on contains self", func(t *testing.T) {
		t.Parallel()

		container := mockUpdate.CreateMockContainerWithConfig(
			"test-container",
			"/test-container",
			"test:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels: map[string]string{
					"com.centurylinklabs.watchtower.depends-on": "test-container",
				},
				ExposedPorts: dockerNetwork.PortSet{},
			},
		)
		result := hasSelfDependency(container)
		assert.True(t, result)
	})

	t.Run("should return true when depends-on contains self among multiple dependencies", func(t *testing.T) {
		t.Parallel()

		container := mockUpdate.CreateMockContainerWithConfig(
			"test-container",
			"/test-container",
			"test:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels: map[string]string{
					"com.centurylinklabs.watchtower.depends-on": "other-container,test-container,another-container",
				},
				ExposedPorts: dockerNetwork.PortSet{},
			},
		)
		result := hasSelfDependency(container)
		assert.True(t, result)
	})

	t.Run("should handle spaces and trimming correctly", func(t *testing.T) {
		t.Parallel()

		container := mockUpdate.CreateMockContainerWithConfig(
			"test-container",
			"/test-container",
			"test:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels: map[string]string{
					"com.centurylinklabs.watchtower.depends-on": " other-container , test-container , another-container ",
				},
				ExposedPorts: dockerNetwork.PortSet{},
			},
		)
		result := hasSelfDependency(container)
		assert.True(t, result)
	})

	t.Run("should handle leading slashes in container names", func(t *testing.T) {
		t.Parallel()

		container := mockUpdate.CreateMockContainerWithConfig(
			"test-container",
			"/test-container",
			"test:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels: map[string]string{
					"com.centurylinklabs.watchtower.depends-on": "/test-container",
				},
				ExposedPorts: dockerNetwork.PortSet{},
			},
		)
		result := hasSelfDependency(container)
		assert.True(t, result)
	})

	t.Run("should return false when Config is nil", func(t *testing.T) {
		t.Parallel()

		container := mockUpdate.CreateMockContainerWithConfig(
			"test-container",
			"/test-container",
			"test:latest",
			true,
			false,
			time.Now(),
			nil,
		) // Config is nil
		result := hasSelfDependency(container)
		assert.False(t, result)
	})

	t.Run("should return false when Labels is nil", func(t *testing.T) {
		t.Parallel()

		container := mockUpdate.CreateMockContainerWithConfig(
			"test-container",
			"/test-container",
			"test:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels:       nil, // Labels is nil
				ExposedPorts: dockerNetwork.PortSet{},
			},
		)
		result := hasSelfDependency(container)
		assert.False(t, result)
	})
}

// TestCheckDependencies verifies the dependency phase. It skips containers
// that name themselves and the members of a cycle. It warns about a
// depends-on entry that names no container but not about one that names an
// unmonitored container. It fails when two containers share a name.
func TestCheckDependencies(t *testing.T) {
	t.Parallel()

	// dependent returns a container with a fresh ID, since each problem warns
	// only the first time the process sees it.
	dependent := func(name, dependsOn string) *container.Container {
		return skipTestContainer(containerfixture.RandomID(), name, map[string]string{labels.DependsOn: dependsOn})
	}

	t.Run("skips self-dependencies and cycle members and warns about unresolved links", func(t *testing.T) {
		t.Parallel()

		self := dependent("self", "self")
		a := dependent("a", "b")
		b := dependent("b", "a")
		app := dependent("app", "missing")
		web := dependent("web", "db")
		db := skipTestContainer(containerfixture.RandomID(), "db", nil)
		monitored := []*container.Container{self, a, b, app, web}
		all := append([]*container.Container{db}, monitored...)

		log, buf := logcapture.NewJSON(zerolog.DebugLevel)
		progress := &state.Progress{}

		graph, inCycle, err := checkDependencies(log, updateParams.Params{}, progress, all, monitored)
		require.NoError(t, err)

		assert.NotNil(t, graph)
		assert.Equal(t, map[container.ContainerID]struct{}{a.ID(): {}, b.ID(): {}}, inCycle)
		assert.Equal(t, map[string]string{
			"self": errSelfDependency.Error(),
			"a":    errCircularDependency.Error(),
			"b":    errCircularDependency.Error(),
		}, reportErrors(progress.Report(log).Skipped()))
		assert.ElementsMatch(t, []string{
			logging.SelfDependency,
			logging.CircularDependency,
			logging.CircularDependency,
			logging.UnresolvedDependsOn,
		}, logcapture.Messages(t, buf.String(), "warn"))
	})

	t.Run("fails with the sort error when the graph cannot be built", func(t *testing.T) {
		t.Parallel()

		monitored := []*container.Container{
			skipTestContainer("a000000000000000000000000000000000000000000000000000000000000000", "app", nil),
			skipTestContainer("b000000000000000000000000000000000000000000000000000000000000000", "app", nil),
		}

		graph, inCycle, err := checkDependencies(newTestLogger(t), updateParams.Params{}, &state.Progress{},
			monitored, monitored)

		require.ErrorIs(t, err, errSortDependenciesFailed)
		assert.Nil(t, graph)
		assert.Nil(t, inCycle)
	})
}

// TestPlanRestarts verifies the restart plan. Containers restart in
// dependency order, without cycle members. The monitored containers are
// sorted in place with cycle members last. A failed Git rebuild is recorded
// as failed and leaves the containers that depend on it running. A no-restart
// build is left out of the restart list.
func TestPlanRestarts(t *testing.T) {
	t.Parallel()

	t.Run("orders the restart list by dependency without cycle members", func(t *testing.T) {
		t.Parallel()

		cyclic := skipTestContainer("cyclic-id", "cyclic", nil)
		app := skipTestContainer("app-id", "app", map[string]string{labels.DependsOn: "db"})
		db := skipTestContainer("db-id", "db", nil)

		for _, c := range []*container.Container{cyclic, app, db} {
			c.SetStale(true)
		}

		monitored := []*container.Container{cyclic, app, db}
		restarts := planTestRestarts(t, rebuild.NewSession(nil), updateParams.Params{}, &state.Progress{},
			monitored, map[container.ContainerID]struct{}{cyclic.ID(): {}})

		assert.Equal(t, []*container.Container{db, app}, restarts)
		// The monitored containers are sorted in place, with cycle members last.
		assert.Equal(t, []*container.Container{db, app, cyclic}, monitored)
	})

	t.Run("keeps a failed rebuild's dependents running", func(t *testing.T) {
		t.Parallel()

		config := updateParams.Params{Git: gitConfig.Defaults{Enable: true}}
		progress := &state.Progress{}
		app, session, _, _ := staleGitTestContainer(t, config, progress)
		web := skipTestContainer("web-id", "web", map[string]string{labels.DependsOn: "app"})

		// A remote without a host fails the build before any image is
		// built, so the client is never called.
		restarts := planTestRestarts(t, session, config, progress, []*container.Container{app, web}, nil)

		assert.Empty(t, restarts)

		failed := progress.Report(newTestLogger(t)).Failed()
		require.Len(t, failed, 1)
		assert.Equal(t, app.ID(), failed[0].ID())
	})

	t.Run("leaves a no-restart build out of the restart list", func(t *testing.T) {
		t.Parallel()

		config := updateParams.Params{Git: gitConfig.Defaults{Enable: true}, NoRestart: true}
		progress := &state.Progress{}
		app, session, gitClient, commit := staleGitTestContainer(t, config, progress)

		// An image an earlier no-restart session built for this commit is
		// reused without a build.
		gitClient.RecordUnrestartedBuild(app.ID(), commit, container.ImageID("sha256:built"))

		restarts := planTestRestarts(t, session, config, progress, []*container.Container{app}, nil)

		assert.Empty(t, restarts)
	})

	t.Run("ends with the sort error when the containers cannot be ordered", func(t *testing.T) {
		t.Parallel()

		// The session passes every cycle member in inCycle. Leaving this
		// cycle out of it makes the first sort fail.
		a := skipTestContainer("a-id", "a", map[string]string{labels.DependsOn: "b"})
		b := skipTestContainer("b-id", "b", map[string]string{labels.DependsOn: "a"})
		monitored := []*container.Container{a, b}

		log := newTestLogger(t)
		graph, err := sorter.NewDependencyGraph(log, monitored, nil, false)
		require.NoError(t, err)

		restarts, err := planRestarts(log, t.Context(), newMockClient(t), updateParams.Params{},
			rebuild.NewSession(nil), &state.Progress{}, graph, monitored, monitored, nil)

		require.ErrorIs(t, err, errSortDependenciesFailed)
		assert.Nil(t, restarts)
	})
}
