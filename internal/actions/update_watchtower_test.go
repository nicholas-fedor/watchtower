package actions_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/nicholas-fedor/watchtower/internal/actions"
	"github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/filters"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

var _ = ginkgo.Describe("Watchtower container handling", func() {
	ginkgo.When("updating a Watchtower container", func() {
		ginkgo.It("should rename and start a new container without cleanup", func() {
			client := mocks.CreateMockClient(
				&mocks.TestData{
					Containers: []types.Container{
						mocks.CreateMockContainerWithConfig(
							"watchtower",
							"/watchtower",
							"watchtower:latest",
							true,
							false,
							time.Now(),
							&container.Config{
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
			report, cleanupImageInfos, err := actions.Update(
				context.Background(),
				client,
				actions.UpdateConfig{
					Cleanup:          true,
					Filter:           filters.WatchtowerContainersFilter,
					CPUCopyMode:      "auto",
					PullFailureDelay: 10 * time.Millisecond,
				},
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(report.Updated()).To(gomega.HaveLen(1))
			gomega.Expect(cleanupImageInfos).
				To(gomega.BeEmpty(), "No cleanup for renamed Watchtower container")
			gomega.Expect(client.TestData.TriedToRemoveImageCount).
				To(gomega.Equal(0), "RemoveImageByID should not be called during Update")
			gomega.Expect(client.TestData.RenameContainerCount).
				To(gomega.Equal(1), "RenameContainer should be called once")
			gomega.Expect(client.TestData.UpdateContainerCount).
				To(gomega.Equal(1), "UpdateContainer should be called once for old Watchtower")
			gomega.Expect(client.TestData.StopContainerCount).
				To(gomega.Equal(1), "StopContainer should be called once for old Watchtower")
			gomega.Expect(client.TestData.IsContainerStaleCount).
				To(gomega.Equal(1), "IsContainerStale should be called once for Watchtower")
		})

		ginkgo.It("should skip rename with no-restart for Watchtower", func() {
			client := mocks.CreateMockClient(
				&mocks.TestData{
					Containers: []types.Container{
						mocks.CreateMockContainerWithConfig(
							"watchtower",
							"/watchtower",
							"watchtower:latest",
							true,
							false,
							time.Now(),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower":              "true",
									"com.centurylinklabs.watchtower.monitor-only": "true",
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
			config := actions.UpdateConfig{
				Cleanup:     true,
				NoRestart:   true,
				Filter:      filters.WatchtowerContainersFilter,
				CPUCopyMode: "auto",
			}
			report, cleanupImageInfos, err := actions.Update(context.Background(), client, config)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(report.Scanned()).
				To(gomega.HaveLen(1), "Container should be scanned but not updated")
			gomega.Expect(report.Updated()).
				To(gomega.BeEmpty(), "No containers should be updated with no-restart")
			gomega.Expect(cleanupImageInfos).
				To(gomega.BeEmpty(), "No images should be collected for cleanup")
			gomega.Expect(client.TestData.RenameContainerCount).
				To(gomega.Equal(0), "RenameContainer should not be called with no-restart")
		})

		ginkgo.It("should not rename Watchtower container in run-once mode", func() {
			client := mocks.CreateMockClient(
				&mocks.TestData{
					Containers: []types.Container{
						mocks.CreateMockContainerWithConfig(
							"watchtower",
							"/watchtower",
							"watchtower:latest",
							true,
							false,
							time.Now(),
							&container.Config{
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
			config := actions.UpdateConfig{
				Cleanup:     true,
				RunOnce:     true,
				Filter:      filters.WatchtowerContainersFilter,
				CPUCopyMode: "auto",
			}
			report, cleanupImageInfos, err := actions.Update(context.Background(), client, config)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(report.Scanned()).
				To(gomega.HaveLen(1), "Container should be scanned")
			gomega.Expect(report.Updated()).
				To(gomega.BeEmpty(), "Watchtower should not be updated in run-once mode")
			gomega.Expect(cleanupImageInfos).
				To(gomega.BeEmpty(), "No images should be collected for cleanup")
			gomega.Expect(client.TestData.RenameContainerCount).
				To(gomega.Equal(0), "RenameContainer should not be called in run-once mode")
			gomega.Expect(client.TestData.IsContainerStaleCount).
				To(gomega.Equal(1), "IsContainerStale should be called once to pull the image")
		})

		ginkgo.It("should not clean up unscoped instances when scope is specified", func() {
			client := mocks.CreateMockClient(
				&mocks.TestData{
					Containers: []types.Container{
						mocks.CreateMockContainerWithConfig(
							"watchtower-scoped",
							"/watchtower-scoped",
							"watchtower:latest",
							true,
							false,
							time.Now().Add(-time.Hour),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower":       "true",
									"com.centurylinklabs.watchtower.scope": "prod",
								},
							}),
						mocks.CreateMockContainerWithConfig(
							"watchtower-unscoped",
							"/watchtower-unscoped",
							"watchtower:old",
							true,
							false,
							time.Now(),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower": "true",
								},
							}),
					},
				},
				false,
				false,
			)
			var cleanupImageInfos []types.CleanedImageInfo
			cleanupOccurred, err := actions.CheckForMultipleWatchtowerInstances(
				client,
				true, // cleanup=true
				"prod",
				&cleanupImageInfos,
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cleanupOccurred).To(gomega.BeFalse())
			gomega.Expect(client.TestData.StopContainerCount).
				To(gomega.Equal(0), "StopContainer should not be called for unscoped container")
			gomega.Expect(cleanupImageInfos).
				To(gomega.BeEmpty(), "No cleanup should occur for unscoped container")
			gomega.Expect(client.TestData.TriedToRemoveImageCount).
				To(gomega.Equal(0), "RemoveImageByID should not be called for unscoped container")
		})

		ginkgo.It("should skip cleanup for shared image", func() {
			client := mocks.CreateMockClient(
				&mocks.TestData{
					Containers: []types.Container{
						mocks.CreateMockContainerWithConfig(
							"old",
							"/watchtower",
							"watchtower:latest",
							true,
							false,
							time.Now().Add(-time.Hour),
							&container.Config{
								Labels: map[string]string{"com.centurylinklabs.watchtower": "true"},
							}),
						mocks.CreateMockContainerWithConfig(
							"new",
							"/watchtower",
							"watchtower:latest",
							true,
							false,
							time.Now(),
							&container.Config{
								Labels: map[string]string{"com.centurylinklabs.watchtower": "true"},
							}),
					},
				},
				false,
				false,
			)
			var cleanupImageInfos []types.CleanedImageInfo
			cleanupOccurred, err := actions.CheckForMultipleWatchtowerInstances(
				client,
				true,
				"",
				&cleanupImageInfos,
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cleanupOccurred).To(gomega.BeTrue())
			gomega.Expect(cleanupImageInfos).
				To(gomega.BeEmpty(), "No image cleanup for shared image")
			gomega.Expect(client.TestData.TriedToRemoveImageCount).To(gomega.Equal(0))
		})
	})
})

func TestSafeguardDelay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := mocks.CreateMockClient(
			&mocks.TestData{
				Containers: []types.Container{
					mocks.CreateMockContainerWithConfig(
						"watchtower",
						"/watchtower",
						"watchtower:latest",
						true,
						false,
						time.Now(),
						&container.Config{
							Labels: map[string]string{
								"com.centurylinklabs.watchtower": "true",
							},
						}),
				},
				Staleness: map[string]bool{
					"watchtower": true, // Simulate stale Watchtower
				},
			},
			false,
			false,
		)

		// Mock IsContainerStale to return an error (simulating pull failure)
		client.TestData.IsContainerStaleError = errors.New("failed to pull image")

		report, cleanupImageInfos, err := actions.Update(
			context.Background(),
			client,
			actions.UpdateConfig{
				Cleanup:          true,
				Filter:           filters.WatchtowerContainersFilter,
				CPUCopyMode:      "auto",
				PullFailureDelay: 10 * time.Millisecond,
			},
		)

		synctest.Wait()

		if err != nil {
			t.Fatal(err)
		}

		if len(report.Updated()) != 0 {
			t.Fatal("Watchtower should not be updated on pull failure")
		}

		if len(cleanupImageInfos) != 0 {
			t.Fatal("No cleanup should occur on pull failure")
		}

		// Note: With synctest, the PullFailureDelay sleep is simulated.
		// The delay behavior is verified by the test completing without hanging.
	})
}
