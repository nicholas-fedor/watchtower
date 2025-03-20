package actions_test

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/internal/actions"
	"github.com/nicholas-fedor/watchtower/pkg/types"

	"github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

func TestActions(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	logrus.SetOutput(ginkgo.GinkgoWriter)
	ginkgo.RunSpecs(t, "Actions Suite")
}

var _ = ginkgo.Describe("the actions package", func() {
	ginkgo.Describe("the check prerequisites method", func() {
		ginkgo.When("given an empty array", func() {
			ginkgo.It("should not do anything", func() {
				client := mocks.CreateMockClient(
					&mocks.TestData{},
					// pullImages:
					false,
					// removeVolumes:
					false,
				)
				gomega.Expect(actions.CheckForMultipleWatchtowerInstances(client, false, "")).To(gomega.Succeed())
			})
		})
		ginkgo.When("given an array of one", func() {
			ginkgo.It("should not do anything", func() {
				client := mocks.CreateMockClient(
					&mocks.TestData{
						Containers: []types.Container{
							mocks.CreateMockContainer(
								"test-container",
								"test-container",
								"watchtower",
								time.Now()),
						},
					},
					// pullImages:
					false,
					// removeVolumes:
					false,
				)
				gomega.Expect(actions.CheckForMultipleWatchtowerInstances(client, false, "")).To(gomega.Succeed())
			})
		})
		ginkgo.When("given multiple containers", func() {
			var client mocks.MockClient
			ginkgo.BeforeEach(func() {
				client = mocks.CreateMockClient(
					&mocks.TestData{
						NameOfContainerToKeep: "test-container-02",
						Containers: []types.Container{
							mocks.CreateMockContainer(
								"test-container-01",
								"test-container-01",
								"watchtower",
								time.Now().AddDate(0, 0, -1)),
							mocks.CreateMockContainer(
								"test-container-02",
								"test-container-02",
								"watchtower",
								time.Now()),
						},
					},
					// pullImages:
					false,
					// removeVolumes:
					false,
				)
			})

			ginkgo.It("should stop all but the latest one", func() {
				err := actions.CheckForMultipleWatchtowerInstances(client, false, "")
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(client.IsContainerRunning(client.TestData.Containers[0])).To(gomega.BeFalse(), "test-container-01 should be stopped")
				gomega.Expect(client.IsContainerRunning(client.TestData.Containers[1])).To(gomega.BeTrue(), "test-container-02 should remain running")
			})
		})
		ginkgo.When("deciding whether to cleanup images", func() {
			var client mocks.MockClient
			ginkgo.BeforeEach(func() {
				client = mocks.CreateMockClient(
					&mocks.TestData{
						Containers: []types.Container{
							mocks.CreateMockContainer(
								"test-container-01",
								"test-container-01",
								"watchtower",
								time.Now().AddDate(0, 0, -1)),
							mocks.CreateMockContainer(
								"test-container-02",
								"test-container-02",
								"watchtower",
								time.Now()),
						},
					},
					// pullImages:
					false,
					// removeVolumes:
					false,
				)
			})
			ginkgo.It("should try to delete the image if the cleanup flag is true", func() {
				err := actions.CheckForMultipleWatchtowerInstances(client, true, "")
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(client.TestData.TriedToRemoveImage()).To(gomega.BeTrue())
			})
			ginkgo.It("should not try to delete the image if the cleanup flag is false", func() {
				err := actions.CheckForMultipleWatchtowerInstances(client, false, "")
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(client.TestData.TriedToRemoveImage()).To(gomega.BeFalse())
			})
		})
	})
})
