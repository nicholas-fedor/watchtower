package actions

import (
	"context"
	"errors"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	dockerContainer "github.com/docker/docker/api/types/container"

	mockActions "github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/filters"
	"github.com/nicholas-fedor/watchtower/pkg/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	mockTypes "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

const (
	currentWatchtowerID = "current-watchtower-id"
	otherWatchtowerID   = "other-watchtower-id"
)

var _ = ginkgo.Describe("restartStaleContainer", func() {
	ginkgo.It("should not rename Watchtower container in run-once mode", func() {
		client := mockActions.CreateMockClient(
			&mockActions.TestData{
				Containers: []types.Container{
					mockActions.CreateMockContainerWithConfig(
						"watchtower",
						"/watchtower",
						"watchtower:latest",
						true,
						false,
						time.Now(),
						&dockerContainer.Config{
							Labels: map[string]string{
								"com.centurylinklabs.watchtower": "true",
							},
						}),
				},
				Staleness: map[string]bool{
					"watchtower": true,
				},
			},
			false,
			false,
		)
		params := types.UpdateParams{
			RunOnce: true,
		}
		testContainer := client.TestData.Containers[0]
		newID, renamed, err := restartStaleContainer(testContainer, client, params)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(renamed).To(gomega.BeFalse())
		gomega.Expect(client.TestData.RenameContainerCount).To(gomega.Equal(0))
		gomega.Expect(newID).NotTo(gomega.BeEmpty())
	})

	ginkgo.It("should rename Watchtower container when not in run-once mode", func() {
		client := mockActions.CreateMockClient(
			&mockActions.TestData{
				Containers: []types.Container{
					mockActions.CreateMockContainerWithConfig(
						"watchtower",
						"/watchtower",
						"watchtower:latest",
						true,
						false,
						time.Now(),
						&dockerContainer.Config{
							Labels: map[string]string{
								"com.centurylinklabs.watchtower": "true",
							},
						}),
				},
				Staleness: map[string]bool{
					"watchtower": true,
				},
			},
			false,
			false,
		)
		params := types.UpdateParams{
			RunOnce: false,
		}
		testContainer := client.TestData.Containers[0]
		newID, renamed, err := restartStaleContainer(testContainer, client, params)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(renamed).To(gomega.BeTrue())
		gomega.Expect(client.TestData.RenameContainerCount).To(gomega.Equal(1))
		gomega.Expect(newID).NotTo(gomega.BeEmpty())
	})
})

var _ = ginkgo.Describe("handleUpdateResult", func() {
	ginkgo.It("should return zero metric when error is not nil", func() {
		mockReport := mockTypes.NewMockReport(ginkgo.GinkgoT())
		err := errors.New("test error")
		result := handleUpdateResult(mockReport, err, nil)
		gomega.Expect(result).To(gomega.Equal(&metrics.Metric{Scanned: 0, Updated: 0, Failed: 0}))
	})

	ginkgo.It("should return zero metric when result is nil", func() {
		var err error
		result := handleUpdateResult(nil, err, nil)
		gomega.Expect(result).To(gomega.Equal(&metrics.Metric{Scanned: 0, Updated: 0, Failed: 0}))
	})

	ginkgo.It("should return nil when result is not nil and error is nil", func() {
		mockReport := mockTypes.NewMockReport(ginkgo.GinkgoT())
		var err error
		result := handleUpdateResult(mockReport, err, nil)
		gomega.Expect(result).To(gomega.BeNil())
	})

	ginkgo.It("should send notification when error occurs and notifier is provided", func() {
		// Create a mock notifier that tracks if SendNotification was called
		mockNotifier := mockTypes.NewMockNotifier(ginkgo.GinkgoT())
		mockNotifier.EXPECT().SendNotification(emptyReport{}).Times(1)

		// Call handleUpdateResult with an error and the mock notifier
		mockReport := mockTypes.NewMockReport(ginkgo.GinkgoT())
		err := errors.New("dependency resolution error")
		result := handleUpdateResult(mockReport, err, mockNotifier)

		// Verify we got the expected metric
		gomega.Expect(result).To(gomega.Equal(&metrics.Metric{Scanned: 0, Updated: 0, Failed: 0}))
	})

	ginkgo.It("should not send notification when error occurs and notifier is nil", func() {
		// Call handleUpdateResult with an error and nil notifier
		mockReport := mockTypes.NewMockReport(ginkgo.GinkgoT())
		err := errors.New("dependency resolution error")
		result := handleUpdateResult(mockReport, err, nil)

		// Verify we got the expected metric
		gomega.Expect(result).To(gomega.Equal(&metrics.Metric{Scanned: 0, Updated: 0, Failed: 0}))
	})

	ginkgo.It("should not send notification when there is no error", func() {
		// Create a mock notifier with no expectations (will fail if any method is called)
		mockNotifier := mockTypes.NewMockNotifier(ginkgo.GinkgoT())

		// Call handleUpdateResult without an error
		mockReport := mockTypes.NewMockReport(ginkgo.GinkgoT())
		var err error
		result := handleUpdateResult(mockReport, err, mockNotifier)

		// Verify we got the expected result
		gomega.Expect(result).To(gomega.BeNil())
	})
})

var _ = ginkgo.Describe("executeUpdate", func() {
	ginkgo.It("should execute update successfully", func() {
		client := mockActions.CreateMockClient(
			&mockActions.TestData{
				Containers: []types.Container{
					mockActions.CreateMockContainerWithConfig(
						"test-container",
						"/test-container",
						"test:latest",
						true,
						false,
						time.Now(),
						&dockerContainer.Config{},
					),
				},
				Staleness: map[string]bool{
					"test-container": false,
				},
			},
			false,
			false,
		)
		config := types.UpdateParams{
			Filter: filters.NoFilter,
		}
		report, cleanupInfos, err := executeUpdate(
			context.Background(),
			client,
			config,
		)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(report).NotTo(gomega.BeNil())
		gomega.Expect(cleanupInfos).NotTo(gomega.BeNil())
	})

	ginkgo.It("should not return error when no containers to update", func() {
		client := mockActions.CreateMockClient(
			&mockActions.TestData{},
			false,
			false,
		)
		config := types.UpdateParams{
			Filter: filters.NoFilter,
		}
		report, cleanupInfos, err := executeUpdate(
			context.Background(),
			client,
			config,
		)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(report).NotTo(gomega.BeNil())
		gomega.Expect(cleanupInfos).NotTo(gomega.BeNil())
	})

	ginkgo.It("should execute update logic for stale containers", func() {
		client := mockActions.CreateMockClient(
			&mockActions.TestData{
				Containers: []types.Container{
					mockActions.CreateMockContainerWithConfig(
						"test-container",
						"/test-container",
						"test:latest",
						true,
						false,
						time.Now(),
						&dockerContainer.Config{},
					),
				},
				Staleness: map[string]bool{
					"test-container": true,
				},
			},
			false,
			false,
		)
		config := types.UpdateParams{
			Filter: filters.NoFilter,
		}
		report, cleanupInfos, err := executeUpdate(
			context.Background(),
			client,
			config,
		)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(report).NotTo(gomega.BeNil())
		gomega.Expect(cleanupInfos).NotTo(gomega.BeNil())
		gomega.Expect(client.TestData.StartContainerCount).To(gomega.Equal(1))
	})

	ginkgo.It("should propagate RunOnce mode and skip Watchtower self-update", func() {
		client := mockActions.CreateMockClient(
			&mockActions.TestData{
				Containers: []types.Container{
					mockActions.CreateMockContainerWithConfig(
						"watchtower",
						"/watchtower",
						"watchtower:latest",
						true,
						false,
						time.Now(),
						&dockerContainer.Config{
							Labels: map[string]string{
								"com.centurylinklabs.watchtower": "true",
							},
						}),
				},
				Staleness: map[string]bool{
					"watchtower": true,
				},
			},
			false,
			false,
		)
		config := types.UpdateParams{
			Filter:  filters.NoFilter,
			RunOnce: true,
		}
		report, cleanupInfos, err := executeUpdate(
			context.Background(),
			client,
			config,
		)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(report).NotTo(gomega.BeNil())
		gomega.Expect(cleanupInfos).NotTo(gomega.BeNil())
		gomega.Expect(client.TestData.RenameContainerCount).To(gomega.Equal(0))
		gomega.Expect(client.TestData.StartContainerCount).To(gomega.Equal(0))
	})

	ginkgo.It("should call UpdateContainer for Watchtower restart policy changes", func() {
		client := mockActions.CreateMockClient(
			&mockActions.TestData{
				Containers: []types.Container{
					mockActions.CreateMockContainerWithConfig(
						"watchtower",
						"/watchtower",
						"watchtower:latest",
						true,
						false,
						time.Now(),
						&dockerContainer.Config{
							Labels: map[string]string{
								"com.centurylinklabs.watchtower": "true",
							},
						}),
				},
				Staleness: map[string]bool{
					"watchtower": true,
				},
			},
			false,
			false,
		)
		config := types.UpdateParams{
			Filter: filters.NoFilter,
		}
		report, cleanupInfos, err := executeUpdate(
			context.Background(),
			client,
			config,
		)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(report).NotTo(gomega.BeNil())
		gomega.Expect(cleanupInfos).NotTo(gomega.BeNil())
		gomega.Expect(client.TestData.UpdateContainerCount).To(gomega.Equal(1))
	})
})

var _ = ginkgo.Describe("shouldUpdateContainer", func() {
	ginkgo.It("should allow self-update of current Watchtower container", func() {
		currentID := currentWatchtowerID
		container := mockActions.CreateMockContainerWithConfig(
			currentID,
			"watchtower-current",
			"watchtower:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels: map[string]string{
					"com.centurylinklabs.watchtower": "true",
				},
			},
		)
		params := types.UpdateParams{
			CurrentContainerID: types.ContainerID(currentID),
		}
		result := shouldUpdateContainer(true, container, params)
		gomega.Expect(result).To(gomega.BeTrue())
	})

	ginkgo.It("should skip other Watchtower containers from self-updates", func() {
		currentID := currentWatchtowerID
		otherID := otherWatchtowerID
		container := mockActions.CreateMockContainerWithConfig(
			otherID,
			"watchtower-other",
			"watchtower:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels: map[string]string{
					"com.centurylinklabs.watchtower": "true",
				},
			},
		)
		params := types.UpdateParams{
			CurrentContainerID: types.ContainerID(currentID),
		}
		result := shouldUpdateContainer(true, container, params)
		gomega.Expect(result).To(gomega.BeFalse())
	})

	ginkgo.It("should not affect non-Watchtower containers", func() {
		currentID := currentWatchtowerID
		container := mockActions.CreateMockContainerWithConfig(
			"non-watchtower-id",
			"nginx",
			"nginx:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels: map[string]string{},
			},
		)
		params := types.UpdateParams{
			CurrentContainerID: types.ContainerID(currentID),
		}
		result := shouldUpdateContainer(true, container, params)
		gomega.Expect(result).To(gomega.BeTrue())
	})

	ginkgo.It("should allow self-update of scoped Watchtower container", func() {
		currentID := currentWatchtowerID
		container := mockActions.CreateMockContainerWithConfig(
			currentID,
			"watchtower-current",
			"watchtower:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels: map[string]string{
					"com.centurylinklabs.watchtower":       "true",
					"com.centurylinklabs.watchtower.scope": "prod",
				},
			},
		)
		params := types.UpdateParams{
			CurrentContainerID: types.ContainerID(currentID),
		}
		result := shouldUpdateContainer(true, container, params)
		gomega.Expect(result).To(gomega.BeTrue())
	})

	ginkgo.It(
		"should skip other scoped Watchtower containers with same scope from self-updates",
		func() {
			currentID := currentWatchtowerID
			otherID := otherWatchtowerID
			container := mockActions.CreateMockContainerWithConfig(
				otherID,
				"watchtower-other",
				"watchtower:latest",
				true,
				false,
				time.Now(),
				&dockerContainer.Config{
					Labels: map[string]string{
						"com.centurylinklabs.watchtower":       "true",
						"com.centurylinklabs.watchtower.scope": "prod",
					},
				},
			)
			params := types.UpdateParams{
				CurrentContainerID: types.ContainerID(currentID),
			}
			result := shouldUpdateContainer(true, container, params)
			gomega.Expect(result).To(gomega.BeFalse())
		},
	)

	ginkgo.It(
		"should skip other scoped Watchtower containers with different scopes from self-updates",
		func() {
			currentID := currentWatchtowerID
			otherID := otherWatchtowerID
			container := mockActions.CreateMockContainerWithConfig(
				otherID,
				"watchtower-other",
				"watchtower:latest",
				true,
				false,
				time.Now(),
				&dockerContainer.Config{
					Labels: map[string]string{
						"com.centurylinklabs.watchtower":       "true",
						"com.centurylinklabs.watchtower.scope": "dev",
					},
				},
			)
			params := types.UpdateParams{
				CurrentContainerID: types.ContainerID(currentID),
			}
			result := shouldUpdateContainer(true, container, params)
			gomega.Expect(result).To(gomega.BeFalse())
		},
	)

	ginkgo.It("should skip unscoped Watchtower containers from scoped self-updates", func() {
		currentID := currentWatchtowerID
		otherID := otherWatchtowerID
		container := mockActions.CreateMockContainerWithConfig(
			otherID,
			"watchtower-other",
			"watchtower:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels: map[string]string{
					"com.centurylinklabs.watchtower": "true",
				},
			},
		)
		params := types.UpdateParams{
			CurrentContainerID: types.ContainerID(currentID),
		}
		result := shouldUpdateContainer(true, container, params)
		gomega.Expect(result).To(gomega.BeFalse())
	})

	ginkgo.It("should skip scoped Watchtower containers from unscoped self-updates", func() {
		currentID := currentWatchtowerID
		otherID := otherWatchtowerID
		container := mockActions.CreateMockContainerWithConfig(
			otherID,
			"watchtower-other",
			"watchtower:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels: map[string]string{
					"com.centurylinklabs.watchtower":       "true",
					"com.centurylinklabs.watchtower.scope": "prod",
				},
			},
		)
		params := types.UpdateParams{
			CurrentContainerID: types.ContainerID(currentID),
		}
		result := shouldUpdateContainer(true, container, params)
		gomega.Expect(result).To(gomega.BeFalse())
	})
})

