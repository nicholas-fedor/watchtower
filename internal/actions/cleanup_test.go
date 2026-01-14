package actions

import (
	"errors"
	"strings"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	cerrdefs "github.com/containerd/errdefs"
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

			var cleanupImageInfo []types.RemovedImageInfo
			cleanupOccurred, err := RemoveExcessWatchtowerInstances(
				mockClient,
				false,
				"",
				&cleanupImageInfo,
				mockContainer,
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cleanupOccurred).To(gomega.Equal(0))
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

				var cleanupImageIDs []types.RemovedImageInfo
				cleanupOccurred, err := RemoveExcessWatchtowerInstances(
					mockClient,
					true,
					"",
					&cleanupImageIDs,
					newContainer,
				)

				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(cleanupOccurred).To(gomega.Equal(1))
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

			var cleanupImageIDs []types.RemovedImageInfo
			cleanupOccurred, err := RemoveExcessWatchtowerInstances(
				mockClient,
				true,
				"prod",
				&cleanupImageIDs,
				scopedOldContainer,
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cleanupOccurred).To(gomega.Equal(0))
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

			var cleanupImageIDs []types.RemovedImageInfo
			cleanupOccurred, err := RemoveExcessWatchtowerInstances(
				mockClient,
				true,
				"prod",
				&cleanupImageIDs,
				newContainer,
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cleanupOccurred).To(gomega.Equal(1))
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

			var cleanupImageIDs []types.RemovedImageInfo
			cleanupOccurred, err := RemoveExcessWatchtowerInstances(
				mockClient,
				false,
				"prod",
				&cleanupImageIDs,
				scopedContainer,
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cleanupOccurred).To(gomega.Equal(0))
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

			var cleanupImageIDs []types.RemovedImageInfo
			cleanupOccurred, err := RemoveExcessWatchtowerInstances(
				mockClient,
				false,
				"",
				&cleanupImageIDs,
				newContainer,
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cleanupOccurred).To(gomega.Equal(1))
			gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty())
		})
	})

	ginkgo.When("error scenarios", func() {
		ginkgo.It("should not perform cleanup when currentContainer is nil", func() {
			mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

			mockClient.EXPECT().ListContainers(mock.Anything).Return([]types.Container{}, nil)

			var cleanupImageIDs []types.RemovedImageInfo
			cleanupOccurred, err := RemoveExcessWatchtowerInstances(
				mockClient,
				false,
				"",
				&cleanupImageIDs,
				nil,
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cleanupOccurred).To(gomega.Equal(0))
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

			var cleanupImageIDs []types.RemovedImageInfo
			cleanupOccurred, err := RemoveExcessWatchtowerInstances(
				mockClient,
				false,
				"",
				&cleanupImageIDs,
				newContainer,
			)

			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring("1 of 1 instances failed to stop"))
			gomega.Expect(cleanupOccurred).To(gomega.Equal(0))
			gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty())
		})

		ginkgo.It("should fail completely when some containers fail to stop after retries", func() {
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

			var cleanupImageIDs []types.RemovedImageInfo
			cleanupOccurred, err := RemoveExcessWatchtowerInstances(
				mockClient,
				true,
				"",
				&cleanupImageIDs,
				newContainer,
			)

			gomega.Expect(err).
				To(gomega.HaveOccurred())
				// Strict enforcement fails if any container fails
			gomega.Expect(cleanupOccurred).To(gomega.Equal(0))
			gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty())
		})

		ginkgo.It(
			"should retry 'already in progress' errors and fail if they persist",
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
					Return(errors.New("removal of container watchtower-old1 is already in progress")).
					Times(3)
				mockClient.EXPECT().
					StopAndRemoveContainer(old2Container, 10*time.Minute).
					Return(nil)

				var cleanupImageIDs []types.RemovedImageInfo
				cleanupOccurred, err := RemoveExcessWatchtowerInstances(
					mockClient,
					true,
					"",
					&cleanupImageIDs,
					newContainer,
				)

				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(cleanupOccurred).To(gomega.Equal(0))
				gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty())
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
					Return(cerrdefs.ErrNotFound)
				mockClient.EXPECT().
					StopAndRemoveContainer(old2Container, 10*time.Minute).
					Return(nil)
				mockClient.EXPECT().
					RemoveImageByID(types.ImageID("watchtower:old2"), "watchtower:old2").
					Return(nil)

				var cleanupImageIDs []types.RemovedImageInfo
				cleanupOccurred, err := RemoveExcessWatchtowerInstances(
					mockClient,
					true,
					"",
					&cleanupImageIDs,
					newContainer,
				)

				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(cleanupOccurred).To(gomega.Equal(2))
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

				var cleanupImageIDs []types.RemovedImageInfo
				cleanupOccurred, err := RemoveExcessWatchtowerInstances(
					mockClient,
					true,
					"",
					&cleanupImageIDs,
					newContainer,
				)

				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(cleanupOccurred).To(gomega.Equal(1))
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

			var cleanupImageIDs []types.RemovedImageInfo
			cleanupOccurred, err := RemoveExcessWatchtowerInstances(
				mockClient,
				false,
				"",
				&cleanupImageIDs,
				newContainer,
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cleanupOccurred).To(gomega.Equal(1))
			gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty())
		})
	})

	ginkgo.When("container chain cleanup", func() {
		ginkgo.It("should cleanup old containers in the chain", func() {
			oldID := types.ContainerID("old123")

			oldContainer := createMockContainer(
				string(oldID),
				"watchtower-old",
				"watchtower:latest",
				true,
				false,
				time.Now().Add(-time.Hour),
				map[string]string{
					"com.centurylinklabs.watchtower": "true",
				},
			)

			newID := types.ContainerID("new456")

			newContainer := createMockContainer(
				string(newID),
				"watchtower",
				"watchtower:latest",
				true,
				false,
				time.Now(),
				map[string]string{
					"com.centurylinklabs.watchtower":                 "true",
					"com.centurylinklabs.watchtower.container-chain": string(oldID),
				},
			)

			mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

			mockClient.EXPECT().
				ListContainers(mock.Anything).
				Return([]types.Container{oldContainer, newContainer}, nil)
			mockClient.EXPECT().
				StopAndRemoveContainer(oldContainer, 10*time.Minute).
				Return(nil).
				Times(1)

			var cleanupImageInfos []types.RemovedImageInfo
			cleanupOccurred, err := RemoveExcessWatchtowerInstances(
				mockClient,
				true,
				"",
				&cleanupImageInfos,
				newContainer,
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cleanupOccurred).To(gomega.Equal(1))
			gomega.Expect(cleanupImageInfos).To(gomega.BeEmpty())
		})

		ginkgo.It("should respect scope boundaries when cleaning up container chains", func() {
			oldID := types.ContainerID("old-scoped")

			oldContainer := createMockContainer(
				string(oldID),
				"watchtower-old",
				"watchtower:latest",
				true,
				false,
				time.Now().Add(-time.Hour),
				map[string]string{
					"com.centurylinklabs.watchtower":       "true",
					"com.centurylinklabs.watchtower.scope": "prod",
				},
			)

			newID := types.ContainerID("new-scoped")

			newContainer := createMockContainer(
				string(newID),
				"watchtower-new",
				"watchtower:latest",
				true,
				false,
				time.Now(),
				map[string]string{
					"com.centurylinklabs.watchtower":                 "true",
					"com.centurylinklabs.watchtower.scope":           "prod",
					"com.centurylinklabs.watchtower.container-chain": string(oldID),
				},
			)

			mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

			mockClient.EXPECT().
				ListContainers(mock.Anything).
				Return([]types.Container{oldContainer, newContainer}, nil)
			mockClient.EXPECT().
				StopAndRemoveContainer(oldContainer, 10*time.Minute).
				Return(nil).
				Times(1)
			// Should not clean up different scope container

			var cleanupImageInfos []types.RemovedImageInfo
			cleanupOccurred, err := RemoveExcessWatchtowerInstances(
				mockClient,
				true,
				"prod", // scope specified
				&cleanupImageInfos,
				newContainer,
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cleanupOccurred).To(gomega.Equal(1)) // Only oldContainer cleaned
			gomega.Expect(cleanupImageInfos).To(gomega.BeEmpty())
		})

		ginkgo.It("should not clean up chain containers from different scopes", func() {
			// Chain references container from different scope - should not clean it
			oldDifferentScopeID := types.ContainerID("old-different-scope")

			oldDifferentScopeContainer := createMockContainer(
				string(oldDifferentScopeID),
				"watchtower-old-different",
				"watchtower:latest",
				true,
				false,
				time.Now().Add(-time.Hour),
				map[string]string{
					"com.centurylinklabs.watchtower":       "true",
					"com.centurylinklabs.watchtower.scope": "different-scope",
				},
			)

			newID := types.ContainerID("new-scoped")

			newContainer := createMockContainer(
				string(newID),
				"watchtower-new",
				"watchtower:latest",
				true,
				false,
				time.Now(),
				map[string]string{
					"com.centurylinklabs.watchtower":                 "true",
					"com.centurylinklabs.watchtower.scope":           "prod",
					"com.centurylinklabs.watchtower.container-chain": string(oldDifferentScopeID),
				},
			)

			mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

			mockClient.EXPECT().
				ListContainers(mock.Anything).
				Return([]types.Container{oldDifferentScopeContainer, newContainer}, nil)
			mockClient.EXPECT().
				StopAndRemoveContainer(oldDifferentScopeContainer, 10*time.Minute).
				Return(nil).
				Times(1)

			// Should attempt to clean up the different scope container
			// Even though it's in the chain, scope isolation does not prevent cleanup when no scope filter

			var cleanupImageInfos []types.RemovedImageInfo
			cleanupOccurred, err := RemoveExcessWatchtowerInstances(
				mockClient,
				true,
				"prod", // scope specified
				&cleanupImageInfos,
				newContainer,
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cleanupOccurred).
				To(gomega.Equal(1))
				// Should clean up chain container even across scopes when no scope filter
			gomega.Expect(cleanupImageInfos).To(gomega.BeEmpty())
		})

		ginkgo.It("should validate chain cleanup isolation with multiple scopes", func() {
			// Multiple containers in different scopes with chains
			prodOldID := types.ContainerID("prod-old")

			prodOldContainer := createMockContainer(
				string(prodOldID),
				"watchtower-prod-old",
				"watchtower:latest",
				true,
				false,
				time.Now().Add(-time.Hour),
				map[string]string{
					"com.centurylinklabs.watchtower":       "true",
					"com.centurylinklabs.watchtower.scope": "prod",
				},
			)

			prodNewContainer := createMockContainer(
				"prod-new",
				"watchtower-prod-new",
				"watchtower:latest",
				true,
				false,
				time.Now(),
				map[string]string{
					"com.centurylinklabs.watchtower":                 "true",
					"com.centurylinklabs.watchtower.scope":           "prod",
					"com.centurylinklabs.watchtower.container-chain": string(prodOldID),
				},
			)

			mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

			mockClient.EXPECT().
				ListContainers(mock.Anything).
				Return([]types.Container{prodOldContainer, prodNewContainer}, nil)
			mockClient.EXPECT().
				StopAndRemoveContainer(prodOldContainer, 10*time.Minute).
				Return(nil).
				Times(1)
			// devOldContainer is not cleaned because unscoped filter excludes scoped containers

			// Test cleanup without scope filter - should clean chained container only
			// Note: unscoped filter excludes containers with scopes, so only chained container is cleaned
			var cleanupImageInfos []types.RemovedImageInfo
			cleanupOccurred, err := RemoveExcessWatchtowerInstances(
				mockClient,
				true,
				"", // no scope filter
				&cleanupImageInfos,
				prodNewContainer, // Use prod new as current for prod scope
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cleanupOccurred).
				To(gomega.Equal(1))
				// Only chained container cleaned (scoped containers excluded from unscoped filter)
			gomega.Expect(cleanupImageInfos).To(gomega.BeEmpty())
		})

		ginkgo.It(
			"should clean cross-scope chained containers as parent containers must be removed",
			func() {
				// Container with chain referencing different scope - chained containers are parent containers that must be removed
				crossScopeChainContainer := createMockContainer(
					"cross-chain",
					"watchtower-cross",
					"watchtower:latest",
					true,
					false,
					time.Now(),
					map[string]string{
						"com.centurylinklabs.watchtower":                 "true",
						"com.centurylinklabs.watchtower.scope":           "scope-a",
						"com.centurylinklabs.watchtower.container-chain": "id-from-scope-b",
					},
				)

				// Referenced container from different scope - should be cleaned as chained parent
				referencedContainer := createMockContainer(
					"id-from-scope-b",
					"watchtower-referenced",
					"watchtower:old",
					true,
					false,
					time.Now().Add(-time.Hour),
					map[string]string{
						"com.centurylinklabs.watchtower":       "true",
						"com.centurylinklabs.watchtower.scope": "scope-b",
					},
				)

				mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

				mockClient.EXPECT().
					ListContainers(mock.Anything).
					Return([]types.Container{referencedContainer, crossScopeChainContainer}, nil)
				mockClient.EXPECT().
					StopAndRemoveContainer(referencedContainer, 10*time.Minute).
					Return(nil).
					Times(1)
				mockClient.EXPECT().
					RemoveImageByID(types.ImageID("watchtower:old"), "watchtower:old").
					Return(nil)

				// Cleanup should clean the referenced container as it's a chained parent container
				var cleanupImageInfos []types.RemovedImageInfo
				cleanupOccurred, err := RemoveExcessWatchtowerInstances(
					mockClient,
					true,
					"scope-a", // Only clean scope-a
					&cleanupImageInfos,
					crossScopeChainContainer,
				)

				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(cleanupOccurred).
					To(gomega.Equal(1))
					// Referenced container cleaned as chained parent
				gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(1))
				gomega.Expect(cleanupImageInfos[0].ImageID).
					To(gomega.Equal(types.ImageID("watchtower:old")))
			},
		)
	})

	ginkgo.When("error scenarios in scoped operations", func() {
		ginkgo.It(
			"should handle partial failure during scoped cleanup with image removal errors",
			func() {
				mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

				// Create containers in the same scope
				container1 := createMockContainer(
					"scoped-1",
					"watchtower-scoped-1",
					"watchtower:v1",
					true,
					false,
					time.Now().Add(-2*time.Hour),
					map[string]string{
						"com.centurylinklabs.watchtower":       "true",
						"com.centurylinklabs.watchtower.scope": "test-scope",
					},
				)
				container2 := createMockContainer(
					"scoped-2",
					"watchtower-scoped-2",
					"watchtower:v2",
					true,
					false,
					time.Now().Add(-time.Hour),
					map[string]string{
						"com.centurylinklabs.watchtower":       "true",
						"com.centurylinklabs.watchtower.scope": "test-scope",
					},
				)
				currentContainer := createMockContainer(
					"current",
					"watchtower-current",
					"watchtower:latest",
					true,
					false,
					time.Now(),
					map[string]string{
						"com.centurylinklabs.watchtower":       "true",
						"com.centurylinklabs.watchtower.scope": "test-scope",
					},
				)

				mockClient.EXPECT().
					ListContainers(mock.Anything).
					Return([]types.Container{container1, container2, currentContainer}, nil)
				// Mock partial failures: container1 stops successfully,
				// container2 fails to stop entirely (should prevent all image cleanup)
				mockClient.EXPECT().StopAndRemoveContainer(container1, 10*time.Minute).Return(nil)
				mockClient.EXPECT().
					StopAndRemoveContainer(container2, 10*time.Minute).
					Return(errors.New("container stop failed"))

				var cleanupImageInfos []types.RemovedImageInfo
				cleanupOccurred, err := RemoveExcessWatchtowerInstances(
					mockClient,
					true,
					"test-scope",
					&cleanupImageInfos,
					currentContainer,
				)

				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(err.Error()).
					To(gomega.ContainSubstring("1 of 2 instances failed to stop"))
				gomega.Expect(cleanupOccurred).
					To(gomega.Equal(0))
					// No successful cleanups due to partial failure
				gomega.Expect(cleanupImageInfos).
					To(gomega.BeEmpty())
				// Image info cleared on failure
			},
		)

		ginkgo.It(
			"should maintain state consistency when scoped cleanup encounters mixed errors",
			func() {
				mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

				// Create containers with different error scenarios in same scope
				notFoundContainer := createMockContainer(
					"not-found",
					"watchtower-not-found",
					"watchtower:missing",
					true,
					false,
					time.Now().Add(-time.Hour),
					map[string]string{
						"com.centurylinklabs.watchtower":       "true",
						"com.centurylinklabs.watchtower.scope": "error-scope",
					},
				)
				stopErrorContainer := createMockContainer(
					"stop-error",
					"watchtower-stop-error",
					"watchtower:error",
					true,
					false,
					time.Now().Add(-time.Hour),
					map[string]string{
						"com.centurylinklabs.watchtower":       "true",
						"com.centurylinklabs.watchtower.scope": "error-scope",
					},
				)
				currentContainer := createMockContainer(
					"current-scoped",
					"watchtower-current",
					"watchtower:latest",
					true,
					false,
					time.Now(),
					map[string]string{
						"com.centurylinklabs.watchtower":       "true",
						"com.centurylinklabs.watchtower.scope": "error-scope",
					},
				)

				mockClient.EXPECT().
					ListContainers(mock.Anything).
					Return([]types.Container{notFoundContainer, stopErrorContainer, currentContainer}, nil)
				// Mock different error types
				mockClient.EXPECT().
					StopAndRemoveContainer(notFoundContainer, 10*time.Minute).
					Return(cerrdefs.ErrNotFound)
				mockClient.EXPECT().
					StopAndRemoveContainer(stopErrorContainer, 10*time.Minute).
					Return(errors.New("stop failed"))

				var cleanupImageInfos []types.RemovedImageInfo
				cleanupOccurred, err := RemoveExcessWatchtowerInstances(
					mockClient,
					false, // No image cleanup to focus on container removal
					"error-scope",
					&cleanupImageInfos,
					currentContainer,
				)

				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(err.Error()).
					To(gomega.ContainSubstring("1 of 2 instances failed to stop"))
				gomega.Expect(cleanupOccurred).
					To(gomega.Equal(0))
					// State consistency: no partial success reported
				gomega.Expect(cleanupImageInfos).To(gomega.BeEmpty())
			},
		)

		ginkgo.It(
			"should handle scoped cleanup when image removal is interrupted by container failure",
			func() {
				mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

				successContainer := createMockContainer(
					"success",
					"watchtower-success",
					"watchtower:v1",
					true,
					false,
					time.Now().Add(-time.Hour),
					map[string]string{
						"com.centurylinklabs.watchtower":       "true",
						"com.centurylinklabs.watchtower.scope": "interrupt-scope",
					},
				)
				failureContainer := createMockContainer(
					"failure",
					"watchtower-failure",
					"watchtower:v2",
					true,
					false,
					time.Now().Add(-time.Hour),
					map[string]string{
						"com.centurylinklabs.watchtower":       "true",
						"com.centurylinklabs.watchtower.scope": "interrupt-scope",
					},
				)
				currentContainer := createMockContainer(
					"current-interrupt",
					"watchtower-current",
					"watchtower:latest",
					true,
					false,
					time.Now(),
					map[string]string{
						"com.centurylinklabs.watchtower":       "true",
						"com.centurylinklabs.watchtower.scope": "interrupt-scope",
					},
				)

				mockClient.EXPECT().
					ListContainers(mock.Anything).
					Return([]types.Container{successContainer, failureContainer, currentContainer}, nil)
				// Success container stops successfully
				mockClient.EXPECT().
					StopAndRemoveContainer(successContainer, 10*time.Minute).
					Return(nil)
				// Failure container causes interruption (should prevent all image cleanup)
				mockClient.EXPECT().
					StopAndRemoveContainer(failureContainer, 10*time.Minute).
					Return(errors.New("interruption error"))

				var cleanupImageInfos []types.RemovedImageInfo
				cleanupOccurred, err := RemoveExcessWatchtowerInstances(
					mockClient,
					true,
					"interrupt-scope",
					&cleanupImageInfos,
					currentContainer,
				)

				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(cleanupOccurred).To(gomega.Equal(0)) // All operations rolled back
				gomega.Expect(cleanupImageInfos).
					To(gomega.BeEmpty())
				// Image info cleared on any failure
			},
		)

		ginkgo.It("should ensure scope isolation prevents error propagation across scopes", func() {
			mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

			// Scope A container that should fail
			scopeAContainer := createMockContainer(
				"scope-a-fail",
				"watchtower-scope-a",
				"watchtower:fail",
				true,
				false,
				time.Now().Add(-time.Hour),
				map[string]string{
					"com.centurylinklabs.watchtower":       "true",
					"com.centurylinklabs.watchtower.scope": "scope-a",
				},
			)

			currentContainer := createMockContainer(
				"current-a",
				"watchtower-current-a",
				"watchtower:latest",
				true,
				false,
				time.Now(),
				map[string]string{
					"com.centurylinklabs.watchtower":       "true",
					"com.centurylinklabs.watchtower.scope": "scope-a",
				},
			)

			mockClient.EXPECT().
				ListContainers(mock.Anything).
				Return([]types.Container{scopeAContainer, currentContainer}, nil)
			// Only scope-a operations should occur
			mockClient.EXPECT().
				StopAndRemoveContainer(scopeAContainer, 10*time.Minute).
				Return(errors.New("scope-a failure"))

			var cleanupImageInfos []types.RemovedImageInfo
			cleanupOccurred, err := RemoveExcessWatchtowerInstances(
				mockClient,
				false,
				"scope-a", // Only clean scope-a
				&cleanupImageInfos,
				currentContainer,
			)

			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring("1 of 1 instances failed to stop"))
			gomega.Expect(cleanupOccurred).To(gomega.Equal(0))
			gomega.Expect(cleanupImageInfos).To(gomega.BeEmpty())
		})
	})
})

