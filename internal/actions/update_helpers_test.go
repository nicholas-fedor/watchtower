package actions_test

import (
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/go-connections/nat"

	"github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

func getCommonTestData() *mocks.TestData {
	return &mocks.TestData{
		NameOfContainerToKeep: "",
		Containers: []types.Container{
			mocks.CreateMockContainer(
				"test-container-01",
				"test-container-01",
				"fake-image:latest",
				time.Now().AddDate(0, 0, -1)),
			mocks.CreateMockContainer(
				"test-container-02",
				"test-container-02",
				"fake-image:latest",
				time.Now()),
			mocks.CreateMockContainer(
				"test-container-03",
				"test-container-03",
				"fake-image:latest",
				time.Now()),
		},
	}
}

func getLinkedTestData(withImageInfo bool) *mocks.TestData {
	staleContainer := mocks.CreateMockContainer(
		"test-container-01",
		"/test-container-01",
		"fake-image1:latest",
		time.Now().AddDate(0, 0, -1))

	var imageInfo *image.InspectResponse
	if withImageInfo {
		imageInfo = mocks.CreateMockImageInfo("test-container-02")
	}

	linkingContainer := mocks.CreateMockContainerWithLinks(
		"test-container-02",
		"/test-container-02",
		"fake-image2:latest",
		time.Now(),
		[]string{staleContainer.Name()},
		imageInfo)

	return &mocks.TestData{
		Staleness: map[string]bool{linkingContainer.Name(): false},
		Containers: []types.Container{
			staleContainer,
			linkingContainer,
		},
	}
}

func getNetworkModeTestData() *mocks.TestData {
	staleContainer := mocks.CreateMockContainer(
		"network-dependency",
		"/network-dependency",
		"fake-image:latest",
		time.Now().AddDate(0, 0, -1))

	dependentContainer := mocks.CreateMockContainerWithConfig(
		"network-dependent",
		"/network-dependent",
		"fake-image2:latest",
		true,
		false,
		time.Now(),
		&container.Config{
			Image:        "fake-image2:latest",
			Labels:       make(map[string]string),
			ExposedPorts: map[nat.Port]struct{}{},
		})

	// Set network mode to container:network-dependency
	dependentContainer.ContainerInfo().HostConfig.NetworkMode = "container:network-dependency"

	return &mocks.TestData{
		Staleness:  map[string]bool{staleContainer.Name(): true, dependentContainer.Name(): false},
		Containers: []types.Container{staleContainer, dependentContainer},
	}
}

func getComposeTestData() *mocks.TestData {
	// Create a database container with service name "db" but container name "myproject_db_1"
	dbContainer := mocks.CreateMockContainerWithConfig(
		"myproject_db_1",
		"/myproject_db_1",
		"postgres:latest",
		true,
		false,
		time.Now().AddDate(0, 0, -1),
		&container.Config{
			Image: "postgres:latest",
			Labels: map[string]string{
				"com.docker.compose.service": "db",
			},
			ExposedPorts: map[nat.Port]struct{}{},
		})

	// Create a web container with service name "web" but container name "myproject_web_1"
	// that depends on "db"
	webContainer := mocks.CreateMockContainerWithConfig(
		"myproject_web_1",
		"/myproject_web_1",
		"web:latest",
		true,
		false,
		time.Now(),
		&container.Config{
			Image: "web:latest",
			Labels: map[string]string{
				"com.docker.compose.service":    "web",
				"com.docker.compose.depends_on": "db",
			},
			ExposedPorts: map[nat.Port]struct{}{},
		})

	return &mocks.TestData{
		Staleness:  map[string]bool{dbContainer.Name(): true, webContainer.Name(): false},
		Containers: []types.Container{dbContainer, webContainer},
	}
}

func getComposeMultiHopTestData() *mocks.TestData {
	// Create containers for a chain: cache -> db -> app
	// depends on db, db depends on cache
	cacheContainer := mocks.CreateMockContainerWithConfig(
		"myproject_cache_1",
		"/myproject_cache_1",
		"redis:latest",
		true,
		false,
		time.Now().AddDate(0, 0, -1),
		&container.Config{
			Image: "redis:latest",
			Labels: map[string]string{
				"com.docker.compose.service": "cache",
			},
			ExposedPorts: map[nat.Port]struct{}{},
		})

	dbContainer := mocks.CreateMockContainerWithConfig(
		"myproject_db_1",
		"/myproject_db_1",
		"postgres:latest",
		true,
		false,
		time.Now(),
		&container.Config{
			Image: "postgres:latest",
			Labels: map[string]string{
				"com.docker.compose.service":    "db",
				"com.docker.compose.depends_on": "cache",
			},
			ExposedPorts: map[nat.Port]struct{}{},
		})

	appContainer := mocks.CreateMockContainerWithConfig(
		"myproject_app_1",
		"/myproject_app_1",
		"app:latest",
		true,
		false,
		time.Now(),
		&container.Config{
			Image: "app:latest",
			Labels: map[string]string{
				"com.docker.compose.service":    "app",
				"com.docker.compose.depends_on": "db",
			},
			ExposedPorts: map[nat.Port]struct{}{},
		})

	return &mocks.TestData{
		Staleness: map[string]bool{
			cacheContainer.Name(): true,
			dbContainer.Name():    false,
			appContainer.Name():   false,
		},
		Containers: []types.Container{cacheContainer, dbContainer, appContainer},
	}
}

func createDependencyChain(names []string) []types.Container {
	containers := make([]types.Container, len(names))
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

		containers[i] = mocks.CreateMockContainerWithConfig(
			name,
			"/"+name,
			image,
			true,
			false,
			time.Now().AddDate(0, 0, -1),
			&container.Config{
				Labels:       labels,
				ExposedPorts: map[nat.Port]struct{}{},
			})
	}

	return containers
}
