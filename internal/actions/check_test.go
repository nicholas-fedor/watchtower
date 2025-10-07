package actions_test

import (
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
				[]string{"container2"},
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

			cleanupImageIDs := make(map[types.ImageID]bool)
			err := actions.CheckForMultipleWatchtowerInstances(client, false, "", cleanupImageIDs)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty())
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

				cleanupImageIDs := make(map[types.ImageID]bool)
				err := actions.CheckForMultipleWatchtowerInstances(
					client,
					true,
					"",
					cleanupImageIDs,
				)

				gomega.Expect(err).NotTo(gomega.HaveOccurred())
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

			cleanupImageIDs := make(map[types.ImageID]bool)
			err := actions.CheckForMultipleWatchtowerInstances(
				client,
				true,
				"prod",
				cleanupImageIDs,
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(client.TestData.StopContainerCount).To(gomega.Equal(0))
			gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty())
		})
	})
})

var _ = ginkgo.Describe("CleanupImages", func() {
	ginkgo.It("should do nothing when no images are provided", func() {
		client := mocks.CreateMockClient(&mocks.TestData{}, false, false)

		actions.CleanupImages(client, nil)
		gomega.Expect(client.TestData.TriedToRemoveImageCount).To(gomega.Equal(0))
	})

	ginkgo.It("should attempt to remove each image ID", func() {
		client := mocks.CreateMockClient(&mocks.TestData{}, false, false)

		imageIDs := map[types.ImageID]bool{
			"image1": true,
			"image2": true,
			"":       true, // empty ID should be skipped
		}

		actions.CleanupImages(client, imageIDs)
		gomega.Expect(client.TestData.TriedToRemoveImageCount).To(gomega.Equal(2))
	})
})