var _ = ginkgo.Describe("CleanupImages", func() {
	ginkgo.It("should do nothing when no images are provided", func() {
		mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

		cleaned, err := RemoveImages(mockClient, nil)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(cleaned).To(gomega.BeEmpty())
	})

	ginkgo.It("should attempt to remove each image ID", func() {
		mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

		cleanedImages := []types.RemovedImageInfo{
			{ImageID: "image1"},
			{ImageID: "image2"},
			{ImageID: ""}, // empty ID should be skipped
		}

		mockClient.EXPECT().RemoveImageByID(types.ImageID("image1"), "").Return(nil)
		mockClient.EXPECT().RemoveImageByID(types.ImageID("image2"), "").Return(nil)

		cleaned, err := RemoveImages(mockClient, cleanedImages)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(cleaned).To(gomega.HaveLen(2))
		gomega.Expect(cleaned[0].ImageID).To(gomega.Equal(types.ImageID("image1")))
		gomega.Expect(cleaned[1].ImageID).To(gomega.Equal(types.ImageID("image2")))
	})

	ginkgo.It("should return error when image removal fails", func() {
		mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

		cleanedImages := []types.RemovedImageInfo{
			{ImageID: "image1"},
			{ImageID: "image2"},
		}

		mockClient.EXPECT().RemoveImageByID(types.ImageID("image1"), "").Return(nil)
		mockClient.EXPECT().
			RemoveImageByID(types.ImageID("image2"), "").
			Return(errors.New("image removal failed"))

		cleaned, err := RemoveImages(mockClient, cleanedImages)
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).
			To(gomega.ContainSubstring("errors occurred during image cleanup"))
		gomega.Expect(cleaned).To(gomega.HaveLen(1))
		gomega.Expect(cleaned[0].ImageID).To(gomega.Equal(types.ImageID("image1")))
	})

	ginkgo.It("should treat 'not found' errors as non-errors and not add to cleaned", func() {
		mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

		cleanedImages := []types.RemovedImageInfo{
			{ImageID: "image1"},
			{ImageID: "image2"},
		}

		mockClient.EXPECT().RemoveImageByID(types.ImageID("image1"), "").Return(nil)
		mockClient.EXPECT().
			RemoveImageByID(types.ImageID("image2"), "").
			Return(cerrdefs.ErrNotFound)

		cleaned, err := RemoveImages(mockClient, cleanedImages)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(cleaned).To(gomega.HaveLen(1))
		gomega.Expect(cleaned[0].ImageID).To(gomega.Equal(types.ImageID("image1")))
	})

	ginkgo.It("should treat 'conflict' errors as non-errors and not add to cleaned", func() {
		mockClient := mockContainer.NewMockClient(ginkgo.GinkgoT())

		cleanedImages := []types.RemovedImageInfo{
			{ImageID: "image1", ImageName: "image1"},
			{ImageID: "image2", ImageName: "image2"},
		}

		mockClient.EXPECT().RemoveImageByID(types.ImageID("image1"), "image1").Return(nil)
		mockClient.EXPECT().
			RemoveImageByID(types.ImageID("image2"), "image2").
			Return(cerrdefs.ErrConflict)

		cleaned, err := RemoveImages(mockClient, cleanedImages)
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
