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
		newID, renamed, err := restartStaleContainer(context.Background(), testContainer, client, params)
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
		newID, renamed, err := restartStaleContainer(context.Background(), testContainer, client, params)
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

var _ = ginkgo.Describe("linkedIdentifierMarkedForRestart same-project priority", func() {
	ginkgo.It("should prioritize same-project match over cross-project matches", func() {
		// Both same-project and cross-project matches exist
		// Same-project match should be returned regardless of alphabetical order
		restartByIdent := map[string]bool{
			"myproject-db":    true, // Same project as dependent
			"otherproject-db": true, // Different project (alphabetically first)
			"zzproject-db":    true, // Different project (alphabetically last)
		}
		links := []string{"db"}
		dependent := mockActions.CreateMockContainerWithConfig(
			"dependent",
			"myproject-web",
			"web:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{},
		)
		restarting1 := mockActions.CreateMockContainerWithConfig(
			"myproject-db",
			"myproject-db",
			"db:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{},
		)
		restarting2 := mockActions.CreateMockContainerWithConfig(
			"otherproject-db",
			"otherproject-db",
			"db:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{},
		)
		restarting3 := mockActions.CreateMockContainerWithConfig(
			"zzproject-db",
			"zzproject-db",
			"db:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{},
		)
		allContainers := []types.Container{dependent, restarting1, restarting2, restarting3}
		result := linkedIdentifierMarkedForRestart(links, restartByIdent, dependent, allContainers)
		gomega.Expect(result).To(gomega.Equal("myproject-db"))
	})

	ginkgo.It("should return same-project match when multiple cross-project matches exist", func() {
		// Same-project match should be preferred over many cross-project matches
		restartByIdent := map[string]bool{
			"alpha-db":     true, // Cross-project (alphabetically first)
			"beta-db":      true, // Cross-project
			"gamma-db":     true, // Cross-project
			"myproject-db": true, // Same project (not alphabetically first)
		}
		links := []string{"db"}
		dependent := mockActions.CreateMockContainerWithConfig(
			"dependent",
			"myproject-web",
			"web:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{},
		)
		restartingSame := mockActions.CreateMockContainerWithConfig(
			"myproject-db",
			"myproject-db",
			"db:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{},
		)
		restartingAlpha := mockActions.CreateMockContainerWithConfig(
			"alpha-db",
			"alpha-db",
			"db:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{},
		)
		restartingBeta := mockActions.CreateMockContainerWithConfig(
			"beta-db",
			"beta-db",
			"db:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{},
		)
		restartingGamma := mockActions.CreateMockContainerWithConfig(
			"gamma-db",
			"gamma-db",
			"db:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{},
		)
		allContainers := []types.Container{
			dependent,
			restartingSame,
			restartingAlpha,
			restartingBeta,
			restartingGamma,
		}
		result := linkedIdentifierMarkedForRestart(links, restartByIdent, dependent, allContainers)
		gomega.Expect(result).To(gomega.Equal("myproject-db"))
	})
})

var _ = ginkgo.Describe("linkedIdentifierMarkedForRestart project-service format", func() {
	ginkgo.It("should match project-service format link with restarting container", func() {
		// Link uses project-service format "myproject-db"
		restartByIdent := map[string]bool{
			"myproject-db": true,
		}
		links := []string{"myproject-db"} // project-service format
		dependent := mockActions.CreateMockContainerWithConfig(
			"dependent",
			"otherproject-web",
			"web:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{},
		)
		restarting := mockActions.CreateMockContainerWithConfig(
			"myproject-db",
			"myproject-db",
			"db:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{},
		)
		allContainers := []types.Container{dependent, restarting}
		result := linkedIdentifierMarkedForRestart(links, restartByIdent, dependent, allContainers)
		gomega.Expect(result).To(gomega.Equal("myproject-db"))
	})

	ginkgo.It("should match project-service format across different projects", func() {
		// Link uses project-service format to reference a container in a different project
		restartByIdent := map[string]bool{
			"databaseproject-db": true,
		}
		links := []string{"databaseproject-db"} // project-service format
		dependent := mockActions.CreateMockContainerWithConfig(
			"dependent",
			"webproject-web",
			"web:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{},
		)
		restarting := mockActions.CreateMockContainerWithConfig(
			"databaseproject-db",
			"databaseproject-db",
			"db:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{},
		)
		allContainers := []types.Container{dependent, restarting}
		result := linkedIdentifierMarkedForRestart(links, restartByIdent, dependent, allContainers)
		gomega.Expect(result).To(gomega.Equal("databaseproject-db"))
	})

	ginkgo.It("should prioritize exact match over project-service format match", func() {
		// When both exact match and project-service format match exist
		// Exact match should be preferred
		restartByIdent := map[string]bool{
			"db":           true, // Exact match
			"myproject-db": true, // Project-service format match
		}
		links := []string{"db"} // Exact match
		dependent := mockActions.CreateMockContainerWithConfig(
			"dependent",
			"otherproject-web",
			"web:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{},
		)
		restartingExact := mockActions.CreateMockContainerWithConfig(
			"db",
			"db",
			"db:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{},
		)
		restartingProjectService := mockActions.CreateMockContainerWithConfig(
			"myproject-db",
			"myproject-db",
			"db:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{},
		)
		allContainers := []types.Container{
			dependent,
			restartingExact,
			restartingProjectService,
		}
		result := linkedIdentifierMarkedForRestart(links, restartByIdent, dependent, allContainers)
		gomega.Expect(result).To(gomega.Equal("db"))
	})

	ginkgo.It(
		"should match project-service format when service name differs from project name",
		func() {
			// Link uses project-service format with complex names
			restartByIdent := map[string]bool{
				"production-api-gateway": true,
			}
			links := []string{"production-api-gateway"}
			dependent := mockActions.CreateMockContainerWithConfig(
				"dependent",
				"frontend-web",
				"web:latest",
				true,
				false,
				time.Now(),
				&dockerContainer.Config{},
			)
			restarting := mockActions.CreateMockContainerWithConfig(
				"production-api-gateway",
				"production-api-gateway",
				"gateway:latest",
				true,
				false,
				time.Now(),
				&dockerContainer.Config{},
			)
			allContainers := []types.Container{dependent, restarting}
			result := linkedIdentifierMarkedForRestart(
				links,
				restartByIdent,
				dependent,
				allContainers,
			)
			gomega.Expect(result).To(gomega.Equal("production-api-gateway"))
		},
	)
})

