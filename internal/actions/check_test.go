package actions_test

import (
	"errors"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/nicholas-fedor/watchtower/internal/actions"
	"github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/filters"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

var _ = ginkgo.Describe("CheckForSanity", func() {
	ginkgo.When("rolling restarts are disabled", func() {
		ginkgo.It("should return nil without checking containers", func() {
			client := mocks.CreateMockClient(&mocks.TestData{}, false, false)

			err := actions.CheckForSanity(client, filters.NoFilter, false)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})
	})

	ginkgo.When("rolling restarts are enabled", func() {
		ginkgo.It("should return nil when no containers have links", func() {
			client := mocks.CreateMockClient(
				&mocks.TestData{
					Containers: []types.Container{
						mocks.CreateMockContainer(
							"container1",
							"container1",
							"image:latest",
							time.Now(),
						),
						mocks.CreateMockContainer(
							"container2",
							"container2",
							"image:latest",
							time.Now(),
						),
					},
				},
				false,
				false,
			)

			err := actions.CheckForSanity(client, filters.NoFilter, true)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})

		ginkgo.It("should return error when container has links", func() {
			containerWithLinks := mocks.CreateMockContainerWithLinks(
				"container1",
				"container1",
				"image:latest",
				time.Now(),
				[]string{"container2:alias"},
				mocks.CreateMockImageInfo("image:latest"),
			)

			client := mocks.CreateMockClient(
				&mocks.TestData{
					Containers: []types.Container{
						containerWithLinks,
						mocks.CreateMockContainer(
							"container2",
							"container2",
							"image:latest",
							time.Now(),
						),
					},
				},
				false,
				false,
			)

			err := actions.CheckForSanity(client, filters.NoFilter, true)

			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring("incompatible with rolling restarts"))
		})
	})
})

