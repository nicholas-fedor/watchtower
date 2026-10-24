package container

import (
	"os"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dockerSystem "github.com/moby/moby/api/types/system"
	dockerClient "github.com/moby/moby/client"
)

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
// or Docker, and that missing markers leave the runtime unknown.
func TestDetectRuntimeByMarker(t *testing.T) {
	t.Parallel()

	tests := []struct {
		title   string
		markers []string
		want    containerRuntime
	}{
		{title: "should return runtimePodman when Podman marker file exists", markers: []string{"run/.containerenv"}, want: runtimePodman},
		{title: "should return runtimeDocker when Docker marker file exists", markers: []string{".dockerenv"}, want: runtimeDocker},
		{title: "should return runtimeUnknown when neither marker file exists", want: runtimeUnknown},
		{
			title:   "should return runtimePodman when Podman marker exists alongside Docker marker",
			markers: []string{"run/.containerenv", ".dockerenv"},
			want:    runtimePodman,
		},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			t.Parallel()

			memFs := fstest.MapFS{}
			for _, marker := range tt.markers {
				memFs[marker] = &fstest.MapFile{}
			}

			result, err := detectRuntimeByMarker(newTestLogger(t), memFs)
			require.NoError(t, err)
			assert.Equal(t, tt.want, result)
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
