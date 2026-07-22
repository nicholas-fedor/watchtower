package actions

import (
	"context"
	"errors"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	dockerContainer "github.com/moby/moby/api/types/container"

	mockActions "github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

var _ = ginkgo.Describe("orchestrateSelfUpdate handoff", func() {
	ginkgo.It("renames old container before create and restores name when create fails", func() {
		old := mockActions.CreateMockContainerWithConfig(
			"wt-old-id",
			"/watchtower",
			"watchtower:v1",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Image: "watchtower:v1",
				Labels: map[string]string{
					"com.centurylinklabs.watchtower": "true",
				},
			},
		)

		client := mockActions.CreateMockClient(
			&mockActions.TestData{
				Containers: []types.Container{old},
				// Mock StartContainer is the create+start path used by the orchestrator.
				StartContainerError: errors.New("simulated create failure"),
			},
			false,
			false,
		)

		err := orchestrateSelfUpdate(
			context.Background(),
			client,
			"wt-old-id",
			"watchtower:v2",
			"watchtower",
			"",
		)

		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to create new container"))
		// Rename off original name, then rename back after create failure.
		gomega.Expect(client.TestData.RenameContainerCount.Load()).To(gomega.Equal(int32(2)))
		gomega.Expect(client.TestData.RenameTargets).To(gomega.HaveLen(2))
		gomega.Expect(client.TestData.RenameTargets[0]).To(gomega.HavePrefix(types.WatchtowerOldPrefix))
		gomega.Expect(client.TestData.RenameTargets[1]).To(gomega.Equal("watchtower"))
		// Old instance must not have been stop/removed before create failed.
		gomega.Expect(client.TestData.StopContainerCount.Load()).To(gomega.Equal(int32(0)))
	})

	ginkgo.It("stops and removes the renamed old container after successful handoff", func() {
		old := mockActions.CreateMockContainerWithConfig(
			"wt-old-id-ok",
			"/watchtower",
			"watchtower:v1",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Image: "watchtower:v1",
				Labels: map[string]string{
					"com.centurylinklabs.watchtower": "true",
				},
			},
		)

		client := mockActions.CreateMockClient(
			&mockActions.TestData{
				Containers: []types.Container{old},
			},
			false,
			false,
		)

		err := orchestrateSelfUpdate(
			context.Background(),
			client,
			"wt-old-id-ok",
			"watchtower:v2",
			"watchtower",
			"",
		)

		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(client.TestData.RenameContainerCount.Load()).To(gomega.Equal(int32(1)))
		gomega.Expect(client.TestData.StartContainerCount.Load()).To(gomega.BeNumerically(">=", int32(1)))
		// Cleanup of renamed predecessor after successful handoff.
		gomega.Expect(client.TestData.StopContainerCount.Load()).To(gomega.BeNumerically(">=", int32(1)))
	})
})
