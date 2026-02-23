package actions

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/sirupsen/logrus"

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
		gomega.Expect(client.TestData.RenameContainerCount.Load()).To(gomega.Equal(int32(0)))
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
		gomega.Expect(client.TestData.RenameContainerCount.Load()).To(gomega.Equal(int32(1)))
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
		gomega.Expect(client.TestData.StartContainerCount.Load()).To(gomega.Equal(int32(1)))
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
		gomega.Expect(client.TestData.RenameContainerCount.Load()).To(gomega.Equal(int32(0)))
		gomega.Expect(client.TestData.StartContainerCount.Load()).To(gomega.Equal(int32(0)))
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
		gomega.Expect(client.TestData.UpdateContainerCount.Load()).To(gomega.Equal(int32(1)))
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

// logCapture captures logrus output for testing purposes.
type logCapture struct {
	entries []logEntry
}

// logEntry represents a single captured log entry.
type logEntry struct {
	level   logrus.Level
	message string
	fields  logrus.Fields
}

// Levels returns logrus levels for capturing logs.
func (lc *logCapture) Levels() []logrus.Level {
	return logrus.AllLevels
}

// Fire captures the log entry.
func (lc *logCapture) Fire(entry *logrus.Entry) error {
	lc.entries = append(lc.entries, logEntry{
		level:   entry.Level,
		message: entry.Message,
		fields:  entry.Data,
	})

	return nil
}

// stopContainersTestCase represents a test case for stopContainersInReversedOrder cancellation.
type stopContainersTestCase struct {
	name                string
	numContainers       int
	cancelAtIndex       int    // Index at which to cancel (from end, -1 means no cancellation)
	expectedStopped     int    // Number of containers that should be stopped
	expectedSkipped     int    // Number of containers that should be skipped
	expectedLogMessages int    // Expected number of log messages for skipped containers
	description         string // Human-readable description
}

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
				gomega.Expect(client.TestData.UpdateContainerCount.Load()).To(gomega.Equal(int32(1)))
			})
		}
	})

	ginkgo.Describe("restartStaleContainer detached context survival", func() {
		ginkgo.It("cleanup operations complete when parent context is canceled during execution", func() {
			// Create a parent context that we will cancel while restartStaleContainer is running.
			parentCtx, parentCancel := context.WithCancel(context.Background())

			// Create a mock client with a Watchtower container.
			// Configure StartContainerError to trigger the cleanup path.
			// Add simulated latency to allow time for operations to complete.
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
					SimulatedLatency:    5 * time.Millisecond, // Allow time for operations
				},
				false,
				false,
			)

			params := types.UpdateParams{
				Timeout: 0, // No deadline on detached context
				RunOnce: false,
			}

			testContainer := client.TestData.Containers[0]

			// Run restartStaleContainer in a goroutine so we can cancel the parent context
			// while it's still executing.
			var (
				err     error
				renamed bool
				wg      sync.WaitGroup
			)

			wg.Go(func() {
				// Call restartStaleContainer with the parent context.
				// The test flow is:
				// 1. RenameContainer succeeds (uses parent context)
				// 2. StartContainer fails due to StartContainerError
				// 3. Cleanup runs using the detached context (should survive parent cancellation)
				_, renamed, err = restartStaleContainer(
					parentCtx,
					testContainer,
					client,
					params,
				)
			})

			// Wait for StartContainer to be called (which means RenameContainer has completed)
			// before canceling the parent context. This ensures we cancel at the right moment -
			// after rename succeeds but during/after start fails.
			for client.TestData.StartContainerCount.Load() == 0 {
				time.Sleep(1 * time.Millisecond)
			}

			// Cancel the parent context after StartContainer has been called.
			// The detached context should allow cleanup to proceed even though
			// the parent context is canceled.
			parentCancel()

			// Wait for the goroutine to complete.
			wg.Wait()

			// The operation should fail due to StartContainer error, but the
			// cleanup (StopAndRemoveContainer) should have been attempted
			// using the detached context, which survives parent cancellation.
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to start container"))
			gomega.Expect(renamed).To(gomega.BeTrue())

			// Verify that StopContainer was called during cleanup.
			// This demonstrates that the detached context allowed the cleanup
			// operation to proceed even though the parent context was canceled.
			gomega.Expect(client.TestData.StopContainerCount.Load()).To(gomega.BeNumerically(">=", int32(1)))
		})

		ginkgo.It("cleanup operations complete when parent context is already canceled", func() {
			// Create a parent context that is already canceled.
			parentCtx, parentCancel := context.WithCancel(context.Background())
			parentCancel() // Cancel immediately before calling restartStaleContainer

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

			// Call restartStaleContainer with an already-canceled parent context.
			// The RenameContainer operation should fail because the parent context is canceled.
			_, renamed, err := restartStaleContainer(
				parentCtx,
				testContainer,
				client,
				params,
			)

			// The operation should fail at RenameContainer due to parent context cancellation.
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to rename Watchtower container"))
			gomega.Expect(renamed).To(gomega.BeFalse())

			// RenameContainer should have been attempted but failed due to context cancellation.
			gomega.Expect(client.TestData.RenameContainerCount.Load()).To(gomega.Equal(int32(1)))
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
			gomega.Expect(client.TestData.StartContainerCount.Load()).To(gomega.Equal(int32(1)))
			gomega.Expect(client.TestData.UpdateContainerCount.Load()).To(gomega.Equal(int32(1)))
		})
	})
})

