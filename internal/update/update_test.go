package update

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	cerrdefs "github.com/containerd/errdefs"

	composeApply "github.com/nicholas-fedor/watchtower/internal/compose/apply"
	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/git"
	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
	"github.com/nicholas-fedor/watchtower/internal/registry/ratelimit"
	dockerConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/docker"
	gitConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/git"
	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/logcapture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/modelfixture"
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
	"github.com/nicholas-fedor/watchtower/internal/update/rebuild"
	mockRebuild "github.com/nicholas-fedor/watchtower/internal/update/rebuild/mocks"
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
//   - skipped: IDs of the containers treated as skipped by the dependency phase.
//
// Returns:
//   - []*container.Container: The containers to restart.
func planTestRestarts(
	t *testing.T,
	session *rebuild.Session,
	config updateParams.Params,
	progress *state.Progress,
	filtered []*container.Container,
	skipped map[container.ContainerID]struct{},
) []*container.Container {
	t.Helper()

	log := newTestLogger(t)
	graph, err := sorter.NewDependencyGraph(log, filtered, nil, config.UseComposeDependsOn)
	require.NoError(t, err)

	restarts, err := planRestarts(log, t.Context(), newMockClient(t), config, session, progress,
		graph, filtered, filtered, skipped)
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
	session := rebuild.NewSession(gitClient, composeApply.NewClient(dockerConfig.Docker{}))

	_, err := checkStaleness(newTestLogger(t), t.Context(), newMockClient(t), config, ratelimit.New(),
		session, progress, []*container.Container{app})
	require.NoError(t, err)
	require.True(t, app.IsStale())

	return app, session, gitClient, commit
}

// TestUpdate_ListingFailure checks that a session ends with the client's own
// error when its container listing fails, so the "Update execution failed"
// line does not repeat the listing prefix.
func TestUpdate_ListingFailure(t *testing.T) {
	t.Parallel()

	listErr := errors.New("failed to list containers: connection refused")

	client := newMockClient(t)
	client.EXPECT().ListContainers(mock.Anything, mock.Anything).Return(nil, listErr).Once()

	report, cleanupImages, err := update(newTestLogger(t), t.Context(), client, updateParams.Params{}, ratelimit.New())

	require.ErrorIs(t, err, listErr)
	require.EqualError(t, err, listErr.Error())
	assert.Nil(t, report)
	assert.Nil(t, cleanupImages)
}

