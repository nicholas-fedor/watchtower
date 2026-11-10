package update

import (
	"maps"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	goGit "github.com/go-git/go-git/v5"
	dockerContainer "github.com/moby/moby/api/types/container"
	dockerImage "github.com/moby/moby/api/types/image"
	dockerNetwork "github.com/moby/moby/api/types/network"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/git"
	"github.com/nicholas-fedor/watchtower/internal/update/implicitrestart"
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
	"github.com/nicholas-fedor/watchtower/internal/update/sorter"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
)

// sessionBatch records what an update session does with the notification
// batch it opens.
type sessionBatch struct {
	sent  []*state.Report // Reports the session sent, in order.
	ended int             // Times the session ended the batch.
}

// The hand-written fake must satisfy the update client interface. Nothing else
// reports a missing or renamed method until a test passes it where the
// interface is required.
var _ Client = mockUpdate.FakeClient{}

// record keeps report as one the session sent. It serves as the send of an
// update session's notification batch.
//
// Parameters:
//   - report: The report sent.
func (b *sessionBatch) record(report *state.Report) {
	b.sent = append(b.sent, report)
}

// newMockClient returns a client mock that stores no copy files. The restart
// package's tests cover copy files, so the update tests leave the copy-file
// calls unchecked.
//
// Parameters:
//   - t: Test the mock belongs to.
//
// Returns:
//   - *mockUpdate.MockClient: The client mock.
func newMockClient(t *testing.T) *mockUpdate.MockClient {
	t.Helper()

	client := mockUpdate.NewMockClient(t)
	client.EXPECT().SnapshotCopyFiles(mock.Anything, mock.Anything).Return(nil).Maybe()
	client.EXPECT().DiscardCopyFiles(mock.Anything).Maybe()

	return client
}

// updateImplicitRestart marks the containers that depend on a restarting
// container as linked to restarting, as an update scan does. The dependency
// graph is built from containers, with the rest of allContainers unmonitored.
//
// Parameters:
//   - log: Process logger.
//   - allContainers: Every listed container.
//   - containers: The monitored containers, which may be marked.
//   - useComposeDependsOn: Whether to include Compose depends_on labels as links.
func updateImplicitRestart(log *zerolog.Logger, allContainers, containers []*container.Container, useComposeDependsOn bool) {
	reconcileImplicitRestarts(log, allContainers, containers, updateParams.Params{UseComposeDependsOn: useComposeDependsOn})
}

// reconcileImplicitRestarts clears linked-restart marks and derives them again
// with a dependency graph built from containers, with the rest of
// allContainers unmonitored.
//
// Parameters:
//   - log: Process logger.
//   - allContainers: Every listed container.
//   - containers: The monitored containers.
//   - params: Update parameters.
//
// Returns:
//   - []*container.Container: Containers that should still restart.
func reconcileImplicitRestarts(
	log *zerolog.Logger,
	allContainers, containers []*container.Container,
	params updateParams.Params,
) []*container.Container {
	graph, err := sorter.NewDependencyGraph(log,
		containers,
		unmonitoredContainers(allContainers, containers),
		params.UseComposeDependsOn,
	)
	if err != nil {
		return nil
	}

	return implicitrestart.Reconcile(log, graph, allContainers, containers, params, nil)
}

func getCommonTestData() *mockUpdate.TestData {
	return &mockUpdate.TestData{
		NameOfContainerToKeep: "",
		Containers: []*container.Container{
			mockUpdate.CreateMockContainer(
				"test-container-01",
				"test-container-01",
				"fake-image:latest",
				time.Now().AddDate(0, 0, -1),
			),
			mockUpdate.CreateMockContainer(
				"test-container-02",
				"test-container-02",
				"fake-image:latest",
				time.Now(),
			),
			mockUpdate.CreateMockContainer(
				"test-container-03",
				"test-container-03",
				"fake-image:latest",
				time.Now(),
			),
		},
	}
}