var _ = ginkgo.Describe("linkedIdentifierMarkedForRestart", func() {
	ginkgo.It("should return the identifier for single project match", func() {
		restartByIdent := map[string]bool{
			"project1-db": true,
			"project2-db": true,
		}
		links := []string{"db"}
		dependent := mockActions.CreateMockContainerWithConfig(
			"dependent",
			"project1-web",
			"web:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{},
		)
		restarting1 := mockActions.CreateMockContainerWithConfig(
			"project1-db",
			"project1-db",
			"db:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{},
		)
		restarting2 := mockActions.CreateMockContainerWithConfig(
			"project2-db",
			"project2-db",
			"db:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{},
		)
		allContainers := []types.Container{dependent, restarting1, restarting2}
		result := linkedIdentifierMarkedForRestart(links, restartByIdent, dependent, allContainers)
		gomega.Expect(result).To(gomega.Equal("project1-db"))
	})

	ginkgo.It("should return the identifier for single partial match", func() {
		restartByIdent := map[string]bool{
			"project1-db": true,
		}
		links := []string{"db"}
		dependent := mockActions.CreateMockContainerWithConfig(
			"dependent",
			"project1-web",
			"web:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{},
		)
		restarting1 := mockActions.CreateMockContainerWithConfig(
			"project1-db",
			"project1-db",
			"db:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{},
		)
		allContainers := []types.Container{dependent, restarting1}
		result := linkedIdentifierMarkedForRestart(links, restartByIdent, dependent, allContainers)
		gomega.Expect(result).To(gomega.Equal("project1-db"))
	})

	ginkgo.It("should prioritize exact matches over partial matches", func() {
		restartByIdent := map[string]bool{
			"db":          true,
			"project1-db": true,
		}
		links := []string{"db"}
		dependent := mockActions.CreateMockContainerWithConfig(
			"dependent",
			"project1-web",
			"web:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{},
		)
		restarting1 := mockActions.CreateMockContainerWithConfig(
			"project1-db",
			"project1-db",
			"db:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{},
		)
		exact := mockActions.CreateMockContainerWithConfig(
			"db",
			"db",
			"db:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{},
		)
		allContainers := []types.Container{dependent, restarting1, exact}
		result := linkedIdentifierMarkedForRestart(links, restartByIdent, dependent, allContainers)
		gomega.Expect(result).To(gomega.Equal("db"))
	})
})

