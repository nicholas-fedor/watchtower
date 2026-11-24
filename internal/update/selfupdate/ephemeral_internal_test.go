package selfupdate

import (
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	cerrdefs "github.com/containerd/errdefs"
	dockerContainer "github.com/moby/moby/api/types/container"
	dockerNetwork "github.com/moby/moby/api/types/network"

	"github.com/nicholas-fedor/watchtower/internal/container"
	containerImage "github.com/nicholas-fedor/watchtower/internal/container/image"
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	mockSelfupdate "github.com/nicholas-fedor/watchtower/internal/update/selfupdate/mocks"
)

// handoffNewID is the ID of the replacement container the mocked client starts.
const handoffNewID container.ContainerID = "wt-new-id"

// createEphemeralHandoffContainer builds a running Watchtower mock used by
// orchestrateSelfUpdate handoff tests.
//
// Parameters:
//   - id: Container ID.
//   - portBindings: Optional host port bindings. Nil yields empty bindings.
//
// Returns:
//   - *container.Container: Configured mock container.
func createEphemeralHandoffContainer(
	id string,
	portBindings dockerNetwork.PortMap,
) *container.Container {
	if portBindings == nil {
		portBindings = dockerNetwork.PortMap{}
	}

	const (
		name  = "/watchtower"
		image = "watchtower:v1"
	)

	ctr := mockUpdate.CreateMockContainerWithConfig(
		id,
		name,
		image,
		true,
		false,
		time.Now(),
		&dockerContainer.Config{
			Image: image,
			Labels: map[string]string{
				"com.centurylinklabs.watchtower": "true",
			},
		},
	)

	info := ctr.ContainerInfo()
	if info != nil && info.HostConfig != nil {
		info.HostConfig.PortBindings = portBindings
	}

	return ctr
}

// handoffEnv returns the orchestrator environment for replacing the
// container with the given ID by one running watchtower:v2 under the name
// watchtower, with no container chain, network container or image cleanup.
//
// Parameters:
//   - oldID: The old container's ID.
//
// Returns:
//   - orchestratorEnv: The environment the old instance would pass.
func handoffEnv(oldID string) orchestratorEnv {
	return orchestratorEnv{
		oldID:            oldID,
		newImage:         orchestratorNewImage,
		originalName:     orchestratorOriginalName,
		containerChain:   "",
		networkContainer: "",
		cleanup:          false,
	}
}

// handoffCalls are the expected client calls of a self-update handoff, so a
// test can order them.
type handoffCalls struct {
	stop   *mock.Call
	rename *mock.Call
	start  *mock.Call
}

// expectHandoffStart expects the old container to be inspected, stopped,
// renamed out of the way, and then used to create and start the replacement,
// which startErr fails when set.
//
// Parameters:
//   - client: Mocked container client.
//   - old: The Watchtower container being replaced.
//   - startErr: Error the replacement start returns.
//
// Returns:
//   - handoffCalls: The stop, rename, and start expectations.
func expectHandoffStart(client *mockSelfupdate.MockHandoffClient, old *container.Container, startErr error) handoffCalls {
	client.EXPECT().GetContainer(mock.Anything, old.ID()).Return(old, nil).Once()

	newID := handoffNewID
	if startErr != nil {
		newID = ""
	}

	return handoffCalls{
		stop: client.EXPECT().StopContainer(mock.Anything, old, orchestratorStopTimeout).Return(nil).Once(),
		rename: client.EXPECT().RenameContainer(mock.Anything, old, old.OldWatchtowerName()).
			Return(nil).Once(),
		start: client.EXPECT().StartContainer(mock.Anything, old).Return(newID, startErr).Once(),
	}
}

// expectHandoffFinish expects the replacement to be verified running and the
// old container to be removed.
//
// Parameters:
//   - client: Mocked container client.
//   - old: The Watchtower container being replaced.
//
// Returns:
//   - *mock.Call: The removal expectation.
func expectHandoffFinish(client *mockSelfupdate.MockHandoffClient, old *container.Container) *mock.Call {
	client.EXPECT().GetContainer(mock.Anything, handoffNewID).
		Return(createEphemeralHandoffContainer(string(handoffNewID), nil), nil).Once()

	return client.EXPECT().RemoveContainer(mock.Anything, old).Return(nil).Once()
}

// expectHandoff expects a complete, successful handoff.
//
// Parameters:
//   - client: Mocked container client.
//   - old: The Watchtower container being replaced.
func expectHandoff(client *mockSelfupdate.MockHandoffClient, old *container.Container) {
	expectHandoffStart(client, old, nil)
	expectHandoffFinish(client, old)
}