func getLinkedTestData(withImageInfo bool) *mockUpdate.TestData {
	staleContainer := mockUpdate.CreateMockContainer(
		"test-container-01",
		"/test-container-01",
		"fake-image1:latest",
		time.Now().AddDate(0, 0, -1),
	)

	var imageInfo *dockerImage.InspectResponse
	if withImageInfo {
		imageInfo = mockUpdate.CreateMockImageInfo("test-container-02")
	}

	linkingContainer := mockUpdate.CreateMockContainerWithLinks(
		"test-container-02",
		"/test-container-02",
		"fake-image2:latest",
		time.Now(),
		[]string{staleContainer.Name()},
		imageInfo,
	)

	return &mockUpdate.TestData{
		Staleness: map[string]bool{linkingContainer.Name(): false},
		Containers: []*container.Container{
			staleContainer,
			linkingContainer,
		},
	}
}

func getNetworkModeTestData() *mockUpdate.TestData {
	staleContainer := mockUpdate.CreateMockContainer(
		"network-dependency",
		"/network-dependency",
		"fake-image:latest",
		time.Now().AddDate(0, 0, -1),
	)

	dependentContainer := mockUpdate.CreateMockContainerWithConfig(
		"network-dependent",
		"/network-dependent",
		"fake-image2:latest",
		true,
		false,
		time.Now(),
		&dockerContainer.Config{
			Image:        "fake-image2:latest",
			Labels:       make(map[string]string),
			ExposedPorts: dockerNetwork.PortSet{},
		},
	)

	// Set network mode to container:network-dependency
	dependentContainer.ContainerInfo().HostConfig.NetworkMode = "container:network-dependency"

	return &mockUpdate.TestData{
		Staleness:  map[string]bool{staleContainer.Name(): true, dependentContainer.Name(): false},
		Containers: []*container.Container{staleContainer, dependentContainer},
	}
}

func getComposeTestData() *mockUpdate.TestData {
	// Create a database container with service name "db" but container name "myproject_db_1"
	dbContainer := mockUpdate.CreateMockContainerWithConfig(
		"myproject_db_1",
		"/myproject_db_1",
		"postgres:latest",
		true,
		false,
		time.Now().AddDate(0, 0, -1),
		&dockerContainer.Config{
			Image: "postgres:latest",
			Labels: map[string]string{
				"com.docker.compose.service": "db",
			},
			ExposedPorts: dockerNetwork.PortSet{},
		},
	)

	// Create a web container with service name "web" but container name "myproject_web_1"
	// that depends on "db"
	webContainer := mockUpdate.CreateMockContainerWithConfig(
		"myproject_web_1",
		"/myproject_web_1",
		"web:latest",
		true,
		false,
		time.Now(),
		&dockerContainer.Config{
			Image: "web:latest",
			Labels: map[string]string{
				"com.docker.compose.service":    "web",
				"com.docker.compose.depends_on": "db",
			},
			ExposedPorts: dockerNetwork.PortSet{},
		},
	)

	return &mockUpdate.TestData{
		Staleness:  map[string]bool{dbContainer.Name(): true, webContainer.Name(): false},
		Containers: []*container.Container{dbContainer, webContainer},
	}
}

func getComposeProjectPrefixedTestData() *mockUpdate.TestData {
	// Create a database container with project and service labels
	dbContainer := mockUpdate.CreateMockContainerWithConfig(
		"myapp-database-1",
		"/myapp-database-1",
		"postgres:latest",
		true,
		false,
		time.Now().AddDate(0, 0, -1),
		&dockerContainer.Config{
			Image: "postgres:latest",
			Labels: map[string]string{
				"com.docker.compose.project": "myapp",
				"com.docker.compose.service": "database",
			},
			ExposedPorts: dockerNetwork.PortSet{},
		},
	)

	// Create a web container with project and service labels that depends on "database"
	webContainer := mockUpdate.CreateMockContainerWithConfig(
		"myapp-watchtower-test-app1-1",
		"/myapp-watchtower-test-app1-1",
		"nginx:latest",
		true,
		false,
		time.Now(),
		&dockerContainer.Config{
			Image: "nginx:latest",
			Labels: map[string]string{
				"com.docker.compose.project":    "myapp",
				"com.docker.compose.service":    "watchtower-test-app1",
				"com.docker.compose.depends_on": "database:service_started:true",
			},
			ExposedPorts: dockerNetwork.PortSet{},
		},
	)

	return &mockUpdate.TestData{
		Staleness:  map[string]bool{dbContainer.Name(): true, webContainer.Name(): false},
		Containers: []*container.Container{dbContainer, webContainer},
	}
}

