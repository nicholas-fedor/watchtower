package restart

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	cerrdefs "github.com/containerd/errdefs"
	dockerContainer "github.com/moby/moby/api/types/container"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/modelfixture"
	"github.com/nicholas-fedor/watchtower/internal/update/cleanup"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
	mockRestart "github.com/nicholas-fedor/watchtower/internal/update/restart/mocks"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
)

// liveContext matches a context that is not canceled, as a stop that must run
// to its end receives.
var liveContext = mock.MatchedBy(func(ctx context.Context) bool {
	return ctx.Err() == nil
})

// newNetworkGroup returns a stale, running vpn container, a running torrent
// container rebound to vpn's network namespace, and a stopped chain container
// rebound to torrent's, in restart order.
func newNetworkGroup() []*container.Container {
	vpn := modelfixture.Container(
		containerfixture.WithName("/vpn"),
		containerfixture.WithID("vpn-id"),
		containerfixture.WithContainerState(dockerContainer.State{Running: true}),
	)
	vpn.SetStale(true)

	chain := rebound("chain", "torrent", containerfixture.WithContainerState(dockerContainer.State{Running: false}))

	return []*container.Container{vpn, newRebindingContainer(true), chain}
}

// TestNetworkGroups verifies that a rebound container joins the group of the
// container whose network namespace it joins, directly or through another
// member, even when other containers come between them. One whose network
// container is not in the list, such as a provider Compose recreated, starts
// a group of its own.
func TestNetworkGroups(t *testing.T) {
	t.Parallel()

	vpn := modelfixture.Container(containerfixture.WithName("/vpn"), containerfixture.WithID("vpn-id"))
	app := modelfixture.Container(containerfixture.WithName("/app"), containerfixture.WithID("app-id"))
	torrent := rebound("torrent", "vpn")
	chain := rebound("chain", "torrent")
	applied := rebound("applied", "compose-db")

	groups := networkGroups([]*container.Container{vpn, torrent, chain, applied, app})

	assert.Equal(t, [][]*container.Container{{vpn, torrent, chain}, {applied}, {app}}, groups)

	// A rebound container placed after other containers it waits for keeps
	// the group of the container whose namespace it joins, which takes in
	// the containers between them.
	db := modelfixture.Container(containerfixture.WithName("/db"), containerfixture.WithID("db-id"))
	proxy := rebound("proxy", "vpn")

	groups = networkGroups([]*container.Container{app, vpn, torrent, db, applied, proxy, chain})

	assert.Equal(t, [][]*container.Container{{app}, {vpn, torrent, db, applied, proxy, chain}}, groups)
}

