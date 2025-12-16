package actions_test

import (
	"context"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/nicholas-fedor/watchtower/internal/actions"
	mockActions "github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/filters"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

var _ = ginkgo.Describe("the update action", func() {
	// Tests for image reference handling to cover isPinned functionality
	ginkgo.When("handling different image reference formats", func() {
		var client *mockActions.MockClient
		var config actions.UpdateConfig

		ginkgo.BeforeEach(func() {
			config = actions.UpdateConfig{
				Cleanup:     true,
				Filter:      filters.NoFilter,
				CPUCopyMode: "auto",
			}
		})

		ginkgo.It("should process tagged images and update if stale", func() {
			client = &mockActions.MockClient{
				TestData: &mockActions.TestData{
					IsContainerStaleCount: 0,
					Containers: []types.Container{
						mockActions.CreateMockContainer(
							"tagged-container",
							"/tagged-container",
							"image:1.0.0",
							time.Now()),
					},
					Staleness: map[string]bool{
						"tagged-container": true,
					},
				},
				Stopped: make(map[string]bool),
			}
			report, cleanupImageInfos, err := actions.Update(context.Background(), client, config)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(report.Scanned()).
				To(gomega.HaveLen(1), "Tagged container should be scanned")
			gomega.Expect(report.Updated()).
				To(gomega.HaveLen(1), "Tagged container should be updated if stale")
			gomega.Expect(cleanupImageInfos).
				To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("image:1.0.0"))))
			gomega.Expect(client.TestData.IsContainerStaleCount).
				To(gomega.Equal(1), "IsContainerStale should be called")
		})

		ginkgo.It("should process untagged images and update if stale", func() {
			client = &mockActions.MockClient{
				TestData: &mockActions.TestData{
					IsContainerStaleCount: 0,
					Containers: []types.Container{
						mockActions.CreateMockContainer(
							"untagged-container",
							"/untagged-container",
							"image",
							time.Now()),
					},
					Staleness: map[string]bool{
						"untagged-container": true,
					},
				},
				Stopped: make(map[string]bool),
			}
			report, cleanupImageInfos, err := actions.Update(context.Background(), client, config)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(report.Scanned()).
				To(gomega.HaveLen(1), "Untagged container should be scanned")
			gomega.Expect(report.Updated()).
				To(gomega.HaveLen(1), "Untagged container should be updated if stale")
			gomega.Expect(cleanupImageInfos).
				To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("image"))))
			gomega.Expect(client.TestData.IsContainerStaleCount).
				To(gomega.Equal(1), "IsContainerStale should be called")
		})

		ginkgo.It("should skip pinned containers and not collect image IDs", func() {
			client = &mockActions.MockClient{
				TestData: &mockActions.TestData{
					IsContainerStaleCount: 0,
					Containers: []types.Container{
						mockActions.CreateMockContainer(
							"pinned-container",
							"/pinned-container",
							"image@sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
							time.Now(),
						),
					},
					Staleness: map[string]bool{
						"pinned-container": true,
					},
				},
				Stopped: make(map[string]bool),
			}
			report, cleanupImageInfos, err := actions.Update(context.Background(), client, config)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(report.Scanned()).
				To(gomega.HaveLen(1), "Pinned container should be scanned")
			gomega.Expect(report.Updated()).
				To(gomega.BeEmpty(), "Pinned container should not be updated")
			gomega.Expect(cleanupImageInfos).
				To(gomega.BeEmpty(), "No image IDs should be collected for pinned container")
			gomega.Expect(client.TestData.TriedToRemoveImageCount).
				To(gomega.Equal(0), "RemoveImageByID should not be called")
			gomega.Expect(client.TestData.IsContainerStaleCount).
				To(gomega.Equal(0), "IsContainerStale should not be called")
		})

		ginkgo.It(
			"should skip pinned containers with tag and digest and not collect image IDs",
			func() {
				client = &mockActions.MockClient{
					TestData: &mockActions.TestData{
						IsContainerStaleCount: 0,
						Containers: []types.Container{
							mockActions.CreateMockContainer(
								"pinned-tagged-container",
								"/pinned-tagged-container",
								"image:latest@sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
								time.Now(),
							),
						},
						Staleness: map[string]bool{
							"pinned-tagged-container": true,
						},
					},
					Stopped: make(map[string]bool),
				}
				report, cleanupImageInfos, err := actions.Update(
					context.Background(),
					client,
					config,
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Scanned()).
					To(gomega.HaveLen(1), "Pinned container should be scanned")
				gomega.Expect(report.Updated()).
					To(gomega.BeEmpty(), "Pinned container should not be updated")
				gomega.Expect(cleanupImageInfos).
					To(gomega.BeEmpty(), "No image IDs should be collected for pinned container")
				gomega.Expect(client.TestData.TriedToRemoveImageCount).
					To(gomega.Equal(0), "RemoveImageByID should not be called")
				gomega.Expect(client.TestData.IsContainerStaleCount).
					To(gomega.Equal(0), "IsContainerStale should not be called")
			},
		)

		ginkgo.It("should process invalid image references with fallback", func() {
			client = &mockActions.MockClient{
				TestData: &mockActions.TestData{
					IsContainerStaleCount: 0,
					Containers: []types.Container{
						mockActions.CreateMockContainer(
							"invalid-container",
							"/invalid-container",
							":latest",
							time.Now()),
					},
					Staleness: map[string]bool{
						"invalid-container": true,
					},
				},
				Stopped: make(map[string]bool),
			}
			report, cleanupImageInfos, err := actions.Update(context.Background(), client, config)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(report.Scanned()).
				To(gomega.HaveLen(1), "Invalid container should be scanned with fallback")
			gomega.Expect(report.Updated()).
				To(gomega.HaveLen(1), "Invalid container should be updated with valid fallback")
			gomega.Expect(cleanupImageInfos).
				To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID(":latest"))))
			gomega.Expect(client.TestData.IsContainerStaleCount).
				To(gomega.Equal(1), "IsContainerStale should be called")
		})

		ginkgo.It(
			"should skip containers with missing Config.Image and imageInfo.ID with error",
			func() {
				client = &mockActions.MockClient{
					TestData: &mockActions.TestData{
						IsContainerStaleCount: 0,
						Containers: []types.Container{
							mockActions.CreateMockContainerWithImageInfoP(
								"edge-container",
								"/edge-container",
								"",
								time.Now(),
								nil,
							),
						},
						Staleness: map[string]bool{
							"edge-container": true,
						},
					},
					Stopped: make(map[string]bool),
				}
				report, cleanupImageInfos, err := actions.Update(
					context.Background(),
					client,
					config,
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Skipped()).
					To(gomega.HaveLen(1), "Container with missing image info should be skipped")
				gomega.Expect(report.Scanned()).
					To(gomega.BeEmpty(), "Container should not be scanned")
				gomega.Expect(report.Updated()).
					To(gomega.BeEmpty(), "Container should not be updated")
				gomega.Expect(cleanupImageInfos).
					To(gomega.BeEmpty(), "No image IDs should be collected")
				gomega.Expect(client.TestData.TriedToRemoveImageCount).
					To(gomega.Equal(0), "RemoveImageByID should not be called")
				gomega.Expect(client.TestData.IsContainerStaleCount).
					To(gomega.Equal(1), "IsContainerStale should be called")
			},
		)

		// This test differs from the previous one ("should process invalid image references with fallback")
		// in that the container name "InvalidContainer" contains uppercase characters, making the fallback
		// image "InvalidContainer:latest" invalid since Docker image names must be lowercase.
		// In contrast, "invalid-container:latest" is valid.
		ginkgo.It("should skip containers with invalid fallback image reference", func() {
			client = &mockActions.MockClient{
				TestData: &mockActions.TestData{
					IsContainerStaleCount: 0,
					Containers: []types.Container{
						mockActions.CreateMockContainer(
							"InvalidContainer",
							"/InvalidContainer",
							":latest",
							time.Now()),
					},
					Staleness: map[string]bool{
						"InvalidContainer": true,
					},
				},
				Stopped: make(map[string]bool),
			}
			report, cleanupImageInfos, err := actions.Update(context.Background(), client, config)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(report.Skipped()).
				To(gomega.HaveLen(1), "Container with invalid fallback image should be skipped")
			gomega.Expect(report.Scanned()).To(gomega.BeEmpty(), "Container should not be scanned")
			gomega.Expect(report.Updated()).To(gomega.BeEmpty(), "Container should not be updated")
			gomega.Expect(cleanupImageInfos).
				To(gomega.BeEmpty(), "No image IDs should be collected")
			gomega.Expect(client.TestData.TriedToRemoveImageCount).
				To(gomega.Equal(0), "RemoveImageByID should not be called")
			gomega.Expect(client.TestData.IsContainerStaleCount).
				To(gomega.Equal(0), "IsContainerStale should not be called")
		})
	})
})