func getComposeMultiHopTestData() *mockUpdate.TestData {
	// Create containers for a chain: cache -> db -> app
	// depends on db, db depends on cache
	cacheContainer := mockUpdate.CreateMockContainerWithConfig(
		"myproject_cache_1",
		"/myproject_cache_1",
		"redis:latest",
		true,
		false,
		time.Now().AddDate(0, 0, -1),
		&dockerContainer.Config{
			Image: "redis:latest",
			Labels: map[string]string{
				"com.docker.compose.service": "cache",
			},
			ExposedPorts: dockerNetwork.PortSet{},
		},
	)

	dbContainer := mockUpdate.CreateMockContainerWithConfig(
		"myproject_db_1",
		"/myproject_db_1",
		"postgres:latest",
		true,
		false,
		time.Now(),
		&dockerContainer.Config{
			Image: "postgres:latest",
			Labels: map[string]string{
				"com.docker.compose.service":    "db",
				"com.docker.compose.depends_on": "cache",
			},
			ExposedPorts: dockerNetwork.PortSet{},
		},
	)

	appContainer := mockUpdate.CreateMockContainerWithConfig(
		"myproject_app_1",
		"/myproject_app_1",
		"app:latest",
		true,
		false,
		time.Now(),
		&dockerContainer.Config{
			Image: "app:latest",
			Labels: map[string]string{
				"com.docker.compose.service":    "app",
				"com.docker.compose.depends_on": "db",
			},
			ExposedPorts: dockerNetwork.PortSet{},
		},
	)

	return &mockUpdate.TestData{
		Staleness: map[string]bool{
			cacheContainer.Name(): true,
			dbContainer.Name():    false,
			appContainer.Name():   false,
		},
		Containers: []*container.Container{cacheContainer, dbContainer, appContainer},
	}
}

// getComposeHyphenatedProjectTestData creates test data for a hyphenated Compose
// project name with explicit container_name declarations and real Compose-emitted
// depends_on label strings in "service:condition:bool" format.
func getComposeHyphenatedProjectTestData() *mockUpdate.TestData {
	// Base service with *explicit container_name* producing a bare runtime name.
	// Project label present so Links() will emit the project-prefixed form ("download-torrent-base").
	baseContainer := mockUpdate.CreateMockContainerWithConfig(
		"base",
		"/base",
		"redis:alpine",
		true,
		false,
		time.Now().AddDate(0, 0, -1),
		&dockerContainer.Config{
			Image: "redis:alpine",
			Labels: map[string]string{
				"com.docker.compose.project":          "download-torrent",
				"com.docker.compose.service":          "base",
				"com.docker.compose.container-number": "1",
			},
			ExposedPorts: dockerNetwork.PortSet{},
		},
	)

	// Dependent with bare container_name + real Compose-emitted depends_on label format.
	dependentContainer := mockUpdate.CreateMockContainerWithConfig(
		"dependent",
		"/dependent",
		"app:latest",
		true,
		false,
		time.Now(),
		&dockerContainer.Config{
			Image: "app:latest",
			Labels: map[string]string{
				"com.docker.compose.project":          "download-torrent",
				"com.docker.compose.service":          "dependent",
				"com.docker.compose.container-number": "1",
				"com.docker.compose.depends_on":       "base:service_started:false",
			},
			ExposedPorts: dockerNetwork.PortSet{},
		},
	)

	return &mockUpdate.TestData{
		Staleness:  map[string]bool{baseContainer.Name(): true, dependentContainer.Name(): false},
		Containers: []*container.Container{baseContainer, dependentContainer},
	}
}

