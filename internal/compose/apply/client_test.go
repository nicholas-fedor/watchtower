package apply

import (
	"context"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/docker/compose/v5/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	composeTypes "github.com/compose-spec/compose-go/v2/types"
	dockerClient "github.com/moby/moby/client"

	mockCompose "github.com/nicholas-fedor/watchtower/internal/compose/apply/mocks"
	composeProject "github.com/nicholas-fedor/watchtower/internal/compose/project"
	"github.com/nicholas-fedor/watchtower/internal/container"
	dockerConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/docker"
	"github.com/nicholas-fedor/watchtower/internal/testutil/httpstub"
)

// TestInjectLabels verifies that labels are merged onto the named service only,
// that no labels leave the project unchanged, and that a nil project is an
// error.
func TestInjectLabels(t *testing.T) {
	t.Parallel()

	t.Run("nil project", func(t *testing.T) {
		t.Parallel()

		_, err := injectLabels(nil, map[string]map[string]string{"api": {"k": "v"}})
		require.ErrorIs(t, err, errNilProject)
	})

	t.Run("empty labels returns the same project", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services:\n  api:\n    image: app\n"), 0o600))

		project, err := composeProject.Load(t.Context(), composeProject.ProjectRef{Dir: dir, Name: "p"})
		require.NoError(t, err)

		got, err := injectLabels(project, nil)
		require.NoError(t, err)
		assert.Equal(t, project, got)
	})

	t.Run("merges labels onto the named service only", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(`
services:
  api:
    build: ./api
    labels:
      keep: "yes"
  db:
    image: postgres:16
`), 0o600))

		project, err := composeProject.Load(t.Context(), composeProject.ProjectRef{Dir: dir, Name: "p"})
		require.NoError(t, err)

		got, err := injectLabels(project, map[string]map[string]string{
			"api": {
				"com.centurylinklabs.watchtower.git-last-commit": "abc",
			},
		})
		require.NoError(t, err)

		api, err := got.GetService("api")
		require.NoError(t, err)
		assert.Equal(t, "yes", api.Labels["keep"])
		assert.Equal(t, "abc", api.Labels["com.centurylinklabs.watchtower.git-last-commit"])

		db, err := got.GetService("db")
		require.NoError(t, err)
		assert.Empty(t, db.Labels["com.centurylinklabs.watchtower.git-last-commit"])
	})
}

// TestAppliedImageID verifies that a container's image ID comes from the
// Compose image summary for the container, or from the container's image when
// that is an ID.
func TestAppliedImageID(t *testing.T) {
	t.Parallel()

	summary := api.ContainerSummary{Name: "webstack-api-1", Image: "webstack-api:latest"}
	assert.Empty(t, appliedImageID(summary, nil))
	assert.Empty(t, appliedImageID(summary, map[string]api.ImageSummary{}))
	assert.Equal(t, container.ImageID("sha256:abc"), appliedImageID(
		summary,
		map[string]api.ImageSummary{"webstack-api-1": {ID: "sha256:abc"}},
	))
	assert.Equal(
		t,
		container.ImageID("sha256:fromps"),
		appliedImageID(api.ContainerSummary{Name: "x", Image: "sha256:fromps"}, nil),
	)
}

// TestClient_newDockerCLI checks that the Docker CLI Compose uses connects over
// TLS to the host the Docker settings name, through an API client built from
// those settings, so that with TLS verification off it needs no CA file. It sets
// environment variables, so it does not run in parallel.
func TestClient_newDockerCLI(t *testing.T) {
	// Keep docker/cli away from the real Docker configuration and context. It
	// reads DOCKER_CONFIG once per process, so the directory spans every case.
	t.Setenv("DOCKER_CONFIG", t.TempDir())

	tests := []struct {
		name string
		// keepCA reports whether ca.pem stays in the certificate path.
		keepCA    bool
		tlsVerify bool
	}{
		{name: "verified TLS", keepCA: true, tlsVerify: true},
		{name: "unverified TLS without a CA file", keepCA: false, tlsVerify: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, name := range []string{"DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_API_VERSION", "DOCKER_TLS", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH"} {
				t.Setenv(name, "")
				require.NoError(t, os.Unsetenv(name))
			}

			server := httpstub.NewTLS(t)
			server.AllowUnhandledRequests()

			for _, method := range []string{http.MethodHead, http.MethodGet} {
				server.RouteToHandler(method, httpstub.Path("/_ping"),
					httpstub.RespondWith(http.StatusOK, "", http.Header{"Api-Version": []string{"1.45"}}))
			}

			certPath := server.WriteDockerCerts(t)
			if !tt.keepCA {
				require.NoError(t, os.Remove(filepath.Join(certPath, "ca.pem")))
			}

			client := NewClient(dockerConfig.Docker{
				Host:       "tcp://" + server.Addr(),
				APIVersion: "",
				CertPath:   certPath,
				TLSVerify:  tt.tlsVerify,
			})

			dockerCLI, err := client.newDockerCLI()
			require.NoError(t, err)

			_, err = dockerCLI.Client().Ping(t.Context(), dockerClient.PingOptions{})
			require.NoError(t, err)

			requests := server.ReceivedRequests()
			require.NotEmpty(t, requests)

			for _, request := range requests {
				assert.NotNil(t, request.TLS, "the ping is sent over TLS")
			}

			// The Compose service is created once and reused.
			service, err := client.composeService()
			require.NoError(t, err)

			again, err := client.composeService()
			require.NoError(t, err)
			assert.Same(t, service, again)
		})
	}
}

