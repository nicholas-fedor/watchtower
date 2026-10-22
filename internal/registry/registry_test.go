package registry

import (
	"bytes"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dockerCliConfig "github.com/docker/cli/cli/config"

	"github.com/nicholas-fedor/watchtower/internal/logging"
	mockTypes "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

// newTestLogger returns a logger that writes each event to the test's log, so a
// failing test shows what it logged.
//
// Parameters:
//   - t: Test that receives the log lines.
//
// Returns:
//   - *zerolog.Logger: Logger writing to t.
func newTestLogger(t *testing.T) *zerolog.Logger {
	t.Helper()

	// Frame 4 skips zerolog's write path, so each line names the log call.
	w := zerolog.TestWriter{T: t, Frame: 4}

	return new(zerolog.New(w))
}

// TestWarnOnAPIConsumption verifies that WarnOnAPIConsumption returns true for
// registries that support HEAD requests (Docker Hub, GHCR) and false otherwise.
func TestWarnOnAPIConsumption(t *testing.T) {
	tests := []struct {
		name      string
		imageName string
		want      bool
	}{
		{
			name:      "ghcr.io image",
			imageName: "ghcr.io/nicholas-fedor/watchtower",
			want:      true,
		},
		{
			name:      "implicit docker hub image",
			imageName: "docker:latest",
			want:      true,
		},
		{
			name:      "explicit index.docker.io image",
			imageName: "index.docker.io/docker:latest",
			want:      true,
		},
		{
			name:      "explicit docker.io image",
			imageName: "docker.io/docker:latest",
			want:      true,
		},
		{
			name:      "other registry fsf.org",
			imageName: "docker.fsf.org/docker:latest",
			want:      false,
		},
		{
			name:      "other registry altavista.com",
			imageName: "altavista.com/docker:latest",
			want:      false,
		},
		{
			name:      "other registry gitlab.com",
			imageName: "gitlab.com/docker:latest",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			container := mockTypes.NewMockContainer(t)
			container.EXPECT().Name().Return("test-container").Maybe()
			container.EXPECT().ImageName().Return(tt.imageName)

			got := WarnOnAPIConsumption(newTestLogger(t), container)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestGetPullOptions_TraceLogsOmitCredentials verifies that GetPullOptions trace
// logging never includes the encoded registry credential payload.
func TestGetPullOptions_TraceLogsOmitCredentials(t *testing.T) {
	const secretPass = "super-secret-pull-pass"

	logBuf := &bytes.Buffer{}
	log := logging.New(logging.LogfmtWriter(logBuf), logging.TraceLevel)

	// Empty Docker config so auth.EncodedAuth falls through to environment credentials.
	tempDir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", tempDir)
	t.Setenv("HOME", tempDir)
	dockerCliConfig.SetDir(tempDir)

	t.Setenv("REPO_USER", "pull-user")
	t.Setenv("REPO_PASS", secretPass)

	opts, err := GetPullOptions(log, "docker.io/library/alpine:latest")
	require.NoError(t, err)
	require.NotEmpty(t, opts.RegistryAuth)

	out := logBuf.String()
	assert.Contains(t, out, "Retrieved authentication credentials")
	assert.Contains(t, out, "has_credentials")
	assert.NotContains(t, out, secretPass)
	assert.NotContains(t, out, opts.RegistryAuth)
}