// getHyphenatedProjectWithContainerNameTestData creates test data for a Compose
// project name containing multiple hyphens, using explicit container_name on all
// services and real Compose-emitted depends_on label strings.
func getHyphenatedProjectWithContainerNameTestData() *mockUpdate.TestData {
	// Uses a project name with multiple hyphens and explicit container_name values
	// to exercise the restart marking logic with realistic Compose label output.
	base := mockUpdate.CreateMockContainerWithConfig(
		"base",
		"/base",
		"redis:alpine",
		true,
		false,
		time.Now().AddDate(0, 0, -1),
		&dockerContainer.Config{
			Image: "redis:alpine",
			// Set container-number so ResolveContainerIdentifier returns the
			// replica form while compose depends_on links remain non-replica.
			Labels: map[string]string{
				"com.docker.compose.project":          "my-app-project",
				"com.docker.compose.service":          "base",
				"com.docker.compose.container-number": "1",
			},
			ExposedPorts: dockerNetwork.PortSet{},
		},
	)

	dependentSimple := mockUpdate.CreateMockContainerWithConfig(
		"dependent-simple",
		"/dependent-simple",
		"alpine:latest",
		true,
		false,
		time.Now(),
		&dockerContainer.Config{
			Image: "alpine:latest",
			Labels: map[string]string{
				"com.docker.compose.project":          "my-app-project",
				"com.docker.compose.service":          "dependent-simple",
				"com.docker.compose.container-number": "1",
				"com.docker.compose.depends_on":       "base:service_started:false",
			},
			ExposedPorts: dockerNetwork.PortSet{},
		},
	)

	dependentNetwork := mockUpdate.CreateMockContainerWithConfig(
		"dependent-network",
		"/dependent-network",
		"alpine:latest",
		true,
		false,
		time.Now(),
		&dockerContainer.Config{
			Image: "alpine:latest",
			Labels: map[string]string{
				"com.docker.compose.project":          "my-app-project",
				"com.docker.compose.service":          "dependent-network",
				"com.docker.compose.container-number": "1",
				"com.docker.compose.depends_on":       "base:service_started:false",
			},
			ExposedPorts: dockerNetwork.PortSet{},
		},
	)
	dependentNetwork.ContainerInfo().HostConfig.NetworkMode = "container:base"

	return &mockUpdate.TestData{
		Staleness: map[string]bool{
			base.Name():             true,
			dependentSimple.Name():  false,
			dependentNetwork.Name(): false,
		},
		Containers: []*container.Container{base, dependentSimple, dependentNetwork},
	}
}

func createDependencyChain(names []string) []*container.Container {
	containers := make([]*container.Container, len(names))
	for i := range names {
		name := names[i]

		suffix := name
		if len(name) > 10 {
			suffix = name[10:]
		}

		image := "image-" + suffix + ":latest"

		labels := make(map[string]string)
		if i < len(names)-1 {
			labels["com.centurylinklabs.watchtower.depends-on"] = names[i+1]
		}

		containers[i] = mockUpdate.CreateMockContainerWithConfig(
			name,
			"/"+name,
			image,
			true,
			false,
			time.Now().AddDate(0, 0, -1),
			&dockerContainer.Config{
				Labels:       labels,
				ExposedPorts: dockerNetwork.PortSet{},
			},
		)
	}

	return containers
}