var _ = ginkgo.Describe("CheckForMultipleWatchtowerInstances", func() {
	ginkgo.When("no scope is specified", func() {
		ginkgo.It("should return nil when only one instance exists", func() {
			client := mocks.CreateMockClient(
				&mocks.TestData{
					Containers: []types.Container{
						mocks.CreateMockContainerWithConfig(
							"watchtower",
							"watchtower",
							"watchtower:latest",
							true,
							false,
							time.Now(),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower": "true",
								},
							},
						),
					},
				},
				false,
				false,
			)

			var cleanupImageInfo []types.CleanedImageInfo
			cleanupOccurred, err := actions.CheckForMultipleWatchtowerInstances(
				client,
				false,
				"",
				&cleanupImageInfo,
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cleanupOccurred).To(gomega.BeFalse())
			gomega.Expect(cleanupImageInfo).To(gomega.BeEmpty())
		})

		ginkgo.It(
			"should stop excess instances and collect image IDs when cleanup enabled",
			func() {
				client := mocks.CreateMockClient(
					&mocks.TestData{
						Containers: []types.Container{
							mocks.CreateMockContainerWithConfig(
								"watchtower-old",
								"watchtower-old",
								"watchtower:old",
								true,
								false,
								time.Now().Add(-time.Hour),
								&container.Config{
									Labels: map[string]string{
										"com.centurylinklabs.watchtower": "true",
									},
								},
							),
							mocks.CreateMockContainerWithConfig(
								"watchtower-new",
								"watchtower-new",
								"watchtower:new",
								true,
								false,
								time.Now(),
								&container.Config{
									Labels: map[string]string{
										"com.centurylinklabs.watchtower": "true",
									},
								},
							),
						},
					},
					false,
					false,
				)

				var cleanupImageIDs []types.CleanedImageInfo
				cleanupOccurred, err := actions.CheckForMultipleWatchtowerInstances(
					client,
					true,
					"",
					&cleanupImageIDs,
				)

				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(cleanupOccurred).To(gomega.BeTrue())
				gomega.Expect(client.TestData.StopContainerCount).To(gomega.Equal(1))
				gomega.Expect(cleanupImageIDs).To(gomega.HaveLen(1))
			},
		)
	})

	ginkgo.When("scope is specified", func() {
		ginkgo.It("should only clean up instances in the same scope", func() {
			client := mocks.CreateMockClient(
				&mocks.TestData{
					Containers: []types.Container{
						mocks.CreateMockContainerWithConfig(
							"watchtower-scoped",
							"watchtower-scoped",
							"watchtower:latest",
							true,
							false,
							time.Now().Add(-time.Hour),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower":       "true",
									"com.centurylinklabs.watchtower.scope": "prod",
								},
							},
						),
						mocks.CreateMockContainerWithConfig(
							"watchtower-unscoped",
							"watchtower-unscoped",
							"watchtower:latest",
							true,
							false,
							time.Now(),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower": "true",
								},
							},
						),
					},
				},
				false,
				false,
			)

			var cleanupImageIDs []types.CleanedImageInfo
			cleanupOccurred, err := actions.CheckForMultipleWatchtowerInstances(
				client,
				true,
				"prod",
				&cleanupImageIDs,
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cleanupOccurred).To(gomega.BeFalse())
			gomega.Expect(client.TestData.StopContainerCount).To(gomega.Equal(0))
			gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty())
		})
		ginkgo.It("should clean up multiple instances within the same scope", func() {
			client := mocks.CreateMockClient(
				&mocks.TestData{
					Containers: []types.Container{
						mocks.CreateMockContainerWithConfig(
							"watchtower-prod-old",
							"watchtower-prod-old",
							"watchtower:1.11.0",
							true,
							false,
							time.Now().Add(-time.Hour),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower":       "true",
									"com.centurylinklabs.watchtower.scope": "prod",
								},
							},
						),
						mocks.CreateMockContainerWithConfig(
							"watchtower-prod-new",
							"watchtower-prod-new",
							"watchtower:1.12.0",
							true,
							false,
							time.Now(),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower":       "true",
									"com.centurylinklabs.watchtower.scope": "prod",
								},
							},
						),
						mocks.CreateMockContainerWithConfig(
							"watchtower-dev",
							"watchtower-dev",
							"watchtower:1.12.0",
							true,
							false,
							time.Now(),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower":       "true",
									"com.centurylinklabs.watchtower.scope": "dev",
								},
							},
						),
					},
				},
				false,
				false,
			)

			var cleanupImageIDs []types.CleanedImageInfo
			cleanupOccurred, err := actions.CheckForMultipleWatchtowerInstances(
				client,
				true,
				"prod",
				&cleanupImageIDs,
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cleanupOccurred).To(gomega.BeTrue())
			gomega.Expect(client.TestData.StopContainerCount).To(gomega.Equal(1))
			gomega.Expect(cleanupImageIDs).
				To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("watchtower:1.11.0"))))
			gomega.Expect(cleanupImageIDs).To(gomega.HaveLen(1))
		})

		ginkgo.It("should return false when only one instance exists in scope", func() {
			client := mocks.CreateMockClient(
				&mocks.TestData{
					Containers: []types.Container{
						mocks.CreateMockContainerWithConfig(
							"watchtower-prod",
							"watchtower-prod",
							"watchtower:latest",
							true,
							false,
							time.Now(),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower":       "true",
									"com.centurylinklabs.watchtower.scope": "prod",
								},
							},
						),
						mocks.CreateMockContainerWithConfig(
							"watchtower-dev",
							"watchtower-dev",
							"watchtower:latest",
							true,
							false,
							time.Now(),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower":       "true",
									"com.centurylinklabs.watchtower.scope": "dev",
								},
							},
						),
					},
				},
				false,
				false,
			)

			var cleanupImageIDs []types.CleanedImageInfo
			cleanupOccurred, err := actions.CheckForMultipleWatchtowerInstances(
				client,
				false,
				"prod",
				&cleanupImageIDs,
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cleanupOccurred).To(gomega.BeFalse())
			gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty())
		})
	})

	ginkgo.When("cleanup is disabled", func() {
		ginkgo.It("should stop excess instances but not collect image IDs", func() {
			client := mocks.CreateMockClient(
				&mocks.TestData{
					Containers: []types.Container{
						mocks.CreateMockContainerWithConfig(
							"watchtower-old",
							"watchtower-old",
							"watchtower:old",
							true,
							false,
							time.Now().Add(-time.Hour),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower": "true",
								},
							},
						),
						mocks.CreateMockContainerWithConfig(
							"watchtower-new",
							"watchtower-new",
							"watchtower:new",
							true,
							false,
							time.Now(),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower": "true",
								},
							},
						),
					},
				},
				false,
				false,
			)

			var cleanupImageIDs []types.CleanedImageInfo
			cleanupOccurred, err := actions.CheckForMultipleWatchtowerInstances(
				client,
				false, // cleanup disabled
				"",
				&cleanupImageIDs,
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cleanupOccurred).To(gomega.BeTrue())
			gomega.Expect(client.TestData.StopContainerCount).To(gomega.Equal(1))
			gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty())
		})
	})

	ginkgo.When("error scenarios", func() {
		ginkgo.It("should return error when ListContainers fails", func() {
			client := mocks.CreateMockClient(&mocks.TestData{
				ListContainersError: errors.New("list containers failed"),
			}, false, false)

			var cleanupImageIDs []types.CleanedImageInfo
			cleanupOccurred, err := actions.CheckForMultipleWatchtowerInstances(
				client,
				false,
				"",
				&cleanupImageIDs,
			)

			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to list containers"))
			gomega.Expect(cleanupOccurred).To(gomega.BeFalse())
			gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty())
		})

		ginkgo.It("should return error when stopping container fails", func() {
			client := mocks.CreateMockClient(
				&mocks.TestData{
					Containers: []types.Container{
						mocks.CreateMockContainerWithConfig(
							"watchtower-old",
							"watchtower-old",
							"watchtower:old",
							true,
							false,
							time.Now().Add(-time.Hour),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower": "true",
								},
							},
						),
						mocks.CreateMockContainerWithConfig(
							"watchtower-new",
							"watchtower-new",
							"watchtower:new",
							true,
							false,
							time.Now(),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower": "true",
								},
							},
						),
					},
					StopContainerError:     errors.New("stop container failed"),
					StopContainerFailCount: 1, // Fail the first stop
				},
				false,
				false,
			)

			var cleanupImageIDs []types.CleanedImageInfo
			cleanupOccurred, err := actions.CheckForMultipleWatchtowerInstances(
				client,
				false,
				"",
				&cleanupImageIDs,
			)

			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring("errors occurred while stopping watchtower containers"))
			gomega.Expect(cleanupOccurred).To(gomega.BeTrue())
			gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty())
		})

		ginkgo.It("should continue cleanup when some containers fail to stop", func() {
			client := mocks.CreateMockClient(
				&mocks.TestData{
					Containers: []types.Container{
						mocks.CreateMockContainerWithConfig(
							"watchtower-old1",
							"watchtower-old1",
							"watchtower:old1",
							true,
							false,
							time.Now().Add(-2*time.Hour),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower": "true",
								},
							},
						),
						mocks.CreateMockContainerWithConfig(
							"watchtower-old2",
							"watchtower-old2",
							"watchtower:old2",
							true,
							false,
							time.Now().Add(-time.Hour),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower": "true",
								},
							},
						),
						mocks.CreateMockContainerWithConfig(
							"watchtower-new",
							"watchtower-new",
							"watchtower:new",
							true,
							false,
							time.Now(),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower": "true",
								},
							},
						),
					},
					StopContainerError:     errors.New("stop container failed"),
					StopContainerFailCount: 1, // Only first stop fails
				},
				false,
				false,
			)

			var cleanupImageIDs []types.CleanedImageInfo
			cleanupOccurred, err := actions.CheckForMultipleWatchtowerInstances(
				client,
				true,
				"",
				&cleanupImageIDs,
			)

			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("1 instances failed to stop"))
			gomega.Expect(cleanupOccurred).To(gomega.BeTrue())
			gomega.Expect(client.TestData.StopContainerCount).To(gomega.Equal(2))
			gomega.Expect(cleanupImageIDs).
				To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("watchtower:old2"))))
			gomega.Expect(cleanupImageIDs).To(gomega.HaveLen(1))
		})
	})

	ginkgo.When("image ID handling", func() {
		ginkgo.It(
			"should not collect image ID when excess container shares image with newest",
			func() {
				client := mocks.CreateMockClient(
					&mocks.TestData{
						Containers: []types.Container{
							mocks.CreateMockContainerWithConfig(
								"watchtower-old",
								"watchtower-old",
								"watchtower:latest",
								true,
								false,
								time.Now().Add(-time.Hour),
								&container.Config{
									Labels: map[string]string{
										"com.centurylinklabs.watchtower": "true",
									},
								},
							),
							mocks.CreateMockContainerWithConfig(
								"watchtower-new",
								"watchtower-new",
								"watchtower:latest",
								true,
								false,
								time.Now(),
								&container.Config{
									Labels: map[string]string{
										"com.centurylinklabs.watchtower": "true",
									},
								},
							),
						},
					},
					false,
					false,
				)

				var cleanupImageIDs []types.CleanedImageInfo
				cleanupOccurred, err := actions.CheckForMultipleWatchtowerInstances(
					client,
					true,
					"",
					&cleanupImageIDs,
				)

				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(cleanupOccurred).To(gomega.BeTrue())
				gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty())
			},
		)

		ginkgo.It("should skip empty image IDs", func() {
			client := mocks.CreateMockClient(
				&mocks.TestData{
					Containers: []types.Container{
						mocks.CreateMockContainerWithConfig(
							"watchtower-old",
							"watchtower-old",
							"", // Empty image ID
							true,
							false,
							time.Now().Add(-time.Hour),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower": "true",
								},
							},
						),
						mocks.CreateMockContainerWithConfig(
							"watchtower-new",
							"watchtower-new",
							"watchtower:new",
							true,
							false,
							time.Now(),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower": "true",
								},
							},
						),
					},
				},
				false,
				false,
			)

			var cleanupImageIDs []types.CleanedImageInfo
			cleanupOccurred, err := actions.CheckForMultipleWatchtowerInstances(
				client,
				false, // cleanup disabled
				"",
				&cleanupImageIDs,
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cleanupOccurred).To(gomega.BeTrue())
			gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty())
		})
	})
})