var _ = ginkgo.Describe("hasSelfDependency", func() {
	ginkgo.It("should return false when no depends-on label is present", func() {
		container := mockActions.CreateMockContainerWithConfig(
			"test-container",
			"/test-container",
			"test:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels:       map[string]string{},
				ExposedPorts: map[nat.Port]struct{}{},
			})
		result := hasSelfDependency(container)
		gomega.Expect(result).To(gomega.BeFalse())
	})

	ginkgo.It("should return false when depends-on label is empty", func() {
		container := mockActions.CreateMockContainerWithConfig(
			"test-container",
			"/test-container",
			"test:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels: map[string]string{
					"com.centurylinklabs.watchtower.depends-on": "",
				},
				ExposedPorts: map[nat.Port]struct{}{},
			})
		result := hasSelfDependency(container)
		gomega.Expect(result).To(gomega.BeFalse())
	})

	ginkgo.It("should return false when depends-on contains other containers", func() {
		container := mockActions.CreateMockContainerWithConfig(
			"test-container",
			"/test-container",
			"test:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels: map[string]string{
					"com.centurylinklabs.watchtower.depends-on": "other-container",
				},
				ExposedPorts: map[nat.Port]struct{}{},
			})
		result := hasSelfDependency(container)
		gomega.Expect(result).To(gomega.BeFalse())
	})

	ginkgo.It("should return true when depends-on contains self", func() {
		container := mockActions.CreateMockContainerWithConfig(
			"test-container",
			"/test-container",
			"test:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels: map[string]string{
					"com.centurylinklabs.watchtower.depends-on": "test-container",
				},
				ExposedPorts: map[nat.Port]struct{}{},
			})
		result := hasSelfDependency(container)
		gomega.Expect(result).To(gomega.BeTrue())
	})

	ginkgo.It(
		"should return true when depends-on contains self among multiple dependencies",
		func() {
			container := mockActions.CreateMockContainerWithConfig(
				"test-container",
				"/test-container",
				"test:latest",
				true,
				false,
				time.Now(),
				&dockerContainer.Config{
					Labels: map[string]string{
						"com.centurylinklabs.watchtower.depends-on": "other-container,test-container,another-container",
					},
					ExposedPorts: map[nat.Port]struct{}{},
				})
			result := hasSelfDependency(container)
			gomega.Expect(result).To(gomega.BeTrue())
		},
	)

	ginkgo.It("should handle spaces and trimming correctly", func() {
		container := mockActions.CreateMockContainerWithConfig(
			"test-container",
			"/test-container",
			"test:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels: map[string]string{
					"com.centurylinklabs.watchtower.depends-on": " other-container , test-container , another-container ",
				},
				ExposedPorts: map[nat.Port]struct{}{},
			})
		result := hasSelfDependency(container)
		gomega.Expect(result).To(gomega.BeTrue())
	})

	ginkgo.It("should handle leading slashes in container names", func() {
		container := mockActions.CreateMockContainerWithConfig(
			"test-container",
			"/test-container",
			"test:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels: map[string]string{
					"com.centurylinklabs.watchtower.depends-on": "/test-container",
				},
				ExposedPorts: map[nat.Port]struct{}{},
			})
		result := hasSelfDependency(container)
		gomega.Expect(result).To(gomega.BeTrue())
	})

	ginkgo.It("should return false when Config is nil", func() {
		container := mockActions.CreateMockContainerWithConfig(
			"test-container",
			"/test-container",
			"test:latest",
			true,
			false,
			time.Now(),
			nil) // Config is nil
		result := hasSelfDependency(container)
		gomega.Expect(result).To(gomega.BeFalse())
	})

	ginkgo.It("should return false when Labels is nil", func() {
		container := mockActions.CreateMockContainerWithConfig(
			"test-container",
			"/test-container",
			"test:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels:       nil, // Labels is nil
				ExposedPorts: map[nat.Port]struct{}{},
			})
		result := hasSelfDependency(container)
		gomega.Expect(result).To(gomega.BeFalse())
	})
})
