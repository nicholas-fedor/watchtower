package client

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dockerClient "github.com/moby/moby/client"

	mockContainer "github.com/nicholas-fedor/watchtower/internal/container/mocks"
	"github.com/nicholas-fedor/watchtower/internal/testutil/modelfixture"
)

// TestExecuteCommand_TimeoutBoundsOutputCapture verifies that a command's
// timeout bounds starting the command and reading its output. The command
// stops at the timeout instead of blocking the update when the daemon never
// answers the attach. It also stops when the hook never exits or closes its
// output.
func TestExecuteCommand_TimeoutBoundsOutputCapture(t *testing.T) {
	t.Parallel()

	const execID = "hanging-exec-id"

	tests := []struct {
		expect func(t *testing.T, api *mockContainer.MockAPIClient)
		name   string
	}{
		{
			name: "attach never answers",
			expect: func(_ *testing.T, api *mockContainer.MockAPIClient) {
				api.EXPECT().ExecAttach(mock.Anything, execID, dockerClient.ExecAttachOptions{TTY: true}).
					RunAndReturn(func(ctx context.Context, _ string, _ dockerClient.ExecAttachOptions) (dockerClient.ExecAttachResult, error) {
						<-ctx.Done()

						return dockerClient.ExecAttachResult{}, ctx.Err()
					}).Once()
			},
		},
		{
			name: "output never ends",
			expect: func(t *testing.T, api *mockContainer.MockAPIClient) {
				t.Helper()

				// The attach starts the exec. Its output never arrives and the
				// stream never closes.
				api.EXPECT().ExecAttach(mock.Anything, execID, dockerClient.ExecAttachOptions{TTY: true}).
					Return(newExecOutput(t, nil), nil).Once()
				api.EXPECT().ExecInspect(mock.Anything, execID, dockerClient.ExecInspectOptions{}).
					Return(dockerClient.ExecInspectResult{ID: execID, Running: true}, nil).Once()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				api := newDockerAPI(t)
				api.EXPECT().ExecCreate(mock.Anything, mockContainerID, mock.Anything).
					Return(dockerClient.ExecCreateResult{ID: execID}, nil).Once()
				tt.expect(t, api)

				start := time.Now()

				_, err := (&Client{log: newTestLogger(t), api: api}).
					ExecuteCommand(t.Context(), modelfixture.Container(), "sleep infinity", 1, 0, 0)

				require.ErrorIs(t, err, context.DeadlineExceeded)
				assert.Equal(t, time.Minute, time.Since(start))
			})
		})
	}
}

// TestTailBuffer verifies that the buffer keeps only the last bytes written to
// it and reports whether it dropped earlier ones.
func TestTailBuffer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		want        string
		chunks      []string
		limit       int
		wantDropped bool
	}{
		{name: "within the limit", chunks: []string{"ab", "c"}, limit: 4, want: "abc"},
		{name: "exactly the limit", chunks: []string{"ab", "cd"}, limit: 4, want: "abcd"},
		{
			name:        "trims more than once",
			chunks:      []string{"ab", "cdef", "g", "hijklmnop", "qrstu", "v"},
			limit:       4,
			want:        "stuv",
			wantDropped: true,
		},
		// "é" is two bytes. The last three bytes of "abcéde" start inside it.
		{name: "split character dropped", chunks: []string{"abcé", "de"}, limit: 3, want: "de", wantDropped: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			buffer := tailBuffer{data: nil, limit: tt.limit, dropped: false}

			for _, chunk := range tt.chunks {
				written, err := buffer.Write([]byte(chunk))
				require.NoError(t, err)
				assert.Len(t, chunk, written)
			}

			assert.Equal(t, tt.want, string(buffer.data))
			assert.Equal(t, tt.wantDropped, buffer.dropped)
		})
	}
}
