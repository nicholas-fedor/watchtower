package actions_test

import (
	"context"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	dockerContainer "github.com/docker/docker/api/types/container"

	"github.com/nicholas-fedor/watchtower/internal/actions"
	mockActions "github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// createDefaultMockContainer wraps mockActions.CreateMockContainerWithConfig with
// common defaults for ephemeral self-update tests. It sets the container to running,
// not restarting, with a current timestamp and the provided labels.
//
// Parameters:
//   - id: Container ID for the mock.
//   - labels: Label map to apply to the container config.
//
// Returns:
//   - types.Container: Configured mock container.
func createDefaultMockContainer(id string, labels map[string]string) types.Container {
	return mockActions.CreateMockContainerWithConfig(
		id,
		"watchtower",
		"watchtower:latest",
		true,
		false,
		time.Now(),
		&dockerContainer.Config{Labels: labels},
	)
}

// createDefaultMockClient wraps mockActions.CreateMockClient with common defaults
// for ephemeral self-update tests. It uses an empty TestData, no image pulling,
// and no volume removal.
//
// Parameters:
//   - td: Test data structure for capturing orchestrator parameters.
//
// Returns:
//   - mockActions.MockClient: Configured mock client.
func createDefaultMockClient(td *mockActions.TestData) mockActions.MockClient {
	return mockActions.CreateMockClient(td, false, false)
}

var _ = ginkgo.Describe("EphemeralSelfUpdate", func() {
	ginkgo.When("the orchestrator is created successfully", func() {
		ginkgo.It("should return empty container ID and false (not renamed)", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			sourceContainer := createDefaultMockContainer("source-123", map[string]string{
				"com.centurylinklabs.watchtower": "true",
			})

			client := createDefaultMockClient(&mockActions.TestData{})

			newID, renamed, err := actions.EphemeralSelfUpdate(
				ctx,
				client,
				sourceContainer,
				types.UpdateParams{},
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(newID).To(gomega.BeEmpty())
			gomega.Expect(renamed).To(gomega.BeFalse())
		})
	})

	ginkgo.When("the source container has no existing chain", func() {
		ginkgo.It("should use the source container ID as the new chain", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			sourceContainer := createDefaultMockContainer("source-456", map[string]string{
				"com.centurylinklabs.watchtower": "true",
			})

			client := createDefaultMockClient(&mockActions.TestData{})

			newID, renamed, err := actions.EphemeralSelfUpdate(
				ctx,
				client,
				sourceContainer,
				types.UpdateParams{},
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(newID).To(gomega.BeEmpty())
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

			sourceContainer := createDefaultMockContainer("source-789", map[string]string{
				"com.centurylinklabs.watchtower":                 "true",
				"com.centurylinklabs.watchtower.container-chain": existingChain,
			})

			// Verify the mock container has the expected chain label.
			chain, present := sourceContainer.GetContainerChain()
			gomega.Expect(present).To(gomega.BeTrue())
			gomega.Expect(chain).To(gomega.Equal(existingChain))

			client := createDefaultMockClient(&mockActions.TestData{})

			newID, renamed, err := actions.EphemeralSelfUpdate(
				ctx,
				client,
				sourceContainer,
				types.UpdateParams{},
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(newID).To(gomega.BeEmpty())
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

			sourceContainer := createDefaultMockContainer("source-err", map[string]string{
				"com.centurylinklabs.watchtower": "true",
			})

			// Use an already-cancelled client context to force CreateEphemeralOrchestrator to fail.
			cancelledCtx, cancelCancelled := context.WithCancel(context.Background())
			cancelCancelled()

			// Note: CreateMockClientWithContext is used here instead of createDefaultMockClient
			// because this test requires a pre-cancelled context to trigger the failure path.
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