func getComposeDependsOnWithNetworkModeTestData() *mockUpdate.TestData {
	staleContainer := mockUpdate.CreateMockContainerWithConfig(
		"download-stack-vpn-1",
		"/download-stack-vpn-1",
		"gluetun:latest",
		true,
		false,
		time.Now().AddDate(0, 0, -1),
		&dockerContainer.Config{
			Image: "gluetun:latest",
			Labels: map[string]string{
				"com.docker.compose.project": "download-stack",
				"com.docker.compose.service": "vpn",
			},
			ExposedPorts: dockerNetwork.PortSet{},
		},
	)

	dependentContainer := mockUpdate.CreateMockContainerWithConfig(
		"download-stack-web-1",
		"/download-stack-web-1",
		"nginx:latest",
		true,
		false,
		time.Now(),
		&dockerContainer.Config{
			Image: "nginx:latest",
			Labels: map[string]string{
				"com.docker.compose.project":    "download-stack",
				"com.docker.compose.service":    "web",
				"com.docker.compose.depends_on": "vpn:service_started:false",
			},
			ExposedPorts: dockerNetwork.PortSet{},
		},
	)

	dependentContainer.ContainerInfo().HostConfig.NetworkMode = "container:download-stack-vpn-1"

	return &mockUpdate.TestData{
		Staleness:  map[string]bool{staleContainer.Name(): true, dependentContainer.Name(): false},
		Containers: []*container.Container{staleContainer, dependentContainer},
	}
}

// expectSessionBatch expects an update session to open its notification batch
// once and to end it once before the test ends.
//
// A report sent after the batch ended fails the test, since the session ends
// its batch after it sends.
//
// Returns:
//   - *sessionBatch: The reports the session sends and its ends.
func expectSessionBatch(t *testing.T, notifier *mockUpdate.MockNotifier) *sessionBatch {
	t.Helper()

	batch := &sessionBatch{}

	t.Cleanup(func() { assert.Equal(t, 1, batch.ended, "the session ends its batch once") })

	notifier.EXPECT().StartSessionNotification().Return(
		func(report *state.Report) {
			assert.Zero(t, batch.ended, "the session sends before it ends its batch")

			batch.record(report)
		},
		func() { batch.ended++ },
	).Once()

	return batch
}

// gitWatchedTestContainer returns a container named app that is associated
// with a local Git repository whose tip is newer than the container's stamp,
// and a Git client that checks it without a network.
//
// Parameters:
//   - t: Test that owns the repository.
//   - extra: Labels added to the Git association labels.
//
// Returns:
//   - *container.Container: The container.
//   - *git.Client: Git client for the check.
//   - string: The repository's tip commit.
func gitWatchedTestContainer(t *testing.T, extra map[string]string) (*container.Container, *git.Client, string) {
	t.Helper()

	repoDir, branch, commit := initGitRepoWithCommit(t)

	containerLabels := map[string]string{
		labels.GitRepo: repoDir,
		labels.GitRef:  branch,
		// The running revision is older than the remote's tip.
		labels.GitLastCommit: "0123456789abcdef0123456789abcdef01234567",
	}
	maps.Copy(containerLabels, extra)

	app := skipTestContainer("app-id", "app", containerLabels)

	return app, git.New(newTestLogger(t), git.Options{Timeout: 5 * time.Second}), commit
}

// initGitRepoWithCommit creates a Git repository with one commit in a
// temporary directory, so a Git check can list its refs without a network.
//
// Parameters:
//   - t: Test that owns the directory.
//
// Returns:
//   - dir: The repository directory.
//   - branch: The branch the commit is on.
//   - commit: The commit's hash.
//
//nolint:nonamedreturns // Same-type returns need names.
func initGitRepoWithCommit(t *testing.T) (dir, branch, commit string) {
	t.Helper()

	dir = t.TempDir()
	repo, err := goGit.PlainInit(dir, false)
	require.NoError(t, err)

	worktree, err := repo.Worktree()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README"), []byte("hello\n"), 0o600))

	_, err = worktree.Add("README")
	require.NoError(t, err)

	hash, err := worktree.Commit("init", &goGit.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@example.com", When: time.Now()},
	})
	require.NoError(t, err)

	head, err := repo.Head()
	require.NoError(t, err)

	return dir, head.Name().Short(), hash.String()
}