// TestCurrentOldInstance verifies that the current container is returned
// only when it is a listed Watchtower container with the watchtower-old- name
// prefix.
func TestCurrentOldInstance(t *testing.T) {
	t.Parallel()

	const (
		oldID     = "01d0000000000000000000000000000000000000000000000000000000000000"
		currentID = "c000000000000000000000000000000000000000000000000000000000000000"
	)

	old := cleanupTestWatchtower(oldID, "watchtower-old-01d000000000", "nickfedor/watchtower:latest")

	tests := []struct {
		name       string
		currentID  container.ContainerID
		containers []*container.Container
		want       *container.Container
	}{
		{
			name:       "an old Watchtower instance",
			currentID:  oldID,
			containers: []*container.Container{old},
			want:       old,
		},
		{
			name:       "no current container",
			currentID:  "",
			containers: []*container.Container{old},
			want:       nil,
		},
		{
			name:       "a current container that is not listed",
			currentID:  currentID,
			containers: []*container.Container{old},
			want:       nil,
		},
		{
			name:       "a current container that is not old",
			currentID:  currentID,
			containers: []*container.Container{cleanupTestWatchtower(currentID, "watchtower", "nickfedor/watchtower:latest")},
			want:       nil,
		},
		{
			name:       "an old name on a container that is not Watchtower",
			currentID:  oldID,
			containers: []*container.Container{skipTestContainer(oldID, "watchtower-old-01d000000000", nil)},
			want:       nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Same(t, tt.want, currentOldInstance(tt.containers, tt.currentID))
		})
	}
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

		graph, skipped, err := checkDependencies(log, updateParams.Params{}, progress, all, monitored)
		require.NoError(t, err)

		assert.NotNil(t, graph)
		assert.Equal(t, map[container.ContainerID]struct{}{self.ID(): {}, a.ID(): {}, b.ID(): {}}, skipped)
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

	t.Run("reports a cycle member that also names itself only as a cycle member", func(t *testing.T) {
		t.Parallel()

		// Each replica's depends-on names its own service, which resolves to
		// both replicas, so each depends on itself and on the other.
		replica := func(number string) *container.Container {
			return skipTestContainer(containerfixture.RandomID(), "proj-web-"+number, map[string]string{
				"com.docker.compose.project":          "proj",
				"com.docker.compose.service":          "web",
				"com.docker.compose.container-number": number,
				labels.DependsOn:                      "web",
			})
		}

		first, second := replica("1"), replica("2")
		monitored := []*container.Container{first, second}

		log, buf := logcapture.NewJSON(zerolog.DebugLevel)
		progress := &state.Progress{}

		_, skipped, err := checkDependencies(log, updateParams.Params{}, progress, monitored, monitored)
		require.NoError(t, err)

		assert.Equal(t, map[container.ContainerID]struct{}{first.ID(): {}, second.ID(): {}}, skipped)
		assert.Equal(t, map[string]string{
			"proj-web-1": errCircularDependency.Error(),
			"proj-web-2": errCircularDependency.Error(),
		}, reportErrors(progress.Report(log).Skipped()))
		assert.Equal(t, []string{logging.CircularDependency, logging.CircularDependency},
			logcapture.Messages(t, buf.String(), "warn"))
	})

	t.Run("fails with the sort error when the graph cannot be built", func(t *testing.T) {
		t.Parallel()

		monitored := []*container.Container{
			skipTestContainer("a000000000000000000000000000000000000000000000000000000000000000", "app", nil),
			skipTestContainer("b000000000000000000000000000000000000000000000000000000000000000", "app", nil),
		}

		graph, skipped, err := checkDependencies(newTestLogger(t), updateParams.Params{}, &state.Progress{},
			monitored, monitored)

		require.ErrorIs(t, err, errSortDependenciesFailed)
		assert.Nil(t, graph)
		assert.Nil(t, skipped)
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
		restarts := planTestRestarts(t, rebuild.NewSession(nil, composeApply.NewClient(dockerConfig.Docker{})), updateParams.Params{}, &state.Progress{},
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

		// The session passes every cycle member in skipped. Leaving this
		// cycle out of it makes the first sort fail.
		a := skipTestContainer("a-id", "a", map[string]string{labels.DependsOn: "b"})
		b := skipTestContainer("b-id", "b", map[string]string{labels.DependsOn: "a"})
		monitored := []*container.Container{a, b}

		log := newTestLogger(t)
		graph, err := sorter.NewDependencyGraph(log, monitored, nil, false)
		require.NoError(t, err)

		restarts, err := planRestarts(log, t.Context(), newMockClient(t), updateParams.Params{},
			rebuild.NewSession(nil, composeApply.NewClient(dockerConfig.Docker{})), &state.Progress{}, graph, monitored, monitored, nil)

		require.ErrorIs(t, err, errSortDependenciesFailed)
		assert.Nil(t, restarts)
	})
}

