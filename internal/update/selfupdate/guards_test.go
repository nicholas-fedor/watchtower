package selfupdate

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/nicholas-fedor/watchtower/pkg/types"
	mockTypes "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

// TestBlocked verifies why a self-update cannot replace the running Watchtower
// container. Published host ports block a rename-based self-update but not an
// ephemeral one. A container that was detected but not inspected blocks both,
// because its ports are unknown. Without a detected container ID there is no
// container to check.
func TestBlocked(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// ports is whether the current container publishes host ports. Nil
		// leaves the current container uninspected.
		ports     *bool
		currentID types.ContainerID
		ephemeral bool
		want      Block
	}{
		{name: "no detected container", want: NotBlocked},
		{name: "container without published ports", ports: new(false), currentID: "c0c0", want: NotBlocked},
		{
			name:      "container with published ports",
			ports:     new(true),
			currentID: "c0c0",
			want:      BlockedByPublishedPorts,
		},
		{
			name:      "ephemeral self-update with published ports",
			ports:     new(true),
			currentID: "c0c0",
			ephemeral: true,
			want:      NotBlocked,
		},
		{name: "detected container that was not inspected", currentID: "c0c0", want: BlockedUninspected},
		{
			name:      "ephemeral self-update of a detected container that was not inspected",
			currentID: "c0c0",
			ephemeral: true,
			want:      BlockedUninspected,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var current types.Container

			if tt.ports != nil {
				mockCurrent := mockTypes.NewMockContainer(t)
				mockCurrent.EXPECT().HasExposedPorts().Return(*tt.ports).Maybe()
				current = mockCurrent
			}

			assert.Equal(t, tt.want, Blocked(current, tt.currentID, tt.ephemeral))
		})
	}
}