// TestStopContainersInReversedOrder_networkGroup verifies that a container
// others join steps aside and its replacement is created before they are
// removed. A failed create restores the container and leaves the others
// running. A failed removal or a joining container that cannot be stopped
// also removes the replacement. The others already removed are recreated
// against the restored container. A container gone before it steps aside is
// skipped. A failed rename aside leaves the group as it was. A container gone
// after its replacement exists counts as removed, as does one the daemon
// removed although its removal failed. When such a container cannot be
// inspected, its replacement is kept.
func TestStopContainersInReversedOrder_networkGroup(t *testing.T) {
	t.Parallel()

	createErr := errors.New("create failed")
	inspectErr := errors.New("inspect failed")
	removeErr := errors.New("remove failed")
	renameErr := errors.New("rename failed")
	stopErr := errors.New("stop failed")

	tests := []struct {
		name string
		// expect sets the client calls, in order.
		expect func(client *mockRestart.MockClient, vpn, torrent, chain *container.Container)
		// wantStopped holds each removed container, with the replacement
		// created before it was removed.
		wantStopped map[container.ContainerID]container.ContainerID
		// wantErrs lists the errors the vpn container's error wraps, empty
		// when the vpn container does not fail.
		wantErrs []error
		// wantNotRestored reports whether the error says the vpn container
		// was not restored.
		wantNotRestored bool
		// wantText is text the vpn container's error holds, empty for none.
		wantText string
		// wantSkipped reports whether the vpn container's error is a skip.
		wantSkipped bool
		// wantOutcomes lists every container with an outcome. Each one but
		// the vpn container is skipped.
		wantOutcomes []container.ContainerID
	}{
		{
			name: "replacement created before the others are removed",
			expect: func(client *mockRestart.MockClient, vpn, torrent, chain *container.Container) {
				mock.InOrder(
					client.EXPECT().SnapshotCopyFiles(mock.Anything, vpn).Return(nil).Once(),
					client.EXPECT().RenameContainer(mock.Anything, vpn, "vpn-replaced-vpn-id").Return(nil).Once(),
					client.EXPECT().CreateContainer(mock.Anything, vpn).Return(container.ContainerID("new-vpn"), nil).Once(),
					client.EXPECT().SnapshotCopyFiles(mock.Anything, chain).Return(nil).Once(),
					client.EXPECT().StopAndRemoveContainer(mock.Anything, chain, mock.Anything).Return(nil).Once(),
					client.EXPECT().SnapshotCopyFiles(mock.Anything, torrent).Return(nil).Once(),
					client.EXPECT().StopAndRemoveContainer(mock.Anything, torrent, mock.Anything).Return(nil).Once(),
					client.EXPECT().StopAndRemoveContainer(mock.Anything, vpn, mock.Anything).Return(nil).Once(),
				)
			},
			wantStopped: map[container.ContainerID]container.ContainerID{"chain-id": "", "torrent-id": "", "vpn-id": "new-vpn"},
		},
		{
			name: "failed create restores the container and leaves the others",
			expect: func(client *mockRestart.MockClient, vpn, _, _ *container.Container) {
				mock.InOrder(
					client.EXPECT().SnapshotCopyFiles(mock.Anything, vpn).Return(nil).Once(),
					client.EXPECT().RenameContainer(mock.Anything, vpn, "vpn-replaced-vpn-id").Return(nil).Once(),
					client.EXPECT().CreateContainer(mock.Anything, vpn).Return(container.ContainerID(""), createErr).Once(),
					client.EXPECT().RenameContainer(mock.Anything, vpn, "vpn").Return(nil).Once(),
					client.EXPECT().StartContainerByID(mock.Anything, container.ContainerID("vpn-id")).Return(nil).Once(),
				)
			},
			wantStopped:  map[container.ContainerID]container.ContainerID{},
			wantErrs:     []error{createErr},
			wantOutcomes: []container.ContainerID{"vpn-id", "torrent-id", "chain-id"},
		},
		{
			name: "failed removal restores the container after the others are removed",
			expect: func(client *mockRestart.MockClient, vpn, torrent, chain *container.Container) {
				replacement := modelfixture.Container(containerfixture.WithID("new-vpn"))

				client.EXPECT().SnapshotCopyFiles(mock.Anything, mock.Anything).Return(nil)
				client.EXPECT().DiscardCopyFiles(vpn.ID()).Return().Once()
				mock.InOrder(
					client.EXPECT().RenameContainer(mock.Anything, vpn, "vpn-replaced-vpn-id").Return(nil).Once(),
					client.EXPECT().CreateContainer(mock.Anything, vpn).Return(container.ContainerID("new-vpn"), nil).Once(),
					client.EXPECT().StopAndRemoveContainer(mock.Anything, chain, mock.Anything).Return(nil).Once(),
					client.EXPECT().StopAndRemoveContainer(mock.Anything, torrent, mock.Anything).Return(nil).Once(),
					client.EXPECT().StopAndRemoveContainer(mock.Anything, vpn, mock.Anything).Return(removeErr).Once(),
					client.EXPECT().GetContainer(mock.Anything, vpn.ID()).Return(vpn, nil).Once(),
					client.EXPECT().GetContainer(mock.Anything, container.ContainerID("new-vpn")).Return(replacement, nil).Once(),
					client.EXPECT().StopAndRemoveContainer(mock.Anything, replacement, mock.Anything).Return(nil).Once(),
					client.EXPECT().RenameContainer(mock.Anything, vpn, "vpn").Return(nil).Once(),
					client.EXPECT().StartContainerByID(mock.Anything, container.ContainerID("vpn-id")).Return(nil).Once(),
				)
			},
			wantStopped:  map[container.ContainerID]container.ContainerID{"chain-id": "", "torrent-id": ""},
			wantErrs:     []error{removeErr},
			wantOutcomes: []container.ContainerID{"vpn-id"},
		},
		{
			name: "joining container that cannot be stopped restores the container",
			expect: func(client *mockRestart.MockClient, vpn, torrent, chain *container.Container) {
				replacement := modelfixture.Container(containerfixture.WithID("new-vpn"))

				client.EXPECT().SnapshotCopyFiles(mock.Anything, mock.Anything).Return(nil)
				client.EXPECT().DiscardCopyFiles(torrent.ID()).Return().Once()
				mock.InOrder(
					client.EXPECT().RenameContainer(mock.Anything, vpn, "vpn-replaced-vpn-id").Return(nil).Once(),
					client.EXPECT().CreateContainer(mock.Anything, vpn).Return(container.ContainerID("new-vpn"), nil).Once(),
					client.EXPECT().StopAndRemoveContainer(mock.Anything, chain, mock.Anything).Return(nil).Once(),
					client.EXPECT().StopAndRemoveContainer(mock.Anything, torrent, mock.Anything).Return(stopErr).Once(),
					client.EXPECT().GetContainer(mock.Anything, container.ContainerID("new-vpn")).Return(replacement, nil).Once(),
					client.EXPECT().StopAndRemoveContainer(mock.Anything, replacement, mock.Anything).Return(nil).Once(),
					client.EXPECT().RenameContainer(mock.Anything, vpn, "vpn").Return(nil).Once(),
					client.EXPECT().StartContainerByID(mock.Anything, container.ContainerID("vpn-id")).Return(nil).Once(),
					client.EXPECT().StartContainerByID(mock.Anything, container.ContainerID("torrent-id")).Return(nil).Once(),
				)
			},
			wantStopped:  map[container.ContainerID]container.ContainerID{"chain-id": ""},
			wantErrs:     []error{errRebindNotStopped},
			wantText:     "torrent: stop failed",
			wantOutcomes: []container.ContainerID{"torrent-id", "vpn-id"},
		},
		{
			name: "container gone before it steps aside is skipped",
			expect: func(client *mockRestart.MockClient, vpn, _, _ *container.Container) {
				mock.InOrder(
					client.EXPECT().SnapshotCopyFiles(mock.Anything, vpn).Return(nil).Once(),
					client.EXPECT().RenameContainer(mock.Anything, vpn, "vpn-replaced-vpn-id").Return(cerrdefs.ErrNotFound).Once(),
					client.EXPECT().DiscardCopyFiles(vpn.ID()).Return().Once(),
				)
			},
			wantStopped:  map[container.ContainerID]container.ContainerID{},
			wantErrs:     []error{errContainerGone, cerrdefs.ErrNotFound},
			wantSkipped:  true,
			wantOutcomes: []container.ContainerID{"vpn-id", "torrent-id", "chain-id"},
		},
		{
			name: "failed rename aside leaves the group as it was",
			expect: func(client *mockRestart.MockClient, vpn, _, _ *container.Container) {
				mock.InOrder(
					client.EXPECT().SnapshotCopyFiles(mock.Anything, vpn).Return(nil).Once(),
					client.EXPECT().RenameContainer(mock.Anything, vpn, "vpn-replaced-vpn-id").Return(renameErr).Once(),
					client.EXPECT().DiscardCopyFiles(vpn.ID()).Return().Once(),
				)
			},
			wantStopped:  map[container.ContainerID]container.ContainerID{},
			wantErrs:     []error{renameErr},
			wantOutcomes: []container.ContainerID{"vpn-id", "torrent-id", "chain-id"},
		},
		{
			name: "container already gone counts as removed",
			expect: func(client *mockRestart.MockClient, vpn, torrent, chain *container.Container) {
				client.EXPECT().SnapshotCopyFiles(mock.Anything, mock.Anything).Return(nil)
				client.EXPECT().DiscardCopyFiles(vpn.ID()).Return().Once()
				mock.InOrder(
					client.EXPECT().RenameContainer(mock.Anything, vpn, "vpn-replaced-vpn-id").Return(nil).Once(),
					client.EXPECT().CreateContainer(mock.Anything, vpn).Return(container.ContainerID("new-vpn"), nil).Once(),
					client.EXPECT().StopAndRemoveContainer(mock.Anything, chain, mock.Anything).Return(nil).Once(),
					client.EXPECT().StopAndRemoveContainer(mock.Anything, torrent, mock.Anything).Return(nil).Once(),
					client.EXPECT().StopAndRemoveContainer(mock.Anything, vpn, mock.Anything).Return(cerrdefs.ErrNotFound).Once(),
				)
			},
			wantStopped: map[container.ContainerID]container.ContainerID{"chain-id": "", "torrent-id": "", "vpn-id": "new-vpn"},
		},
		{
			name: "container removed although its removal failed counts as removed",
			expect: func(client *mockRestart.MockClient, vpn, torrent, chain *container.Container) {
				client.EXPECT().SnapshotCopyFiles(mock.Anything, mock.Anything).Return(nil)
				client.EXPECT().DiscardCopyFiles(vpn.ID()).Return().Once()
				mock.InOrder(
					client.EXPECT().RenameContainer(mock.Anything, vpn, "vpn-replaced-vpn-id").Return(nil).Once(),
					client.EXPECT().CreateContainer(mock.Anything, vpn).Return(container.ContainerID("new-vpn"), nil).Once(),
					client.EXPECT().StopAndRemoveContainer(mock.Anything, chain, mock.Anything).Return(nil).Once(),
					client.EXPECT().StopAndRemoveContainer(mock.Anything, torrent, mock.Anything).Return(nil).Once(),
					client.EXPECT().StopAndRemoveContainer(mock.Anything, vpn, mock.Anything).Return(removeErr).Once(),
					client.EXPECT().GetContainer(mock.Anything, vpn.ID()).Return(nil, cerrdefs.ErrNotFound).Once(),
				)
			},
			wantStopped: map[container.ContainerID]container.ContainerID{"chain-id": "", "torrent-id": "", "vpn-id": "new-vpn"},
		},
		{
			// Removing the replacement could leave neither container, so it
			// stays and vpn fails.
			name: "container that cannot be inspected after a failed removal keeps its replacement",
			expect: func(client *mockRestart.MockClient, vpn, torrent, chain *container.Container) {
				client.EXPECT().SnapshotCopyFiles(mock.Anything, mock.Anything).Return(nil)
				client.EXPECT().DiscardCopyFiles(vpn.ID()).Return().Once()
				mock.InOrder(
					client.EXPECT().RenameContainer(mock.Anything, vpn, "vpn-replaced-vpn-id").Return(nil).Once(),
					client.EXPECT().CreateContainer(mock.Anything, vpn).Return(container.ContainerID("new-vpn"), nil).Once(),
					client.EXPECT().StopAndRemoveContainer(mock.Anything, chain, mock.Anything).Return(nil).Once(),
					client.EXPECT().StopAndRemoveContainer(mock.Anything, torrent, mock.Anything).Return(nil).Once(),
					client.EXPECT().StopAndRemoveContainer(mock.Anything, vpn, mock.Anything).Return(removeErr).Once(),
					client.EXPECT().GetContainer(mock.Anything, vpn.ID()).Return(nil, inspectErr).Once(),
				)
			},
			wantStopped:  map[container.ContainerID]container.ContainerID{"chain-id": "", "torrent-id": ""},
			wantErrs:     []error{errReplacementKept, removeErr, inspectErr},
			wantOutcomes: []container.ContainerID{"vpn-id"},
		},
		{
			name: "failed restore is reported with the cause",
			expect: func(client *mockRestart.MockClient, vpn, _, _ *container.Container) {
				mock.InOrder(
					client.EXPECT().SnapshotCopyFiles(mock.Anything, vpn).Return(nil).Once(),
					client.EXPECT().RenameContainer(mock.Anything, vpn, "vpn-replaced-vpn-id").Return(nil).Once(),
					client.EXPECT().CreateContainer(mock.Anything, vpn).Return(container.ContainerID(""), createErr).Once(),
					client.EXPECT().RenameContainer(mock.Anything, vpn, "vpn").Return(renameErr).Once(),
					client.EXPECT().StartContainerByID(mock.Anything, container.ContainerID("vpn-id")).Return(nil).Once(),
				)
			},
			wantStopped:     map[container.ContainerID]container.ContainerID{},
			wantErrs:        []error{createErr, renameErr},
			wantNotRestored: true,
			wantOutcomes:    []container.ContainerID{"vpn-id", "torrent-id", "chain-id"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			group := newNetworkGroup()
			vpn, torrent, chain := group[0], group[1], group[2]

			// The strict mock fails on any call the case does not expect.
			client := mockRestart.NewMockClient(t)
			tt.expect(client, vpn, torrent, chain)

			failed, stopped := stopContainersInReversedOrder(newTestLogger(t), t.Context(),
				group, client, updateParams.Params{})

			assert.Equal(t, tt.wantStopped, stopped)
			assert.ElementsMatch(t, tt.wantOutcomes, slices.Collect(maps.Keys(failed)))

			for _, wantErr := range tt.wantErrs {
				require.ErrorIs(t, failed[vpn.ID()], wantErr)
			}

			if tt.wantText != "" {
				require.ErrorContains(t, failed[vpn.ID()], tt.wantText)
			}

			for id, err := range failed {
				if id != vpn.ID() {
					assert.True(t, isSkip(err), "outcome of %s is a skip", id)
				}
			}

			assert.Equal(t, tt.wantNotRestored, errors.Is(failed[vpn.ID()], errNotRestored))
			assert.Equal(t, tt.wantSkipped, isSkip(failed[vpn.ID()]))
		})
	}
}

