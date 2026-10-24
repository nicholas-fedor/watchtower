package container

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dockerClient "github.com/moby/moby/client"
)

// TestExecuteCommand_TimeoutBoundsOutputCapture verifies that a command's
// timeout also ends the read of its output, so a hook that never exits or closes
// its output stops at the timeout instead of blocking the update.
func TestExecuteCommand_TimeoutBoundsOutputCapture(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		const execID = "hanging-exec-id"

		api := newDockerAPI(t)
		api.EXPECT().ExecCreate(mock.Anything, mockContainerID, mock.Anything).
			Return(dockerClient.ExecCreateResult{ID: execID}, nil).Once()
		api.EXPECT().ExecStart(mock.Anything, execID, dockerClient.ExecStartOptions{Detach: true, TTY: true}).
			Return(dockerClient.ExecStartResult{}, nil).Once()
		// The output never arrives and the stream never closes.
		api.EXPECT().ExecAttach(mock.Anything, execID, dockerClient.ExecAttachOptions{TTY: true}).
			Return(newExecOutput(t, nil), nil).Once()
		api.EXPECT().ExecInspect(mock.Anything, execID, dockerClient.ExecInspectOptions{}).
			Return(dockerClient.ExecInspectResult{ID: execID, Running: true}, nil).Once()

		start := time.Now()

		_, err := (&client{log: newTestLogger(t), api: api}).
			ExecuteCommand(t.Context(), MockContainer(), "sleep infinity", 1, 0, 0)

		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Equal(t, time.Minute, time.Since(start))
	})
}
