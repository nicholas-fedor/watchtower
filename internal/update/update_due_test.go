package update

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"
	dockerNetwork "github.com/moby/moby/api/types/network"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/filters"
	"github.com/nicholas-fedor/watchtower/internal/registry/ratelimit"
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
)

// dueTestContainer returns a running container whose image is a day old,
// with the given labels.
func dueTestContainer(name string, labels map[string]string) *container.Container {
	return mockUpdate.CreateMockContainerWithConfig(
		name,
		"/"+name,
		"fake-"+name+":latest",
		true,
		false,
		time.Now().AddDate(0, 0, -1),
		&dockerContainer.Config{
			Labels:       labels,
			ExposedPorts: dockerNetwork.PortSet{},
		},
	)
}

// dueNamed returns a Due filter that accepts the named containers.
func dueNamed(names ...string) filters.Filter {
	return func(c filters.FilterableContainer) bool {
		for _, name := range names {
			if c.Name() == "/"+name || c.Name() == name {
				return true
			}
		}

		return false
	}
}

// statusNames returns the names of the statuses.
func statusNames(statuses []*state.ContainerStatus) []string {
	names := make([]string, 0, len(statuses))
	for _, s := range statuses {
		names = append(names, s.Name())
	}

	return names
}

// TestUpdate_Due verifies that a session with Due checks only the due
// containers, and that a container that is not due still restarts when a
// container it depends on is updated, as it does in a session that checks
// every container.
func TestUpdate_Due(t *testing.T) {
	t.Parallel()

	dependsOn := "com.centurylinklabs.watchtower.depends-on"

	t.Run("dependent that is not due restarts with its updated dependency", func(t *testing.T) {
		t.Parallel()

		provider := dueTestContainer("provider", map[string]string{})
		dependent := dueTestContainer("dependent", map[string]string{dependsOn: "provider"})

		client := mockUpdate.CreateFakeClient(&mockUpdate.TestData{
			Containers: []*container.Container{provider, dependent},
			Staleness:  map[string]bool{"provider": true, "dependent": true},
		}, false, false)

		report, _, err := update(newTestLogger(t), t.Context(), client,
			updateParams.Params{Due: dueNamed("provider")}, ratelimit.New())
		require.NoError(t, err)

		assert.ElementsMatch(t, []string{"provider"}, statusNames(report.Updated()))
		assert.ElementsMatch(t, []string{"dependent"}, statusNames(report.Restarted()))
	})

	t.Run("stale container that is not due is left alone", func(t *testing.T) {
		t.Parallel()

		due := dueTestContainer("due", map[string]string{})
		other := dueTestContainer("other", map[string]string{})

		client := mockUpdate.CreateFakeClient(&mockUpdate.TestData{
			Containers: []*container.Container{due, other},
			Staleness:  map[string]bool{"due": false, "other": true},
		}, false, false)

		report, _, err := update(newTestLogger(t), t.Context(), client,
			updateParams.Params{Due: dueNamed("due")}, ratelimit.New())
		require.NoError(t, err)

		assert.Empty(t, report.Updated())
		assert.Empty(t, report.Restarted())
		assert.ElementsMatch(t, []string{"due"}, statusNames(report.Scanned()))
	})

	t.Run("dependency that is not due is not updated for its due dependent", func(t *testing.T) {
		t.Parallel()

		provider := dueTestContainer("provider", map[string]string{})
		dependent := dueTestContainer("dependent", map[string]string{dependsOn: "provider"})

		client := mockUpdate.CreateFakeClient(&mockUpdate.TestData{
			Containers: []*container.Container{provider, dependent},
			Staleness:  map[string]bool{"provider": true, "dependent": false},
		}, false, false)

		report, _, err := update(newTestLogger(t), t.Context(), client,
			updateParams.Params{Due: dueNamed("dependent")}, ratelimit.New())
		require.NoError(t, err)

		assert.Empty(t, report.Updated())
		assert.Empty(t, report.Restarted())
	})

	t.Run("chain restarts through containers that are not due", func(t *testing.T) {
		t.Parallel()

		a := dueTestContainer("a", map[string]string{})
		b := dueTestContainer("b", map[string]string{dependsOn: "a"})
		c := dueTestContainer("c", map[string]string{dependsOn: "b"})

		client := mockUpdate.CreateFakeClient(&mockUpdate.TestData{
			Containers: []*container.Container{a, b, c},
			Staleness:  map[string]bool{"a": true, "b": true, "c": true},
		}, false, false)

		report, _, err := update(newTestLogger(t), t.Context(), client,
			updateParams.Params{Due: dueNamed("a")}, ratelimit.New())
		require.NoError(t, err)

		assert.ElementsMatch(t, []string{"a"}, statusNames(report.Updated()))
		assert.ElementsMatch(t, []string{"b", "c"}, statusNames(report.Restarted()))
	})
}

// TestUpdate_Listed verifies that a session hands every container it lists to
// Listed, managed or not, so its caller need not list them again.
func TestUpdate_Listed(t *testing.T) {
	t.Parallel()

	client := mockUpdate.CreateFakeClient(&mockUpdate.TestData{
		Containers: []*container.Container{
			dueTestContainer("web", map[string]string{}),
			dueTestContainer("other", map[string]string{}),
		},
	}, false, false)

	var listed []string

	_, _, err := update(newTestLogger(t), t.Context(), client, updateParams.Params{
		Filter: dueNamed("web"),
		Listed: func(containers []filters.FilterableContainer) {
			for _, c := range containers {
				listed = append(listed, c.Name())
			}
		},
	}, ratelimit.New())
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"web", "other"}, listed)
}

// TestDueFilter_PostChecksFollowTheSessionStart verifies that the post-checks
// run in the containers due when the session began: an updated container's
// replacement matches by name even if its labels changed, and a container
// created during the session does not match.
func TestDueFilter_PostChecksFollowTheSessionStart(t *testing.T) {
	t.Parallel()

	const tier = "tier"

	dueByLabel := func(c filters.FilterableContainer) bool {
		value, _ := c.GetLabel(tier)

		return value == "fast"
	}

	web := dueTestContainer("web", map[string]string{tier: "fast"})
	filter := dueFilter(updateParams.Params{Due: dueByLabel}, []*container.Container{web})

	assert.True(t, filter(dueTestContainer("web", map[string]string{tier: "slow"})),
		"the replacement of a due container lost its post-check")
	assert.False(t, filter(dueTestContainer("newcomer", map[string]string{tier: "fast"})),
		"a container created during the session got a post-check")

	assert.Nil(t, dueFilter(updateParams.Params{}, nil), "without Due the session filter applies")
}