// TestStopContainersInReversedOrder_networkGroupNameNotRestored verifies that
// the containers already removed join the restored vpn by its ID when vpn
// could not take its name back, since a recreation that names vpn would find
// no container under that name. The chain container joins torrent, which is
// recreated under its own name, so it keeps naming torrent.
func TestStopContainersInReversedOrder_networkGroupNameNotRestored(t *testing.T) {
	t.Parallel()

	group := newNetworkGroup()
	vpn, torrent, chain := group[0], group[1], group[2]
	replacement := modelfixture.Container(containerfixture.WithID("new-vpn"))

	client := mockRestart.NewMockClient(t)
	client.EXPECT().SnapshotCopyFiles(mock.Anything, mock.Anything).Return(nil)
	client.EXPECT().DiscardCopyFiles(vpn.ID()).Return().Once()
	mock.InOrder(
		client.EXPECT().RenameContainer(mock.Anything, vpn, "vpn-replaced-vpn-id").Return(nil).Once(),
		client.EXPECT().CreateContainer(mock.Anything, vpn).Return(container.ContainerID("new-vpn"), nil).Once(),
		client.EXPECT().StopAndRemoveContainer(mock.Anything, chain, mock.Anything).Return(nil).Once(),
		client.EXPECT().StopAndRemoveContainer(mock.Anything, torrent, mock.Anything).Return(nil).Once(),
		client.EXPECT().StopAndRemoveContainer(mock.Anything, vpn, mock.Anything).Return(errors.New("remove failed")).Once(),
		client.EXPECT().GetContainer(mock.Anything, vpn.ID()).Return(vpn, nil).Once(),
		client.EXPECT().GetContainer(mock.Anything, container.ContainerID("new-vpn")).Return(replacement, nil).Once(),
		client.EXPECT().StopAndRemoveContainer(mock.Anything, replacement, mock.Anything).Return(nil).Once(),
		client.EXPECT().RenameContainer(mock.Anything, vpn, "vpn").Return(errors.New("rename failed")).Once(),
		client.EXPECT().StartContainerByID(mock.Anything, vpn.ID()).Return(nil).Once(),
	)

	failed, stopped := stopContainersInReversedOrder(newTestLogger(t), t.Context(), group, client, updateParams.Params{})

	require.ErrorIs(t, failed[vpn.ID()], errNotRestored)
	assert.Equal(t, map[container.ContainerID]container.ContainerID{"chain-id": "", "torrent-id": ""}, stopped)
	assert.Equal(t, "vpn-id", torrent.NetworkContainer())
	assert.Equal(t, "torrent", chain.NetworkContainer())
}