var _ = ginkgo.Describe("CleanupImages", func() {
	ginkgo.It("should do nothing when no images are provided", func() {
		client := mocks.CreateMockClient(&mocks.TestData{}, false, false)

		cleaned, err := actions.CleanupImages(client, nil)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(cleaned).To(gomega.BeEmpty())
		gomega.Expect(client.TestData.TriedToRemoveImageCount).To(gomega.Equal(0))
	})

	ginkgo.It("should attempt to remove each image ID", func() {
		client := mocks.CreateMockClient(&mocks.TestData{}, false, false)

		cleanedImages := []types.CleanedImageInfo{
			{ImageID: "image1"},
			{ImageID: "image2"},
			{ImageID: ""}, // empty ID should be skipped
		}

		cleaned, err := actions.CleanupImages(client, cleanedImages)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(cleaned).To(gomega.HaveLen(2))
		gomega.Expect(cleaned[0].ImageID).To(gomega.Equal(types.ImageID("image1")))
		gomega.Expect(cleaned[1].ImageID).To(gomega.Equal(types.ImageID("image2")))
		gomega.Expect(client.TestData.TriedToRemoveImageCount).To(gomega.Equal(2))
	})

	ginkgo.It("should return error when image removal fails", func() {
		client := mocks.CreateMockClient(&mocks.TestData{
			RemoveImageError: errors.New("image removal failed"),
			FailedImageIDs:   []types.ImageID{"image2"},
		}, false, false)

		cleanedImages := []types.CleanedImageInfo{
			{ImageID: "image1"},
			{ImageID: "image2"},
		}

		cleaned, err := actions.CleanupImages(client, cleanedImages)
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).
			To(gomega.ContainSubstring("errors occurred during image cleanup"))
		gomega.Expect(cleaned).To(gomega.HaveLen(1))
		gomega.Expect(cleaned[0].ImageID).To(gomega.Equal(types.ImageID("image1")))
		gomega.Expect(client.TestData.TriedToRemoveImageCount).To(gomega.Equal(2))
	})
})
