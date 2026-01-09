package actions

import (
	"errors"
	"strings"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	dockerContainer "github.com/docker/docker/api/types/container"
	dockerImage "github.com/docker/docker/api/types/image"

	"github.com/nicholas-fedor/watchtower/pkg/container"
	mockContainer "github.com/nicholas-fedor/watchtower/pkg/container/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

var _ = ginkgo.Describe("CheckForMultipleWatchtowerInstances", func() {
	ginkgo.When("no scope is specified", func() {
		ginkgo.It("should return nil when only one instance exists", func() {
			mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

			mockContainer := createMockContainer(
				"watchtower",
				"watchtower",
				"watchtower:latest",
				true,
				false,
				time.Now(),
				map[string]string{
					"com.centurylinklabs.watchtower": "true",
				},
			)

			mockClient.EXPECT().
				ListContainers(mock.Anything).
				Return([]types.Container{mockContainer}, nil)

			var cleanupImageInfo []types.CleanedImageInfo
			cleanupOccurred, err := CheckForMultipleWatchtowerInstances(
				mockClient,
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
				mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

				oldContainer := createMockContainer(
					"watchtower-old",
					"watchtower-old",
					"watchtower:old",
					true,
					false,
					time.Now().Add(-time.Hour),
					map[string]string{
						"com.centurylinklabs.watchtower": "true",
					},
				)
				newContainer := createMockContainer(
					"watchtower-new",
					"watchtower-new",
					"watchtower:new",
					true,
					false,
					time.Now(),
					map[string]string{
						"com.centurylinklabs.watchtower": "true",
					},
				)

				mockClient.EXPECT().
					ListContainers(mock.Anything).
					Return([]types.Container{oldContainer, newContainer}, nil)
				mockClient.EXPECT().StopAndRemoveContainer(oldContainer, 10*time.Minute).Return(nil)
				mockClient.EXPECT().
					RemoveImageByID(types.ImageID("watchtower:old"), "watchtower:old").
					Return(nil)

				var cleanupImageIDs []types.CleanedImageInfo
				cleanupOccurred, err := CheckForMultipleWatchtowerInstances(
					mockClient,
					true,
					"",
					&cleanupImageIDs,
				)

				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(cleanupOccurred).To(gomega.BeTrue())
				gomega.Expect(cleanupImageIDs).To(gomega.HaveLen(1))
				gomega.Expect(cleanupImageIDs[0].ImageID).
					To(gomega.Equal(types.ImageID("watchtower:old")))
			},
		)
	})

	ginkgo.When("scope is specified", func() {
		ginkgo.It("should only clean up instances in the same scope", func() {
			mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

			scopedOldContainer := createMockContainer(
				"watchtower-scoped",
				"watchtower-scoped",
				"watchtower:latest",
				true,
				false,
				time.Now().Add(-time.Hour),
				map[string]string{
					"com.centurylinklabs.watchtower":       "true",
					"com.centurylinklabs.watchtower.scope": "prod",
				},
			)

			mockClient.EXPECT().
				ListContainers(mock.Anything).
				Return([]types.Container{scopedOldContainer}, nil)

			var cleanupImageIDs []types.CleanedImageInfo
			cleanupOccurred, err := CheckForMultipleWatchtowerInstances(
				mockClient,
				true,
				"prod",
				&cleanupImageIDs,
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cleanupOccurred).To(gomega.BeFalse())
			gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty())
		})

		ginkgo.It("should clean up multiple instances within the same scope", func() {
			mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

			oldContainer := createMockContainer(
				"watchtower-prod-old",
				"watchtower-prod-old",
				"watchtower:1.11.0",
				true,
				false,
				time.Now().Add(-time.Hour),
				map[string]string{
					"com.centurylinklabs.watchtower":       "true",
					"com.centurylinklabs.watchtower.scope": "prod",
				},
			)
			newContainer := createMockContainer(
				"watchtower-prod-new",
				"watchtower-prod-new",
				"watchtower:1.12.0",
				true,
				false,
				time.Now(),
				map[string]string{
					"com.centurylinklabs.watchtower":       "true",
					"com.centurylinklabs.watchtower.scope": "prod",
				},
			)

			mockClient.EXPECT().
				ListContainers(mock.Anything).
				Return([]types.Container{oldContainer, newContainer}, nil)
			mockClient.EXPECT().StopAndRemoveContainer(oldContainer, 10*time.Minute).Return(nil)
			mockClient.EXPECT().
				RemoveImageByID(types.ImageID("watchtower:1.11.0"), "watchtower:1.11.0").
				Return(nil)

			var cleanupImageIDs []types.CleanedImageInfo
			cleanupOccurred, err := CheckForMultipleWatchtowerInstances(
				mockClient,
				true,
				"prod",
				&cleanupImageIDs,
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cleanupOccurred).To(gomega.BeTrue())
			gomega.Expect(cleanupImageIDs).
				To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("watchtower:1.11.0"))))
			gomega.Expect(cleanupImageIDs).To(gomega.HaveLen(1))
		})

		ginkgo.It("should return false when only one instance exists in scope", func() {
			mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

			scopedContainer := createMockContainer(
				"watchtower-prod",
				"watchtower-prod",
				"watchtower:latest",
				true,
				false,
				time.Now(),
				map[string]string{
					"com.centurylinklabs.watchtower":       "true",
					"com.centurylinklabs.watchtower.scope": "prod",
				},
			)

			mockClient.EXPECT().
				ListContainers(mock.Anything).
				Return([]types.Container{scopedContainer}, nil)

			var cleanupImageIDs []types.CleanedImageInfo
			cleanupOccurred, err := CheckForMultipleWatchtowerInstances(
				mockClient,
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
			mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

			oldContainer := createMockContainer(
				"watchtower-old",
				"watchtower-old",
				"watchtower:old",
				true,
				false,
				time.Now().Add(-time.Hour),
				map[string]string{
					"com.centurylinklabs.watchtower": "true",
				},
			)
			newContainer := createMockContainer(
				"watchtower-new",
				"watchtower-new",
				"watchtower:new",
				true,
				false,
				time.Now(),
				map[string]string{
					"com.centurylinklabs.watchtower": "true",
				},
			)

			mockClient.EXPECT().
				ListContainers(mock.Anything).
				Return([]types.Container{oldContainer, newContainer}, nil)
			mockClient.EXPECT().StopAndRemoveContainer(oldContainer, 10*time.Minute).Return(nil)

			var cleanupImageIDs []types.CleanedImageInfo
			cleanupOccurred, err := CheckForMultipleWatchtowerInstances(
				mockClient,
				false,
				"",
				&cleanupImageIDs,
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cleanupOccurred).To(gomega.BeTrue())
			gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty())
		})
	})

	ginkgo.When("error scenarios", func() {
		ginkgo.It("should return error when ListContainers fails", func() {
			mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

			mockClient.EXPECT().
				ListContainers(mock.Anything).
				Return(nil, errors.New("list containers failed"))

			var cleanupImageIDs []types.CleanedImageInfo
			cleanupOccurred, err := CheckForMultipleWatchtowerInstances(
				mockClient,
				false,
				"",
				&cleanupImageIDs,
			)

			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("list containers failed"))
			gomega.Expect(cleanupOccurred).To(gomega.BeFalse())
			gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty())
		})

		ginkgo.It("should return error when stopping container fails", func() {
			mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

			oldContainer := createMockContainer(
				"watchtower-old",
				"watchtower-old",
				"watchtower:old",
				true,
				false,
				time.Now().Add(-time.Hour),
				map[string]string{
					"com.centurylinklabs.watchtower": "true",
				},
			)
			newContainer := createMockContainer(
				"watchtower-new",
				"watchtower-new",
				"watchtower:new",
				true,
				false,
				time.Now(),
				map[string]string{
					"com.centurylinklabs.watchtower": "true",
				},
			)

			mockClient.EXPECT().
				ListContainers(mock.Anything).
				Return([]types.Container{oldContainer, newContainer}, nil)
			mockClient.EXPECT().
				StopAndRemoveContainer(oldContainer, 10*time.Minute).
				Return(errors.New("stop container failed"))

			var cleanupImageIDs []types.CleanedImageInfo
			cleanupOccurred, err := CheckForMultipleWatchtowerInstances(
				mockClient,
				false,
				"",
				&cleanupImageIDs,
			)

			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("all 1 instances failed to stop"))
			gomega.Expect(cleanupOccurred).To(gomega.BeTrue())
			gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty())
		})

		ginkgo.It("should continue cleanup when some containers fail to stop", func() {
			mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

			old1Container := createMockContainer(
				"watchtower-old1",
				"watchtower-old1",
				"watchtower:old1",
				true,
				false,
				time.Now().Add(-2*time.Hour),
				map[string]string{
					"com.centurylinklabs.watchtower": "true",
				},
			)
			old2Container := createMockContainer(
				"watchtower-old2",
				"watchtower-old2",
				"watchtower:old2",
				true,
				false,
				time.Now().Add(-time.Hour),
				map[string]string{
					"com.centurylinklabs.watchtower": "true",
				},
			)
			newContainer := createMockContainer(
				"watchtower-new",
				"watchtower-new",
				"watchtower:new",
				true,
				false,
				time.Now(),
				map[string]string{
					"com.centurylinklabs.watchtower": "true",
				},
			)

			mockClient.EXPECT().
				ListContainers(mock.Anything).
				Return([]types.Container{old1Container, old2Container, newContainer}, nil)
			mockClient.EXPECT().
				StopAndRemoveContainer(old1Container, 10*time.Minute).
				Return(errors.New("stop container failed"))
			mockClient.EXPECT().StopAndRemoveContainer(old2Container, 10*time.Minute).Return(nil)
			mockClient.EXPECT().
				RemoveImageByID(types.ImageID("watchtower:old2"), "watchtower:old2").
				Return(nil)

			var cleanupImageIDs []types.CleanedImageInfo
			cleanupOccurred, err := CheckForMultipleWatchtowerInstances(
				mockClient,
				true,
				"",
				&cleanupImageIDs,
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred()) // Partial success returns no error
			gomega.Expect(cleanupOccurred).To(gomega.BeTrue())
			gomega.Expect(cleanupImageIDs).
				To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("watchtower:old2"))))
			gomega.Expect(cleanupImageIDs).To(gomega.HaveLen(1))
		})

		ginkgo.It(
			"should treat 'already in progress' errors as non-errors and continue cleanup",
			func() {
				mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

				old1Container := createMockContainer(
					"watchtower-old1",
					"watchtower-old1",
					"watchtower:old1",
					true,
					false,
					time.Now().Add(-2*time.Hour),
					map[string]string{
						"com.centurylinklabs.watchtower": "true",
					},
				)
				old2Container := createMockContainer(
					"watchtower-old2",
					"watchtower-old2",
					"watchtower:old2",
					true,
					false,
					time.Now().Add(-time.Hour),
					map[string]string{
						"com.centurylinklabs.watchtower": "true",
					},
				)
				newContainer := createMockContainer(
					"watchtower-new",
					"watchtower-new",
					"watchtower:new",
					true,
					false,
					time.Now(),
					map[string]string{
						"com.centurylinklabs.watchtower": "true",
					},
				)

				mockClient.EXPECT().
					ListContainers(mock.Anything).
					Return([]types.Container{old1Container, old2Container, newContainer}, nil)
				mockClient.EXPECT().
					StopAndRemoveContainer(old1Container, 10*time.Minute).
					Return(errors.New("removal of container watchtower-old1 is already in progress"))
				mockClient.EXPECT().
					StopAndRemoveContainer(old2Container, 10*time.Minute).
					Return(nil)
				mockClient.EXPECT().
					RemoveImageByID(types.ImageID("watchtower:old2"), "watchtower:old2").
					Return(nil)

				var cleanupImageIDs []types.CleanedImageInfo
				cleanupOccurred, err := CheckForMultipleWatchtowerInstances(
					mockClient,
					true,
					"",
					&cleanupImageIDs,
				)

				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(cleanupOccurred).To(gomega.BeTrue())
				gomega.Expect(cleanupImageIDs).
					To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("watchtower:old2"))))
				gomega.Expect(cleanupImageIDs).To(gomega.HaveLen(1))
			},
		)

		ginkgo.It(
			"should treat 'no such container' errors as non-errors and continue cleanup",
			func() {
				mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

				old1Container := createMockContainer(
					"watchtower-old1",
					"watchtower-old1",
					"watchtower:old1",
					true,
					false,
					time.Now().Add(-2*time.Hour),
					map[string]string{
						"com.centurylinklabs.watchtower": "true",
					},
				)
				old2Container := createMockContainer(
					"watchtower-old2",
					"watchtower-old2",
					"watchtower:old2",
					true,
					false,
					time.Now().Add(-time.Hour),
					map[string]string{
						"com.centurylinklabs.watchtower": "true",
					},
				)
				newContainer := createMockContainer(
					"watchtower-new",
					"watchtower-new",
					"watchtower:new",
					true,
					false,
					time.Now(),
					map[string]string{
						"com.centurylinklabs.watchtower": "true",
					},
				)

				mockClient.EXPECT().
					ListContainers(mock.Anything).
					Return([]types.Container{old1Container, old2Container, newContainer}, nil)
				mockClient.EXPECT().
					StopAndRemoveContainer(old1Container, 10*time.Minute).
					Return(errors.New("no such container"))
				mockClient.EXPECT().
					StopAndRemoveContainer(old2Container, 10*time.Minute).
					Return(nil)
				mockClient.EXPECT().
					RemoveImageByID(types.ImageID("watchtower:old2"), "watchtower:old2").
					Return(nil)

				var cleanupImageIDs []types.CleanedImageInfo
				cleanupOccurred, err := CheckForMultipleWatchtowerInstances(
					mockClient,
					true,
					"",
					&cleanupImageIDs,
				)

				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(cleanupOccurred).To(gomega.BeTrue())
				gomega.Expect(cleanupImageIDs).
					To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("watchtower:old2"))))
				gomega.Expect(cleanupImageIDs).To(gomega.HaveLen(1))
			},
		)
	})

	ginkgo.When("image ID handling", func() {
		ginkgo.It(
			"should not collect image ID when excess container shares image with newest",
			func() {
				mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

				oldContainer := createMockContainer(
					"watchtower-old",
					"watchtower-old",
					"watchtower:latest",
					true,
					false,
					time.Now().Add(-time.Hour),
					map[string]string{
						"com.centurylinklabs.watchtower": "true",
					},
				)
				newContainer := createMockContainer(
					"watchtower-new",
					"watchtower-new",
					"watchtower:latest",
					true,
					false,
					time.Now(),
					map[string]string{
						"com.centurylinklabs.watchtower": "true",
					},
				)

				mockClient.EXPECT().
					ListContainers(mock.Anything).
					Return([]types.Container{oldContainer, newContainer}, nil)
				mockClient.EXPECT().StopAndRemoveContainer(oldContainer, 10*time.Minute).Return(nil)

				var cleanupImageIDs []types.CleanedImageInfo
				cleanupOccurred, err := CheckForMultipleWatchtowerInstances(
					mockClient,
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
			mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

			oldContainer := createMockContainer(
				"watchtower-old",
				"watchtower-old",
				"",
				true,
				false,
				time.Now().Add(-time.Hour),
				map[string]string{
					"com.centurylinklabs.watchtower": "true",
				},
			)
			newContainer := createMockContainer(
				"watchtower-new",
				"watchtower-new",
				"watchtower:new",
				true,
				false,
				time.Now(),
				map[string]string{
					"com.centurylinklabs.watchtower": "true",
				},
			)

			mockClient.EXPECT().
				ListContainers(mock.Anything).
				Return([]types.Container{oldContainer, newContainer}, nil)
			mockClient.EXPECT().StopAndRemoveContainer(oldContainer, 10*time.Minute).Return(nil)

			var cleanupImageIDs []types.CleanedImageInfo
			cleanupOccurred, err := CheckForMultipleWatchtowerInstances(
				mockClient,
				false,
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
		mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

		cleaned, err := CleanupImages(mockClient, nil)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(cleaned).To(gomega.BeEmpty())
	})

	ginkgo.It("should attempt to remove each image ID", func() {
		mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

		cleanedImages := []types.CleanedImageInfo{
			{ImageID: "image1"},
			{ImageID: "image2"},
			{ImageID: ""}, // empty ID should be skipped
		}

		mockClient.EXPECT().RemoveImageByID(types.ImageID("image1"), "").Return(nil)
		mockClient.EXPECT().RemoveImageByID(types.ImageID("image2"), "").Return(nil)

		cleaned, err := CleanupImages(mockClient, cleanedImages)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(cleaned).To(gomega.HaveLen(2))
		gomega.Expect(cleaned[0].ImageID).To(gomega.Equal(types.ImageID("image1")))
		gomega.Expect(cleaned[1].ImageID).To(gomega.Equal(types.ImageID("image2")))
	})

	ginkgo.It("should return error when image removal fails", func() {
		mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

		cleanedImages := []types.CleanedImageInfo{
			{ImageID: "image1"},
			{ImageID: "image2"},
		}

		mockClient.EXPECT().RemoveImageByID(types.ImageID("image1"), "").Return(nil)
		mockClient.EXPECT().
			RemoveImageByID(types.ImageID("image2"), "").
			Return(errors.New("image removal failed"))

		cleaned, err := CleanupImages(mockClient, cleanedImages)
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).
			To(gomega.ContainSubstring("errors occurred during image cleanup"))
		gomega.Expect(cleaned).To(gomega.HaveLen(1))
		gomega.Expect(cleaned[0].ImageID).To(gomega.Equal(types.ImageID("image1")))
	})

	ginkgo.It("should treat 'No such image' errors as non-errors and not add to cleaned", func() {
		mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

		cleanedImages := []types.CleanedImageInfo{
			{ImageID: "image1"},
			{ImageID: "image2"},
		}

		mockClient.EXPECT().RemoveImageByID(types.ImageID("image1"), "").Return(nil)
		mockClient.EXPECT().
			RemoveImageByID(types.ImageID("image2"), "").
			Return(errors.New("No such image"))

		cleaned, err := CleanupImages(mockClient, cleanedImages)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(cleaned).To(gomega.HaveLen(1))
		gomega.Expect(cleaned[0].ImageID).To(gomega.Equal(types.ImageID("image1")))
	})
})

var _ = ginkgo.Describe("containerNames", func() {
	ginkgo.It("should return empty slice for empty container list", func() {
		containers := []types.Container{}
		result := containerNames(containers)
		gomega.Expect(result).To(gomega.BeEmpty())
	})

	ginkgo.It("should return container names", func() {
		container1 := createMockContainer("id1", "name1", "image1", true, false, time.Now(), nil)
		container2 := createMockContainer("id2", "name2", "image2", true, false, time.Now(), nil)

		containers := []types.Container{container1, container2}
		result := containerNames(containers)
		gomega.Expect(result).To(gomega.Equal([]string{"name1", "name2"}))
	})
})

// createMockContainer is a helper function to create a mock container for testing.
//
//nolint:unparam // running parameter is intentionally fixed for test purposes
func createMockContainer(
	id, name, image string,
	running, restarting bool,
	created time.Time,
	labels map[string]string,
) types.Container {
	if labels == nil {
		labels = make(map[string]string)
	}

	content := dockerContainer.InspectResponse{
		ContainerJSONBase: &dockerContainer.ContainerJSONBase{
			ID:    id,
			Image: image,
			Name:  name,
			State: &dockerContainer.State{
				Running:    running,
				Restarting: restarting,
			},
			Created: created.Format(time.RFC3339Nano),
			HostConfig: &dockerContainer.HostConfig{
				PortBindings: map[nat.Port][]nat.PortBinding{},
			},
		},
		Config: &dockerContainer.Config{
			Image:        image,
			Labels:       labels,
			ExposedPorts: map[nat.Port]struct{}{},
		},
	}

	imageInfo := &dockerImage.InspectResponse{
		ID: image,
		RepoDigests: []string{
			image + "@sha256:" + strings.ReplaceAll(image, ":", "_"),
		},
	}

	return container.NewContainer(&content, imageInfo)
}