// TestStopContainersInReversedOrder_networkGroupStartAfterSlowRenameBack
// verifies that a container whose removal failed is started again even when
// its rename back runs out its time.
//
// The container may already be stopped, and the containers that join it lose
// their network until it runs again. The rename back and the start each get
// the stop timeout plus 30 seconds, so a rename back that uses all of its time
// leaves the start a live context.
func TestStopContainersInReversedOrder_networkGroupStartAfterSlowRenameBack(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		group := newNetworkGroup()
		vpn, torrent, chain := group[0], group[1], group[2]
		replacement := modelfixture.Container(containerfixture.WithID("new-vpn"))

		client := mockRestart.NewMockClient(t)
		client.EXPECT().SnapshotCopyFiles(mock.Anything, mock.Anything).Return(nil)
		client.EXPECT().DiscardCopyFiles(vpn.ID()).Return().Once()
		mock.InOrder(
			client.EXPECT().RenameContainer(mock.Anything, vpn, "vpn-replaced-vpn-id").Return(nil).Once(),
			client.EXPECT().CreateContainer(mock.Anything, vpn).Return(container.ContainerID("new-vpn"), nil).Once(),
			client.EXPECT().StopAndRemoveContainer(mock.Anything, chain, mock.Anything).Return(nil).Once(),
			client.EXPECT().StopAndRemoveContainer(mock.Anything, torrent, mock.Anything).Return(nil).Once(),
			client.EXPECT().StopAndRemoveContainer(mock.Anything, vpn, mock.Anything).Return(errors.New("remove failed")).Once(),
			client.EXPECT().GetContainer(mock.Anything, vpn.ID()).Return(vpn, nil).Once(),
			client.EXPECT().GetContainer(mock.Anything, container.ContainerID("new-vpn")).Return(replacement, nil).Once(),
			client.EXPECT().StopAndRemoveContainer(mock.Anything, replacement, mock.Anything).Return(nil).Once(),
			client.EXPECT().RenameContainer(mock.Anything, vpn, "vpn").
				RunAndReturn(func(ctx context.Context, _ *container.Container, _ string) error {
					<-ctx.Done()

					return ctx.Err()
				}).Once(),
			client.EXPECT().StartContainerByID(liveContext, vpn.ID()).Return(nil).Once(),
		)

		failed, _ := stopContainersInReversedOrder(newTestLogger(t), t.Context(), group, client, updateParams.Params{})

		require.ErrorIs(t, failed[vpn.ID()], errNotRestored)
		require.ErrorIs(t, failed[vpn.ID()], context.DeadlineExceeded)
	})
}

// TestStopContainersInReversedOrder_networkGroupAfterDependency verifies that
// a container others join is not replaced early when it joins the network of
// a container earlier in the list. A replacement created then would stay
// bound to that container after it is recreated, so the group stops one by
// one in reverse order. The strict mock fails on a rename or create.
func TestStopContainersInReversedOrder_networkGroupAfterDependency(t *testing.T) {
	t.Parallel()

	gateway := modelfixture.Container(
		containerfixture.WithName("/gateway"),
		containerfixture.WithID("gateway-id"),
		containerfixture.WithContainerState(dockerContainer.State{Running: true}),
	)
	gateway.SetStale(true)

	vpn := modelfixture.Container(
		containerfixture.WithName("/vpn"),
		containerfixture.WithID("vpn-id"),
		containerfixture.WithNetworkMode("container:gateway"),
		containerfixture.WithContainerState(dockerContainer.State{Running: true}),
	)
	vpn.SetStale(true)

	torrent := newRebindingContainer(true)

	client := mockRestart.NewMockClient(t)
	client.EXPECT().SnapshotCopyFiles(mock.Anything, mock.Anything).Return(nil)
	mock.InOrder(
		client.EXPECT().StopAndRemoveContainer(mock.Anything, torrent, mock.Anything).Return(nil).Once(),
		client.EXPECT().StopAndRemoveContainer(mock.Anything, vpn, mock.Anything).Return(nil).Once(),
		client.EXPECT().StopAndRemoveContainer(mock.Anything, gateway, mock.Anything).Return(nil).Once(),
	)

	failed, stopped := stopContainersInReversedOrder(newTestLogger(t), t.Context(),
		[]*container.Container{gateway, vpn, torrent}, client, updateParams.Params{})

	assert.Empty(t, failed)
	assert.Equal(t, map[container.ContainerID]container.ContainerID{"gateway-id": "", "vpn-id": "", "torrent-id": ""}, stopped)
}