// Tests for stopContainersInReversedOrder cancellation handling.
// These tests verify that when context cancellation occurs during container stopping:
// 1. All remaining containers are logged with appropriate fields
// 2. All remaining containers are added to the failed map with wrapped errors
// 3. Edge cases (cancellation at start, middle, end) are handled correctly.
//
// Important: When context is canceled at index i, the function adds the current container
// at index i to the failed map, then adds containers from i-1 down to 0.
// All containers from i down to 0 are properly logged and tracked as failed.
var _ = ginkgo.Describe("stopContainersInReversedOrder", func() {
	ginkgo.When("context is canceled during iteration", func() {
		// Table-driven tests for various cancellation scenarios.
		// Note: When context is already canceled at the start of iteration (i = len-1),
		// all containers from i down to 0 are added to failed.
		testCases := []stopContainersTestCase{
			{
				name:                "cancellation_at_start_all_skipped",
				numContainers:       3,
				cancelAtIndex:       0, // Context already canceled - at i=2, containers 2,1,0 are skipped
				expectedStopped:     0,
				expectedSkipped:     3, // containers 2, 1, and 0 are added to failed
				expectedLogMessages: 3,
				description:         "When context is canceled at the start, all containers should be skipped",
			},
			{
				name:                "cancellation_in_middle_partial_skip",
				numContainers:       5,
				cancelAtIndex:       0, // Context already canceled - at i=4, containers 4,3,2,1,0 are skipped
				expectedStopped:     0,
				expectedSkipped:     5, // containers 4,3,2,1,0 are added to failed
				expectedLogMessages: 5,
				description:         "When context is canceled mid-iteration, all containers should be skipped",
			},
			{
				name:                "cancellation_at_end_no_skip",
				numContainers:       3,
				cancelAtIndex:       -1, // No cancellation
				expectedStopped:     3,
				expectedSkipped:     0,
				expectedLogMessages: 0,
				description:         "When no cancellation occurs, all containers should be stopped",
			},
			{
				name:                "single_container_canceled",
				numContainers:       1,
				cancelAtIndex:       0, // Context already canceled - at i=0, container 0 is skipped
				expectedStopped:     0,
				expectedSkipped:     1, // container 0 is added to failed
				expectedLogMessages: 1,
				description:         "Single container scenario with cancellation",
			},
			{
				name:                "single_container_not_canceled",
				numContainers:       1,
				cancelAtIndex:       -1, // No cancellation
				expectedStopped:     1,
				expectedSkipped:     0,
				expectedLogMessages: 0,
				description:         "Single container scenario without cancellation",
			},
		}

		for _, tc := range testCases {
			ginkgo.It(tc.name, func() {
				ginkgo.By(tc.description)

				// Create mock containers with ToRestart set to true.
				containers := make([]types.Container, tc.numContainers)
				for i := range tc.numContainers {
					containerID := fmt.Sprintf("container-%d", i)
					containerName := fmt.Sprintf("/container-%d", i)
					imageName := fmt.Sprintf("image-%d:latest", i)

					c := mockActions.CreateMockContainerWithConfig(
						containerID,
						containerName,
						imageName,
						true,
						false,
						time.Now(),
						&dockerContainer.Config{
							Labels:       map[string]string{},
							ExposedPorts: map[nat.Port]struct{}{},
						},
					)
					// Mark container for restart so it will be processed.
					c.SetStale(true)
					containers[i] = c
				}

				// Create mock client.
				client := mockActions.CreateMockClient(
					&mockActions.TestData{
						Containers: containers,
						Staleness:  make(map[string]bool),
					},
					false,
					false,
				)

				// Mark all containers as stale.
				for i := range tc.numContainers {
					client.TestData.Staleness[fmt.Sprintf("container-%d", i)] = true
				}

				// Set up log capture to verify log messages.
				logHook := &logCapture{entries: make([]logEntry, 0)}
				logrus.AddHook(logHook)

				defer logrus.StandardLogger().ReplaceHooks(make(map[logrus.Level][]logrus.Hook))

				// Create context - either canceled or not based on test case.
				ctx := context.Background()
				if tc.cancelAtIndex >= 0 {
					// Create an already-canceled context to simulate cancellation.
					canceledCtx, cancel := context.WithCancel(context.Background())
					cancel() // Cancel immediately

					ctx = canceledCtx
				}

				// Call stopContainersInReversedOrder.
				failed, stopped := stopContainersInReversedOrder(
					ctx,
					containers,
					client,
					types.UpdateParams{},
				)

				// Verify the number of stopped containers.
				gomega.Expect(stopped).
					To(gomega.HaveLen(tc.expectedStopped), "Expected %d stopped containers", tc.expectedStopped)

				// Verify the number of failed containers.
				gomega.Expect(failed).
					To(gomega.HaveLen(tc.expectedSkipped), "Expected %d failed containers", tc.expectedSkipped)

				// Verify log messages for skipped containers.
				skippedLogCount := 0

				for _, entry := range logHook.entries {
					if entry.message == "Skipped container stop due to context cancellation" {
						skippedLogCount++

						// Verify log fields contain expected keys.
						gomega.Expect(entry.fields).To(gomega.HaveKey("container"))
						gomega.Expect(entry.fields).To(gomega.HaveKey("image"))
						gomega.Expect(entry.fields).To(gomega.HaveKey("container_id"))
					}
				}

				gomega.Expect(skippedLogCount).
					To(gomega.Equal(tc.expectedLogMessages), "Expected %d log messages for skipped containers", tc.expectedLogMessages)
			})
		}
	})

	ginkgo.When("context is canceled mid-iteration", func() {
		ginkgo.It("should add remaining containers to failed map with wrapped error", func() {
			// Create 4 containers.
			// When context is already canceled at the start:
			// - At i=3, ctx.Err() != nil, so container 3 is added to failed
			// - Then containers 2,1,0 are also added to failed
			// - All 4 containers are properly tracked as failed
			containers := make([]types.Container, 4)

			for i := range 4 {
				containerID := fmt.Sprintf("container-%d", i)
				containerName := fmt.Sprintf("/container-%d", i)
				imageName := fmt.Sprintf("image-%d:latest", i)

				c := mockActions.CreateMockContainerWithConfig(
					containerID,
					containerName,
					imageName,
					true,
					false,
					time.Now(),
					&dockerContainer.Config{
						Labels:       map[string]string{},
						ExposedPorts: map[nat.Port]struct{}{},
					},
				)
				c.SetStale(true)
				containers[i] = c
			}

			client := mockActions.CreateMockClient(
				&mockActions.TestData{
					Containers: containers,
					Staleness: map[string]bool{
						"container-0": true,
						"container-1": true,
						"container-2": true,
						"container-3": true,
					},
				},
				false,
				false,
			)

			// Create a canceled context.
			canceledCtx, cancel := context.WithCancel(context.Background())
			cancel()

			// Call stopContainersInReversedOrder.
			failed, stopped := stopContainersInReversedOrder(
				canceledCtx,
				containers,
				client,
				types.UpdateParams{},
			)

			// All 4 containers should be in failed map (containers 0, 1, 2, 3).
			gomega.Expect(failed).To(gomega.HaveLen(4))
			gomega.Expect(stopped).To(gomega.BeEmpty())

			// Verify all containers 0, 1, 2, 3 are in failed map with wrapped error.
			for i := range 4 {
				containerID := types.ContainerID(fmt.Sprintf("container-%d", i))
				err, exists := failed[containerID]
				gomega.Expect(exists).To(gomega.BeTrue(), "Container %d should be in failed map", i)

				// Verify error message contains "stop skipped".
				gomega.Expect(err.Error()).To(gomega.ContainSubstring("stop skipped"))

				// Verify error wraps context.Canceled.
				gomega.Expect(errors.Is(err, context.Canceled)).To(gomega.BeTrue(),
					"Error should wrap context.Canceled")
			}
		})

		ginkgo.It("should log each skipped container with correct fields", func() {
			// Create containers.
			// When context is already canceled at the start:
			// - At i=2, ctx.Err() != nil, so container 2 is logged and added to failed
			// - Then containers 1,0 are also logged and added to failed
			// - All 3 containers are properly logged
			containers := make([]types.Container, 3)
			expectedNames := []string{"container-0", "container-1", "container-2"} // All 3 are logged

			for i := range 3 {
				c := mockActions.CreateMockContainerWithConfig(
					fmt.Sprintf("container-%d", i),
					fmt.Sprintf("/container-%d", i),
					fmt.Sprintf("image-%d:latest", i),
					true,
					false,
					time.Now(),
					&dockerContainer.Config{
						Labels:       map[string]string{},
						ExposedPorts: map[nat.Port]struct{}{},
					},
				)
				c.SetStale(true)
				containers[i] = c
			}

			client := mockActions.CreateMockClient(
				&mockActions.TestData{
					Containers: containers,
					Staleness: map[string]bool{
						"container-0": true,
						"container-1": true,
						"container-2": true,
					},
				},
				false,
				false,
			)

			// Set up log capture.
			logHook := &logCapture{entries: make([]logEntry, 0)}
			logrus.AddHook(logHook)

			defer logrus.StandardLogger().ReplaceHooks(make(map[logrus.Level][]logrus.Hook))

			// Create a canceled context.
			canceledCtx, cancel := context.WithCancel(context.Background())
			cancel()

			// Call stopContainersInReversedOrder.
			_, _ = stopContainersInReversedOrder(
				canceledCtx,
				containers,
				client,
				types.UpdateParams{},
			)

			// Verify log entries contain expected container details.
			loggedNames := make(map[string]bool)

			for _, entry := range logHook.entries {
				if entry.message == "Skipped container stop due to context cancellation" {
					if containerName, ok := entry.fields["container"]; ok {
						loggedNames[containerName.(string)] = true
					}

					// Verify all expected fields are present.
					gomega.Expect(entry.fields).To(gomega.HaveKey("container"))
					gomega.Expect(entry.fields).To(gomega.HaveKey("image"))
					gomega.Expect(entry.fields).To(gomega.HaveKey("container_id"))
				}
			}

			// Verify all containers were logged.
			for _, name := range expectedNames {
				gomega.Expect(loggedNames).To(gomega.HaveKey(name),
					"Container %s should have been logged", name)
			}

			// Verify we got the expected number of log messages.
			gomega.Expect(loggedNames).To(gomega.HaveLen(3))
		})
	})

	ginkgo.When("context is not canceled", func() {
		ginkgo.It("should process all containers without adding to failed map", func() {
			// Create containers.
			containers := make([]types.Container, 3)

			for i := range 3 {
				c := mockActions.CreateMockContainerWithConfig(
					fmt.Sprintf("container-%d", i),
					fmt.Sprintf("/container-%d", i),
					fmt.Sprintf("image-%d:latest", i),
					true,
					false,
					time.Now(),
					&dockerContainer.Config{
						Labels:       map[string]string{},
						ExposedPorts: map[nat.Port]struct{}{},
					},
				)
				c.SetStale(true)
				containers[i] = c
			}

			client := mockActions.CreateMockClient(
				&mockActions.TestData{
					Containers: containers,
					Staleness: map[string]bool{
						"container-0": true,
						"container-1": true,
						"container-2": true,
					},
				},
				false,
				false,
			)

			// Set up log capture.
			logHook := &logCapture{entries: make([]logEntry, 0)}
			logrus.AddHook(logHook)

			defer logrus.StandardLogger().ReplaceHooks(make(map[logrus.Level][]logrus.Hook))

			// Call with valid context.
			failed, stopped := stopContainersInReversedOrder(
				context.Background(),
				containers,
				client,
				types.UpdateParams{},
			)

			// All containers should be stopped, none failed.
			gomega.Expect(stopped).To(gomega.HaveLen(3))
			gomega.Expect(failed).To(gomega.BeEmpty())

			// Verify no "Skipped container stop" log messages.
			for _, entry := range logHook.entries {
				gomega.Expect(entry.message).
					NotTo(gomega.Equal("Skipped container stop due to context cancellation"))
			}
		})
	})

	ginkgo.When("containers are processed in reverse order", func() {
		ginkgo.It("should stop containers from last to first", func() {
			// Create containers.
			containers := make([]types.Container, 3)

			for i := range 3 {
				c := mockActions.CreateMockContainerWithConfig(
					fmt.Sprintf("container-%d", i),
					fmt.Sprintf("/container-%d", i),
					fmt.Sprintf("image-%d:latest", i),
					true,
					false,
					time.Now(),
					&dockerContainer.Config{
						Labels:       map[string]string{},
						ExposedPorts: map[nat.Port]struct{}{},
					},
				)
				c.SetStale(true)
				containers[i] = c
			}

			client := mockActions.CreateMockClient(
				&mockActions.TestData{
					Containers: containers,
					Staleness: map[string]bool{
						"container-0": true,
						"container-1": true,
						"container-2": true,
					},
					StopOrder: []string{},
				},
				false,
				false,
			)

			// Call with valid context.
			_, _ = stopContainersInReversedOrder(
				context.Background(),
				containers,
				client,
				types.UpdateParams{},
			)

			// Verify stop order is reverse (container-2, container-1, container-0).
			gomega.Expect(client.TestData.StopOrder).To(gomega.HaveLen(3))
			gomega.Expect(client.TestData.StopOrder[0]).To(gomega.Equal("container-2"))
			gomega.Expect(client.TestData.StopOrder[1]).To(gomega.Equal("container-1"))
			gomega.Expect(client.TestData.StopOrder[2]).To(gomega.Equal("container-0"))
		})
	})
})