// TestClient_newDockerCLI_apiVersion checks which API version the Docker CLI
// Compose uses sends its requests with. A configured version newer than the
// daemon's falls back to the daemon's version, as the container client does. The
// fallback comes from the Docker CLI's own startup ping, which negotiates even a
// fixed version. A Docker CLI update that stops doing that fails this test. It
// sets environment variables, so it does not run in parallel.
func TestClient_newDockerCLI_apiVersion(t *testing.T) {
	// Keep docker/cli away from the real Docker configuration and context. It
	// reads DOCKER_CONFIG once per process, so the directory spans every case.
	t.Setenv("DOCKER_CONFIG", t.TempDir())

	const daemonVersion = "1.44"

	tests := []struct {
		name string
		// apiVersion is the configured API version, empty for negotiation.
		apiVersion string
		wantPath   string
	}{
		{name: "configured version newer than the daemon's", apiVersion: "1.47", wantPath: "/v1.44/containers/json"},
		{name: "configured version the daemon supports", apiVersion: "1.41", wantPath: "/v1.41/containers/json"},
		{name: "no configured version", apiVersion: "", wantPath: "/v1.44/containers/json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, name := range []string{"DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_API_VERSION", "DOCKER_TLS", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH"} {
				t.Setenv(name, "")
				require.NoError(t, os.Unsetenv(name))
			}

			server := httpstub.New(t)
			server.AllowUnhandledRequests()

			for _, method := range []string{http.MethodHead, http.MethodGet} {
				server.RouteToHandler(method, httpstub.Path("/_ping"),
					httpstub.RespondWith(http.StatusOK, "", http.Header{"Api-Version": []string{daemonVersion}}))
			}

			server.RouteToHandler(http.MethodGet, httpstub.Path(tt.wantPath),
				httpstub.RespondWith(http.StatusOK, "[]", http.Header{"Content-Type": []string{"application/json"}}))

			client := NewClient(dockerConfig.Docker{
				Host:       "tcp://" + server.Addr(),
				APIVersion: tt.apiVersion,
				CertPath:   "",
				TLSVerify:  false,
			})

			dockerCLI, err := client.newDockerCLI()
			require.NoError(t, err)

			_, err = dockerCLI.Client().ContainerList(t.Context(), dockerClient.ContainerListOptions{})
			require.NoError(t, err, "the listing is sent to %s", tt.wantPath)
		})
	}
}

// TestClient_newDockerCLI_errors checks that settings the Docker CLI cannot
// connect with return an error instead of a Docker CLI, and that the Compose
// service passes the error on.
func TestClient_newDockerCLI_errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		settings func(t *testing.T) dockerConfig.Docker
		wantErr  string
	}{
		{
			name: "missing certificate files",
			settings: func(t *testing.T) dockerConfig.Docker {
				t.Helper()

				return dockerConfig.Docker{
					Host:       "tcp://docker.example.com:2376",
					APIVersion: "",
					CertPath:   t.TempDir(),
					TLSVerify:  true,
				}
			},
			wantErr: "configure TLS",
		},
		{
			name: "unparsable host",
			settings: func(*testing.T) dockerConfig.Docker {
				return dockerConfig.Docker{Host: "docker.example.com", APIVersion: "", CertPath: "", TLSVerify: false}
			},
			wantErr: "unable to parse docker host",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := NewClient(tt.settings(t))

			dockerCLI, err := client.newDockerCLI()
			require.ErrorContains(t, err, tt.wantErr)
			assert.Nil(t, dockerCLI)

			service, err := client.composeService()
			require.ErrorContains(t, err, tt.wantErr)
			assert.Nil(t, service)
		})
	}
}