// TestStopContainersInReversedOrder_networkGroupStopFails verifies that a
// group stopped one by one leaves its head running when a rebound container
// cannot be stopped, since that container still holds the head's namespace.
// The head fails naming the rebound container and its error, and the rebound
// container is skipped, since it keeps its network. The running rebound
// container starts again in case its removal failed after it stopped. The
// gateway whose namespace the head joins stays running too, since removing it
// would cut the head's network.
func TestStopContainersInReversedOrder_networkGroupStopFails(t *testing.T) {
	t.Parallel()

	gateway := modelfixture.Container(
		containerfixture.WithName("/gateway"),
		containerfixture.WithID("gateway-id"),
		containerfixture.WithContainerState(dockerContainer.State{Running: true}),
	)
	gateway.SetStale(true)

	vpn := modelfixture.Container(
		containerfixture.WithName("/vpn"),
		containerfixture.WithID("vpn-id"),
		containerfixture.WithNetworkMode("container:gateway"),
		containerfixture.WithContainerState(dockerContainer.State{Running: true}),
	)
	vpn.SetStale(true)

	torrent := newRebindingContainer(true)
	stopErr := errors.New("stop failed")

	// The strict mock fails on a stop of vpn or gateway.
	client := mockRestart.NewMockClient(t)
	client.EXPECT().SnapshotCopyFiles(mock.Anything, torrent).Return(nil).Once()
	client.EXPECT().DiscardCopyFiles(torrent.ID()).Return().Once()
	mock.InOrder(
		client.EXPECT().StopAndRemoveContainer(mock.Anything, torrent, mock.Anything).Return(stopErr).Once(),
		client.EXPECT().StartContainerByID(mock.Anything, torrent.ID()).Return(nil).Once(),
	)

	failed, stopped := stopContainersInReversedOrder(newTestLogger(t), t.Context(),
		[]*container.Container{gateway, vpn, torrent}, client, updateParams.Params{})

	assert.Empty(t, stopped)
	require.ErrorIs(t, failed[torrent.ID()], stopErr)
	assert.True(t, isSkip(failed[torrent.ID()]))
	require.ErrorIs(t, failed[vpn.ID()], errRebindNotStopped)
	require.ErrorContains(t, failed[vpn.ID()], "torrent: stop failed")
	assert.False(t, isSkip(failed[vpn.ID()]))
	require.ErrorIs(t, failed[gateway.ID()], errRebindNotStopped)
	assert.ErrorContains(t, failed[gateway.ID()], "vpn")
}

// TestStopContainersInReversedOrder_networkGroupPausedJoiner verifies a paused
// container joining vpn whose stop fails, which keeps vpn in place. Its state
// is read again before it is started. Still paused, as when Podman refuses
// the stop, it is left as it is, since Docker and Podman refuse to start a
// paused container. Stopped, as when Docker stops it but its removal fails,
// it starts again. Either way it is skipped, so vpn's failure alone reports
// the cause. The strict mock fails on any other start.
func TestStopContainersInReversedOrder_networkGroupPausedJoiner(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// now is the joining container's state when it is read again.
		now dockerContainer.State
		// wantStart reports whether it starts again.
		wantStart bool
	}{
		{name: "still paused after a refused stop", now: dockerContainer.State{Running: true, Paused: true}},
		{name: "stopped before its removal failed", now: dockerContainer.State{Status: "exited"}, wantStart: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			group := newNetworkGroup()
			vpn, chain := group[0], group[2]
			paused := rebound("torrent", "vpn",
				containerfixture.WithContainerState(dockerContainer.State{Running: true, Paused: true}))
			group[1] = paused
			current := rebound("torrent", "vpn", containerfixture.WithContainerState(tt.now))
			replacement := modelfixture.Container(containerfixture.WithID("new-vpn"))
			stopErr := errors.New("stop or removal failed")

			client := mockRestart.NewMockClient(t)
			client.EXPECT().SnapshotCopyFiles(mock.Anything, mock.Anything).Return(nil)
			client.EXPECT().DiscardCopyFiles(paused.ID()).Return().Once()

			calls := []*mock.Call{
				client.EXPECT().RenameContainer(mock.Anything, vpn, "vpn-replaced-vpn-id").Return(nil).Once(),
				client.EXPECT().CreateContainer(mock.Anything, vpn).Return(container.ContainerID("new-vpn"), nil).Once(),
				client.EXPECT().StopAndRemoveContainer(mock.Anything, chain, mock.Anything).Return(nil).Once(),
				client.EXPECT().StopAndRemoveContainer(mock.Anything, paused, mock.Anything).Return(stopErr).Once(),
				client.EXPECT().GetContainer(mock.Anything, container.ContainerID("new-vpn")).Return(replacement, nil).Once(),
				client.EXPECT().StopAndRemoveContainer(mock.Anything, replacement, mock.Anything).Return(nil).Once(),
				client.EXPECT().RenameContainer(mock.Anything, vpn, "vpn").Return(nil).Once(),
				client.EXPECT().StartContainerByID(mock.Anything, vpn.ID()).Return(nil).Once(),
				client.EXPECT().GetContainer(mock.Anything, paused.ID()).Return(current, nil).Once(),
			}
			if tt.wantStart {
				calls = append(calls, client.EXPECT().StartContainerByID(mock.Anything, paused.ID()).Return(nil).Once())
			}

			mock.InOrder(calls...)

			failed, _ := stopContainersInReversedOrder(newTestLogger(t), t.Context(), group, client, updateParams.Params{})

			require.ErrorIs(t, failed[paused.ID()], stopErr)
			assert.True(t, isSkip(failed[paused.ID()]))
			require.ErrorIs(t, failed[vpn.ID()], errRebindNotStopped)
		})
	}
}

