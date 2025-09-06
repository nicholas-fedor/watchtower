package actions_test

import (
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/internal/actions"
	"github.com/nicholas-fedor/watchtower/internal/actions/mocks"
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
				mockClient := mocks.CreateMockClient(
					&mocks.TestData{},
					false,
					false,
				)
				cleanupImageIDs := make(map[types.ImageID]bool)
				gomega.Expect(actions.CheckForMultipleWatchtowerInstances(mockClient, false, "", cleanupImageIDs)).
					To(gomega.Succeed())
				gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty())
				gomega.Expect(mockClient.TestData.TriedToRemoveImageCount).To(gomega.Equal(0))
			})
		})
		ginkgo.When("given an array of one", func() {
			ginkgo.It("should not do anything", func() {
				client := mocks.CreateMockClient(
					&mocks.TestData{
						Containers: []types.Container{
							mocks.CreateMockContainerWithConfig(
								"test-container",
								"test-container",
								"watchtower",
								true,
								false,
								time.Now(),
								&container.Config{
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
				cleanupImageIDs := make(map[types.ImageID]bool)
				gomega.Expect(actions.CheckForMultipleWatchtowerInstances(client, false, "", cleanupImageIDs)).
					To(gomega.Succeed())
				gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty())
				gomega.Expect(client.TestData.TriedToRemoveImageCount).To(gomega.Equal(0))
			})
		})
		ginkgo.When("given multiple containers", func() {
			var client mocks.MockClient
			ginkgo.BeforeEach(func() {
				client = mocks.CreateMockClient(
					&mocks.TestData{
						NameOfContainerToKeep: "test-container-02",
						Containers: []types.Container{
							mocks.CreateMockContainerWithConfig(
								"test-container-01",
								"test-container-01",
								"watchtower:old",
								true,
								false,
								time.Now().AddDate(0, 0, -1),
								&container.Config{
									Labels: map[string]string{
										"com.centurylinklabs.watchtower": "true",
									},
									ExposedPorts: map[nat.Port]struct{}{},
								},
							),
							mocks.CreateMockContainerWithConfig(
								"test-container-02",
								"test-container-02",
								"watchtower:latest",
								true,
								false,
								time.Now(),
								&container.Config{
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
				cleanupImageIDs := make(map[types.ImageID]bool)
				err := actions.CheckForMultipleWatchtowerInstances(
					client,
					false,
					"",
					cleanupImageIDs,
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(client.IsContainerRunning(client.TestData.Containers[0])).
					To(gomega.BeFalse(), "test-container-01 should be stopped")
				gomega.Expect(client.IsContainerRunning(client.TestData.Containers[1])).
					To(gomega.BeTrue(), "test-container-02 should remain running")
				gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty())
				gomega.Expect(client.TestData.TriedToRemoveImageCount).To(gomega.Equal(0))
			})

			ginkgo.It("should collect image IDs and clean up when cleanup is enabled", func() {
				cleanupImageIDs := make(map[types.ImageID]bool)
				err := actions.CheckForMultipleWatchtowerInstances(
					client,
					true,
					"",
					cleanupImageIDs,
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(client.IsContainerRunning(client.TestData.Containers[0])).
					To(gomega.BeFalse(), "test-container-01 should be stopped")
				gomega.Expect(client.IsContainerRunning(client.TestData.Containers[1])).
					To(gomega.BeTrue(), "test-container-02 should remain running")
				gomega.Expect(cleanupImageIDs).
					To(gomega.HaveKey(types.ImageID("watchtower:old")))
				gomega.Expect(cleanupImageIDs).To(gomega.HaveLen(1))
				gomega.Expect(client.TestData.TriedToRemoveImageCount).
					To(gomega.Equal(1), "RemoveImageByID should be called for deferred cleanup")
			})
		})
		ginkgo.When("simulating a self-update with excess Watchtower instances", func() {
			var client mocks.MockClient
			ginkgo.BeforeEach(func() {
				client = mocks.CreateMockClient(
					&mocks.TestData{
						NameOfContainerToKeep: "test-container-new",
						Containers: []types.Container{
							mocks.CreateMockContainerWithConfig(
								"test-container-old",
								"test-container-old",
								"watchtower:1.11.0",
								true,
								false,
								time.Now().AddDate(0, 0, -1),
								&container.Config{
									Labels: map[string]string{
										"com.centurylinklabs.watchtower": "true",
									},
								}),
							mocks.CreateMockContainerWithConfig(
								"test-container-new",
								"test-container-new",
								"watchtower:1.11.1",
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
			})

			ginkgo.It("should stop the old instance and clean up its image", func() {
				cleanupImageIDs := make(map[types.ImageID]bool)
				err := actions.CheckForMultipleWatchtowerInstances(
					client,
					true,
					"",
					cleanupImageIDs,
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(client.IsContainerRunning(client.TestData.Containers[0])).
					To(gomega.BeFalse(), "test-container-old should be stopped")
				gomega.Expect(client.IsContainerRunning(client.TestData.Containers[1])).
					To(gomega.BeTrue(), "test-container-new should remain running")
				gomega.Expect(cleanupImageIDs).
					To(gomega.HaveKey(types.ImageID("watchtower:1.11.0")))
				gomega.Expect(cleanupImageIDs).
					To(gomega.HaveLen(1), "cleanupImageIDs should only include old container’s image")
				gomega.Expect(client.TestData.TriedToRemoveImageCount).
					To(gomega.Equal(1), "RemoveImageByID should be called for old image")
			})
		})
	})
})
