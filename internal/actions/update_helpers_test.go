package actions_test

import (
	"time"

	"github.com/docker/go-connections/nat"

	dockerContainer "github.com/docker/docker/api/types/container"
	dockerImage "github.com/docker/docker/api/types/image"

	mockActions "github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

func getCommonTestData() *mockActions.TestData {
	return &mockActions.TestData{
		NameOfContainerToKeep: "",
		Containers: []types.Container{
			mockActions.CreateMockContainer(
				"test-container-01",
				"test-container-01",
				"fake-image:latest",
				time.Now().AddDate(0, 0, -1)),
			mockActions.CreateMockContainer(
				"test-container-02",
				"test-container-02",
				"fake-image:latest",
				time.Now()),
			mockActions.CreateMockContainer(
				"test-container-03",
				"test-container-03",
				"fake-image:latest",
				time.Now()),
		},
	}
}

func getLinkedTestData(withImageInfo bool) *mockActions.TestData {
	staleContainer := mockActions.CreateMockContainer(
		"test-container-01",
		"/test-container-01",
		"fake-image1:latest",
		time.Now().AddDate(0, 0, -1))

	var imageInfo *dockerImage.InspectResponse
	if withImageInfo {
		imageInfo = mockActions.CreateMockImageInfo("test-container-02")
	}

	linkingContainer := mockActions.CreateMockContainerWithLinks(
		"test-container-02",
		"/test-container-02",
		"fake-image2:latest",
		time.Now(),
		[]string{staleContainer.Name()},
		imageInfo)

	return &mockActions.TestData{
		Staleness: map[string]bool{linkingContainer.Name(): false},
		Containers: []types.Container{
			staleContainer,
			linkingContainer,
		},
	}
}

func getNetworkModeTestData() *mockActions.TestData {
	staleContainer := mockActions.CreateMockContainer(
		"network-dependency",
		"/network-dependency",
		"fake-image:latest",
		time.Now().AddDate(0, 0, -1))

	dependentContainer := mockActions.CreateMockContainerWithConfig(
		"network-dependent",
		"/network-dependent",
		"fake-image2:latest",
		true,
		false,
		time.Now(),
		&dockerContainer.Config{
			Image:        "fake-image2:latest",
			Labels:       make(map[string]string),
			ExposedPorts: map[nat.Port]struct{}{},
		})

	// Set network mode to container:network-dependency
	dependentContainer.ContainerInfo().HostConfig.NetworkMode = "container:network-dependency"

	return &mockActions.TestData{
		Staleness:  map[string]bool{staleContainer.Name(): true, dependentContainer.Name(): false},
		Containers: []types.Container{staleContainer, dependentContainer},
	}
}

func getComposeTestData() *mockActions.TestData {
	// Create a database container with service name "db" but container name "myproject_db_1"
	dbContainer := mockActions.CreateMockContainerWithConfig(
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
			ExposedPorts: map[nat.Port]struct{}{},
		})

	// Create a web container with service name "web" but container name "myproject_web_1"
	// that depends on "db"
	webContainer := mockActions.CreateMockContainerWithConfig(
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
			ExposedPorts: map[nat.Port]struct{}{},
		})

	return &mockActions.TestData{
		Staleness:  map[string]bool{dbContainer.Name(): true, webContainer.Name(): false},
		Containers: []types.Container{dbContainer, webContainer},
	}
}

func getComposeProjectPrefixedTestData() *mockActions.TestData {
	// Create a database container with project and service labels
	dbContainer := mockActions.CreateMockContainerWithConfig(
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
			ExposedPorts: map[nat.Port]struct{}{},
		})

	// Create a web container with project and service labels that depends on "database"
	webContainer := mockActions.CreateMockContainerWithConfig(
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
			ExposedPorts: map[nat.Port]struct{}{},
		})

	return &mockActions.TestData{
		Staleness:  map[string]bool{dbContainer.Name(): true, webContainer.Name(): false},
		Containers: []types.Container{dbContainer, webContainer},
	}
}

func getComposeMultiHopTestData() *mockActions.TestData {
	// Create containers for a chain: cache -> db -> app
	// depends on db, db depends on cache
	cacheContainer := mockActions.CreateMockContainerWithConfig(
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
			ExposedPorts: map[nat.Port]struct{}{},
		})

	dbContainer := mockActions.CreateMockContainerWithConfig(
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
			ExposedPorts: map[nat.Port]struct{}{},
		})

	appContainer := mockActions.CreateMockContainerWithConfig(
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
			ExposedPorts: map[nat.Port]struct{}{},
		})

	return &mockActions.TestData{
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

		containers[i] = mockActions.CreateMockContainerWithConfig(
			name,
			"/"+name,
			image,
			true,
			false,
			time.Now().AddDate(0, 0, -1),
			&dockerContainer.Config{
				Labels:       labels,
				ExposedPorts: map[nat.Port]struct{}{},
			})
	}

	return containers
}
