package run

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/testing/e2e/assert"
	"github.com/nicholas-fedor/watchtower/testing/e2e/engine"
)

func TestAssertOutcomeScannedNone(t *testing.T) {
	s := &session{
		item: engine.Case{Expect: engine.Expect{Outcome: engine.OutcomeUpdated}},
		before: assert.InspectSnapshot{
			ImageID:  "sha256:aaa",
			ImageRef: "127.0.0.1:5000/e2e/app:latest",
		},
		after: assert.InspectSnapshot{ImageID: "sha256:aaa"},
		logs:  `{"level":"info","scanned":0,"updated":0,"failed":0,"skipped":0,"message":"Update session completed"}`,
	}

	err := s.assertOutcome()
	require.ErrorIs(t, err, ErrWatchtowerSawNoContainers)
	require.Contains(t, err.Error(), "127.0.0.1:5000/e2e/app:latest")
}

func TestAssertOutcomePullDenied(t *testing.T) {
	s := &session{
		item:   engine.Case{Expect: engine.Expect{Outcome: engine.OutcomeUpdated}},
		before: assert.InspectSnapshot{ImageID: "sha256:aaa", ImageRef: "127.0.0.1:5000/e2e/app:latest"},
		after:  assert.InspectSnapshot{ImageID: "sha256:aaa"},
		logs:   `{"level":"debug","message":"Failed to pull image"}`,
	}

	err := s.assertOutcome()
	require.ErrorIs(t, err, errRegistryPullDenied)
}

func TestAssertOutcomeImageUnchangedWithoutEmptyScan(t *testing.T) {
	s := &session{
		item:   engine.Case{Expect: engine.Expect{Outcome: engine.OutcomeUpdated}},
		before: assert.InspectSnapshot{ImageID: "sha256:aaa"},
		after:  assert.InspectSnapshot{ImageID: "sha256:aaa"},
		logs:   `{"level":"info","scanned":1,"updated":0,"message":"Update session completed"}`,
	}

	require.ErrorIs(t, s.assertOutcome(), ErrImageIDUnchanged)
}