// Test_orchestrateSelfUpdate verifies the order of the self-update handoff,
// that a failed stop or create leaves the old container in charge, that the
// replacement is created from the new image with the container chain, and
// that old image cleanup tolerates an image in use or already gone.
func Test_orchestrateSelfUpdate(t *testing.T) {
	t.Parallel()

	// Stop frees host ports. Rename keeps the stopped predecessor for recovery.
	// Remove runs only after the replacement is verified running.
	t.Run("stops, renames, creates, then removes the old container on success", func(t *testing.T) {
		t.Parallel()

		old := createEphemeralHandoffContainer("wt-old-id-ok", nil)
		client := mockSelfupdate.NewMockHandoffClient(t)
		calls := expectHandoffStart(client, old, nil)
		remove := expectHandoffFinish(client, old)
		mock.InOrder(calls.stop, calls.rename, calls.start, remove)

		err := orchestrateSelfUpdate(newTestLogger(t), t.Context(), client, handoffEnv("wt-old-id-ok"))
		require.NoError(t, err)
	})

	t.Run("stops the old container before create when host ports are published", func(t *testing.T) {
		t.Parallel()

		old := createEphemeralHandoffContainer(
			"wt-old-ports",
			dockerNetwork.PortMap{
				dockerNetwork.MustParsePort("8080/tcp"): {
					{HostIP: netip.MustParseAddr("0.0.0.0"), HostPort: "8080"},
				},
			},
		)
		require.True(t, old.HasExposedPorts())

		client := mockSelfupdate.NewMockHandoffClient(t)
		calls := expectHandoffStart(client, old, nil)
		expectHandoffFinish(client, old)
		mock.InOrder(calls.stop, calls.start)

		err := orchestrateSelfUpdate(newTestLogger(t), t.Context(), client, handoffEnv("wt-old-ports"))
		require.NoError(t, err)
	})

	t.Run("does not create a new container when stopping the old container fails", func(t *testing.T) {
		t.Parallel()

		// The old container is still running after the failed stop, so the
		// handoff ends before any rename or create.
		old := createEphemeralHandoffContainer("wt-old-stop-fail", nil)
		stopErr := errors.New("failed to stop container: simulated stop failure")
		client := mockSelfupdate.NewMockHandoffClient(t)
		client.EXPECT().GetContainer(mock.Anything, old.ID()).Return(old, nil).Twice()
		client.EXPECT().StopContainer(mock.Anything, old, orchestratorStopTimeout).
			Return(stopErr).Once()

		err := orchestrateSelfUpdate(newTestLogger(t), t.Context(), client, handoffEnv("wt-old-stop-fail"))
		require.Same(t, stopErr, err)
	})

	t.Run("restores and restarts the old container when create fails after rename", func(t *testing.T) {
		t.Parallel()

		// The predecessor gets its name back and is restarted, and is never
		// removed.
		old := createEphemeralHandoffContainer("wt-old-create-fail", nil)
		client := mockSelfupdate.NewMockHandoffClient(t)
		createErr := errors.New("failed to create container: simulated create failure")
		calls := expectHandoffStart(client, old, createErr)
		renameBack := client.EXPECT().RenameContainer(mock.Anything, old, "watchtower").Return(nil).Once()
		restart := client.EXPECT().StartContainerByID(mock.Anything, old.ID()).Return(nil).Once()
		mock.InOrder(calls.stop, calls.rename, calls.start, renameBack, restart)

		err := orchestrateSelfUpdate(newTestLogger(t), t.Context(), client, handoffEnv("wt-old-create-fail"))
		require.Same(t, createErr, err)
	})

	t.Run("pins Config.Image to the new image reference before create", func(t *testing.T) {
		t.Parallel()

		old := createEphemeralHandoffContainer("wt-old-pin", nil)
		client := mockSelfupdate.NewMockHandoffClient(t)
		calls := expectHandoffStart(client, old, nil)
		calls.start.Run(func(args mock.Arguments) {
			created, ok := args.Get(1).(*container.Container)
			require.True(t, ok)
			assert.Equal(t, "watchtower:v2", created.ImageName())
		})
		expectHandoffFinish(client, old)

		err := orchestrateSelfUpdate(newTestLogger(t), t.Context(), client, handoffEnv("wt-old-pin"))
		require.NoError(t, err)
	})

	// Docker stores a network container by the ID it had when the old
	// container was created. A network container recreated in the same
	// session has a new ID, so the replacement joins it by the name the old
	// instance passed. A network container that still exists is inspected
	// under its own name and stays as it is.
	t.Run("creates the replacement against the network container's name", func(t *testing.T) {
		t.Parallel()

		staleID := "container:" + strings.Repeat("ab", 32)

		tests := []struct {
			name             string
			networkMode      string
			networkContainer string
			want             string
		}{
			{
				name:             "stale ID replaced by the passed name",
				networkMode:      staleID,
				networkContainer: "vpn",
				want:             "container:vpn",
			},
			{
				name:             "existing network container kept",
				networkMode:      "container:vpn-old",
				networkContainer: "vpn",
				want:             "container:vpn-old",
			},
			{
				name:        "no name passed by an older instance",
				networkMode: staleID,
				want:        staleID,
			},
			{
				name:             "own network left as it is",
				networkMode:      "bridge",
				networkContainer: "vpn",
				want:             "bridge",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				old := createEphemeralHandoffContainer("wt-old-network", nil)
				old.ContainerInfo().HostConfig.NetworkMode = dockerContainer.NetworkMode(tt.networkMode)

				client := mockSelfupdate.NewMockHandoffClient(t)
				calls := expectHandoffStart(client, old, nil)
				calls.start.Run(func(args mock.Arguments) {
					created, ok := args.Get(1).(*container.Container)
					require.True(t, ok)
					assert.Equal(t, tt.want, string(created.GetCreateHostConfig().NetworkMode))
				})
				expectHandoffFinish(client, old)

				env := handoffEnv("wt-old-network")
				env.networkContainer = tt.networkContainer

				err := orchestrateSelfUpdate(newTestLogger(t), t.Context(), client, env)
				require.NoError(t, err)
			})
		}
	})

	t.Run("propagates the container chain label onto the source config used for create", func(t *testing.T) {
		t.Parallel()

		const chain = "prev-id,wt-old-chain"

		old := createEphemeralHandoffContainer("wt-old-chain", nil)
		client := mockSelfupdate.NewMockHandoffClient(t)
		calls := expectHandoffStart(client, old, nil)
		calls.start.Run(func(args mock.Arguments) {
			created, ok := args.Get(1).(*container.Container)
			require.True(t, ok)

			gotChain, present := created.GetContainerChain()
			assert.True(t, present)
			assert.Equal(t, chain, gotChain)
		})
		expectHandoffFinish(client, old)

		env := handoffEnv("wt-old-chain")
		env.containerChain = chain

		err := orchestrateSelfUpdate(newTestLogger(t), t.Context(), client, env)
		require.NoError(t, err)
	})

	t.Run("removes the old image after a successful handoff when cleanup is enabled", func(t *testing.T) {
		t.Parallel()

		old := createEphemeralHandoffContainer("wt-old-cleanup", nil)
		client := mockSelfupdate.NewMockHandoffClient(t)
		expectHandoff(client, old)
		client.EXPECT().RemoveImageByID(mock.Anything, old.ImageID(), "watchtower:v1").Return(nil).Once()

		env := handoffEnv("wt-old-cleanup")
		env.cleanup = true

		err := orchestrateSelfUpdate(newTestLogger(t), t.Context(), client, env)
		require.NoError(t, err)
	})

	t.Run("does not remove the old image when cleanup is disabled", func(t *testing.T) {
		t.Parallel()

		// The mock has no image removal expectation, so a removal fails the test.
		old := createEphemeralHandoffContainer("wt-old-no-cleanup", nil)
		client := mockSelfupdate.NewMockHandoffClient(t)
		expectHandoff(client, old)

		err := orchestrateSelfUpdate(newTestLogger(t), t.Context(), client, handoffEnv("wt-old-no-cleanup"))
		require.NoError(t, err)
	})

	t.Run("treats an in-use old image as a non-fatal skip", func(t *testing.T) {
		t.Parallel()

		old := createEphemeralHandoffContainer("wt-old-in-use", nil)
		client := mockSelfupdate.NewMockHandoffClient(t)
		expectHandoff(client, old)
		client.EXPECT().RemoveImageByID(mock.Anything, old.ImageID(), "watchtower:v1").
			Return(containerImage.ErrImageInUse).Once()

		env := handoffEnv("wt-old-in-use")
		env.cleanup = true

		err := orchestrateSelfUpdate(newTestLogger(t), t.Context(), client, env)
		require.NoError(t, err)
	})

	t.Run("treats a missing old image as a non-fatal skip", func(t *testing.T) {
		t.Parallel()

		old := createEphemeralHandoffContainer("wt-old-missing-image", nil)
		client := mockSelfupdate.NewMockHandoffClient(t)
		expectHandoff(client, old)
		client.EXPECT().RemoveImageByID(mock.Anything, old.ImageID(), "watchtower:v1").
			Return(cerrdefs.ErrNotFound).Once()

		env := handoffEnv("wt-old-missing-image")
		env.cleanup = true

		err := orchestrateSelfUpdate(newTestLogger(t), t.Context(), client, env)
		require.NoError(t, err)
	})
}