// TestPlanRestarts_rebinds verifies that the containers sharing the network
// namespace of a container to restart are rebound after it, stopped ones
// included, whether they are unmonitored, monitor-only or skipped by the
// dependency phase. The stopped ones are
// listed only when something restarts.
func TestPlanRestarts_rebinds(t *testing.T) {
	t.Parallel()

	// joining returns a container in the network namespace of vpn.
	joining := func(name string, containerLabels map[string]string) *container.Container {
		return modelfixture.Container(
			containerfixture.WithName("/"+name),
			containerfixture.WithID(name+"-id"),
			containerfixture.WithNetworkMode("container:vpn"),
			containerfixture.WithLabels(containerLabels),
		)
	}

	t.Run("rebinds unmonitored, monitor-only and stopped dependents", func(t *testing.T) {
		t.Parallel()

		vpn := skipTestContainer("vpn-id", "vpn", nil)
		vpn.SetStale(true)

		excluded := joining("excluded", nil)
		monitorOnly := joining("monitor-only", map[string]string{labels.MonitorOnly: "true"})
		stopped := joining("stopped", nil)
		other := skipTestContainer("other-id", "other", nil)

		all := []*container.Container{vpn, other, excluded, monitorOnly}
		monitored := []*container.Container{vpn, monitorOnly}

		log := newTestLogger(t)
		graph, err := sorter.NewDependencyGraph(log, monitored, []*container.Container{other, excluded}, false)
		require.NoError(t, err)

		client := mockUpdate.NewMockClient(t)
		client.EXPECT().ListNetworkDependents(mock.Anything).
			Return([]*container.Container{stopped}, nil).Once()

		restarts, err := planRestarts(log, t.Context(), client, updateParams.Params{},
			rebuild.NewSession(nil, composeApply.NewClient(dockerConfig.Docker{})), &state.Progress{}, graph, all, monitored, nil)
		require.NoError(t, err)

		assert.Equal(t, []*container.Container{vpn, excluded, monitorOnly, stopped}, restarts)

		for _, c := range []*container.Container{excluded, monitorOnly, stopped} {
			assert.True(t, c.IsRebinding(), c.Name())
		}

		assert.False(t, vpn.IsRebinding())
	})

	t.Run("rebinds a dependent the dependency phase skipped", func(t *testing.T) {
		t.Parallel()

		vpn := skipTestContainer("vpn-id", "vpn", nil)
		vpn.SetStale(true)

		cyclic := joining("cyclic", nil)
		monitored := []*container.Container{vpn, cyclic}

		log := newTestLogger(t)
		graph, err := sorter.NewDependencyGraph(log, monitored, nil, false)
		require.NoError(t, err)

		client := mockUpdate.NewMockClient(t)
		client.EXPECT().ListNetworkDependents(mock.Anything).Return(nil, nil).Once()

		// The session passes a cycle member in skipped. It keeps the image
		// it runs, so it is still rebound rather than left on a removed
		// namespace.
		restarts, err := planRestarts(log, t.Context(), client, updateParams.Params{},
			rebuild.NewSession(nil, composeApply.NewClient(dockerConfig.Docker{})), &state.Progress{}, graph, monitored, monitored,
			map[container.ContainerID]struct{}{cyclic.ID(): {}})
		require.NoError(t, err)

		assert.Equal(t, []*container.Container{vpn, cyclic}, restarts)
		assert.True(t, cyclic.IsRebinding())
	})

	t.Run("lists no stopped dependents without restarts", func(t *testing.T) {
		t.Parallel()

		vpn := skipTestContainer("vpn-id", "vpn", nil)
		monitored := []*container.Container{vpn}

		log := newTestLogger(t)
		graph, err := sorter.NewDependencyGraph(log, monitored, nil, false)
		require.NoError(t, err)

		// The strict mock fails on a call to ListNetworkDependents.
		restarts, err := planRestarts(log, t.Context(), mockUpdate.NewMockClient(t), updateParams.Params{},
			rebuild.NewSession(nil, composeApply.NewClient(dockerConfig.Docker{})), &state.Progress{}, graph, monitored, monitored, nil)
		require.NoError(t, err)
		assert.Empty(t, restarts)
	})

	t.Run("a failed listing ends the plan", func(t *testing.T) {
		t.Parallel()

		vpn := skipTestContainer("vpn-id", "vpn", nil)
		vpn.SetStale(true)

		monitored := []*container.Container{vpn}
		listErr := errors.New("failed to list containers: daemon unavailable")

		log := newTestLogger(t)
		graph, err := sorter.NewDependencyGraph(log, monitored, nil, false)
		require.NoError(t, err)

		client := mockUpdate.NewMockClient(t)
		client.EXPECT().ListNetworkDependents(mock.Anything).Return(nil, listErr).Once()

		_, err = planRestarts(log, t.Context(), client, updateParams.Params{},
			rebuild.NewSession(nil, composeApply.NewClient(dockerConfig.Docker{})), &state.Progress{}, graph, monitored, monitored, nil)
		require.ErrorIs(t, err, listErr)
	})
}

// TestPlanRestarts_rebindsComposeCreated verifies that a container a Compose
// apply created during planning is rebound after a container Watchtower
// recreates when it joins that container's network namespace, including one
// that joins another rebound container Compose created. A container that joins
// a container Compose applied already joins its replacement and is left
// alone. The containers Compose applied are left out of the restarts.
func TestPlanRestarts_rebindsComposeCreated(t *testing.T) {
	t.Parallel()

	// created returns a container that Compose created for service, in the
	// network namespace that networkMode names.
	created := func(service, networkMode string) *container.Container {
		return modelfixture.Container(
			containerfixture.WithName("/"+service),
			containerfixture.WithID("new-"+service+"-id"),
			containerfixture.WithNetworkMode(networkMode),
		)
	}

	// plan applies the services through an applier that reports newContainers,
	// which the client returns on inspection, and plans the session's restarts
	// with recreated, a stale container Watchtower recreates itself.
	plan := func(
		t *testing.T,
		recreated *container.Container,
		services []string,
		newContainers ...*container.Container,
	) []*container.Container {
		t.Helper()

		applied := make([]composeApply.Container, 0, len(newContainers))

		client := newMockClient(t)

		for i, c := range newContainers {
			applied = append(applied, composeApply.Container{Service: services[i], Name: c.Name(), ID: c.ID()})
			client.EXPECT().GetContainer(mock.Anything, c.ID()).Return(c, nil).Once()
		}

		applier := mockRebuild.NewMockApplier(t)
		applier.EXPECT().Apply(mock.Anything, mock.Anything).Return(applied, nil).Once()

		config := updateParams.Params{Git: gitConfig.Defaults{Enable: true}}
		progress := &state.Progress{}
		members, session := staleComposeServices(t, config, progress, applier, services...)

		recreated.SetStale(true)

		monitored := append([]*container.Container{recreated}, members...)

		log := newTestLogger(t)
		graph, err := sorter.NewDependencyGraph(log, monitored, nil, false)
		require.NoError(t, err)

		restarts, err := planRestarts(log, t.Context(), client, config, session, progress,
			graph, monitored, monitored, nil)
		require.NoError(t, err)

		return restarts
	}

	t.Run("rebinds a joiner of a container Watchtower recreates", func(t *testing.T) {
		t.Parallel()

		vpn := skipTestContainer("vpn-id", "vpn", nil)
		app := created("app", "container:vpn")

		restarts := plan(t, vpn, []string{"app"}, app)

		assert.Equal(t, []*container.Container{vpn, app}, restarts)
		assert.True(t, app.IsRebinding())
	})

	t.Run("rebinds a joiner of a rebound container Compose created", func(t *testing.T) {
		t.Parallel()

		vpn := skipTestContainer("vpn-id", "vpn", nil)
		app := created("app", "container:vpn")
		worker := created("worker", "container:app")

		restarts := plan(t, vpn, []string{"app", "worker"}, app, worker)

		assert.Equal(t, []*container.Container{vpn, app, worker}, restarts)
		assert.True(t, worker.IsRebinding())
	})

	t.Run("leaves a joiner of an applied container alone", func(t *testing.T) {
		t.Parallel()

		db := skipTestContainer("db-id", "db", nil)
		vpn := created("vpn", "")
		app := created("app", "container:vpn")

		restarts := plan(t, db, []string{"vpn", "app"}, vpn, app)

		assert.Equal(t, []*container.Container{db}, restarts)
		assert.False(t, app.IsRebinding())
	})
}