// TestStopContainersInReversedOrder_networkGroupWithWaitingJoiner verifies
// that a container is still replaced before a rebound container joining it is
// removed when that rebound container was placed after another container it
// waits for. The container in between stops first, before the container's
// own pre-update command, as it would without the replacement. Its failure to
// stop does not keep the container in place because it does not join the
// namespace. It starts again at once, since nothing recreates it.
func TestStopContainersInReversedOrder_networkGroupWithWaitingJoiner(t *testing.T) {
	t.Parallel()

	vpn := modelfixture.Container(
		containerfixture.WithName("/vpn"),
		containerfixture.WithID("vpn-id"),
		containerfixture.WithContainerState(dockerContainer.State{Running: true}),
	)
	vpn.SetStale(true)

	db := modelfixture.Container(
		containerfixture.WithName("/db"),
		containerfixture.WithID("db-id"),
		containerfixture.WithContainerState(dockerContainer.State{Running: true}),
	)
	db.SetStale(true)

	proxy := newRebindingContainer(true)
	stopErr := errors.New("stop failed")

	client := mockRestart.NewMockClient(t)
	client.EXPECT().SnapshotCopyFiles(mock.Anything, db).Return(nil).Once()
	client.EXPECT().SnapshotCopyFiles(mock.Anything, proxy).Return(nil).Once()
	client.EXPECT().DiscardCopyFiles(db.ID()).Return().Once()
	mock.InOrder(
		client.EXPECT().StopAndRemoveContainer(mock.Anything, db, mock.Anything).Return(stopErr).Once(),
		client.EXPECT().StartContainerByID(mock.Anything, db.ID()).Return(nil).Once(),
		client.EXPECT().SnapshotCopyFiles(mock.Anything, vpn).Return(nil).Once(),
		client.EXPECT().RenameContainer(mock.Anything, vpn, "vpn-replaced-vpn-id").Return(nil).Once(),
		client.EXPECT().CreateContainer(mock.Anything, vpn).Return(container.ContainerID("new-vpn"), nil).Once(),
		client.EXPECT().StopAndRemoveContainer(mock.Anything, proxy, mock.Anything).Return(nil).Once(),
		client.EXPECT().StopAndRemoveContainer(mock.Anything, vpn, mock.Anything).Return(nil).Once(),
	)

	failed, stopped := stopContainersInReversedOrder(newTestLogger(t), t.Context(),
		[]*container.Container{vpn, db, proxy}, client, updateParams.Params{})

	assert.Equal(t, map[container.ContainerID]container.ContainerID{"vpn-id": "new-vpn", "torrent-id": ""}, stopped)
	assert.Equal(t, []container.ContainerID{"db-id"}, slices.Collect(maps.Keys(failed)))
	require.ErrorIs(t, failed[db.ID()], stopErr)
}

// newMergedGroup returns a stale, running vpn container, a stale k container
// placed after it that joins its network namespace, a rebound r container
// that joins vpn, and a rebound r2 container that joins k, in restart order.
// The rebound containers wait for k, so one group holds all four.
func newMergedGroup() []*container.Container {
	running := containerfixture.WithContainerState(dockerContainer.State{Running: true})

	vpn := modelfixture.Container(containerfixture.WithName("/vpn"), containerfixture.WithID("vpn-id"), running)
	vpn.SetStale(true)

	k := modelfixture.Container(
		containerfixture.WithName("/k"),
		containerfixture.WithID("k-id"),
		containerfixture.WithNetworkMode("container:vpn"),
		running,
	)
	k.SetStale(true)

	r := rebound("r", "vpn", running)
	r2 := rebound("r2", "k", running)

	return []*container.Container{vpn, k, r, r2}
}

// TestStopContainersInReversedOrder_mergedGroup verifies the stop order of a
// group that holds containers other than vpn's rebound joiners. The k
// container and the rebound r2 container that joins it stop first, in reverse
// order, before vpn's own pre-update work, as they would without the
// replacement. The rebound r container, which reaches vpn, stops only after
// vpn's replacement exists.
func TestStopContainersInReversedOrder_mergedGroup(t *testing.T) {
	t.Parallel()

	group := newMergedGroup()
	vpn, k, r, r2 := group[0], group[1], group[2], group[3]

	require.Len(t, networkGroups(group), 1)

	client := mockRestart.NewMockClient(t)
	mock.InOrder(
		client.EXPECT().SnapshotCopyFiles(mock.Anything, r2).Return(nil).Once(),
		client.EXPECT().StopAndRemoveContainer(mock.Anything, r2, mock.Anything).Return(nil).Once(),
		client.EXPECT().SnapshotCopyFiles(mock.Anything, k).Return(nil).Once(),
		client.EXPECT().StopAndRemoveContainer(mock.Anything, k, mock.Anything).Return(nil).Once(),
		client.EXPECT().SnapshotCopyFiles(mock.Anything, vpn).Return(nil).Once(),
		client.EXPECT().RenameContainer(mock.Anything, vpn, "vpn-replaced-vpn-id").Return(nil).Once(),
		client.EXPECT().CreateContainer(mock.Anything, vpn).Return(container.ContainerID("new-vpn"), nil).Once(),
		client.EXPECT().SnapshotCopyFiles(mock.Anything, r).Return(nil).Once(),
		client.EXPECT().StopAndRemoveContainer(mock.Anything, r, mock.Anything).Return(nil).Once(),
		client.EXPECT().StopAndRemoveContainer(mock.Anything, vpn, mock.Anything).Return(nil).Once(),
	)

	failed, stopped := stopContainersInReversedOrder(newTestLogger(t), t.Context(), group, client, updateParams.Params{})

	assert.Empty(t, failed)
	assert.Equal(t, map[container.ContainerID]container.ContainerID{
		"vpn-id": "new-vpn", "k-id": "", "r-id": "", "r2-id": "",
	}, stopped)
}

