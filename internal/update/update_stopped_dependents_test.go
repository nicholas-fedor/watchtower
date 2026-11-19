package update

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/filters"
	"github.com/nicholas-fedor/watchtower/internal/registry/ratelimit"
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
)

// stoppedDependentsTestData returns a stale or fresh provider "vpn", a running
// container "app" in its network namespace, and the stopped containers
// "off" and "other". "off" joins the provider's network namespace and
// "other" joins the namespace of a container that is not listed. It returns
// "off" too.
func stoppedDependentsTestData(providerStale bool) (*mockUpdate.TestData, *container.Container) {
	newContainer := func(id, name string, running bool, networkMode string) *container.Container {
		c := mockUpdate.CreateMockContainerWithConfig(id, "/"+name, name+":latest", running, false,
			time.Now(), &dockerContainer.Config{Image: name + ":latest"})
		c.ContainerInfo().HostConfig.NetworkMode = dockerContainer.NetworkMode(networkMode)

		return c
	}

	vpn := newContainer("vpn-id", "vpn", true, "bridge")
	app := newContainer("app-id", "app", true, "container:vpn")
	off := newContainer("off-id", "off", false, "container:vpn")
	other := newContainer("other-id", "other", false, "container:gone")

	return &mockUpdate.TestData{
		Staleness:         map[string]bool{"vpn": providerStale, "app": false},
		Containers:        []*container.Container{vpn, app},
		StoppedContainers: []*container.Container{off, other},
	}, off
}

// countOperations returns how often the client recorded the operation.
func countOperations(testData *mockUpdate.TestData, name string) int {
	count := 0

	for _, op := range testData.OperationOrder {
		if op == name {
			count++
		}
	}

	return count
}

// TestUpdate_StoppedNetworkDependents verifies that a stopped container in the
// network namespace of an updated container is recreated after it and left
// stopped, without an update check of its own, when stopped containers are
// not included. Stopped containers whose network container does not restart,
// and stopped containers the filter excludes, are left alone.
func TestUpdate_StoppedNetworkDependents(t *testing.T) {
	t.Parallel()

	t.Run("recreates a stopped dependent of an updated container and keeps it stopped", func(t *testing.T) {
		t.Parallel()

		testData, _ := stoppedDependentsTestData(true)
		client := mockUpdate.CreateFakeClient(testData, false, false)

		report, _, err := update(newTestLogger(t), t.Context(), client, updateParams.Params{}, ratelimit.New())
		require.NoError(t, err)

		assert.Equal(t, []string{"vpn", "app", "off"}, testData.CreateOrder,
			"the stopped dependent is recreated after the container whose network it joins")
		assert.Equal(t, 2, countOperations(testData, "StartContainerByID"),
			"only the containers that were running are started")
		assert.Equal(t, int32(2), testData.IsContainerStaleCount.Load(),
			"the stopped dependent is not checked for updates")

		restarted := make([]string, 0, len(report.Restarted()))
		for _, status := range report.Restarted() {
			restarted = append(restarted, status.Name())
		}

		slices.Sort(restarted)
		assert.Equal(t, []string{"app", "off"}, restarted)
	})

	t.Run("leaves a stopped dependent of a fresh container alone", func(t *testing.T) {
		t.Parallel()

		testData, _ := stoppedDependentsTestData(false)
		client := mockUpdate.CreateFakeClient(testData, false, false)

		report, _, err := update(newTestLogger(t), t.Context(), client, updateParams.Params{}, ratelimit.New())
		require.NoError(t, err)

		assert.Empty(t, testData.CreateOrder)
		assert.Len(t, report.Scanned(), 2, "stopped containers are not reported when nothing recreates them")
	})

	t.Run("leaves a stopped dependent the filter excludes alone", func(t *testing.T) {
		t.Parallel()

		testData, off := stoppedDependentsTestData(true)
		client := mockUpdate.CreateFakeClient(testData, false, false)

		params := updateParams.Params{
			Filter: func(c filters.FilterableContainer) bool { return c.Name() != off.Name() },
		}

		_, _, err := update(newTestLogger(t), t.Context(), client, params, ratelimit.New())
		require.NoError(t, err)

		assert.Equal(t, []string{"vpn", "app"}, testData.CreateOrder)
	})

	t.Run("recreates a stopped container listed already once", func(t *testing.T) {
		t.Parallel()

		testData, off := stoppedDependentsTestData(true)
		testData.Containers = append(testData.Containers, off)
		testData.Staleness[off.Name()] = false
		client := mockUpdate.CreateFakeClient(testData, false, false)

		_, _, err := update(newTestLogger(t), t.Context(), client, updateParams.Params{}, ratelimit.New())
		require.NoError(t, err)

		assert.Equal(t, []string{"vpn", "app", "off"}, testData.CreateOrder)
	})

	t.Run("leaves stopped dependents alone with revive-stopped", func(t *testing.T) {
		t.Parallel()

		testData, _ := stoppedDependentsTestData(true)
		client := mockUpdate.CreateFakeClient(testData, false, false)

		_, _, err := update(newTestLogger(t), t.Context(), client, updateParams.Params{ReviveStopped: true}, ratelimit.New())
		require.NoError(t, err)

		assert.Equal(t, []string{"vpn", "app"}, testData.CreateOrder,
			"revive-stopped would start a recreated stopped dependent")
	})

	t.Run("goes on without stopped dependents when listing them fails", func(t *testing.T) {
		t.Parallel()

		testData, _ := stoppedDependentsTestData(true)
		testData.ListStoppedError = errors.New("list failed")
		client := mockUpdate.CreateFakeClient(testData, false, false)

		_, _, err := update(newTestLogger(t), t.Context(), client, updateParams.Params{}, ratelimit.New())
		require.NoError(t, err)

		assert.Equal(t, []string{"vpn", "app"}, testData.CreateOrder)
	})
}
