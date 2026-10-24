package runtime

import (
	"errors"
	"io/fs"
	"os"
	"testing"
	"testing/fstest"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dockerSystem "github.com/moby/moby/api/types/system"
	dockerClient "github.com/moby/moby/client"

	mockContainer "github.com/nicholas-fedor/watchtower/internal/container/mocks"
)

// deniedFS serves files from a map but denies a stat of the listed paths, as a
// marker file the process may not read would.
type deniedFS struct {
	fstest.MapFS

	// denied holds the paths whose stat fails with fs.ErrPermission.
	denied map[string]bool
}

// Stat returns fs.ErrPermission for a denied path and the map's file otherwise.
//
// Parameters:
//   - name: Path to stat.
//
// Returns:
//   - fs.FileInfo: Information about the file.
//   - error: fs.ErrPermission for a denied path, or the map's error.
func (f deniedFS) Stat(name string) (fs.FileInfo, error) {
	if f.denied[name] {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrPermission}
	}

	return f.MapFS.Stat(name)
}

// newDeniedFS returns a filesystem with the given marker files that denies a
// stat of the denied paths.
//
// Parameters:
//   - markers: Paths of the marker files that exist.
//   - denied: Paths whose stat fails with fs.ErrPermission.
//
// Returns:
//   - deniedFS: The filesystem.
func newDeniedFS(markers, denied []string) deniedFS {
	files := fstest.MapFS{}
	for _, marker := range markers {
		files[marker] = &fstest.MapFile{}
	}

	deniedPaths := map[string]bool{}
	for _, path := range denied {
		deniedPaths[path] = true
	}

	return deniedFS{MapFS: files, denied: deniedPaths}
}

// TestDetectRuntimeByEnv_PodmanVariable verifies that the lowercase container
// variable, which Podman sets, identifies Podman. It sets environment
// variables, so it runs sequentially.
func TestDetectRuntimeByEnv_PodmanVariable(t *testing.T) {
	// A nonmatching uppercase value keeps an inherited CONTAINER from passing
	// the test. It is set first because Windows treats both names as one
	// variable, so the lowercase value replaces it there.
	t.Setenv("CONTAINER", "docker")
	t.Setenv("container", "podman")

	assert.True(t, detectRuntimeByEnv(newTestLogger(t)))
}