// TestStopContainersInReversedOrder_mergedGroupHeld verifies that a container
// in a group which joins vpn and cannot be stopped keeps vpn in place before
// vpn steps aside. Nothing is renamed or created. vpn fails naming the
// container, which keeps its own failure, and the rebound r container, which
// was not reached, is skipped. The r2 container stopped before is recreated
// against vpn as it is. The strict mock fails on a rename or create.
func TestStopContainersInReversedOrder_mergedGroupHeld(t *testing.T) {
	t.Parallel()

	group := newMergedGroup()
	k, r, r2 := group[1], group[2], group[3]
	stopErr := errors.New("stop failed")

	client := mockRestart.NewMockClient(t)
	client.EXPECT().SnapshotCopyFiles(mock.Anything, mock.Anything).Return(nil)
	client.EXPECT().DiscardCopyFiles(k.ID()).Return().Once()
	mock.InOrder(
		client.EXPECT().StopAndRemoveContainer(mock.Anything, r2, mock.Anything).Return(nil).Once(),
		client.EXPECT().StopAndRemoveContainer(mock.Anything, k, mock.Anything).Return(stopErr).Once(),
		client.EXPECT().StartContainerByID(mock.Anything, k.ID()).Return(nil).Once(),
	)

	failed, stopped := stopContainersInReversedOrder(newTestLogger(t), t.Context(), group, client, updateParams.Params{})

	assert.Equal(t, map[container.ContainerID]container.ContainerID{"r2-id": ""}, stopped)
	require.ErrorIs(t, failed["vpn-id"], errRebindNotStopped)
	require.ErrorContains(t, failed["vpn-id"], "k: stop failed")
	require.ErrorIs(t, failed[k.ID()], stopErr)
	assert.False(t, isSkip(failed[k.ID()]))
	require.ErrorIs(t, failed[r.ID()], errGroupKept)
	assert.True(t, isSkip(failed[r.ID()]))
}

// TestStopContainersInReversedOrder_mergedGroupCanceled verifies that a stop
// already begun runs to its end when the update is canceled, since Docker
// finishes it anyway. No further container of the group is stopped, and vpn
// is not replaced, also when the canceled stop was the last before vpn's own
// steps. The containers not stopped are skipped, while those removed are
// recreated as usual. The strict mock fails on any other stop or on a rename
// of vpn.
func TestStopContainersInReversedOrder_mergedGroupCanceled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// canceledAt picks the container whose stop is under way when the
		// update is canceled.
		canceledAt func(k, r2 *container.Container) *container.Container
		// stops returns the containers stopped, in order.
		stops func(k, r2 *container.Container) []*container.Container
	}{
		{
			name:       "during the first stop",
			canceledAt: func(_, r2 *container.Container) *container.Container { return r2 },
			stops:      func(_, r2 *container.Container) []*container.Container { return []*container.Container{r2} },
		},
		{
			name:       "during the last stop before vpn",
			canceledAt: func(k, _ *container.Container) *container.Container { return k },
			stops:      func(k, r2 *container.Container) []*container.Container { return []*container.Container{r2, k} },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			group := newMergedGroup()
			k, r2 := group[1], group[3]
			canceledAt := tt.canceledAt(k, r2)

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			client := mockRestart.NewMockClient(t)
			wantStopped := map[container.ContainerID]container.ContainerID{}

			for _, c := range tt.stops(k, r2) {
				snapshot := client.EXPECT().SnapshotCopyFiles(mock.Anything, c)
				if c == canceledAt {
					snapshot = snapshot.Run(func(context.Context, *container.Container) { cancel() })
				}

				snapshot.Return(nil).Once()

				client.EXPECT().StopAndRemoveContainer(liveContext, c, mock.Anything).Return(nil).Once()

				wantStopped[c.ID()] = ""
			}

			failed, stopped := stopContainersInReversedOrder(newTestLogger(t), ctx, group, client, updateParams.Params{})

			assert.Equal(t, wantStopped, stopped)
			assert.Len(t, failed, len(group)-len(wantStopped))

			for id, err := range failed {
				require.ErrorIs(t, err, context.Canceled, id)
				assert.True(t, isSkip(err), id)
			}
		})
	}
}

// TestStopContainersInReversedOrder_networkGroupCanceled verifies that once
// vpn's replacement exists, the stops of the containers that join it and of
// vpn itself run to their end when the update is canceled, since Docker
// finishes a stop it has begun. A stop cut short would leave the joining
// container stopped behind a restored vpn.
func TestStopContainersInReversedOrder_networkGroupCanceled(t *testing.T) {
	t.Parallel()

	group := newNetworkGroup()
	vpn := group[0]
	ctx, cancel := context.WithCancel(t.Context())

	client := mockRestart.NewMockClient(t)
	client.EXPECT().SnapshotCopyFiles(mock.Anything, mock.Anything).Return(nil)
	client.EXPECT().RenameContainer(mock.Anything, vpn, "vpn-replaced-vpn-id").Return(nil).Once()
	client.EXPECT().CreateContainer(mock.Anything, vpn).
		Run(func(context.Context, *container.Container) { cancel() }).
		Return(container.ContainerID("new-vpn"), nil).Once()
	client.EXPECT().StopAndRemoveContainer(liveContext, mock.Anything, mock.Anything).Return(nil).Times(3)

	failed, stopped := stopContainersInReversedOrder(newTestLogger(t), ctx, group, client, updateParams.Params{})

	assert.Empty(t, failed)
	assert.Equal(t, map[container.ContainerID]container.ContainerID{
		"vpn-id": "new-vpn", "torrent-id": "", "chain-id": "",
	}, stopped)
}

// TestStopContainersInReversedOrder_heldByJoiner verifies that a container
// stays running when a container that joins its network namespace could not
// be stopped. A pre-update command that skips the joining container keeps it
// running in that namespace, so the container it joins fails instead of
// being removed. The joining container starts again at once, since the
// container it joins stays. Containers that join nothing still stop.
func TestStopContainersInReversedOrder_heldByJoiner(t *testing.T) {
	t.Parallel()

	vpn := modelfixture.Container(
		containerfixture.WithName("/vpn"),
		containerfixture.WithID("vpn-id"),
		containerfixture.WithContainerState(dockerContainer.State{Running: true}),
	)
	vpn.SetStale(true)

	db := modelfixture.Container(
		containerfixture.WithName("/db"),
		containerfixture.WithID("db-id"),
		containerfixture.WithContainerState(dockerContainer.State{Running: true}),
	)
	db.SetStale(true)

	app := modelfixture.Container(
		containerfixture.WithName("/app"),
		containerfixture.WithID("app-id"),
		containerfixture.WithNetworkMode("container:vpn"),
		containerfixture.WithContainerState(dockerContainer.State{Running: true}),
	)
	app.SetStale(true)

	stopErr := errors.New("stop failed")

	// The strict mock fails on a stop of vpn.
	client := mockRestart.NewMockClient(t)
	client.EXPECT().SnapshotCopyFiles(mock.Anything, mock.Anything).Return(nil)
	client.EXPECT().DiscardCopyFiles(app.ID()).Return().Once()
	mock.InOrder(
		client.EXPECT().StopAndRemoveContainer(mock.Anything, app, mock.Anything).Return(stopErr).Once(),
		client.EXPECT().StartContainerByID(mock.Anything, app.ID()).Return(nil).Once(),
		client.EXPECT().StopAndRemoveContainer(mock.Anything, db, mock.Anything).Return(nil).Once(),
	)

	failed, stopped := stopContainersInReversedOrder(newTestLogger(t), t.Context(),
		[]*container.Container{vpn, db, app}, client, updateParams.Params{})

	assert.Equal(t, map[container.ContainerID]container.ContainerID{"db-id": ""}, stopped)
	require.ErrorIs(t, failed[app.ID()], stopErr)
	require.ErrorIs(t, failed[vpn.ID()], errRebindNotStopped)
	assert.ErrorContains(t, failed[vpn.ID()], "app")
}

