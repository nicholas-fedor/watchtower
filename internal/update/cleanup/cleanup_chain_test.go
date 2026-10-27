package cleanup

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"

	mockContainerClient "github.com/nicholas-fedor/watchtower/internal/container/client/mocks"
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Container IDs used by the chain tests.
const (
	// chainCurrentID is the running Watchtower container.
	chainCurrentID = "c000000000000000000000000000000000000000000000000000000000000000"
	// chainOldAID is a Watchtower container the current one replaced.
	chainOldAID = "a000000000000000000000000000000000000000000000000000000000000000"
	// chainOldBID is another Watchtower container the current one replaced.
	chainOldBID = "b000000000000000000000000000000000000000000000000000000000000000"
	// chainAppID is a container that is not a Watchtower container.
	chainAppID = "d000000000000000000000000000000000000000000000000000000000000000"
)

// chainTestContainer returns a running container with the given labels.
func chainTestContainer(id, name string, labels map[string]string) types.Container {
	return mockUpdate.CreateMockContainerWithConfig(
		id,
		"/"+name,
		"org/"+name+":1",
		true,
		false,
		time.Now(),
		&dockerContainer.Config{Image: "org/" + name + ":1", Labels: labels},
	)
}

// chainTestContainers returns the current Watchtower container with the given
// chain label, two Watchtower containers it may have replaced, and an
// ordinary container.
func chainTestContainers(chain string) (types.Container, []types.Container) {
	current := chainTestContainer(chainCurrentID, "watchtower", map[string]string{
		"com.centurylinklabs.watchtower":                 "true",
		"com.centurylinklabs.watchtower.container-chain": chain,
	})
	oldA := chainTestContainer(chainOldAID, "watchtower-a", map[string]string{"com.centurylinklabs.watchtower": "true"})
	oldB := chainTestContainer(chainOldBID, "watchtower-b", map[string]string{"com.centurylinklabs.watchtower": "true"})
	app := chainTestContainer(chainAppID, "app", map[string]string{})

	return current, []types.Container{current, oldA, oldB, app}
}

// containerIDs returns the ID of each container, in order.
func containerIDs(containers []types.Container) []types.ContainerID {
	ids := make([]types.ContainerID, 0, len(containers))
	for _, c := range containers {
		ids = append(ids, c.ID())
	}

	return ids
}

// TestGetChainedContainers verifies which containers named in the current
// Watchtower container's chain label are returned for removal.
func TestGetChainedContainers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		chain string
		want  []types.ContainerID
	}{
		{
			name:  "Watchtower containers in the chain",
			chain: chainOldAID + "," + chainOldBID,
			want:  []types.ContainerID{chainOldAID, chainOldBID},
		},
		{
			name:  "a container that is not Watchtower is never returned",
			chain: chainOldAID + "," + chainAppID,
			want:  []types.ContainerID{chainOldAID},
		},
		{
			name:  "spaces around IDs",
			chain: " " + chainOldAID + " , " + chainOldBID + " ",
			want:  []types.ContainerID{chainOldAID, chainOldBID},
		},
		{
			name:  "empty entries",
			chain: "," + chainOldAID + ",, ," + chainOldBID + ",",
			want:  []types.ContainerID{chainOldAID, chainOldBID},
		},
		{
			name:  "the current container is never returned",
			chain: chainOldAID + "," + chainCurrentID,
			want:  []types.ContainerID{chainOldAID},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			current, all := chainTestContainers(tt.chain)

			got := getChainedContainers(newTestLogger(t), all, current)

			assert.Equal(t, tt.want, containerIDs(got))
		})
	}
}

// TestRemoveExcessWatchtowerInstances_ChainKeepsOtherContainers verifies that
// startup cleanup leaves a container that is not Watchtower running when the
// chain label names it, and still removes the Watchtower container it names.
func TestRemoveExcessWatchtowerInstances_ChainKeepsOtherContainers(t *testing.T) {
	t.Parallel()

	current := chainTestContainer(chainCurrentID, "watchtower", map[string]string{
		"com.centurylinklabs.watchtower":                 "true",
		"com.centurylinklabs.watchtower.container-chain": chainOldAID + "," + chainAppID,
	})
	oldA := chainTestContainer(chainOldAID, "watchtower-a", map[string]string{"com.centurylinklabs.watchtower": "true"})
	app := chainTestContainer(chainAppID, "app", map[string]string{})

	// The mock fails the test on any other stop.
	client := mockContainerClient.NewMockClient(t)
	client.EXPECT().ListContainers(mock.Anything, mock.Anything).Return([]types.Container{current, oldA, app}, nil)
	client.EXPECT().StopAndRemoveContainer(mock.Anything, oldA, mock.Anything).Return(nil).Once()

	removed, err := RemoveExcessWatchtowerInstances(newTestLogger(t), t.Context(), client, false, "", nil, current)
	require.NoError(t, err)
	assert.Equal(t, 1, removed)
}

// TestGetChainedContainers_OldCurrent verifies the chained containers when the
// current container is an old predecessor. Its successor is the non-old
// Watchtower container in its scope whose chain label lists it, and that
// successor's chain label names the containers to remove. Without such a
// successor nothing is removed through the chain.
func TestGetChainedContainers_OldCurrent(t *testing.T) {
	t.Parallel()

	const (
		oldID       = "e000000000000000000000000000000000000000000000000000000000000000"
		successorID = "f000000000000000000000000000000000000000000000000000000000000000"
	)

	watchtower := func(id, name, scope, chain string) types.Container {
		containerLabels := map[string]string{"com.centurylinklabs.watchtower": "true"}
		if scope != "" {
			containerLabels["com.centurylinklabs.watchtower.scope"] = scope
		}

		if chain != "" {
			containerLabels["com.centurylinklabs.watchtower.container-chain"] = chain
		}

		return chainTestContainer(id, name, containerLabels)
	}

	old := watchtower(oldID, "watchtower-old-e00000000000", "", "")
	oldA := watchtower(chainOldAID, "watchtower-a", "", "")

	tests := []struct {
		name       string
		candidates []types.Container
		want       []types.ContainerID
	}{
		{
			name:       "successor whose chain lists the old container",
			candidates: []types.Container{watchtower(successorID, "watchtower", "", oldID+","+chainOldAID)},
			want:       []types.ContainerID{oldID, chainOldAID},
		},
		{
			name: "chain match wins over an earlier container without a chain label",
			candidates: []types.Container{
				watchtower(chainOldBID, "watchtower-b", "", ""),
				watchtower(successorID, "watchtower", "", oldID+","+chainOldAID),
			},
			want: []types.ContainerID{oldID, chainOldAID},
		},
		{
			name:       "container without a chain label",
			candidates: []types.Container{watchtower(successorID, "watchtower", "", "")},
			want:       []types.ContainerID{},
		},
		{
			name:       "chain that does not list the old container",
			candidates: []types.Container{watchtower(successorID, "watchtower", "", chainOldAID)},
			want:       []types.ContainerID{},
		},
		{
			name:       "chain match in another scope",
			candidates: []types.Container{watchtower(successorID, "watchtower", "other", oldID+","+chainOldAID)},
			want:       []types.ContainerID{},
		},
		{
			name:       "chain match on another old container",
			candidates: []types.Container{watchtower(successorID, "watchtower-old-f00000000000", "", oldID+","+chainOldAID)},
			want:       []types.ContainerID{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			all := append([]types.Container{old, oldA}, tt.candidates...)

			got := getChainedContainers(newTestLogger(t), all, old)

			assert.Equal(t, tt.want, containerIDs(got))
		})
	}
}