// TestExportConnection checks that the DOCKER_* connection variables follow
// the Docker settings, and that an empty or false setting unsets its variable
// rather than leaving the environment's value in place. It sets environment
// variables, so it does not run in parallel.
func TestExportConnection(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://old.example.com:2375")
	t.Setenv("DOCKER_TLS", "1")
	t.Setenv("DOCKER_TLS_VERIFY", "0")
	t.Setenv("DOCKER_API_VERSION", "1.41")
	t.Setenv("DOCKER_CERT_PATH", "/old/certs")

	require.NoError(t, ExportConnection(dockerConfig.Docker{
		Host:       "tcp://docker.example.com:2376",
		TLSVerify:  true,
		APIVersion: "1.47",
		CertPath:   "/certs",
	}))
	assert.Equal(t, "tcp://docker.example.com:2376", os.Getenv("DOCKER_HOST"))
	assert.Equal(t, "1", os.Getenv("DOCKER_TLS"))
	assert.Equal(t, "1", os.Getenv("DOCKER_TLS_VERIFY"))
	assert.Equal(t, "1.47", os.Getenv("DOCKER_API_VERSION"))
	assert.Equal(t, "/certs", os.Getenv("DOCKER_CERT_PATH"))

	require.NoError(t, ExportConnection(dockerConfig.Docker{}))

	for _, name := range []string{"DOCKER_HOST", "DOCKER_TLS", "DOCKER_TLS_VERIFY", "DOCKER_API_VERSION", "DOCKER_CERT_PATH"} {
		value, ok := os.LookupEnv(name)
		assert.False(t, ok, "%s is set to %q", name, value)
	}
}

// TestClientApplyScopesProject verifies that applying a service scopes the project to that service and its dependencies.
func TestClientApplyScopesProject(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(`
services:
  app:
    image: app
    depends_on:
      - db
  db:
    image: db
  cache:
    image: cache
`), 0o600))

	var (
		upProject *composeTypes.Project
		upOptions api.UpOptions
	)

	service := mockCompose.NewMockCompose(t)
	service.EXPECT().Up(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, project *composeTypes.Project, options api.UpOptions) error {
			upProject = project
			upOptions = options

			return nil
		}).Once()
	service.EXPECT().Ps(mock.Anything, "project", mock.Anything).
		Return([]api.ContainerSummary{{Service: "app", Name: "project-app-1", ID: "app-id"}}, nil).Once()
	service.EXPECT().Images(mock.Anything, "project", mock.Anything).
		Return(map[string]api.ImageSummary{}, nil).Once()

	client := &Client{service: service}
	containers, err := client.Apply(t.Context(), Request{
		Ref:      composeProject.ProjectRef{Dir: dir, Name: "project"},
		Services: []string{"app"},
	})
	require.NoError(t, err)
	assert.Equal(t, []Container{{
		Service: "app",
		Name:    "project-app-1",
		ID:      container.ContainerID("app-id"),
	}}, containers)

	require.NotNil(t, upProject)
	assert.ElementsMatch(t, []string{"app", "db"}, upProject.ServiceNames())
	assert.NotContains(t, upProject.ServiceNames(), "cache")
	assert.Equal(t, []string{"app"}, upOptions.Create.Services)
	assert.Equal(t, api.RecreateNever, upOptions.Create.RecreateDependencies)
	assert.Empty(t, upOptions.Start.Services)
}

// TestClientApplyLeavesOutJobs verifies that Apply passes Compose a project
// without the file's jobs, scheduled, manual and profile-disabled alike, so
// compose up neither requires the engine's jobs extension nor registers jobs.
func TestClientApplyLeavesOutJobs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(`
services:
  app:
    image: app
jobs:
  backup:
    image: backup
    triggers:
      schedule:
        - cron: "0 3 * * *"
  migrate:
    image: migrate
    triggers:
      manual: true
  report:
    image: report
    profiles: [reports]
    triggers:
      schedule:
        - cron: "0 4 * * *"
`), 0o600))

	ref := composeProject.ProjectRef{Dir: dir, Name: "project"}

	loaded, err := composeProject.Load(t.Context(), ref)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"backup", "migrate"}, slices.Collect(maps.Keys(loaded.Jobs)))
	require.Contains(t, loaded.DisabledJobs, "report")

	var upProject *composeTypes.Project

	service := mockCompose.NewMockCompose(t)
	service.EXPECT().Up(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, project *composeTypes.Project, _ api.UpOptions) error {
			upProject = project

			return nil
		}).Once()
	service.EXPECT().Ps(mock.Anything, "project", mock.Anything).
		Return([]api.ContainerSummary{{Service: "app", Name: "project-app-1", ID: "app-id"}}, nil).Once()
	service.EXPECT().Images(mock.Anything, "project", mock.Anything).
		Return(map[string]api.ImageSummary{}, nil).Once()

	client := &Client{service: service}
	_, err = client.Apply(t.Context(), Request{Ref: ref, Services: []string{"app"}})
	require.NoError(t, err)

	require.NotNil(t, upProject)
	assert.Equal(t, []string{"app"}, upProject.ServiceNames())
	assert.Empty(t, upProject.Jobs)
	assert.Empty(t, upProject.DisabledJobs)
}

