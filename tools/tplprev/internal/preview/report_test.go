package preview

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/tplprev/internal/testutil/contract"
)

// TestStateNamesMatchWatchtower checks that a container's state reads the same
// in the preview as in Watchtower. Each state's sample must read its own name.
// Together the names must match the list generated from Watchtower's reports
// by task tplprev:gen.
func TestStateNamesMatchWatchtower(t *testing.T) {
	t.Parallel()

	tests := []struct {
		// state is the sample's state.
		state State
		// name is how a container in that state reads in Watchtower.
		name string
	}{
		{state: UpdatedState, name: "Updated"},
		{state: FailedState, name: "Failed"},
		{state: SkippedState, name: "Skipped"},
		{state: RestartedState, name: "Restarted"},
		{state: StaleState, name: "Stale"},
		{state: FreshState, name: "Fresh"},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			t.Parallel()

			generator := New()
			generator.AddFromState(tt.state)

			all := generator.NotificationData().Report.All()
			require.Len(t, all, 1)
			assert.Equal(t, tt.name, all[0].State())
		})
	}

	t.Run("all states", func(t *testing.T) {
		t.Parallel()

		want := contract.Lines(t, "testdata/container-states.txt")

		generator := New()
		for _, tt := range tests {
			generator.AddFromState(tt.state)
		}

		got := make([]string, 0, len(want))
		for _, status := range generator.NotificationData().Report.All() {
			got = append(got, status.State())
		}

		assert.ElementsMatch(t, want, got)
	})
}
