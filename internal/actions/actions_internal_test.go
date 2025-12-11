package actions

import (
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

var _ = ginkgo.Describe("restartStaleContainer", func() {
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
		params := types.UpdateParams{
			RunOnce: true,
		}
		container := client.TestData.Containers[0]
		newID, renamed, err := restartStaleContainer(container, client, params)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(renamed).To(gomega.BeFalse())
		gomega.Expect(client.TestData.RenameContainerCount).To(gomega.Equal(0))
		gomega.Expect(newID).NotTo(gomega.BeEmpty())
	})

	ginkgo.It("should rename Watchtower container when not in run-once mode", func() {
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
		params := types.UpdateParams{
			RunOnce: false,
		}
		container := client.TestData.Containers[0]
		newID, renamed, err := restartStaleContainer(container, client, params)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(renamed).To(gomega.BeTrue())
		gomega.Expect(client.TestData.RenameContainerCount).To(gomega.Equal(1))
		gomega.Expect(newID).NotTo(gomega.BeEmpty())
	})
})