// TestClientApplyRejectsInvalidRequests verifies that Client.Apply rejects invalid requests before invoking Compose.
func TestClientApplyRejectsInvalidRequests(t *testing.T) {
	t.Parallel()

	// The mock has no expectations, so any Compose call fails the test.
	client := &Client{service: mockCompose.NewMockCompose(t)}

	_, err := client.Apply(t.Context(), Request{
		Ref:      composeProject.ProjectRef{},
		Services: []string{"app"},
	})
	require.ErrorIs(t, err, composeProject.ErrEmptyProjectDir)

	_, err = client.Apply(t.Context(), Request{
		Ref:      composeProject.ProjectRef{Dir: t.TempDir()},
		Services: nil,
	})
	require.ErrorIs(t, err, errNoComposeServices)
}

// TestClientApplyStampsComposeIdentity verifies Apply sets the labels compose start filters on.
func TestClientApplyStampsComposeIdentity(t *testing.T) {
	t.Parallel()

	dir := writeIdentityComposeDir(t)

	t.Run("up", func(t *testing.T) {
		t.Parallel()

		var (
			upProject *composeTypes.Project
			upOptions api.UpOptions
		)

		service := mockCompose.NewMockCompose(t)
		service.EXPECT().Up(mock.Anything, mock.Anything, mock.Anything).
			RunAndReturn(func(_ context.Context, project *composeTypes.Project, options api.UpOptions) error {
				upProject = project
				upOptions = options

				return nil
			}).Once()
		service.EXPECT().Ps(mock.Anything, "jobs", mock.Anything).
			Return([]api.ContainerSummary{{Service: "api", Name: "jobs-api-1", ID: "api-id"}}, nil).Once()
		service.EXPECT().Images(mock.Anything, "jobs", mock.Anything).
			Return(map[string]api.ImageSummary{}, nil).Once()

		client := &Client{service: service}
		_, err := client.Apply(t.Context(), Request{
			Ref:      composeProject.ProjectRef{Dir: dir, Name: "jobs"},
			Services: []string{"api"},
			Labels: map[string]map[string]string{
				"api": {"com.centurylinklabs.watchtower.git-last-commit": "abc"},
			},
		})
		require.NoError(t, err)

		require.NotNil(t, upProject)
		assert.True(t, upOptions.Start.Wait)
		assert.Nil(t, upOptions.Start.Attach)
		assert.Equal(t, api.RecreateForce, upOptions.Create.Recreate)
		assertComposeIdentity(t, upProject, dir)

		apiService, err := upProject.GetService("api")
		require.NoError(t, err)
		assert.Equal(t, "yes", apiService.Labels["keep"])
		assert.Equal(t, "from-file", apiService.Labels[api.ProjectLabel])
		assert.Equal(t, "abc", apiService.Labels["com.centurylinklabs.watchtower.git-last-commit"])

		dbService, err := upProject.GetService("db")
		require.NoError(t, err)
		assert.Equal(t, "db", dbService.CustomLabels[api.ServiceLabel])
	})

	t.Run("build", func(t *testing.T) {
		t.Parallel()

		var built *composeTypes.Project

		// Without an Up expectation, a compose up fails the test.
		service := mockCompose.NewMockCompose(t)
		service.EXPECT().Build(mock.Anything, mock.Anything, mock.Anything).
			RunAndReturn(func(_ context.Context, project *composeTypes.Project, _ api.BuildOptions) error {
				built = project

				return nil
			}).Once()
		service.EXPECT().Ps(mock.Anything, "jobs", mock.Anything).Return([]api.ContainerSummary{}, nil).Once()
		service.EXPECT().Images(mock.Anything, "jobs", mock.Anything).Return(map[string]api.ImageSummary{}, nil).Once()

		client := &Client{service: service}
		_, err := client.Apply(t.Context(), Request{
			Ref:       composeProject.ProjectRef{Dir: dir, Name: "jobs"},
			Services:  []string{"api"},
			BuildOnly: true,
		})
		require.NoError(t, err)
		assertComposeIdentity(t, built, dir)
	})
}