// TestDetectRuntimeByAPI verifies which daemon responses identify Podman,
// including Podman versions that only name themselves in the version
// components.
func TestDetectRuntimeByAPI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		// versionErr fails the server version request.
		versionErr error
		name       string
		info       dockerSystem.Info
		// components are the server version components. Nil expects no
		// server version request.
		components []dockerSystem.ComponentVersion
		want       bool
		wantErr    bool
	}{
		{
			name: "Podman named in the info",
			info: dockerSystem.Info{Name: "podman", ServerVersion: "4.0.0"},
			want: true,
		},
		{
			name: "Podman named in the info server version",
			info: dockerSystem.Info{Name: "host-1", ServerVersion: "podman/4.0.0"},
			want: true,
		},
		{
			name:       "Podman named only in the version components",
			info:       dockerSystem.Info{Name: "host-1", ServerVersion: "6.1.3"},
			components: []dockerSystem.ComponentVersion{{Name: "Podman Engine", Version: "6.1.3"}},
			want:       true,
		},
		{
			name:       "Docker",
			info:       dockerSystem.Info{Name: "host-1", ServerVersion: "28.0.0"},
			components: []dockerSystem.ComponentVersion{{Name: "Engine", Version: "28.0.0"}},
			want:       false,
		},
		{
			name:       "version request fails",
			info:       dockerSystem.Info{Name: "host-1", ServerVersion: "6.1.3"},
			versionErr: daemonError("version failed"),
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			api := newDockerAPI(t)
			expectInfo(api, tt.info)

			if tt.components != nil || tt.versionErr != nil {
				api.EXPECT().ServerVersion(mock.Anything, dockerClient.ServerVersionOptions{}).
					Return(dockerClient.ServerVersionResult{Components: tt.components}, tt.versionErr).Once()
			}

			got, err := detectRuntimeByAPI(newTestLogger(t), t.Context(), api)
			if tt.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestDetectRuntimeByMarker verifies which runtime marker files identify Podman
// or Docker and that missing markers leave the runtime unknown. A marker the
// process may not read is reported with its path.
func TestDetectRuntimeByMarker(t *testing.T) {
	t.Parallel()

	tests := []struct {
		title   string
		markers []string
		// denied lists the marker paths whose stat fails with a permission error.
		denied []string
		want   containerRuntime
		// wantErrPaths lists the marker paths a failed check must name.
		wantErrPaths []string
	}{
		{title: "should return runtimePodman when Podman marker file exists", markers: []string{"run/.containerenv"}, want: runtimePodman},
		{title: "should return runtimeDocker when Docker marker file exists", markers: []string{".dockerenv"}, want: runtimeDocker},
		{title: "should return runtimeUnknown when neither marker file exists", want: runtimeUnknown},
		{
			title:   "should return runtimePodman when Podman marker exists alongside Docker marker",
			markers: []string{"run/.containerenv", ".dockerenv"},
			want:    runtimePodman,
		},
		{
			title:        "should report a Podman marker that cannot be read",
			denied:       []string{"run/.containerenv"},
			want:         runtimeUnknown,
			wantErrPaths: []string{"/run/.containerenv"},
		},
		{
			title:        "should report a Docker marker that cannot be read",
			denied:       []string{".dockerenv"},
			want:         runtimeUnknown,
			wantErrPaths: []string{"/.dockerenv"},
		},
		{
			title:        "should report both markers when neither can be read",
			denied:       []string{"run/.containerenv", ".dockerenv"},
			want:         runtimeUnknown,
			wantErrPaths: []string{"/run/.containerenv", "/.dockerenv"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			t.Parallel()

			result, err := detectRuntimeByMarker(newTestLogger(t), newDeniedFS(tt.markers, tt.denied))
			assert.Equal(t, tt.want, result)

			if tt.wantErrPaths == nil {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, fs.ErrPermission)

			for _, path := range tt.wantErrPaths {
				assert.ErrorContains(t, err, path)
			}
		})
	}
}

// TestIsPodman verifies the order of the detection steps. A marker file decides
// before the environment, and the environment decides before the daemon. A
// marker that cannot be read stops detection with its error. The mocked daemon
// has no expectations unless a case lists them, so a step that should not run
// fails the test. The cases set environment variables, so they run
// sequentially.
func TestIsPodman(t *testing.T) {
	tests := []struct {
		name string
		fsys fs.FS
		// containerEnv is the value of the container variable Podman sets.
		containerEnv string
		// info, when set, is the daemon info the API step receives.
		info    *dockerSystem.Info
		want    bool
		wantErr bool
	}{
		{
			name:         "a marker that cannot be read stops detection",
			fsys:         newDeniedFS(nil, []string{"run/.containerenv"}),
			containerEnv: "podman",
			wantErr:      true,
		},
		{
			name:         "a Docker marker wins over the Podman environment",
			fsys:         fstest.MapFS{".dockerenv": &fstest.MapFile{}},
			containerEnv: "podman",
			want:         false,
		},
		{
			name:         "the Podman environment skips the daemon",
			fsys:         fstest.MapFS{},
			containerEnv: "podman",
			want:         true,
		},
		{
			name: "the daemon decides without a marker or environment",
			fsys: fstest.MapFS{},
			info: &dockerSystem.Info{Name: "podman"},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// CONTAINER is set first because Windows treats both names as one
			// variable, so the lowercase value replaces it there.
			t.Setenv("CONTAINER", "")
			t.Setenv("container", tt.containerEnv)

			api := newDockerAPI(t)
			if tt.info != nil {
				expectInfo(api, *tt.info)
			}

			got, err := IsPodman(newTestLogger(t), t.Context(), api, tt.fsys)
			if tt.wantErr {
				require.ErrorIs(t, err, fs.ErrPermission)
				assert.False(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestDetectRuntimeByAPI_InfoFields verifies that the info Name and ServerVersion
// fields identify Podman, and how an empty response and a failed call are reported.
func TestDetectRuntimeByAPI_InfoFields(t *testing.T) {
	t.Parallel()

	t.Run("should return true when API returns Name podman", func(t *testing.T) {
		t.Parallel()

		api := newDockerAPI(t)
		expectInfo(api, dockerSystem.Info{Name: "podman"})

		result, err := detectRuntimeByAPI(newTestLogger(t), t.Context(), api)
		require.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("should return true when API returns ServerVersion containing podman", func(t *testing.T) {
		t.Parallel()

		api := newDockerAPI(t)
		expectInfo(api, dockerSystem.Info{ServerVersion: "podman/4.0.0"})

		result, err := detectRuntimeByAPI(newTestLogger(t), t.Context(), api)
		require.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("should return false when API returns neither podman Name nor ServerVersion", func(t *testing.T) {
		t.Parallel()

		api := newDockerAPI(t)
		expectInfo(api, dockerSystem.Info{Name: "docker", ServerVersion: "20.10.0", OperatingSystem: "linux"})
		expectServerVersion(api, dockerSystem.ComponentVersion{Name: "Engine", Version: "20.10.0"})

		result, err := detectRuntimeByAPI(newTestLogger(t), t.Context(), api)
		require.NoError(t, err)
		assert.False(t, result)
	})

	t.Run("should return error when API call fails", func(t *testing.T) {
		t.Parallel()

		api := newDockerAPI(t)
		api.EXPECT().Info(mock.Anything, dockerClient.InfoOptions{}).
			Return(dockerClient.SystemInfoResult{}, daemonError("server error")).Once()

		result, err := detectRuntimeByAPI(newTestLogger(t), t.Context(), api)
		require.Error(t, err)
		assert.False(t, result)
	})

	t.Run("should return false when API returns empty response", func(t *testing.T) {
		t.Parallel()

		api := newDockerAPI(t)
		expectInfo(api, dockerSystem.Info{})
		expectServerVersion(api)

		result, err := detectRuntimeByAPI(newTestLogger(t), t.Context(), api)
		require.NoError(t, err)
		assert.False(t, result)
	})
}

// TestDetectRuntimeByEnv verifies which container environment variable values
// identify Podman.
func TestDetectRuntimeByEnv(t *testing.T) {
	tests := []struct {
		title string
		// env lists the variables to set. A nil value unsets the variable.
		env  map[string]*string
		want bool
	}{
		{title: "should return true when CONTAINER is podman", env: map[string]*string{"CONTAINER": new("podman")}, want: true},
		{title: "should return true when CONTAINER is oci", env: map[string]*string{"CONTAINER": new("oci")}, want: true},
		{
			title: "should return false when CONTAINER is docker",
			env:   map[string]*string{"CONTAINER": new("docker"), "container": new("")},
		},
		{
			title: "should return false when CONTAINER is other",
			env:   map[string]*string{"CONTAINER": new("other"), "container": new("")},
		},
		{
			title: "should return false when CONTAINER is not set",
			env:   map[string]*string{"container": new(""), "CONTAINER": nil},
		},
		{
			title: "should return false when CONTAINER is empty",
			env:   map[string]*string{"CONTAINER": new(""), "container": new("")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			for name, value := range tt.env {
				// t.Setenv restores the variable, so it is set before it
				// is unset.
				t.Setenv(name, "")

				if value == nil {
					require.NoError(t, os.Unsetenv(name))

					continue
				}

				t.Setenv(name, *value)
			}

			assert.Equal(t, tt.want, detectRuntimeByEnv(newTestLogger(t)))
		})
	}
}

// daemonError returns an error formatted as the Docker SDK reports a daemon
// error response.
//
// Parameters:
//   - message: Message the daemon responded with.
//
// Returns:
//   - error: Error with the daemon's message.
func daemonError(message string) error {
	return errors.New("Error response from daemon: " + message)
}

// expectInfo expects one daemon info request and answers it with info.
//
// Parameters:
//   - api: Mocked Docker API.
//   - info: Daemon information to answer with.
func expectInfo(api *mockContainer.MockAPIClient, info dockerSystem.Info) {
	api.EXPECT().Info(mock.Anything, dockerClient.InfoOptions{}).
		Return(dockerClient.SystemInfoResult{Info: info}, nil).Once()
}

// newDockerAPI returns a mocked Docker API whose client version is available
// for diagnostic logs.
//
// Parameters:
//   - t: Test the mock belongs to.
//
// Returns:
//   - *mockContainer.MockAPIClient: Mocked Docker API.
func newDockerAPI(t *testing.T) *mockContainer.MockAPIClient {
	t.Helper()

	api := mockContainer.NewMockAPIClient(t)
	api.EXPECT().ClientVersion().Return("1.47").Maybe()

	return api
}

// expectServerVersion expects one server version request and answers it with
// the given components.
//
// Parameters:
//   - api: Mocked Docker API.
//   - components: Version components the daemon reports.
func expectServerVersion(api *mockContainer.MockAPIClient, components ...dockerSystem.ComponentVersion) {
	api.EXPECT().ServerVersion(mock.Anything, dockerClient.ServerVersionOptions{}).
		Return(dockerClient.ServerVersionResult{Components: components}, nil).Once()
}

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