// TestInspectUnlisted verifies that only the containers the listing does not
// hold are inspected, each once and in the given order. A removed container is
// left out quietly. A failed inspection warns and moves on, and an ended
// session stops the inspections without a warning.
func TestInspectUnlisted(t *testing.T) {
	t.Parallel()

	listed := skipTestContainer("listed-id", "listed", nil)

	t.Run("inspects each unlisted container once", func(t *testing.T) {
		t.Parallel()

		first := skipTestContainer("first-id", "first", nil)
		second := skipTestContainer("second-id", "second", nil)

		client := mockUpdate.NewMockClient(t)
		client.EXPECT().GetContainer(mock.Anything, first.ID()).Return(first, nil).Once()
		client.EXPECT().GetContainer(mock.Anything, second.ID()).Return(second, nil).Once()

		got := inspectUnlisted(newTestLogger(t), t.Context(), client,
			[]container.ContainerID{first.ID(), listed.ID(), second.ID(), first.ID()},
			[]*container.Container{listed})
		assert.Equal(t, []*container.Container{first, second}, got)
	})

	// A failed inspection warns and moves on. A removed container is left
	// out quietly.
	for _, tt := range []struct {
		name     string
		err      error
		wantWarn []string
	}{
		{
			name:     "a failed inspection warns and moves on",
			err:      errors.New("failed to inspect container: daemon unavailable"),
			wantWarn: []string{logging.ComposeContainerUninspected},
		},
		{
			name:     "leaves out a container that no longer exists",
			err:      fmt.Errorf("failed to inspect container: %w", cerrdefs.ErrNotFound),
			wantWarn: nil,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			kept := skipTestContainer("kept-id", "kept", nil)

			client := mockUpdate.NewMockClient(t)
			client.EXPECT().GetContainer(mock.Anything, container.ContainerID("lost-id")).Return(nil, tt.err).Once()
			client.EXPECT().GetContainer(mock.Anything, kept.ID()).Return(kept, nil).Once()

			log, logs := logcapture.NewJSON(zerolog.DebugLevel)

			got := inspectUnlisted(log, t.Context(), client, []container.ContainerID{"lost-id", kept.ID()}, nil)
			assert.Equal(t, []*container.Container{kept}, got)
			assert.Equal(t, tt.wantWarn, logcapture.Messages(t, logs.String(), "warn"))
		})
	}

	t.Run("an ended session stops the inspections", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		// The strict mock fails on a second inspection.
		client := mockUpdate.NewMockClient(t)
		client.EXPECT().GetContainer(mock.Anything, container.ContainerID("first-id")).
			Return(nil, fmt.Errorf("failed to inspect container: %w", context.Canceled)).Once()

		log, logs := logcapture.NewJSON(zerolog.DebugLevel)

		got := inspectUnlisted(log, ctx, client, []container.ContainerID{"first-id", "second-id"}, nil)
		assert.Empty(t, got)
		assert.Empty(t, logcapture.Messages(t, logs.String(), "warn"))
	})

	t.Run("inspects nothing without IDs", func(t *testing.T) {
		t.Parallel()

		// The strict mock fails on any call.
		got := inspectUnlisted(newTestLogger(t), t.Context(), mockUpdate.NewMockClient(t), nil,
			[]*container.Container{listed})
		assert.Empty(t, got)
	})
}