// TestStampComposeIdentity verifies identity labels are merged and an empty name is rejected.
func TestStampComposeIdentity(t *testing.T) {
	t.Parallel()

	t.Run("nil project", func(t *testing.T) {
		t.Parallel()

		_, err := stampComposeIdentity(nil)
		require.ErrorIs(t, err, errNilProject)
	})

	t.Run("empty name", func(t *testing.T) {
		t.Parallel()

		_, err := stampComposeIdentity(&composeTypes.Project{
			Services: composeTypes.Services{
				"api": {Name: "api", Image: "app"},
			},
		})
		require.ErrorIs(t, err, errEmptyProjectName)
	})

	t.Run("loaded project keeps other custom labels", func(t *testing.T) {
		t.Parallel()

		dir := writeIdentityComposeDir(t)
		project, err := composeProject.Load(t.Context(), composeProject.ProjectRef{Dir: dir, Name: "jobs"})
		require.NoError(t, err)

		service, err := project.GetService("api")
		require.NoError(t, err)

		service.CustomLabels = composeTypes.Labels{
			"keep-custom":   "yes",
			api.OneoffLabel: "True",
		}
		project.Services["api"] = service

		got, err := stampComposeIdentity(project)
		require.NoError(t, err)
		assertComposeIdentity(t, got, dir)

		stamped, err := got.GetService("api")
		require.NoError(t, err)
		assert.Equal(t, "yes", stamped.CustomLabels["keep-custom"])
		assert.Equal(t, "False", stamped.CustomLabels[api.OneoffLabel])

		original, err := project.GetService("api")
		require.NoError(t, err)
		assert.Equal(t, "True", original.CustomLabels[api.OneoffLabel])
		assert.Equal(t, "yes", original.CustomLabels["keep-custom"])
	})
}

// assertComposeIdentity checks the labels compose start uses to find service containers.
//
// Parameters:
//   - t: Test that receives the failures.
//   - project: Project whose services are checked.
//   - dir: Working directory the project must have.
func assertComposeIdentity(t *testing.T, project *composeTypes.Project, dir string) {
	t.Helper()

	require.NotNil(t, project)
	require.NotEmpty(t, project.ServiceNames())
	require.Equal(t, dir, project.WorkingDir)
	require.NotEmpty(t, project.Name)

	configFiles := strings.Join(project.ComposeFiles, ",")

	for _, serviceName := range project.ServiceNames() {
		service, err := project.GetService(serviceName)
		require.NoError(t, err)
		assert.Equal(t, "False", service.CustomLabels[api.OneoffLabel])
		assert.Equal(t, project.Name, service.CustomLabels[api.ProjectLabel])
		assert.Equal(t, serviceName, service.CustomLabels[api.ServiceLabel])
		assert.Equal(t, project.WorkingDir, service.CustomLabels[api.WorkingDirLabel])
		assert.Equal(t, api.ComposeVersion, service.CustomLabels[api.VersionLabel])
		assert.Equal(t, configFiles, service.CustomLabels[api.ConfigFilesLabel])
	}
}

// writeIdentityComposeDir writes a project whose file labels are not compose identity labels.
//
// Parameters:
//   - t: Test that owns the directory.
//
// Returns:
//   - string: Project directory path.
func writeIdentityComposeDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(`
name: webstack
services:
  api:
    image: app
    depends_on:
      - db
    labels:
      keep: "yes"
      com.docker.compose.project: from-file
      com.docker.compose.service: from-file
  db:
    image: postgres:16
`), 0o600))

	return dir
}

// TestScopeComposeProjectRejectsMissingService verifies that selecting an unknown service returns an error.
func TestScopeComposeProjectRejectsMissingService(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services:\n  app:\n    image: app\n"), 0o600))

	project, err := composeProject.Load(t.Context(), composeProject.ProjectRef{Dir: dir, Name: "project"})
	require.NoError(t, err)

	_, err = scopeComposeProject(project, []string{"missing"})
	require.Error(t, err)
	assert.ErrorContains(t, err, `no such service: missing`)
}