// rollingRestartTestCase represents a test case for performRollingRestart cancellation.
type rollingRestartTestCase struct {
	name                string
	numContainers       int
	cancelAtIndex       int    // Index at which to cancel (-1 means no cancellation)
	expectedProcessed   int    // Number of containers that should be processed
	expectedSkipped     int    // Number of containers that should be skipped
	expectedLogMessages int    // Expected number of log messages for skipped containers
	description         string // Human-readable description
}

// Tests for performRollingRestart cancellation handling.
// These tests verify that when context cancellation occurs during rolling restart:
// 1. All remaining containers are logged with appropriate fields
// 2. All remaining containers are added to the failed map with wrapped errors
// 3. Error messages contain "restart skipped"
// 4. The returned error wraps context.Canceled
// 5. Edge cases (cancellation at start, middle, end) are handled correctly.
//
// Important: When context is canceled at index i, the function adds the current container
// at index i to the failed map, then adds containers from i+1 to the end.
// All containers from i to the end are properly logged and tracked as failed.
var _ = ginkgo.Describe("performRollingRestart", func() {
	ginkgo.When("context is canceled during iteration", func() {
		// Table-driven tests for various cancellation scenarios.
		// Note: When context is already canceled at the start (i = 0),
		// all containers from 0 to len-1 are added to failed.
		testCases := []rollingRestartTestCase{
			{
				name:                "cancellation_at_start_all_skipped",
				numContainers:       3,
				cancelAtIndex:       0, // Context already canceled - at i=0, containers 0,1,2 are skipped
				expectedProcessed:   0,
				expectedSkipped:     3, // containers 0, 1, and 2 are added to failed
				expectedLogMessages: 3,
				description:         "When context is canceled at the start, all containers should be skipped",
			},
			{
				name:                "cancellation_in_middle_partial_skip",
				numContainers:       5,
				cancelAtIndex:       0, // Context already canceled - at i=0, containers 0,1,2,3,4 are skipped
				expectedProcessed:   0,
				expectedSkipped:     5, // containers 0,1,2,3,4 are added to failed
				expectedLogMessages: 5,
				description:         "When context is canceled mid-iteration, all containers should be skipped",
			},
			{
				name:                "cancellation_at_end_no_skip",
				numContainers:       3,
				cancelAtIndex:       -1, // No cancellation
				expectedProcessed:   3,
				expectedSkipped:     0,
				expectedLogMessages: 0,
				description:         "When no cancellation occurs, all containers should be processed",
			},
			{
				name:                "single_container_canceled",
				numContainers:       1,
				cancelAtIndex:       0, // Context already canceled - at i=0, container 0 is skipped
				expectedProcessed:   0,
				expectedSkipped:     1, // container 0 is added to failed
				expectedLogMessages: 1,
				description:         "Single container scenario with cancellation",
			},
			{
				name:                "single_container_not_canceled",
				numContainers:       1,
				cancelAtIndex:       -1, // No cancellation
				expectedProcessed:   1,
				expectedSkipped:     0,
				expectedLogMessages: 0,
				description:         "Single container scenario without cancellation",
			},
		}

		for _, tc := range testCases {
			ginkgo.It(tc.name, func() {
				ginkgo.By(tc.description)

				// Create mock containers with ToRestart set to true.
				containers := make([]types.Container, tc.numContainers)
				for i := range tc.numContainers {
					containerID := fmt.Sprintf("container-%d", i)
					containerName := fmt.Sprintf("/container-%d", i)
					imageName := fmt.Sprintf("image-%d:latest", i)

					c := mockActions.CreateMockContainerWithConfig(
						containerID,
						containerName,
						imageName,
						true,
						false,
						time.Now(),
						&dockerContainer.Config{
							Labels:       map[string]string{},
							ExposedPorts: map[nat.Port]struct{}{},
						},
					)
					// Mark container for restart so it will be processed.
					c.SetStale(true)
					containers[i] = c
				}

				// Create mock client.
				client := mockActions.CreateMockClient(
					&mockActions.TestData{
						Containers: containers,
						Staleness:  make(map[string]bool),
					},
					false,
					false,
				)

				// Mark all containers as stale.
				for i := range tc.numContainers {
					client.TestData.Staleness[fmt.Sprintf("container-%d", i)] = true
				}

				// Set up log capture to verify log messages.
				logHook := &logCapture{entries: make([]logEntry, 0)}
				logrus.AddHook(logHook)

				defer logrus.StandardLogger().ReplaceHooks(make(map[logrus.Level][]logrus.Hook))

				// Create context - either canceled or not based on test case.
				ctx := context.Background()
				if tc.cancelAtIndex >= 0 {
					// Create an already-canceled context to simulate cancellation.
					canceledCtx, cancel := context.WithCancel(context.Background())
					cancel() // Cancel immediately

					ctx = canceledCtx
				}

				// Call performRollingRestart.
				var cleanupImageInfos []types.RemovedImageInfo

				failed, err := performRollingRestart(
					ctx,
					containers,
					client,
					types.UpdateParams{},
					&cleanupImageInfos,
					nil, // progress is not needed for this test
				)

				// Verify the number of failed containers.
				gomega.Expect(failed).
					To(gomega.HaveLen(tc.expectedSkipped), "Expected %d failed containers", tc.expectedSkipped)

				// Verify error is returned when context is canceled.
				if tc.cancelAtIndex >= 0 {
					gomega.Expect(err).To(gomega.HaveOccurred(), "Expected an error when context is canceled")
					gomega.Expect(errors.Is(err, context.Canceled)).To(gomega.BeTrue(),
						"Error should wrap context.Canceled")
					gomega.Expect(err.Error()).To(gomega.ContainSubstring("rolling restart canceled"),
						"Error message should contain 'rolling restart canceled'")
				} else {
					gomega.Expect(err).NotTo(gomega.HaveOccurred(), "Expected no error when context is not canceled")
				}

				// Verify log messages for skipped containers.
				skippedLogCount := 0

				for _, entry := range logHook.entries {
					if entry.message == "Skipped container restart due to context cancellation" {
						skippedLogCount++

						// Verify log fields contain expected keys.
						gomega.Expect(entry.fields).To(gomega.HaveKey("container"))
						gomega.Expect(entry.fields).To(gomega.HaveKey("image"))
						gomega.Expect(entry.fields).To(gomega.HaveKey("container_id"))
					}
				}

				gomega.Expect(skippedLogCount).
					To(gomega.Equal(tc.expectedLogMessages), "Expected %d log messages for skipped containers", tc.expectedLogMessages)
			})
		}
	})

	ginkgo.When("context is canceled mid-iteration", func() {
		ginkgo.It("should add remaining containers to failed map with wrapped error", func() {
			// Create 4 containers.
			// When context is already canceled at the start:
			// - At i=0, ctx.Done() is triggered, so container 0 is added to failed
			// - Then containers 1,2,3 are also added to failed
			// - All 4 containers are properly tracked as failed
			containers := make([]types.Container, 4)

			for i := range 4 {
				containerID := fmt.Sprintf("container-%d", i)
				containerName := fmt.Sprintf("/container-%d", i)
				imageName := fmt.Sprintf("image-%d:latest", i)

				c := mockActions.CreateMockContainerWithConfig(
					containerID,
					containerName,
					imageName,
					true,
					false,
					time.Now(),
					&dockerContainer.Config{
						Labels:       map[string]string{},
						ExposedPorts: map[nat.Port]struct{}{},
					},
				)
				c.SetStale(true)
				containers[i] = c
			}

			client := mockActions.CreateMockClient(
				&mockActions.TestData{
					Containers: containers,
					Staleness: map[string]bool{
						"container-0": true,
						"container-1": true,
						"container-2": true,
						"container-3": true,
					},
				},
				false,
				false,
			)

			// Create a canceled context.
			canceledCtx, cancel := context.WithCancel(context.Background())
			cancel()

			// Call performRollingRestart.
			var cleanupImageInfos []types.RemovedImageInfo

			failed, err := performRollingRestart(
				canceledCtx,
				containers,
				client,
				types.UpdateParams{},
				&cleanupImageInfos,
				nil,
			)

			// All 4 containers should be in failed map (containers 0, 1, 2, 3).
			gomega.Expect(failed).To(gomega.HaveLen(4))

			// Verify error is returned.
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(errors.Is(err, context.Canceled)).To(gomega.BeTrue(),
				"Error should wrap context.Canceled")
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("rolling restart canceled"))

			// Verify all containers 0, 1, 2, 3 are in failed map with wrapped error.
			for i := range 4 {
				containerID := types.ContainerID(fmt.Sprintf("container-%d", i))
				containerErr, exists := failed[containerID]
				gomega.Expect(exists).To(gomega.BeTrue(), "Container %d should be in failed map", i)

				// Verify error message contains "restart skipped".
				gomega.Expect(containerErr.Error()).To(gomega.ContainSubstring("restart skipped"))

				// Verify error wraps context.Canceled.
				gomega.Expect(errors.Is(containerErr, context.Canceled)).To(gomega.BeTrue(),
					"Error should wrap context.Canceled")
			}
		})

		ginkgo.It("should log each skipped container with correct fields", func() {
			// Create containers.
			// When context is already canceled at the start:
			// - At i=0, ctx.Done() is triggered, so container 0 is logged and added to failed
			// - Then containers 1,2 are also logged and added to failed
			// - All 3 containers are properly logged
			containers := make([]types.Container, 3)
			expectedNames := []string{"container-0", "container-1", "container-2"} // All 3 are logged

			for i := range 3 {
				c := mockActions.CreateMockContainerWithConfig(
					fmt.Sprintf("container-%d", i),
					fmt.Sprintf("/container-%d", i),
					fmt.Sprintf("image-%d:latest", i),
					true,
					false,
					time.Now(),
					&dockerContainer.Config{
						Labels:       map[string]string{},
						ExposedPorts: map[nat.Port]struct{}{},
					},
				)
				c.SetStale(true)
				containers[i] = c
			}

			client := mockActions.CreateMockClient(
				&mockActions.TestData{
					Containers: containers,
					Staleness: map[string]bool{
						"container-0": true,
						"container-1": true,
						"container-2": true,
					},
				},
				false,
				false,
			)

			// Set up log capture.
			logHook := &logCapture{entries: make([]logEntry, 0)}
			logrus.AddHook(logHook)

			defer logrus.StandardLogger().ReplaceHooks(make(map[logrus.Level][]logrus.Hook))

			// Create a canceled context.
			canceledCtx, cancel := context.WithCancel(context.Background())
			cancel()

			// Call performRollingRestart.
			var cleanupImageInfos []types.RemovedImageInfo

			_, _ = performRollingRestart(
				canceledCtx,
				containers,
				client,
				types.UpdateParams{},
				&cleanupImageInfos,
				nil,
			)

			// Verify log entries contain expected container details.
			loggedNames := make(map[string]bool)

			for _, entry := range logHook.entries {
				if entry.message == "Skipped container restart due to context cancellation" {
					if containerName, ok := entry.fields["container"]; ok {
						loggedNames[containerName.(string)] = true
					}

					// Verify all expected fields are present.
					gomega.Expect(entry.fields).To(gomega.HaveKey("container"))
					gomega.Expect(entry.fields).To(gomega.HaveKey("image"))
					gomega.Expect(entry.fields).To(gomega.HaveKey("container_id"))
				}
			}

			// Verify all containers were logged.
			for _, name := range expectedNames {
				gomega.Expect(loggedNames).To(gomega.HaveKey(name),
					"Container %s should have been logged", name)
			}

			// Verify we got the expected number of log messages.
			gomega.Expect(loggedNames).To(gomega.HaveLen(3))
		})
	})

	ginkgo.When("context is not canceled", func() {
		ginkgo.It("should process all containers without adding to failed map", func() {
			// Create containers.
			containers := make([]types.Container, 3)

			for i := range 3 {
				c := mockActions.CreateMockContainerWithConfig(
					fmt.Sprintf("container-%d", i),
					fmt.Sprintf("/container-%d", i),
					fmt.Sprintf("image-%d:latest", i),
					true,
					false,
					time.Now(),
					&dockerContainer.Config{
						Labels:       map[string]string{},
						ExposedPorts: map[nat.Port]struct{}{},
					},
				)
				c.SetStale(true)
				containers[i] = c
			}

			client := mockActions.CreateMockClient(
				&mockActions.TestData{
					Containers: containers,
					Staleness: map[string]bool{
						"container-0": true,
						"container-1": true,
						"container-2": true,
					},
				},
				false,
				false,
			)

			// Set up log capture.
			logHook := &logCapture{entries: make([]logEntry, 0)}
			logrus.AddHook(logHook)

			defer logrus.StandardLogger().ReplaceHooks(make(map[logrus.Level][]logrus.Hook))

			// Call with valid context.
			var cleanupImageInfos []types.RemovedImageInfo

			failed, err := performRollingRestart(
				context.Background(),
				containers,
				client,
				types.UpdateParams{},
				&cleanupImageInfos,
				nil,
			)

			// All containers should be processed, none failed due to cancellation.
			gomega.Expect(failed).To(gomega.BeEmpty())
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Verify no "Skipped container restart" log messages.
			for _, entry := range logHook.entries {
				gomega.Expect(entry.message).
					NotTo(gomega.Equal("Skipped container restart due to context cancellation"))
			}
		})
	})

	ginkgo.When("containers are processed in forward order", func() {
		ginkgo.It("should process containers from first to last", func() {
			// Create containers.
			containers := make([]types.Container, 3)

			for i := range 3 {
				c := mockActions.CreateMockContainerWithConfig(
					fmt.Sprintf("container-%d", i),
					fmt.Sprintf("/container-%d", i),
					fmt.Sprintf("image-%d:latest", i),
					true,
					false,
					time.Now(),
					&dockerContainer.Config{
						Labels:       map[string]string{},
						ExposedPorts: map[nat.Port]struct{}{},
					},
				)
				c.SetStale(true)
				containers[i] = c
			}

			client := mockActions.CreateMockClient(
				&mockActions.TestData{
					Containers: containers,
					Staleness: map[string]bool{
						"container-0": true,
						"container-1": true,
						"container-2": true,
					},
					StopOrder:  []string{},
					StartOrder: []string{},
				},
				false,
				false,
			)

			// Call with valid context.
			var cleanupImageInfos []types.RemovedImageInfo

			_, _ = performRollingRestart(
				context.Background(),
				containers,
				client,
				types.UpdateParams{},
				&cleanupImageInfos,
				nil,
			)

			// Verify start order is forward (container-0, container-1, container-2).
			gomega.Expect(client.TestData.StartOrder).To(gomega.HaveLen(3))
			gomega.Expect(client.TestData.StartOrder[0]).To(gomega.Equal("container-0"))
			gomega.Expect(client.TestData.StartOrder[1]).To(gomega.Equal("container-1"))
			gomega.Expect(client.TestData.StartOrder[2]).To(gomega.Equal("container-2"))
		})
	})

	ginkgo.When("verifying error wrapping", func() {
		ginkgo.It("should return error that wraps context.Canceled", func() {
			// Create a single container.
			containers := []types.Container{
				mockActions.CreateMockContainerWithConfig(
					"container-0",
					"/container-0",
					"image-0:latest",
					true,
					false,
					time.Now(),
					&dockerContainer.Config{
						Labels:       map[string]string{},
						ExposedPorts: map[nat.Port]struct{}{},
					},
				),
			}
			containers[0].SetStale(true)

			client := mockActions.CreateMockClient(
				&mockActions.TestData{
					Containers: containers,
					Staleness:  map[string]bool{"container-0": true},
				},
				false,
				false,
			)

			// Create a canceled context.
			canceledCtx, cancel := context.WithCancel(context.Background())
			cancel()

			// Call performRollingRestart.
			var cleanupImageInfos []types.RemovedImageInfo

			failed, err := performRollingRestart(
				canceledCtx,
				containers,
				client,
				types.UpdateParams{},
				&cleanupImageInfos,
				nil,
			)

			// Verify error is returned.
			gomega.Expect(err).To(gomega.HaveOccurred())

			// Verify error wraps context.Canceled using errors.Is.
			gomega.Expect(errors.Is(err, context.Canceled)).To(gomega.BeTrue(),
				"Error should wrap context.Canceled")

			// Verify error message format.
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("rolling restart canceled"))

			// Verify failed container error also wraps context.Canceled.
			gomega.Expect(failed).To(gomega.HaveLen(1))
			containerErr := failed[types.ContainerID("container-0")]
			gomega.Expect(containerErr).To(gomega.HaveOccurred())
			gomega.Expect(errors.Is(containerErr, context.Canceled)).To(gomega.BeTrue(),
				"Container error should wrap context.Canceled")
			gomega.Expect(containerErr.Error()).To(gomega.ContainSubstring("restart skipped"))
		})

		ginkgo.It("should return error that wraps context.DeadlineExceeded when deadline exceeded", func() {
			// Create a single container.
			containers := []types.Container{
				mockActions.CreateMockContainerWithConfig(
					"container-0",
					"/container-0",
					"image-0:latest",
					true,
					false,
					time.Now(),
					&dockerContainer.Config{
						Labels:       map[string]string{},
						ExposedPorts: map[nat.Port]struct{}{},
					},
				),
			}
			containers[0].SetStale(true)

			client := mockActions.CreateMockClient(
				&mockActions.TestData{
					Containers: containers,
					Staleness:  map[string]bool{"container-0": true},
				},
				false,
				false,
			)

			// Create a context with an already-expired deadline.
			deadlineCtx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Hour))
			defer cancel()

			// Call performRollingRestart.
			var cleanupImageInfos []types.RemovedImageInfo

			failed, err := performRollingRestart(
				deadlineCtx,
				containers,
				client,
				types.UpdateParams{},
				&cleanupImageInfos,
				nil,
			)

			// Verify error is returned.
			gomega.Expect(err).To(gomega.HaveOccurred())

			// Verify error wraps context.DeadlineExceeded using errors.Is.
			gomega.Expect(errors.Is(err, context.DeadlineExceeded)).To(gomega.BeTrue(),
				"Error should wrap context.DeadlineExceeded")

			// Verify error message format.
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("rolling restart canceled"))

			// Verify failed container error also wraps context.DeadlineExceeded.
			gomega.Expect(failed).To(gomega.HaveLen(1))
			containerErr := failed[types.ContainerID("container-0")]
			gomega.Expect(containerErr).To(gomega.HaveOccurred())
			gomega.Expect(errors.Is(containerErr, context.DeadlineExceeded)).To(gomega.BeTrue(),
				"Container error should wrap context.DeadlineExceeded")
			gomega.Expect(containerErr.Error()).To(gomega.ContainSubstring("restart skipped"))
		})
	})
})
