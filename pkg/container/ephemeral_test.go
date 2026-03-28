package container

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"

	dockerContainer "github.com/docker/docker/api/types/container"
	dockerClient "github.com/docker/docker/client"
	mock "github.com/stretchr/testify/mock"

	mockContainer "github.com/nicholas-fedor/watchtower/pkg/container/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

var _ = ginkgo.Describe("Ephemeral Orchestrator", func() {
	ginkgo.Describe("buildOrchestratorConfig", func() {
		ginkgo.When("given a source container, new image, and container chain", func() {
			ginkgo.It("should return a config with correct image, command, environment, and labels", func() {
				source := MockContainer(
					WithID("abc123def456"),
					WithName("watchtower-test"),
					WithImageName("watchtower:latest"),
				)

				config := buildOrchestratorConfig(source, "watchtower:v2", "old1,old2")

				gomega.Expect(config).NotTo(gomega.BeNil())
				gomega.Expect(config.Image).To(gomega.Equal("watchtower:v2"))
				gomega.Expect(config.Cmd).To(gomega.HaveLen(1))
				gomega.Expect(config.Cmd).To(gomega.ContainElement("--self-update-orchestrator"))

				// Verify environment variables are correctly set.
				gomega.Expect(config.Env).To(gomega.ContainElement(
					"WT_ORCHESTRATOR_OLD_ID=abc123def456",
				))
				gomega.Expect(config.Env).To(gomega.ContainElement(
					"WT_ORCHESTRATOR_NEW_IMAGE=watchtower:v2",
				))
				gomega.Expect(config.Env).To(gomega.ContainElement(
					"WT_ORCHESTRATOR_ORIGINAL_NAME=watchtower-test",
				))
				gomega.Expect(config.Env).To(gomega.ContainElement(
					"WT_ORCHESTRATOR_CONTAINER_CHAIN=old1,old2",
				))

				// Verify the orchestrator label is set but the watchtower label is NOT set.
				gomega.Expect(config.Labels).To(gomega.HaveKeyWithValue(OrchestratorLabel, "true"))
				gomega.Expect(config.Labels).NotTo(gomega.HaveKey("com.centurylinklabs.watchtower"))
			})
		})

		ginkgo.When("the container chain is empty", func() {
			ginkgo.It("should include an empty container chain env var", func() {
				source := MockContainer(
					WithID("abc123"),
					WithName("watchtower"),
					WithImageName("watchtower:latest"),
				)

				config := buildOrchestratorConfig(source, "watchtower:v2", "")

				gomega.Expect(config.Env).To(gomega.ContainElement(
					"WT_ORCHESTRATOR_CONTAINER_CHAIN=",
				))
			})
		})
	})

	ginkgo.Describe("buildOrchestratorHostConfig", func() {
		ginkgo.It("should return a host config with AutoRemove and Docker socket mount", func() {
			hostConfig := buildOrchestratorHostConfig()

			gomega.Expect(hostConfig).NotTo(gomega.BeNil())
			gomega.Expect(hostConfig.AutoRemove).To(gomega.BeTrue())
			gomega.Expect(hostConfig.Binds).To(gomega.Equal(
				[]string{"/var/run/docker.sock:/var/run/docker.sock"},
			))
		})

		ginkgo.It("should not set port bindings", func() {
			hostConfig := buildOrchestratorHostConfig()

			gomega.Expect(hostConfig.PortBindings).To(gomega.BeNil())
		})

		ginkgo.It("should not set a restart policy", func() {
			hostConfig := buildOrchestratorHostConfig()

			gomega.Expect(hostConfig.RestartPolicy.Name).To(gomega.BeEmpty())
		})
	})

	ginkgo.Describe("CreateEphemeralOrchestrator", func() {
		var (
			mockServer *ghttp.Server
			dockerAPI  dockerClient.APIClient
			testClient *client
			source     types.Container
			ctx        context.Context
		)

		ginkgo.BeforeEach(func() {
			ctx = context.Background()
			mockServer = ghttp.NewServer()
			docker, err := dockerClient.NewClientWithOpts(
				dockerClient.WithHost(mockServer.URL()),
				dockerClient.WithHTTPClient(mockServer.HTTPTestServer.Client()),
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			dockerAPI = docker
			testClient = &client{api: dockerAPI}
			source = MockContainer(
				WithID("source123"),
				WithName("watchtower-source"),
				WithImageName("watchtower:latest"),
			)
		})

		ginkgo.AfterEach(func() {
			mockServer.Close()
		})

		ginkgo.When("creation and start succeed", func() {
			ginkgo.BeforeEach(func() {
				mockServer.AppendHandlers(
					// ContainerCreate handler.
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("POST", gomega.HaveSuffix("/containers/create")),
						func(w http.ResponseWriter, r *http.Request) {
							w.Header().Set("Content-Type", "application/json")
							w.WriteHeader(http.StatusCreated)
							json.NewEncoder(w).Encode(dockerContainer.CreateResponse{
								ID: "orchestrator-id-123",
							})
						},
					),
					// ContainerStart handler.
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("POST", gomega.HaveSuffix("/containers/orchestrator-id-123/start")),
						func(w http.ResponseWriter, _ *http.Request) {
							w.WriteHeader(http.StatusNoContent)
						},
					),
				)
			})

			ginkgo.It("should return the orchestrator container ID", func() {
				orchestratorID, err := testClient.CreateEphemeralOrchestrator(
					ctx, source, "watchtower:v2", "chain1",
				)

				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(orchestratorID).To(gomega.Equal(types.ContainerID("orchestrator-id-123")))
			})
		})

		ginkgo.When("ContainerCreate fails", func() {
			ginkgo.BeforeEach(func() {
				mockServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("POST", gomega.HaveSuffix("/containers/create")),
						func(w http.ResponseWriter, _ *http.Request) {
							w.WriteHeader(http.StatusInternalServerError)
						},
					),
				)
			})

			ginkgo.It("should return an error wrapping ErrEphemeralCreateFailed", func() {
				orchestratorID, err := testClient.CreateEphemeralOrchestrator(
					ctx, source, "watchtower:v2", "chain1",
				)

				gomega.Expect(err).To(gomega.MatchError(gomega.ContainSubstring(
					ErrEphemeralCreateFailed.Error(),
				)))
				gomega.Expect(orchestratorID).To(gomega.BeEmpty())
			})
		})

		ginkgo.When("ContainerStart fails", func() {
			ginkgo.BeforeEach(func() {
				mockServer.AppendHandlers(
					// ContainerCreate succeeds.
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("POST", gomega.HaveSuffix("/containers/create")),
						func(w http.ResponseWriter, r *http.Request) {
							w.Header().Set("Content-Type", "application/json")
							w.WriteHeader(http.StatusCreated)
							json.NewEncoder(w).Encode(dockerContainer.CreateResponse{
								ID: "orchestrator-fail-start",
							})
						},
					),
					// ContainerStart fails.
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("POST", gomega.HaveSuffix("/containers/orchestrator-fail-start/start")),
						func(w http.ResponseWriter, _ *http.Request) {
							w.WriteHeader(http.StatusInternalServerError)
						},
					),
					// ContainerRemove for cleanup.
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("DELETE", gomega.HaveSuffix("/containers/orchestrator-fail-start")),
						func(w http.ResponseWriter, _ *http.Request) {
							w.WriteHeader(http.StatusNoContent)
						},
					),
				)
			})

			ginkgo.It("should attempt cleanup and return ErrEphemeralStartFailed", func() {
				orchestratorID, err := testClient.CreateEphemeralOrchestrator(
					ctx, source, "watchtower:v2", "chain1",
				)

				gomega.Expect(err).To(gomega.MatchError(gomega.ContainSubstring(
					ErrEphemeralStartFailed.Error(),
				)))
				gomega.Expect(orchestratorID).To(gomega.BeEmpty())
				// Verify all handlers were called (including cleanup remove).
				gomega.Expect(mockServer.ReceivedRequests()).To(gomega.HaveLen(3))
			})
		})
	})

	ginkgo.Describe("RemoveOrphanedOrchestrators", func() {
		var ctx context.Context

		ginkgo.BeforeEach(func() {
			ctx = context.Background()
		})

		ginkgo.When("there are no containers", func() {
			ginkgo.It("should return 0 removed and no error", func() {
				mockAPIClient := mockContainer.NewMockClient(ginkgo.GinkgoT())
				mockAPIClient.EXPECT().
					ListContainers(ctx).
					Return([]types.Container{}, nil)

				count, err := RemoveOrphanedOrchestrators(ctx, mockAPIClient)

				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(count).To(gomega.Equal(0))
			})
		})

		ginkgo.When("there are containers but none are orchestrators", func() {
			ginkgo.It("should return 0 removed and no error", func() {
				regularContainer := MockContainer(
					WithID("regular123"),
					WithName("regular-container"),
					WithLabels(map[string]string{
						"com.centurylinklabs.watchtower": "true",
					}),
				)

				mockAPIClient := mockContainer.NewMockClient(ginkgo.GinkgoT())
				mockAPIClient.EXPECT().
					ListContainers(ctx).
					Return([]types.Container{regularContainer}, nil)

				count, err := RemoveOrphanedOrchestrators(ctx, mockAPIClient)

				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(count).To(gomega.Equal(0))
			})
		})

		ginkgo.When("there is an orphaned orchestrator", func() {
			ginkgo.It("should remove the orchestrator and return 1", func() {
				orchestrator := MockContainer(
					WithID("orch123"),
					WithName("watchtower-orchestrator-abc"),
					WithLabels(map[string]string{
						OrchestratorLabel: "true",
					}),
				)

				mockAPIClient := mockContainer.NewMockClient(ginkgo.GinkgoT())
				mockAPIClient.EXPECT().
					ListContainers(ctx).
					Return([]types.Container{orchestrator}, nil)
				mockAPIClient.EXPECT().
					StopAndRemoveContainer(
						ctx,
						orchestrator,
						mock.AnythingOfType("time.Duration"),
					).
					Return(nil)

				count, err := RemoveOrphanedOrchestrators(ctx, mockAPIClient)

				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(count).To(gomega.Equal(1))
			})
		})

		ginkgo.When("there are multiple orphaned orchestrators", func() {
			ginkgo.It("should remove all orchestrators and return the count", func() {
				orch1 := MockContainer(
					WithID("orch001"),
					WithName("watchtower-orchestrator-001"),
					WithLabels(map[string]string{
						OrchestratorLabel: "true",
					}),
				)
				orch2 := MockContainer(
					WithID("orch002"),
					WithName("watchtower-orchestrator-002"),
					WithLabels(map[string]string{
						OrchestratorLabel: "true",
					}),
				)
				regular := MockContainer(
					WithID("regular1"),
					WithName("app-container"),
				)

				mockAPIClient := mockContainer.NewMockClient(ginkgo.GinkgoT())
				mockAPIClient.EXPECT().
					ListContainers(ctx).
					Return([]types.Container{orch1, regular, orch2}, nil)
				mockAPIClient.EXPECT().
					StopAndRemoveContainer(
						ctx,
						orch1,
						mock.AnythingOfType("time.Duration"),
					).
					Return(nil)
				mockAPIClient.EXPECT().
					StopAndRemoveContainer(
						ctx,
						orch2,
						mock.AnythingOfType("time.Duration"),
					).
					Return(nil)

				count, err := RemoveOrphanedOrchestrators(ctx, mockAPIClient)

				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(count).To(gomega.Equal(2))
			})
		})

		ginkgo.When("ListContainers fails", func() {
			ginkgo.It("should return an error", func() {
				mockAPIClient := mockContainer.NewMockClient(ginkgo.GinkgoT())
				mockAPIClient.EXPECT().
					ListContainers(ctx).
					Return(nil, errors.New("docker daemon unavailable"))

				count, err := RemoveOrphanedOrchestrators(ctx, mockAPIClient)

				gomega.Expect(err).To(gomega.MatchError(gomega.ContainSubstring(
					"failed to list containers",
				)))
				gomega.Expect(count).To(gomega.Equal(0))
			})
		})

		ginkgo.When("StopAndRemoveContainer fails for one orchestrator", func() {
			ginkgo.It("should continue and remove others, returning partial count", func() {
				orch1 := MockContainer(
					WithID("orch001"),
					WithName("watchtower-orchestrator-001"),
					WithLabels(map[string]string{
						OrchestratorLabel: "true",
					}),
				)
				orch2 := MockContainer(
					WithID("orch002"),
					WithName("watchtower-orchestrator-002"),
					WithLabels(map[string]string{
						OrchestratorLabel: "true",
					}),
				)

				mockAPIClient := mockContainer.NewMockClient(ginkgo.GinkgoT())
				mockAPIClient.EXPECT().
					ListContainers(ctx).
					Return([]types.Container{orch1, orch2}, nil)
				mockAPIClient.EXPECT().
					StopAndRemoveContainer(
						ctx,
						orch1,
						mock.AnythingOfType("time.Duration"),
					).
					Return(errors.New("removal failed"))
				mockAPIClient.EXPECT().
					StopAndRemoveContainer(
						ctx,
						orch2,
						mock.AnythingOfType("time.Duration"),
					).
					Return(nil)

				count, err := RemoveOrphanedOrchestrators(ctx, mockAPIClient)

				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(count).To(gomega.Equal(1))
			})
		})

		ginkgo.When("a container has no orchestrator label", func() {
			ginkgo.It("should skip the container without error", func() {
				noLabelContainer := MockContainer(
					WithID("no-label"),
					WithName("no-label-container"),
				)

				mockAPIClient := mockContainer.NewMockClient(ginkgo.GinkgoT())
				mockAPIClient.EXPECT().
					ListContainers(ctx).
					Return([]types.Container{noLabelContainer}, nil)

				count, err := RemoveOrphanedOrchestrators(ctx, mockAPIClient)

				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(count).To(gomega.Equal(0))
			})
		})
	})
})