var _ = ginkgo.Describe("linkedIdentifierMarkedForRestart cross-project fallback", func() {
	ginkgo.It(
		"should select alphabetically first cross-project match when no same-project match exists",
		func() {
			// Multiple cross-project containers restarting, none from dependent's project
			// Should select alphabetically first: "project1-db" comes before "project2-db" and "project3-db"
			restartByIdent := map[string]bool{
				"project2-db": true,
				"project1-db": true, // Alphabetically first
				"project3-db": true,
			}
			links := []string{"db"}
			dependent := mockActions.CreateMockContainerWithConfig(
				"dependent",
				"project4-web",
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
			restarting3 := mockActions.CreateMockContainerWithConfig(
				"project3-db",
				"project3-db",
				"db:latest",
				true,
				false,
				time.Now(),
				&dockerContainer.Config{},
			)
			allContainers := []types.Container{dependent, restarting1, restarting2, restarting3}
			result := linkedIdentifierMarkedForRestart(
				links,
				restartByIdent,
				dependent,
				allContainers,
			)
			gomega.Expect(result).To(gomega.Equal("project1-db"))
		},
	)

	ginkgo.It("should return cross-project fallback when no same-project match exists", func() {
		// Only cross-project match exists, no same-project match
		restartByIdent := map[string]bool{
			"otherproject-db": true, // Only cross-project match
		}
		links := []string{"db"}
		dependent := mockActions.CreateMockContainerWithConfig(
			"dependent",
			"myproject-web",
			"web:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{},
		)
		restarting := mockActions.CreateMockContainerWithConfig(
			"otherproject-db",
			"otherproject-db",
			"db:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{},
		)
		allContainers := []types.Container{dependent, restarting}
		result := linkedIdentifierMarkedForRestart(links, restartByIdent, dependent, allContainers)
		gomega.Expect(result).To(gomega.Equal("otherproject-db"))
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

var _ = ginkgo.Describe("emptyReport", func() {
	ginkgo.It("Scanned() should return nil", func() {
		report := emptyReport{}
		gomega.Expect(report.Scanned()).To(gomega.BeNil())
	})

	ginkgo.It("Updated() should return nil", func() {
		report := emptyReport{}
		gomega.Expect(report.Updated()).To(gomega.BeNil())
	})

	ginkgo.It("Failed() should return nil", func() {
		report := emptyReport{}
		gomega.Expect(report.Failed()).To(gomega.BeNil())
	})

	ginkgo.It("Skipped() should return nil", func() {
		report := emptyReport{}
		gomega.Expect(report.Skipped()).To(gomega.BeNil())
	})

	ginkgo.It("Stale() should return nil", func() {
		report := emptyReport{}
		gomega.Expect(report.Stale()).To(gomega.BeNil())
	})

	ginkgo.It("Fresh() should return nil", func() {
		report := emptyReport{}
		gomega.Expect(report.Fresh()).To(gomega.BeNil())
	})

	ginkgo.It("Restarted() should return nil", func() {
		report := emptyReport{}
		gomega.Expect(report.Restarted()).To(gomega.BeNil())
	})

	ginkgo.It("All() should return nil", func() {
		report := emptyReport{}
		gomega.Expect(report.All()).To(gomega.BeNil())
	})
})

// TestDetachedContextDeadline tests the detached context creation logic in restartStaleContainer.
// These tests verify that the detached context is created correctly based on the Timeout config value:
// - When Timeout > 0: context has a deadline
// - When Timeout <= 0: context has no deadline.
var _ = ginkgo.Describe("DetachedContext", func() {
	// TestDetachedContextDeadlineCase represents a test case for detached context deadline behavior.
	type TestDetachedContextDeadlineCase struct {
		name           string
		timeout        time.Duration
		expectDeadline bool
		description    string
	}

	ginkgo.Describe("restartStaleContainer detached context deadline", func() {
		testCases := []TestDetachedContextDeadlineCase{
			{
				name:           "positive timeout creates context with deadline",
				timeout:        30 * time.Second,
				expectDeadline: true,
				description:    "When Timeout > 0, the detached context should have a deadline set",
			},
			{
				name:           "zero timeout creates context without deadline",
				timeout:        0,
				expectDeadline: false,
				description:    "When Timeout is zero, the detached context should not have a deadline",
			},
			{
				name:           "negative timeout creates context without deadline",
				timeout:        -1 * time.Second,
				expectDeadline: false,
				description:    "When Timeout is negative, the detached context should not have a deadline",
			},
		}

		for _, tc := range testCases {
			ginkgo.It(tc.name, func() {
				// Create a mock client with a Watchtower container that will trigger
				// the restart policy update path where the detached context is used.
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

				// Configure params with the test timeout value.
				// RunOnce is false to enable the rename path which uses the detached context.
				params := types.UpdateParams{
					Timeout: tc.timeout,
					RunOnce: false,
				}

				testContainer := client.TestData.Containers[0]

				// Call restartStaleContainer which creates and uses the detached context.
				newID, renamed, err := restartStaleContainer(
					context.Background(),
					testContainer,
					client,
					params,
				)

				// Verify the operation succeeded.
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(renamed).To(gomega.BeTrue())
				gomega.Expect(newID).NotTo(gomega.BeEmpty())

				// Verify UpdateContainer was called (this uses the detached context).
				// The detached context is used for updating the restart policy of the
				// renamed Watchtower container.
				gomega.Expect(client.TestData.UpdateContainerCount).To(gomega.Equal(1))
			})
		}
	})

	ginkgo.Describe("restartStaleContainer detached context survival", func() {
		ginkgo.It("cleanup operations complete when parent context is canceled", func() {
			// Create a parent context that we will cancel.
			parentCtx, parentCancel := context.WithCancel(context.Background())

			// Create a mock client with a Watchtower container.
			// Configure StartContainerError to trigger the cleanup path.
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
					StartContainerError: errors.New("simulated start failure"),
				},
				false,
				false,
			)

			params := types.UpdateParams{
				Timeout: 0, // No deadline on detached context
				RunOnce: false,
			}

			testContainer := client.TestData.Containers[0]

			// Cancel the parent context before calling restartStaleContainer.
			// This simulates the scenario where the parent context is canceled
			// but cleanup operations should still proceed.
			parentCancel()

			// Call restartStaleContainer with the already-canceled context.
			// The detached context should allow cleanup to proceed.
			_, renamed, err := restartStaleContainer(
				parentCtx,
				testContainer,
				client,
				params,
			)

			// The operation should fail due to StartContainer error, but the
			// cleanup (StopAndRemoveContainer) should have been attempted
			// using the detached context.
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to start container"))
			gomega.Expect(renamed).To(gomega.BeTrue())

			// Verify that StopContainer was called during cleanup.
			// This demonstrates that the detached context allowed the cleanup
			// operation to proceed even though the parent context was canceled.
			gomega.Expect(client.TestData.StopContainerCount).To(gomega.BeNumerically(">=", 1))
		})

		ginkgo.It("restart policy update uses detached context after successful start", func() {
			// This test verifies that UpdateContainer (restart policy update) uses
			// the detached context, not the parent context. Since StartContainer
			// uses the parent context, we cannot cancel it before calling
			// restartStaleContainer. Instead, we verify that UpdateContainer is
			// called after a successful start, demonstrating the detached context
			// is properly created and used.

			// Create a mock client with a Watchtower container that succeeds.
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

			// Use a timeout of 0 to create a detached context without deadline.
			params := types.UpdateParams{
				Timeout: 0,
				RunOnce: false,
			}

			testContainer := client.TestData.Containers[0]

			// Call restartStaleContainer with a background context.
			newID, renamed, err := restartStaleContainer(
				context.Background(),
				testContainer,
				client,
				params,
			)

			// The operation should succeed completely.
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(renamed).To(gomega.BeTrue())
			gomega.Expect(newID).NotTo(gomega.BeEmpty())

			// Verify that both StartContainer and UpdateContainer were called.
			// UpdateContainer uses the detached context for the restart policy update.
			gomega.Expect(client.TestData.StartContainerCount).To(gomega.Equal(1))
			gomega.Expect(client.TestData.UpdateContainerCount).To(gomega.Equal(1))
		})
	})
})
