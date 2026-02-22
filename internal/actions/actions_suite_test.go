package actions_test

import (
	"context"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/sirupsen/logrus"

	dockerContainer "github.com/docker/docker/api/types/container"

	"github.com/nicholas-fedor/watchtower/internal/actions"
	mockActions "github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

func TestActions(t *testing.T) {
	t.Parallel()
	gomega.RegisterFailHandler(ginkgo.Fail)
	logrus.SetOutput(ginkgo.GinkgoWriter)
	logrus.SetLevel(logrus.DebugLevel) // Enable debug logging for tests.
	ginkgo.RunSpecs(t, "Actions Suite")
}

var _ = ginkgo.Describe("the actions package", func() {
	ginkgo.Describe("the check prerequisites method", func() {
		ginkgo.When("given an empty array", func() {
			ginkgo.It("should not do anything", func() {
				mockClient := mockActions.CreateMockClient(
					&mockActions.TestData{},
					false,
					false,
				)

				var cleanupImageIDs []types.RemovedImageInfo

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				cleanupOccurred, err := actions.RemoveExcessWatchtowerInstances(
					ctx,
					mockClient,
					false,
					"",
					&cleanupImageIDs,
					nil,
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(cleanupOccurred).To(gomega.Equal(0))
				gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty())
				gomega.Expect(mockClient.TestData.TriedToRemoveImageCount).To(gomega.Equal(0))
			})
		})
		ginkgo.When("given an array of one", func() {
			ginkgo.It("should not do anything", func() {
				client := mockActions.CreateMockClient(
					&mockActions.TestData{
						Containers: []types.Container{
							mockActions.CreateMockContainerWithConfig(
								"test-container",
								"test-container",
								"watchtower",
								true,
								false,
								time.Now(),
								&dockerContainer.Config{
									Labels: map[string]string{
										"com.centurylinklabs.watchtower": "true",
									},
									ExposedPorts: map[nat.Port]struct{}{},
								},
							),
						},
					},
					false,
					false,
				)

				var cleanupImageIDs []types.RemovedImageInfo

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				cleanupOccurred, err := actions.RemoveExcessWatchtowerInstances(
					ctx,
					client,
					false,
					"",
					&cleanupImageIDs,
					nil,
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(cleanupOccurred).To(gomega.Equal(0))
				gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty())
				gomega.Expect(client.TestData.TriedToRemoveImageCount).To(gomega.Equal(0))
			})
		})
		ginkgo.When("given multiple containers", func() {
			var client mockActions.MockClient

			ginkgo.BeforeEach(func() {
				client = mockActions.CreateMockClient(
					&mockActions.TestData{
						NameOfContainerToKeep: "test-container-02",
						Containers: []types.Container{
							mockActions.CreateMockContainerWithConfig(
								"test-container-01",
								"test-container-01",
								"watchtower:old",
								true,
								false,
								time.Now().AddDate(0, 0, -1),
								&dockerContainer.Config{
									Labels: map[string]string{
										"com.centurylinklabs.watchtower": "true",
									},
									ExposedPorts: map[nat.Port]struct{}{},
								},
							),
							mockActions.CreateMockContainerWithConfig(
								"test-container-02",
								"test-container-02",
								"watchtower:latest",
								true,
								false,
								time.Now(),
								&dockerContainer.Config{
									Labels: map[string]string{
										"com.centurylinklabs.watchtower": "true",
									},
									ExposedPorts: map[nat.Port]struct{}{},
								},
							),
						},
					},
					false,
					false,
				)
			})

			ginkgo.It("should stop all but the latest one", func() {
				var cleanupImageIDs []types.RemovedImageInfo

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				cleanupOccurred, err := actions.RemoveExcessWatchtowerInstances(
					ctx,
					client,
					false,
					"",
					&cleanupImageIDs,
					client.TestData.Containers[1], // current is the latest
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(cleanupOccurred).To(gomega.Equal(1))
				gomega.Expect(client.IsContainerRunning(client.TestData.Containers[0])).
					To(gomega.BeFalse(), "test-container-01 should be stopped")
				gomega.Expect(client.IsContainerRunning(client.TestData.Containers[1])).
					To(gomega.BeTrue(), "test-container-02 should remain running")
				gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty())
				gomega.Expect(client.TestData.TriedToRemoveImageCount).To(gomega.Equal(0))
			})

			ginkgo.It("should collect image IDs and clean up when cleanup is enabled", func() {
				var cleanupImageIDs []types.RemovedImageInfo

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				cleanupOccurred, err := actions.RemoveExcessWatchtowerInstances(
					ctx,
					client,
					true,
					"",
					&cleanupImageIDs,
					client.TestData.Containers[1], // current is the latest
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(cleanupOccurred).To(gomega.Equal(1))
				gomega.Expect(client.IsContainerRunning(client.TestData.Containers[0])).
					To(gomega.BeFalse(), "test-container-01 should be stopped")
				gomega.Expect(client.IsContainerRunning(client.TestData.Containers[1])).
					To(gomega.BeTrue(), "test-container-02 should remain running")
				gomega.Expect(cleanupImageIDs).
					To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("watchtower:old"))))
				gomega.Expect(cleanupImageIDs).To(gomega.HaveLen(1))
				gomega.Expect(client.TestData.TriedToRemoveImageCount).
					To(gomega.Equal(1), "RemoveImageByID should be called for deferred cleanup")
			})
		})
		ginkgo.When("simulating a self-update with excess Watchtower instances", func() {
			var client mockActions.MockClient

			ginkgo.BeforeEach(func() {
				client = mockActions.CreateMockClient(
					&mockActions.TestData{
						NameOfContainerToKeep: "test-container-new",
						Containers: []types.Container{
							mockActions.CreateMockContainerWithConfig(
								"test-container-old",
								"test-container-old",
								"watchtower:1.11.0",
								true,
								false,
								time.Now().AddDate(0, 0, -1),
								&dockerContainer.Config{
									Labels: map[string]string{
										"com.centurylinklabs.watchtower": "true",
									},
								}),
							mockActions.CreateMockContainerWithConfig(
								"test-container-new",
								"test-container-new",
								"watchtower:1.11.1",
								true,
								false,
								time.Now(),
								&dockerContainer.Config{
									Labels: map[string]string{
										"com.centurylinklabs.watchtower": "true",
									},
								}),
						},
					},
					false,
					false,
				)
			})

			ginkgo.It("should stop the old instance and clean up its image", func() {
				var cleanupImageIDs []types.RemovedImageInfo

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				cleanupOccurred, err := actions.RemoveExcessWatchtowerInstances(
					ctx,
					client,
					true,
					"",
					&cleanupImageIDs,
					client.TestData.Containers[1], // current is the new one
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(cleanupOccurred).To(gomega.Equal(1))
				gomega.Expect(client.IsContainerRunning(client.TestData.Containers[0])).
					To(gomega.BeFalse(), "test-container-old should be stopped")
				gomega.Expect(client.IsContainerRunning(client.TestData.Containers[1])).
					To(gomega.BeTrue(), "test-container-new should remain running")
				gomega.Expect(cleanupImageIDs).
					To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("watchtower:1.11.0"))))
				gomega.Expect(cleanupImageIDs).
					To(gomega.HaveLen(1), "cleanupImageIDs should only include old container’s image")
				gomega.Expect(client.TestData.TriedToRemoveImageCount).
					To(gomega.Equal(1), "RemoveImageByID should be called for old image")
			})
		})

		ginkgo.When("unscoped and scoped instances coexist", func() {
			var client mockActions.MockClient

			ginkgo.BeforeEach(func() {
				client = mockActions.CreateMockClient(
					&mockActions.TestData{
						Containers: []types.Container{
							// Unscoped Watchtower (older)
							mockActions.CreateMockContainerWithConfig(
								"unscoped-old",
								"/unscoped-old",
								"watchtower:old",
								true,
								false,
								time.Now().AddDate(0, 0, -1),
								&dockerContainer.Config{
									Labels: map[string]string{
										"com.centurylinklabs.watchtower": "true",
									},
									ExposedPorts: map[nat.Port]struct{}{},
								},
							),
							// Scoped Watchtower (should be ignored by unscoped cleanup)
							mockActions.CreateMockContainerWithConfig(
								"scoped-new",
								"/scoped-new",
								"watchtower:new",
								true,
								false,
								time.Now(),
								&dockerContainer.Config{
									Labels: map[string]string{
										"com.centurylinklabs.watchtower":       "true",
										"com.centurylinklabs.watchtower.scope": "prod",
									},
									ExposedPorts: map[nat.Port]struct{}{},
								},
							),
							// Unscoped Watchtower (newer)
							mockActions.CreateMockContainerWithConfig(
								"unscoped-new",
								"/unscoped-new",
								"watchtower:latest",
								true,
								false,
								time.Now(),
								&dockerContainer.Config{
									Labels: map[string]string{
										"com.centurylinklabs.watchtower": "true",
									},
									ExposedPorts: map[nat.Port]struct{}{},
								},
							),
						},
					},
					false,
					false,
				)
			})

			ginkgo.It("should only clean up unscoped instances when scope is empty", func() {
				var cleanupImageIDs []types.RemovedImageInfo

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				cleanupOccurred, err := actions.RemoveExcessWatchtowerInstances(
					ctx,
					client,
					false,
					"",
					&cleanupImageIDs,
					client.TestData.Containers[2], // current is the newer unscoped
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(cleanupOccurred).To(gomega.Equal(1))

				// Should stop the older unscoped instance
				gomega.Expect(client.IsContainerRunning(client.TestData.Containers[0])).
					To(gomega.BeFalse(), "unscoped-old should be stopped")
				// Should keep the scoped instance running (not affected by unscoped cleanup)
				gomega.Expect(client.IsContainerRunning(client.TestData.Containers[1])).
					To(gomega.BeTrue(), "scoped-new should remain running")
				// Should keep the newer unscoped instance running
				gomega.Expect(client.IsContainerRunning(client.TestData.Containers[2])).
					To(gomega.BeTrue(), "unscoped-new should remain running")
				gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty())
				gomega.Expect(client.TestData.TriedToRemoveImageCount).To(gomega.Equal(0))
			})

			ginkgo.It("should clean up within scoped instances when scope is specified", func() {
				var cleanupImageIDs []types.RemovedImageInfo

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				cleanupOccurred, err := actions.RemoveExcessWatchtowerInstances(
					ctx,
					client,
					false,
					"prod",
					&cleanupImageIDs,
					client.TestData.Containers[1], // current is the scoped one
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(cleanupOccurred).To(gomega.Equal(0))

				// Scoped cleanup should only see the scoped container, so no cleanup needed
				gomega.Expect(client.IsContainerRunning(client.TestData.Containers[0])).
					To(gomega.BeTrue(), "unscoped-old should remain running")
				gomega.Expect(client.IsContainerRunning(client.TestData.Containers[1])).
					To(gomega.BeTrue(), "scoped-new should remain running")
				gomega.Expect(client.IsContainerRunning(client.TestData.Containers[2])).
					To(gomega.BeTrue(), "unscoped-new should remain running")
				gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty())
				gomega.Expect(client.TestData.TriedToRemoveImageCount).To(gomega.Equal(0))
			})
		})
	})
})
