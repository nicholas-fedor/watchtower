package actions_test

import (
	"context"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	dockerContainer "github.com/docker/docker/api/types/container"

	"github.com/nicholas-fedor/watchtower/internal/actions"
	mockActions "github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

var _ = ginkgo.Describe("EphemeralSelfUpdate", func() {
	ginkgo.When("the orchestrator is created successfully", func() {
		ginkgo.It("should return the orchestrator ID and false (not renamed)", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			sourceContainer := mockActions.CreateMockContainerWithConfig(
				"source-123",
				"watchtower",
				"watchtower:latest",
				true,
				false,
				time.Now(),
				&dockerContainer.Config{
					Labels: map[string]string{
						"com.centurylinklabs.watchtower": "true",
					},
				},
			)

			client := mockActions.CreateMockClient(
				&mockActions.TestData{},
				false,
				false,
			)

			orchID, renamed, err := actions.EphemeralSelfUpdate(
				ctx,
				client,
				sourceContainer,
				types.UpdateParams{},
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(orchID).To(gomega.Equal(types.ContainerID("mock-ephemeral-orchestrator")))
			gomega.Expect(renamed).To(gomega.BeFalse())
		})
	})

	ginkgo.When("the source container has no existing chain", func() {
		ginkgo.It("should use the source container ID as the new chain", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			sourceContainer := mockActions.CreateMockContainerWithConfig(
				"source-456",
				"watchtower",
				"watchtower:latest",
				true,
				false,
				time.Now(),
				&dockerContainer.Config{
					Labels: map[string]string{
						"com.centurylinklabs.watchtower": "true",
					},
				},
			)

			client := mockActions.CreateMockClient(
				&mockActions.TestData{},
				false,
				false,
			)

			orchID, renamed, err := actions.EphemeralSelfUpdate(
				ctx,
				client,
				sourceContainer,
				types.UpdateParams{},
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(orchID).NotTo(gomega.BeEmpty())
			gomega.Expect(renamed).To(gomega.BeFalse())

			// Verify the orchestrator's chain was set to the source container ID.
			gomega.Expect(client.TestData.LastContainerChain).To(
				gomega.Equal("source-456"),
				"orchestrator chain should be the source container ID when no existing chain is present",
			)
		})
	})

	ginkgo.When("the source container has an existing chain", func() {
		ginkgo.It("should append the source ID to the existing chain", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			existingChain := "old-id-1,old-id-2"

			sourceContainer := mockActions.CreateMockContainerWithConfig(
				"source-789",
				"watchtower",
				"watchtower:latest",
				true,
				false,
				time.Now(),
				&dockerContainer.Config{
					Labels: map[string]string{
						"com.centurylinklabs.watchtower":                 "true",
						"com.centurylinklabs.watchtower.container-chain": existingChain,
					},
				},
			)

			// Verify the mock container has the expected chain label.
			c, ok := sourceContainer.(*container.Container)
			gomega.Expect(ok).To(gomega.BeTrue(), "sourceContainer should be *container.Container")

			chain, present := c.GetContainerChain()
			gomega.Expect(present).To(gomega.BeTrue())
			gomega.Expect(chain).To(gomega.Equal(existingChain))

			client := mockActions.CreateMockClient(
				&mockActions.TestData{},
				false,
				false,
			)

			orchID, renamed, err := actions.EphemeralSelfUpdate(
				ctx,
				client,
				sourceContainer,
				types.UpdateParams{},
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(orchID).NotTo(gomega.BeEmpty())
			gomega.Expect(renamed).To(gomega.BeFalse())

			// Verify the orchestrator's chain has the source ID appended to the existing chain.
			gomega.Expect(client.TestData.LastContainerChain).To(
				gomega.Equal("old-id-1,old-id-2,source-789"),
				"orchestrator chain should have source container ID appended to existing chain",
			)
		})
	})

	ginkgo.When("CreateEphemeralOrchestrator fails", func() {
		ginkgo.It("should return an error containing the orchestrator failure message", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			sourceContainer := mockActions.CreateMockContainerWithConfig(
				"source-err",
				"watchtower",
				"watchtower:latest",
				true,
				false,
				time.Now(),
				&dockerContainer.Config{
					Labels: map[string]string{
						"com.centurylinklabs.watchtower": "true",
					},
				},
			)

			// Use an already-cancelled client context to force CreateEphemeralOrchestrator to fail.
			cancelledCtx, cancelCancelled := context.WithCancel(context.Background())
			cancelCancelled()

			client := mockActions.CreateMockClientWithContext(
				cancelledCtx,
				&mockActions.TestData{},
				false,
				false,
			)

			_, _, err := actions.EphemeralSelfUpdate(
				ctx,
				client,
				sourceContainer,
				types.UpdateParams{},
			)

			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("ephemeral orchestrator failed"))
		})
	})
})