// TestRun_networkGroup verifies that both restart modes start the
// replacement created before the containers joining it were removed and then
// recreate those containers. A rolling restart waits for each started
// replacement to become healthy but not for the stopped chain container's.
func TestRun_networkGroup(t *testing.T) {
	t.Parallel()

	for _, rolling := range []bool{true, false} {
		t.Run(fmt.Sprintf("rolling %t", rolling), func(t *testing.T) {
			t.Parallel()

			group := newNetworkGroup()
			vpn, torrent, chain := group[0], group[1], group[2]

			client := mockRestart.NewMockClient(t)
			client.EXPECT().SnapshotCopyFiles(mock.Anything, mock.Anything).Return(nil)
			client.EXPECT().StopAndRemoveContainer(mock.Anything, mock.Anything, mock.Anything).Return(nil)

			if rolling {
				client.EXPECT().WaitForContainerHealthy(mock.Anything, container.ContainerID("new-vpn"), mock.Anything).Return(nil).Once()
				client.EXPECT().WaitForContainerHealthy(mock.Anything, container.ContainerID("new-torrent"), mock.Anything).Return(nil).Once()
			}

			mock.InOrder(
				client.EXPECT().RenameContainer(mock.Anything, vpn, "vpn-replaced-vpn-id").Return(nil).Once(),
				client.EXPECT().CreateContainer(mock.Anything, vpn).Return(container.ContainerID("new-vpn"), nil).Once(),
				client.EXPECT().StartContainerByID(mock.Anything, container.ContainerID("new-vpn")).Return(nil).Once(),
				client.EXPECT().CreateContainer(mock.Anything, torrent).Return(container.ContainerID("new-torrent"), nil).Once(),
				client.EXPECT().StartContainerByID(mock.Anything, container.ContainerID("new-torrent")).Return(nil).Once(),
				client.EXPECT().CreateContainer(mock.Anything, chain).Return(container.ContainerID("new-chain"), nil).Once(),
			)

			err := Run(newTestLogger(t), t.Context(), group, client,
				updateParams.Params{RollingRestart: rolling}, &[]cleanup.RemovedImageInfo{}, &state.Progress{})
			require.NoError(t, err)
		})
	}
}

// TestStopOne_failedRemoval verifies that a running container whose stop or
// removal fails is started again, since nothing recreates it. It covers the
// containers that are not started:
//   - a container that was not running
//   - a container that no longer exists
//   - a container that joins another member of its group, which the group
//     starts itself
//
// The strict mock fails on any start the case does not expect.
func TestStopOne_failedRemoval(t *testing.T) {
	t.Parallel()

	errRemove := errors.New("failed to remove container: device or resource busy")
	errStart := errors.New("failed to start container")

	vpn := modelfixture.Container(containerfixture.WithName("/vpn"), containerfixture.WithID("vpn-id"))

	tests := []struct {
		name     string
		running  bool
		joins    bool
		stopErr  error
		startErr error
		starts   bool
		wantErrs []error
		wantSkip bool
	}{
		{
			name:     "running container is started again",
			running:  true,
			stopErr:  errRemove,
			starts:   true,
			wantErrs: []error{errRemove},
		},
		{
			name:     "a failed start is reported with the removal",
			running:  true,
			stopErr:  errRemove,
			startErr: errStart,
			starts:   true,
			wantErrs: []error{errRemove, errStart},
		},
		{
			name:     "stopped container stays stopped",
			running:  false,
			stopErr:  errRemove,
			wantErrs: []error{errRemove},
		},
		{
			name:     "member joining another member waits for its group",
			running:  true,
			joins:    true,
			stopErr:  errRemove,
			wantErrs: []error{errRemove},
		},
		{
			name:     "missing container is skipped without a start",
			running:  true,
			stopErr:  cerrdefs.ErrNotFound,
			wantErrs: []error{errContainerGone, cerrdefs.ErrNotFound},
			wantSkip: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			updates := []containerfixture.Update{
				containerfixture.WithName("/app"),
				containerfixture.WithID("app-id"),
				containerfixture.WithContainerState(dockerContainer.State{Running: tt.running}),
			}
			if tt.joins {
				updates = append(updates, containerfixture.WithNetworkMode("container:vpn"))
			}

			c := modelfixture.Container(updates...)
			c.SetStale(true)

			group := []*container.Container{c}
			if tt.joins {
				group = []*container.Container{vpn, c}
			}

			client := mockRestart.NewMockClient(t)
			client.EXPECT().SnapshotCopyFiles(mock.Anything, c).Return(nil).Once()
			client.EXPECT().StopAndRemoveContainer(mock.Anything, c, mock.Anything).Return(tt.stopErr).Once()
			client.EXPECT().DiscardCopyFiles(c.ID()).Once()

			if tt.starts {
				client.EXPECT().StartContainerByID(mock.Anything, c.ID()).Return(tt.startErr).Once()
			}

			failed := map[container.ContainerID]error{}
			stopped := map[container.ContainerID]container.ContainerID{}

			err := stopOne(newTestLogger(t), t.Context(), group, c, client, updateParams.Params{}, failed, stopped)
			assert.Empty(t, stopped)
			require.Equal(t, err, failed[c.ID()])

			for _, want := range tt.wantErrs {
				require.ErrorIs(t, err, want)
			}

			assert.Equal(t, tt.wantSkip, isSkip(err))
		})
	}
}
