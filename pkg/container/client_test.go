package container

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/ghttp"
	"github.com/sirupsen/logrus"
	"github.com/spf13/afero"

	dockerBackend "github.com/docker/docker/api/types/backend"
	dockerContainer "github.com/docker/docker/api/types/container"
	dockerImage "github.com/docker/docker/api/types/image"
	dockerClient "github.com/docker/docker/client"
	gomegaTypes "github.com/onsi/gomega/types"

	mockContainer "github.com/nicholas-fedor/watchtower/pkg/container/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/filters"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

const (
	methodPost   = "POST"
	methodDelete = "DELETE"
	pingPath     = "/_ping"
)

var _ = ginkgo.Describe("the client", func() {
	var (
		docker     *dockerClient.Client
		mockServer *ghttp.Server
	)

	// Set up a mock Docker server before each test.

	ginkgo.BeforeEach(func() {
		mockServer = ghttp.NewServer()
		docker, _ = dockerClient.NewClientWithOpts(
			dockerClient.WithHost(mockServer.URL()),
			dockerClient.WithHTTPClient(mockServer.HTTPTestServer.Client()))
	})

	// Clean up the mock server after each test.
	ginkgo.AfterEach(func() {
		mockServer.Close()
	})

	// Test suite for stopping and removing a running container.
	ginkgo.When("removing a running container", func() {
		ginkgo.When("the container still exists after stopping", func() {
			ginkgo.It("should attempt to remove the container", func() {
				// Create a mock mockedContainer in running state.
				mockedContainer := MockContainer(
					WithContainerState(dockerContainer.State{Running: true}),
				)
				cid := mockedContainer.ContainerInfo().ID
				// Set up mock server handlers for stop and remove operations.
				mockServer.AppendHandlers(
					StopContainerHandler(
						cid,
						mockContainer.Found,
					), // Simulate successful stop
					mockContainer.RemoveContainerHandler(
						cid,
						mockContainer.Found,
					), // Simulate successful removal
				)
				// Execute StopAndRemoveContainer and verify no error occurs.
				err := client{api: docker}.StopAndRemoveContainer(mockedContainer, time.Second)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			})
		})

		ginkgo.When("the container does not exist after stopping", func() {
			ginkgo.It("should not cause an error", func() {
				// Create a mock container in running state.
				mockedContainer := MockContainer(
					WithContainerState(dockerContainer.State{Running: true}),
				)
				cid := mockedContainer.ContainerInfo().ID
				// Set up mock server handlers for stop and removal.
				mockServer.AppendHandlers(
					StopContainerHandler(
						cid,
						mockContainer.Found,
					), // Simulate successful stop
					mockContainer.RemoveContainerHandler(
						cid,
						mockContainer.Missing,
					), // Removal fails gracefully
				)
				// Execute StopAndRemoveContainer and verify no error occurs.
				err := client{api: docker}.StopAndRemoveContainer(mockedContainer, time.Second)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			})
		})

		ginkgo.When("stopping fails with an unexpected error", func() {
			ginkgo.It("should return an error", func() {
				// Create a mock container in running state.
				mockedContainer := MockContainer(
					WithContainerState(dockerContainer.State{Running: true}),
				)
				cid := mockedContainer.ContainerInfo().ID
				// Set up mock server handler for stop failure.
				mockServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest(
							methodPost,
							gomega.HaveSuffix(fmt.Sprintf("containers/%s/stop", cid)),
						),
						ghttp.RespondWith(http.StatusInternalServerError, "server error"),
					),
				)
				// Execute StopContainer and verify the error is propagated.
				err := client{api: docker}.StopContainer(mockedContainer, time.Second)
				gomega.Expect(err).
					To(gomega.MatchError(gomega.ContainSubstring("failed to stop container: Error response from daemon: server error")))
			})
		})

		ginkgo.When("stopping fails with an unexpected error in StopAndRemoveContainer", func() {
			ginkgo.It("should return an error without attempting removal", func() {
				// Create a mock container in running state.
				mockedContainer := MockContainer(
					WithContainerState(dockerContainer.State{Running: true}),
				)
				cid := mockedContainer.ContainerInfo().ID
				// Set up mock server handler for stop failure (no remove handler needed).
				mockServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest(
							methodPost,
							gomega.HaveSuffix(fmt.Sprintf("containers/%s/stop", cid)),
						),
						ghttp.RespondWith(http.StatusInternalServerError, "stop error"),
					),
				)
				// Execute StopAndRemoveContainer and verify the stop error is propagated.
				err := client{api: docker}.StopAndRemoveContainer(mockedContainer, time.Second)
				gomega.Expect(err).
					To(gomega.MatchError(gomega.ContainSubstring("failed to stop container: Error response from daemon: stop error")))
			})
		})

		ginkgo.When("removal fails with an unexpected error", func() {
			ginkgo.It("should return an error", func() {
				// Create a mock container in running state.
				mockedContainer := MockContainer(
					WithContainerState(dockerContainer.State{Running: true}),
				)
				cid := mockedContainer.ContainerInfo().ID
				// Set up mock server handlers for stop and removal failure.
				mockServer.AppendHandlers(
					StopContainerHandler(cid, mockContainer.Found), // Simulate successful stop
					ghttp.CombineHandlers( // Removal fails
						ghttp.VerifyRequest(methodDelete, gomega.HaveSuffix(cid)),
						ghttp.RespondWith(http.StatusInternalServerError, "server error"),
					),
				)
				// Execute StopAndRemoveContainer and verify the removal error is propagated.
				err := client{api: docker}.StopAndRemoveContainer(mockedContainer, time.Second)
				gomega.Expect(err).
					To(gomega.MatchError(gomega.ContainSubstring("failed to remove container: Error response from daemon: server error")))
			})
		})

		ginkgo.When("removing a stopped container", func() {
			ginkgo.It("should only call remove, not stop", func() {
				// Create a mock container in stopped state.
				mockedContainer := MockContainer(
					WithContainerState(dockerContainer.State{Running: false}),
				)
				cid := mockedContainer.ContainerInfo().ID
				// Set up mock server handler for removal only.
				mockServer.AppendHandlers(
					mockContainer.RemoveContainerHandler(
						cid,
						mockContainer.Found,
					), // Simulate successful removal
				)
				// Execute StopAndRemoveContainer and verify no error occurs.
				err := client{api: docker}.StopAndRemoveContainer(mockedContainer, time.Second)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			})
		})

		ginkgo.When("stopping a container with AutoRemove enabled", func() {
			ginkgo.It("should skip removal after stopping", func() {
				// Create a mock container with AutoRemove enabled.
				mockedContainer := MockContainer(
					WithContainerState(dockerContainer.State{Running: true}),
					WithAutoRemove(true),
				)
				cid := mockedContainer.ContainerInfo().ID
				// Set up mock server handler for stop only (no remove call expected).
				mockServer.AppendHandlers(
					StopContainerHandler(cid, mockContainer.Found),
				)
				// Execute StopAndRemoveContainer and verify no error occurs.
				err := client{api: docker}.StopAndRemoveContainer(mockedContainer, time.Second)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			})
		})
	})

	// Test suite for stopping containers.
	ginkgo.When("stopping a container", func() {
		ginkgo.When("the container is running", func() {
			ginkgo.It("should stop the container successfully", func() {
				// Create a mock container in running state.
				mockedContainer := MockContainer(
					WithContainerState(dockerContainer.State{Running: true}),
				)
				cid := mockedContainer.ContainerInfo().ID
				// Set up mock server handler for stop operation.
				mockServer.AppendHandlers(
					StopContainerHandler(
						cid,
						mockContainer.Found,
					),
				)
				// Execute StopContainer and verify no error occurs.
				err := client{api: docker}.StopContainer(mockedContainer, time.Second)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			})
		})

		ginkgo.When("the container is already stopped", func() {
			ginkgo.It("should not attempt to stop and return no error", func() {
				// Create a mock container in stopped state.
				mockedContainer := MockContainer(
					WithContainerState(dockerContainer.State{Running: false}),
				)
				// Execute StopContainer and verify no error occurs (no API calls expected).
				err := client{api: docker}.StopContainer(mockedContainer, time.Second)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				// Verify no requests were made to the mock server.
				gomega.Expect(mockServer.ReceivedRequests()).To(gomega.BeEmpty())
			})
		})
	})

	// Test suite for removing containers.
	ginkgo.When("removing a container", func() {
		ginkgo.When("the container exists", func() {
			ginkgo.It("should remove the container successfully", func() {
				// Create a mock container.
				mockedContainer := MockContainer()
				cid := mockedContainer.ContainerInfo().ID
				// Set up mock server handler for removal.
				mockServer.AppendHandlers(
					mockContainer.RemoveContainerHandler(
						cid,
						mockContainer.Found,
					),
				)
				// Execute RemoveContainer and verify no error occurs.
				err := client{api: docker}.RemoveContainer(mockedContainer)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			})
		})

		ginkgo.When("the container does not exist", func() {
			ginkgo.It("should not return an error", func() {
				// Create a mock container.
				mockedContainer := MockContainer()
				cid := mockedContainer.ContainerInfo().ID
				// Set up mock server handler for removal failure (container not found).
				mockServer.AppendHandlers(
					mockContainer.RemoveContainerHandler(
						cid,
						mockContainer.Missing,
					),
				)
				// Execute RemoveContainer and verify no error occurs.
				err := client{api: docker}.RemoveContainer(mockedContainer)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			})
		})

		ginkgo.When("removal fails with an unexpected error", func() {
			ginkgo.It("should return an error", func() {
				// Create a mock container.
				mockedContainer := MockContainer()
				cid := mockedContainer.ContainerInfo().ID
				// Set up mock server handler for removal failure.
				mockServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest(methodDelete, gomega.HaveSuffix(cid)),
						ghttp.RespondWith(http.StatusInternalServerError, "server error"),
					),
				)
				// Execute RemoveContainer and verify the error is propagated.
				err := client{api: docker}.RemoveContainer(mockedContainer)
				gomega.Expect(err).
					To(gomega.MatchError(gomega.ContainSubstring("failed to remove container")))
			})
		})
	})

	// Test suite for listing containers with various filters and options.
	ginkgo.When("listing containers", func() {
		ginkgo.When("no filter is provided", func() {
			ginkgo.It("should return all available containers", func() {
				// Set up mock server to return running containers.
				mockServer.AppendHandlers(mockContainer.ListContainersHandler("running"))
				mockServer.AppendHandlers(
					mockContainer.GetContainerHandlers(
						&mockContainer.Watchtower,
						&mockContainer.Running,
					)...)

				client := client{
					api:           docker,
					ClientOptions: ClientOptions{},
				}
				// Execute ListContainers and verify results.
				containers, err := client.ListContainers(filters.NoFilter)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(containers).To(gomega.HaveLen(2))
			})
		})

		ginkgo.When("a filter matching nothing", func() {
			ginkgo.It("should return an empty array", func() {
				// Set up mock server to return running containers.
				mockServer.AppendHandlers(mockContainer.ListContainersHandler("running"))
				mockServer.AppendHandlers(
					mockContainer.GetContainerHandlers(
						&mockContainer.Watchtower,
						&mockContainer.Running,
					)...)

				filter := filters.FilterByNames([]string{"lollercoaster"}, filters.NoFilter)
				client := client{
					api:           docker,
					ClientOptions: ClientOptions{},
				}
				// Execute ListContainers and verify empty result.
				containers, err := client.ListContainers(filter)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(containers).To(gomega.BeEmpty())
			})
		})

		ginkgo.When("a watchtower filter is provided", func() {
			ginkgo.It("should return only the watchtower container", func() {
				// Set up mock server to return running containers.
				mockServer.AppendHandlers(mockContainer.ListContainersHandler("running"))
				mockServer.AppendHandlers(
					mockContainer.GetContainerHandlers(
						&mockContainer.Watchtower,
						&mockContainer.Running,
					)...)

				client := client{
					api:           docker,
					ClientOptions: ClientOptions{},
				}
				// Execute ListContainers with Watchtower filter and verify result.
				containers, err := client.ListContainers(filters.WatchtowerContainersFilter)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(containers).
					To(gomega.ConsistOf(withContainerImageName(gomega.Equal("nickfedor/watchtower:latest"))))
			})
		})

		ginkgo.When(`include stopped is enabled`, func() {
			ginkgo.It("should return both stopped and running containers", func() {
				// Set up mock server to return running, stopped, and created containers.
				mockServer.AppendHandlers(
					mockContainer.ListContainersHandler("running", "exited", "created"),
				)
				mockServer.AppendHandlers(
					mockContainer.GetContainerHandlers(
						&mockContainer.Stopped,
						&mockContainer.Watchtower,
						&mockContainer.Running,
					)...)

				client := client{
					api:           docker,
					ClientOptions: ClientOptions{IncludeStopped: true},
				}
				// Execute ListContainers and verify stopped containers are included.
				containers, err := client.ListContainers(filters.NoFilter)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(containers).To(gomega.ContainElement(havingRunningState(false)))
			})
		})

		ginkgo.When(`include restarting is enabled`, func() {
			ginkgo.It("should return both restarting and running containers", func() {
				// Set up mock server to return running and restarting containers.
				mockServer.AppendHandlers(
					mockContainer.ListContainersHandler("running", "restarting"),
				)
				mockServer.AppendHandlers(
					mockContainer.GetContainerHandlers(
						&mockContainer.Watchtower,
						&mockContainer.Running,
						&mockContainer.Restarting,
					)...)

				client := client{
					api:           docker,
					ClientOptions: ClientOptions{IncludeRestarting: true},
				}
				// Execute ListContainers and verify restarting containers are included.
				containers, err := client.ListContainers(filters.NoFilter)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(containers).To(gomega.ContainElement(havingRestartingState(true)))
			})
		})

		ginkgo.When(`include restarting is disabled`, func() {
			ginkgo.It("should not return restarting containers", func() {
				// Set up mock server to return running containers only.
				mockServer.AppendHandlers(mockContainer.ListContainersHandler("running"))
				mockServer.AppendHandlers(
					mockContainer.GetContainerHandlers(
						&mockContainer.Watchtower,
						&mockContainer.Running,
					)...)

				client := client{
					api:           docker,
					ClientOptions: ClientOptions{IncludeRestarting: false},
				}
				// Execute ListContainers and verify no restarting containers are included.
				containers, err := client.ListContainers(filters.NoFilter)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(containers).NotTo(gomega.ContainElement(havingRestartingState(true)))
			})
		})

		ginkgo.When("multiple filters are provided", func() {
			ginkgo.It("should combine filters with logical AND", func() {
				// Set up mock server to return running containers.
				mockServer.AppendHandlers(mockContainer.ListContainersHandler("running"))
				mockServer.AppendHandlers(
					mockContainer.GetContainerHandlers(
						&mockContainer.Watchtower,
						&mockContainer.Running,
					)...)

				client := client{
					api:           docker,
					ClientOptions: ClientOptions{},
				}
				// Apply two filters: one for name "portainer" and one that always passes
				nameFilter := filters.FilterByNames([]string{"portainer"}, filters.NoFilter)
				containers, err := client.ListContainers(nameFilter, filters.NoFilter)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				// Should return only the "portainer" container
				gomega.Expect(containers).To(gomega.HaveLen(1))
				gomega.Expect(containers[0].Name()).To(gomega.Equal("portainer"))
			})

			ginkgo.It("should return empty when filters are mutually exclusive", func() {
				// Set up mock server to return running containers.
				mockServer.AppendHandlers(mockContainer.ListContainersHandler("running"))
				mockServer.AppendHandlers(
					mockContainer.GetContainerHandlers(
						&mockContainer.Watchtower,
						&mockContainer.Running,
					)...)

				client := client{
					api:           docker,
					ClientOptions: ClientOptions{},
				}
				// Apply two mutually exclusive name filters
				portainerFilter := filters.FilterByNames([]string{"portainer"}, filters.NoFilter)
				watchtowerFilter := filters.FilterByNames(
					[]string{"watchtower-running"},
					filters.NoFilter,
				)
				containers, err := client.ListContainers(portainerFilter, watchtowerFilter)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				// Should return empty since no container can be both "portainer" and "watchtower-running" named
				gomega.Expect(containers).To(gomega.BeEmpty())
			})
		})

		ginkgo.When(`a container uses container network mode`, func() {
			ginkgo.When(`the network container can be resolved`, func() {
				ginkgo.It("should return the container name instead of the ID", func() {
					// Set up mock server for a container with network mode.
					consumerContainerRef := mockContainer.NetConsumerOK
					mockServer.AppendHandlers(
						mockContainer.GetContainerHandlers(&consumerContainerRef)...)

					client := client{
						api:           docker,
						ClientOptions: ClientOptions{},
					}
					// Execute GetContainer and verify network mode resolution.
					container, err := client.GetContainer(consumerContainerRef.ContainerID())
					gomega.Expect(err).NotTo(gomega.HaveOccurred())

					networkMode := container.ContainerInfo().HostConfig.NetworkMode
					gomega.Expect(networkMode.ConnectedContainer()).
						To(gomega.Equal(mockContainer.NetSupplierContainerName))
				})
			})

			ginkgo.When(`the network container cannot be resolved`, func() {
				ginkgo.It("should still return the container ID", func() {
					// Set up mock server for a container with invalid network supplier.
					consumerContainerRef := mockContainer.NetConsumerInvalidSupplier
					mockServer.AppendHandlers(
						mockContainer.GetContainerHandlers(&consumerContainerRef)...)

					client := client{
						api:           docker,
						ClientOptions: ClientOptions{},
					}
					// Execute GetContainer and verify fallback to container ID.
					container, err := client.GetContainer(consumerContainerRef.ContainerID())
					gomega.Expect(err).NotTo(gomega.HaveOccurred())

					networkMode := container.ContainerInfo().HostConfig.NetworkMode
					gomega.Expect(networkMode.ConnectedContainer()).
						To(gomega.Equal(mockContainer.NetSupplierNotFoundID))
				})
			})

			// Test suite for waiting for container health.
			ginkgo.Describe("WaitForContainerHealthy", func() {
				ginkgo.When("container has no health check", func() {
					ginkgo.It("should return immediately without error", func() {
						mockedContainer := MockContainer()
						cid := mockedContainer.ContainerInfo().ID
						// Mock inspect response with no health check
						mockServer.AppendHandlers(
							ghttp.CombineHandlers(
								ghttp.VerifyRequest(
									"GET",
									gomega.MatchRegexp(
										fmt.Sprintf(`^/v[0-9.]+/containers/%s/json$`, cid),
									),
								),
								ghttp.RespondWithJSONEncoded(
									http.StatusOK,
									dockerContainer.InspectResponse{
										ContainerJSONBase: &dockerContainer.ContainerJSONBase{
											ID:    cid,
											State: &dockerContainer.State{Status: "running"},
										},
										Config: &dockerContainer.Config{},
									},
								),
							),
						)

						client := client{api: docker}
						err := client.WaitForContainerHealthy(types.ContainerID(cid), 5*time.Second)
						gomega.Expect(err).NotTo(gomega.HaveOccurred())
					})
				})

				ginkgo.When("container becomes healthy", func() {
					ginkgo.It("should return without error", func() {
						mockedContainer := MockContainer()
						cid := mockedContainer.ContainerInfo().ID
						callCount := 0
						// Mock inspect responses: first starting, then healthy
						mockServer.AppendHandlers(
							ghttp.CombineHandlers(
								ghttp.VerifyRequest(
									"GET",
									gomega.MatchRegexp(
										fmt.Sprintf(`^/v[0-9.]+/containers/%s/json$`, cid),
									),
								),
								func(w http.ResponseWriter, _ *http.Request) {
									callCount++

									var response dockerContainer.InspectResponse
									if callCount <= 2 { // First two calls return starting
										response = dockerContainer.InspectResponse{
											ContainerJSONBase: &dockerContainer.ContainerJSONBase{
												ID: cid,
												State: &dockerContainer.State{
													Status: "running",
													Health: &dockerContainer.Health{
														Status: "starting",
													},
												},
											},
											Config: &dockerContainer.Config{},
										}
									} else { // Third call returns healthy
										response = dockerContainer.InspectResponse{
											ContainerJSONBase: &dockerContainer.ContainerJSONBase{
												ID: cid,
												State: &dockerContainer.State{
													Status: "running",
													Health: &dockerContainer.Health{
														Status: "healthy",
													},
												},
											},
											Config: &dockerContainer.Config{},
										}
									}

									w.Header().Set("Content-Type", "application/json")
									w.WriteHeader(http.StatusOK)
									json.NewEncoder(w).Encode(response)
								},
							),
							ghttp.CombineHandlers(
								ghttp.VerifyRequest(
									"GET",
									gomega.MatchRegexp(
										fmt.Sprintf(`^/v[0-9.]+/containers/%s/json$`, cid),
									),
								),
								func(w http.ResponseWriter, _ *http.Request) {
									callCount++

									var response dockerContainer.InspectResponse
									if callCount <= 2 { // First two calls return starting
										response = dockerContainer.InspectResponse{
											ContainerJSONBase: &dockerContainer.ContainerJSONBase{
												ID: cid,
												State: &dockerContainer.State{
													Status: "running",
													Health: &dockerContainer.Health{
														Status: "starting",
													},
												},
											},
											Config: &dockerContainer.Config{},
										}
									} else { // Third call returns healthy
										response = dockerContainer.InspectResponse{
											ContainerJSONBase: &dockerContainer.ContainerJSONBase{
												ID: cid,
												State: &dockerContainer.State{
													Status: "running",
													Health: &dockerContainer.Health{
														Status: "healthy",
													},
												},
											},
											Config: &dockerContainer.Config{},
										}
									}

									w.Header().Set("Content-Type", "application/json")
									w.WriteHeader(http.StatusOK)
									json.NewEncoder(w).Encode(response)
								},
							),
							ghttp.CombineHandlers(
								ghttp.VerifyRequest(
									"GET",
									gomega.MatchRegexp(
										fmt.Sprintf(`^/v[0-9.]+/containers/%s/json$`, cid),
									),
								),
								func(w http.ResponseWriter, _ *http.Request) {
									callCount++

									var response dockerContainer.InspectResponse
									if callCount <= 2 { // First two calls return starting
										response = dockerContainer.InspectResponse{
											ContainerJSONBase: &dockerContainer.ContainerJSONBase{
												ID: cid,
												State: &dockerContainer.State{
													Status: "running",
													Health: &dockerContainer.Health{
														Status: "starting",
													},
												},
											},
											Config: &dockerContainer.Config{},
										}
									} else { // Third call returns healthy
										response = dockerContainer.InspectResponse{
											ContainerJSONBase: &dockerContainer.ContainerJSONBase{
												ID: cid,
												State: &dockerContainer.State{
													Status: "running",
													Health: &dockerContainer.Health{
														Status: "healthy",
													},
												},
											},
											Config: &dockerContainer.Config{},
										}
									}

									w.Header().Set("Content-Type", "application/json")
									w.WriteHeader(http.StatusOK)
									json.NewEncoder(w).Encode(response)
								},
							),
						)

						client := client{api: docker}
						err := client.WaitForContainerHealthy(types.ContainerID(cid), 5*time.Second)
						gomega.Expect(err).NotTo(gomega.HaveOccurred())
					})
				})

				ginkgo.When("container becomes unhealthy", func() {
					ginkgo.It("should return an error", func() {
						mockedContainer := MockContainer()
						cid := mockedContainer.ContainerInfo().ID
						// Mock inspect response with unhealthy status
						mockServer.AppendHandlers(
							ghttp.CombineHandlers(
								ghttp.VerifyRequest(
									"GET",
									gomega.MatchRegexp(
										fmt.Sprintf(`^/v[0-9.]+/containers/%s/json$`, cid),
									),
								),
								ghttp.RespondWithJSONEncoded(
									http.StatusOK,
									dockerContainer.InspectResponse{
										ContainerJSONBase: &dockerContainer.ContainerJSONBase{
											ID: cid,
											State: &dockerContainer.State{
												Status: "running",
												Health: &dockerContainer.Health{
													Status: "unhealthy",
												},
											},
										},
										Config: &dockerContainer.Config{},
									},
								),
							),
						)

						client := client{api: docker}
						err := client.WaitForContainerHealthy(types.ContainerID(cid), 5*time.Second)
						gomega.Expect(err).To(gomega.HaveOccurred())
						gomega.Expect(err.Error()).
							To(gomega.ContainSubstring("health check failed"))
					})
				})
			})
		})

		ginkgo.Describe("getPodmanFlag", func() {
			ginkgo.When("CPUCopyMode is auto", func() {
				ginkgo.It("should detect Podman via marker file", func() {
					memFs := afero.NewMemMapFs()
					afero.WriteFile(memFs, "/run/.containerenv", []byte{}, 0o644)
					testClient := client{
						api: docker,
						ClientOptions: ClientOptions{
							CPUCopyMode: CPUCopyModeAuto,
							Fs:          memFs,
						},
					}
					result := testClient.getPodmanFlag()
					gomega.Expect(result).To(gomega.BeTrue())
				})

				ginkgo.It("should detect Podman via CONTAINER environment variable", func() {
					memFs := afero.NewMemMapFs()

					restore := withEnvVars(map[string]string{"CONTAINER": "podman"})
					defer restore()

					testClient := client{
						api: docker,
						ClientOptions: ClientOptions{
							CPUCopyMode: CPUCopyModeAuto,
							Fs:          memFs,
						},
					}
					result := testClient.getPodmanFlag()
					gomega.Expect(result).To(gomega.BeTrue())
				})

				ginkgo.It("should detect Podman via API Name field", func() {
					memFs := afero.NewMemMapFs()

					mockServer.AppendHandlers(
						ghttp.CombineHandlers(
							ghttp.VerifyRequest("GET", gomega.MatchRegexp(`^/v[0-9.]+/info$`)),
							ghttp.RespondWithJSONEncoded(http.StatusOK, map[string]any{
								"Name": "podman",
							}),
						),
					)

					testClient := client{
						api: docker,
						ClientOptions: ClientOptions{
							CPUCopyMode: CPUCopyModeAuto,
							Fs:          memFs,
						},
					}
					result := testClient.getPodmanFlag()
					gomega.Expect(result).To(gomega.BeTrue())
				})

				ginkgo.It("should detect Podman via API ServerVersion field", func() {
					memFs := afero.NewMemMapFs()

					mockServer.AppendHandlers(
						ghttp.CombineHandlers(
							ghttp.VerifyRequest("GET", gomega.MatchRegexp(`^/v[0-9.]+/info$`)),
							ghttp.RespondWithJSONEncoded(http.StatusOK, map[string]any{
								"ServerVersion": "podman/4.0.0",
							}),
						),
					)

					testClient := client{
						api: docker,
						ClientOptions: ClientOptions{
							CPUCopyMode: CPUCopyModeAuto,
							Fs:          memFs,
						},
					}
					result := testClient.getPodmanFlag()
					gomega.Expect(result).To(gomega.BeTrue())
				})

				ginkgo.It("should detect Docker via marker file", func() {
					memFs := afero.NewMemMapFs()
					afero.WriteFile(memFs, "/.dockerenv", []byte{}, 0o644)
					mockServer.AppendHandlers(
						ghttp.CombineHandlers(
							ghttp.VerifyRequest("GET", gomega.MatchRegexp(`^/v[0-9.]+/info$`)),
							ghttp.RespondWithJSONEncoded(http.StatusOK, map[string]any{
								"Name": "docker",
							}),
						),
					)

					testClient := client{
						api: docker,
						ClientOptions: ClientOptions{
							CPUCopyMode: CPUCopyModeAuto,
							Fs:          memFs,
						},
					}
					result := testClient.getPodmanFlag()
					gomega.Expect(result).To(gomega.BeFalse())
				})

				ginkgo.It("should fall back to Docker when detection fails", func() {
					memFs := afero.NewMemMapFs()

					mockServer.AppendHandlers(
						ghttp.CombineHandlers(
							ghttp.VerifyRequest("GET", gomega.MatchRegexp(`^/v[0-9.]+/info$`)),
							ghttp.RespondWith(http.StatusInternalServerError, "server error"),
						),
					)

					resetLogrus, logbuf := captureLogrus(logrus.DebugLevel)
					defer resetLogrus()

					testClient := client{
						api: docker,
						ClientOptions: ClientOptions{
							CPUCopyMode: CPUCopyModeAuto,
							Fs:          memFs,
						},
					}
					result := testClient.getPodmanFlag()
					gomega.Expect(result).To(gomega.BeFalse())
					gomega.Eventually(logbuf).
						Should(gbytes.Say("Failed to detect container runtime, falling back to Docker"))
				})
			})

			ginkgo.When("CPUCopyMode is not auto", func() {
				ginkgo.It("should return false without calling detection", func() {
					memFs := afero.NewMemMapFs()
					testClient := client{
						api: docker,
						ClientOptions: ClientOptions{
							CPUCopyMode: "manual",
							Fs:          memFs,
						},
					}
					result := testClient.getPodmanFlag()
					gomega.Expect(result).To(gomega.BeFalse())
					// No API calls should have been made
					gomega.Expect(mockServer.ReceivedRequests()).To(gomega.BeEmpty())
				})
			})
		})
	})

	// Test suite for executing commands in a container.
	ginkgo.Describe("ExecuteCommand", func() {
		ginkgo.When("logging", func() {
			ginkgo.It("should include container id field", func() {
				client := client{
					api:           docker,
					ClientOptions: ClientOptions{},
				}
				// Capture logrus output in buffer.
				resetLogrus, logbuf := captureLogrus(logrus.DebugLevel)
				defer resetLogrus()

				user := ""
				containerID := types.ContainerID("ex-cont-id")
				execID := "ex-exec-id"
				cmd := "exec-cmd"
				// Set up mock server handlers for GetContainer and exec operations.
				mockServer.AppendHandlers(
					// Handler for ContainerInspect (GetContainer)
					ghttp.CombineHandlers(
						ghttp.VerifyRequest(
							"GET",
							gomega.MatchRegexp(
								fmt.Sprintf(`^/v[0-9.]+/containers/%v/json$`, containerID),
							),
						),
						ghttp.RespondWithJSONEncoded(
							http.StatusOK,
							dockerContainer.InspectResponse{
								ContainerJSONBase: &dockerContainer.ContainerJSONBase{
									ID:    string(containerID),
									Name:  "/test-container",
									Image: "test-image:latest",
									State: &dockerContainer.State{
										Status: "running",
									},
									HostConfig: &dockerContainer.HostConfig{},
								},
								Config: &dockerContainer.Config{
									Image:  "test-image:latest",
									Labels: map[string]string{},
								},
							},
						),
					),
					// Handler for ImageInspect
					ghttp.CombineHandlers(
						ghttp.VerifyRequest(
							"GET",
							gomega.MatchRegexp(`^/v[0-9.]+/images/test-image:latest/json$`),
						),
						ghttp.RespondWithJSONEncoded(
							http.StatusOK,
							dockerImage.InspectResponse{
								ID: "test-image-id",
							},
						),
					),
					// Handler for ContainerExecCreate
					ghttp.CombineHandlers(
						ghttp.VerifyRequest(
							"POST",
							gomega.MatchRegexp(
								fmt.Sprintf(`^/v[0-9.]+/containers/%v/exec$`, containerID),
							),
						),
						ghttp.VerifyJSONRepresenting(dockerContainer.ExecOptions{
							User:   user,
							Detach: true,
							Tty:    true,
							Cmd: []string{
								"sh",
								"-c",
								cmd,
							},
							Env: []string{
								"WT_CONTAINER={\"name\":\"test-container\",\"id\":\"ex-cont-id\",\"image_name\":\"test-image:latest\",\"stop_signal\":\"SIGTERM\",\"labels\":{}}",
							},
						}),
						ghttp.RespondWithJSONEncoded(
							http.StatusOK,
							dockerContainer.CommitResponse{ID: execID},
						),
					),
					// Handler for ContainerExecStart
					ghttp.CombineHandlers(
						ghttp.VerifyRequest(
							"POST",
							gomega.MatchRegexp(fmt.Sprintf(`^/v[0-9.]+/exec/%v/start$`, execID)),
						),
						ghttp.VerifyJSONRepresenting(dockerContainer.ExecStartOptions{
							Tty: true,
						}),
						ghttp.RespondWith(http.StatusOK, nil),
					),
					// Handler for ContainerExecInspect
					ghttp.CombineHandlers(
						ghttp.VerifyRequest(
							"GET",
							gomega.MatchRegexp(`^/v[0-9.]+/exec/ex-exec-id/json$`),
						),
						ghttp.RespondWithJSONEncoded(
							http.StatusOK,
							dockerBackend.ExecInspect{
								ID:       execID,
								Running:  false,
								ExitCode: nil,
								ProcessConfig: &dockerBackend.ExecProcessConfig{
									Entrypoint: "sh",
									Arguments:  []string{"-c", cmd},
									User:       user,
								},
								ContainerID: string(containerID),
							},
						),
					),
				)
				// Get the container first
				container, err := client.GetContainer(containerID)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				// Execute command and verify log output includes container id.
				_, err = client.ExecuteCommand(container, cmd, 1, 0, 0)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Eventually(logbuf).Should(gbytes.Say("container_id=ex-cont-id"))
			})

			ginkgo.It("should skip updates when command exits with code 75", func() {
				client := client{
					api:           docker,
					ClientOptions: ClientOptions{},
				}
				// Capture logrus output in buffer.
				resetLogrus, logbuf := captureLogrus(logrus.DebugLevel)
				defer resetLogrus()

				user := ""
				containerID := types.ContainerID("ex-cont-id")
				execID := "ex-exec-id"
				cmd := "exec-cmd"
				// Set up mock server handlers for GetContainer and exec operations.
				mockServer.AppendHandlers(
					// Handler for ContainerInspect (GetContainer)
					ghttp.CombineHandlers(
						ghttp.VerifyRequest(
							"GET",
							gomega.MatchRegexp(
								fmt.Sprintf("^/v[0-9.]+/containers/%v/json$", containerID),
							),
						),
						ghttp.RespondWithJSONEncoded(
							http.StatusOK,
							dockerContainer.InspectResponse{
								ContainerJSONBase: &dockerContainer.ContainerJSONBase{
									ID:    string(containerID),
									Name:  "/test-container",
									Image: "test-image:latest",
									State: &dockerContainer.State{
										Status: "running",
									},
									HostConfig: &dockerContainer.HostConfig{},
								},
								Config: &dockerContainer.Config{
									Image:  "test-image:latest",
									Labels: map[string]string{},
								},
							},
						),
					),
					// Handler for ImageInspect
					ghttp.CombineHandlers(
						ghttp.VerifyRequest(
							"GET",
							gomega.MatchRegexp("^/v[0-9.]+/images/test-image:latest/json$"),
						),
						ghttp.RespondWithJSONEncoded(
							http.StatusOK,
							dockerImage.InspectResponse{
								ID: "test-image-id",
							},
						),
					),
					// Handler for ContainerExecCreate
					ghttp.CombineHandlers(
						ghttp.VerifyRequest(
							"POST",
							gomega.MatchRegexp(
								fmt.Sprintf("^/v[0-9.]+/containers/%v/exec$", containerID),
							),
						),
						ghttp.VerifyJSONRepresenting(dockerContainer.ExecOptions{
							User:   user,
							Detach: true,
							Tty:    true,
							Cmd: []string{
								"sh",
								"-c",
								cmd,
							},
							Env: []string{
								"WT_CONTAINER={\"name\":\"test-container\",\"id\":\"ex-cont-id\",\"image_name\":\"test-image:latest\",\"stop_signal\":\"SIGTERM\",\"labels\":{}}",
							},
						}),
						ghttp.RespondWithJSONEncoded(
							http.StatusOK,
							dockerContainer.CommitResponse{ID: execID},
						),
					),
					// Handler for ContainerExecStart
					ghttp.CombineHandlers(
						ghttp.VerifyRequest(
							"POST",
							gomega.MatchRegexp(fmt.Sprintf("^/v[0-9.]+/exec/%v/start$", execID)),
						),
						ghttp.VerifyJSONRepresenting(dockerContainer.ExecStartOptions{
							Tty: true,
						}),
						ghttp.RespondWith(http.StatusOK, nil),
					),
					// Handler for ContainerExecInspect with exit code 75
					ghttp.CombineHandlers(
						ghttp.VerifyRequest(
							"GET",
							gomega.MatchRegexp("^/v[0-9.]+/exec/ex-exec-id/json$"),
						),
						ghttp.RespondWithJSONEncoded(
							http.StatusOK,
							dockerBackend.ExecInspect{
								ID:       execID,
								Running:  false,
								ExitCode: &[]int{75}[0],
								ProcessConfig: &dockerBackend.ExecProcessConfig{
									Entrypoint: "sh",
									Arguments:  []string{"-c", cmd},
									User:       user,
								},
								ContainerID: string(containerID),
							},
						),
					),
				)
				// Get the container first
				container, err := client.GetContainer(containerID)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				// Execute command and verify skip update is true
				skipUpdate, err := client.ExecuteCommand(container, cmd, 1, 0, 0)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(skipUpdate).To(gomega.BeTrue())
				gomega.Eventually(logbuf).Should(gbytes.Say("container_id=ex-cont-id"))
			})
		})
	})

	// Test suite for captureExecOutput.
	ginkgo.Describe("captureExecOutput", func() {
		ginkgo.It("should return error when attach fails", func() {
			client := client{api: docker}
			ctx := context.Background()
			_, err := client.captureExecOutput(ctx, "exec-id")
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to attach"))
		})

		ginkgo.It("should handle context cancellation", func() {
			client := client{api: docker}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			_, err := client.captureExecOutput(ctx, "exec-id")
			gomega.Expect(err).To(gomega.HaveOccurred())
		})
	})

	// Test suite for handling 404 responses when listing containers.
	ginkgo.When("listing containers with 404 response", func() {
		ginkgo.It("should return empty list and log warning", func() {
			// Capture logrus output.
			resetLogrus, logbuf := captureLogrus(logrus.WarnLevel)
			defer resetLogrus()

			// Set up mock server to return 404 for /containers/json.
			mockServer.AppendHandlers(ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", gomega.MatchRegexp(`^/v[0-9.]+/containers/json$`)),
				ghttp.RespondWith(http.StatusNotFound, "page not found"),
			))

			// Create client instance.
			client := client{api: docker, ClientOptions: ClientOptions{}}
			// Execute ListContainers and verify empty result with warning log.
			containers, err := client.ListContainers(filters.NoFilter)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(containers).To(gomega.BeEmpty())
			gomega.Eventually(logbuf).
				Should(gbytes.Say("Docker API returned 404 for container list"))
		})
	})

	// Test suite for listing containers with 500 server error.
	ginkgo.When("listing containers with 500 server error", func() {
		ginkgo.It("should return error", func() {
			// Set up mock server to return 500 for /containers/json.
			mockServer.AppendHandlers(ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", gomega.MatchRegexp("^/v[0-9.]+/containers/json$")),
				ghttp.RespondWith(http.StatusInternalServerError, "server error"),
			))

			// Create client instance.
			client := client{api: docker, ClientOptions: ClientOptions{}}
			// Execute ListContainers and verify error is returned.
			containers, err := client.ListContainers(filters.NoFilter)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(containers).To(gomega.BeNil())
		})
	})

	// Test suite for listing containers with 401 unauthorized error.
	ginkgo.When("listing containers with 401 unauthorized error", func() {
		ginkgo.It("should return error", func() {
			// Set up mock server to return 401 for /containers/json.
			mockServer.AppendHandlers(ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", gomega.MatchRegexp("^/v[0-9.]+/containers/json$")),
				ghttp.RespondWith(http.StatusUnauthorized, "unauthorized"),
			))

			// Create client instance.
			client := client{api: docker, ClientOptions: ClientOptions{}}
			// Execute ListContainers and verify error is returned.
			containers, err := client.ListContainers(filters.NoFilter)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(containers).To(gomega.BeNil())
		})
	})

	// Test suite for listing containers with container inspect 500 error.
	ginkgo.When("listing containers with container inspect 500 error", func() {
		ginkgo.It("should return error", func() {
			// Set up mock server to return containers for list, then 500 for inspect.
			mockServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", gomega.MatchRegexp("^/v[0-9.]+/containers/json$")),
					ghttp.RespondWithJSONEncoded(http.StatusOK, []dockerContainer.Summary{
						{ID: "container1", Names: []string{"/test1"}},
					}),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest(
						"GET",
						gomega.MatchRegexp("^/v[0-9.]+/containers/container1/json$"),
					),
					ghttp.RespondWith(http.StatusInternalServerError, "inspect error"),
				),
			)

			// Create client instance.
			client := client{api: docker, ClientOptions: ClientOptions{}}
			// Execute ListContainers and verify error is returned.
			containers, err := client.ListContainers(filters.NoFilter)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(containers).To(gomega.BeNil())
		})
	})

	// Test suite for listing all containers with ghost container handling.
	ginkgo.Describe("ListContainers", func() {
		ginkgo.When("containers disappear during enumeration", func() {
			ginkgo.It("should gracefully handle ghost containers and continue processing", func() {
				// Create mock containers: two valid ones and one that will disappear
				validContainer1 := MockContainer()
				validContainer1ID := validContainer1.ContainerInfo().ID
				validContainer2 := MockContainer()
				validContainer2ID := validContainer2.ContainerInfo().ID
				ghostContainerID := "ghost-container-id"

				// Set up mock server handlers
				mockServer.AppendHandlers(
					// Handler for ContainerList - returns all three containers
					ghttp.CombineHandlers(
						ghttp.VerifyRequest(
							"GET",
							gomega.MatchRegexp(`^/v[0-9.]+/containers/json$`),
						),
						ghttp.RespondWithJSONEncoded(http.StatusOK, []dockerContainer.Summary{
							{ID: validContainer1ID, Names: []string{"/valid-container-1"}},
							{ID: ghostContainerID, Names: []string{"/ghost-container"}},
							{ID: validContainer2ID, Names: []string{"/valid-container-2"}},
						}),
					),
					// Handler for first valid container inspect
					ghttp.CombineHandlers(
						ghttp.VerifyRequest(
							"GET",
							gomega.MatchRegexp(
								fmt.Sprintf(`^/v[0-9.]+/containers/%s/json$`, validContainer1ID),
							),
						),
						ghttp.RespondWithJSONEncoded(
							http.StatusOK,
							dockerContainer.InspectResponse{
								ContainerJSONBase: &dockerContainer.ContainerJSONBase{
									ID:    validContainer1ID,
									Name:  "/valid-container-1",
									Image: "test-image-1:latest",
									State: &dockerContainer.State{
										Status: "running",
									},
									HostConfig: &dockerContainer.HostConfig{},
								},
								Config: &dockerContainer.Config{
									Image: "test-image-1:latest",
								},
							},
						),
					),
					// Handler for image inspect for first container
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", gomega.MatchRegexp(`^/v[0-9.]+/images/.*json$`)),
						ghttp.RespondWithJSONEncoded(http.StatusOK, dockerImage.InspectResponse{
							ID: "image-id-1",
						}),
					),
					// Handler for ghost container inspect - returns "No such container" error
					ghttp.CombineHandlers(
						ghttp.VerifyRequest(
							"GET",
							gomega.MatchRegexp(
								fmt.Sprintf(`^/v[0-9.]+/containers/%s/json$`, ghostContainerID),
							),
						),
						ghttp.RespondWith(
							http.StatusNotFound,
							`{"message":"No such container: `+ghostContainerID+`"}`,
						),
					),
					// Handler for second valid container inspect
					ghttp.CombineHandlers(
						ghttp.VerifyRequest(
							"GET",
							gomega.MatchRegexp(
								fmt.Sprintf(`^/v[0-9.]+/containers/%s/json$`, validContainer2ID),
							),
						),
						ghttp.RespondWithJSONEncoded(
							http.StatusOK,
							dockerContainer.InspectResponse{
								ContainerJSONBase: &dockerContainer.ContainerJSONBase{
									ID:    validContainer2ID,
									Name:  "/valid-container-2",
									Image: "test-image-2:latest",
									State: &dockerContainer.State{
										Status: "running",
									},
									HostConfig: &dockerContainer.HostConfig{},
								},
								Config: &dockerContainer.Config{
									Image: "test-image-2:latest",
								},
							},
						),
					),
					// Handler for image inspect for second container
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", gomega.MatchRegexp(`^/v[0-9.]+/images/.*json$`)),
						ghttp.RespondWithJSONEncoded(http.StatusOK, dockerImage.InspectResponse{
							ID: "image-id-2",
						}),
					),
				)

				// Execute ListContainers
				resetLogrus, logbuf := captureLogrus(logrus.DebugLevel)
				defer resetLogrus()

				client := client{api: docker, ClientOptions: ClientOptions{}}
				containers, err := client.ListContainers()

				// Verify no error is returned and only valid containers are included
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(containers).To(gomega.HaveLen(2))

				// Verify the ghost container is not in the result
				containerIDs := make([]string, len(containers))
				for i, c := range containers {
					containerIDs[i] = string(c.ID())
				}

				gomega.Expect(containerIDs).To(gomega.ContainElement(validContainer1ID))
				gomega.Expect(containerIDs).To(gomega.ContainElement(validContainer2ID))
				gomega.Expect(containerIDs).NotTo(gomega.ContainElement(ghostContainerID))
				gomega.Eventually(logbuf).Should(gbytes.Say(ghostContainerID))
			})
		})
	})

	ginkgo.Describe("TLS client methods", func() {
		var (
			tlsServer  *ghttp.Server
			testClient Client
		)

		ginkgo.BeforeEach(func() {
			tlsServer = ghttp.NewTLSServer()
			docker, _ := dockerClient.NewClientWithOpts(
				dockerClient.WithHost(tlsServer.URL()),
				dockerClient.WithHTTPClient(tlsServer.HTTPTestServer.Client()))
			testClient = &client{api: docker}
			gomega.Expect(testClient).NotTo(gomega.BeNil())
		})

		ginkgo.AfterEach(func() {
			tlsServer.Close()
		})

		ginkgo.It("GetVersion returns correct API version with TLS client", func() {
			version := testClient.GetVersion()
			gomega.Expect(version).To(gomega.MatchRegexp(`^\d+\.\d+$`))
		})

		ginkgo.It("GetInfo successfully retrieves system information over TLS", func() {
			tlsServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", gomega.MatchRegexp(`^/v[0-9.]+/info$`)),
					ghttp.RespondWithJSONEncoded(http.StatusOK, map[string]any{
						"Name":            "docker-server",
						"ServerVersion":   "24.0.0",
						"OSType":          "linux",
						"OperatingSystem": "Ubuntu 20.04",
						"Driver":          "overlay2",
					}),
				),
			)

			info, err := testClient.GetInfo()
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(info).NotTo(gomega.BeNil())
			gomega.Expect(info["Name"]).To(gomega.Equal("docker-server"))
		})

		ginkgo.It("GetInfo handles TLS connection failures gracefully", func() {
			// Create a non-TLS server to simulate TLS failure
			httpServer := ghttp.NewServer()
			defer httpServer.Close()

			httpServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.RespondWith(http.StatusOK, "OK"),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", gomega.MatchRegexp(`^/v[0-9.]+/version$`)),
					ghttp.RespondWithJSONEncoded(http.StatusOK, map[string]any{
						"ApiVersion": "1.44",
						"Version":    "24.0.0",
					}),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", gomega.MatchRegexp(`^/v[0-9.]+/info$`)),
					ghttp.RespondWith(http.StatusInternalServerError, "TLS connection failed"),
				),
			)
			// Override DOCKER_HOST to point to HTTP server while TLS is required
			restore := withEnvVars(map[string]string{
				"DOCKER_TLS_VERIFY": "1",
				"DOCKER_HOST":       httpServer.URL(),
			})
			defer restore()
			// Create client that expects TLS but gets HTTP
			failingClient := NewClient(ClientOptions{})
			_, err := failingClient.GetInfo()
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to get system info"))
		})

		ginkgo.It("GetInfo returns expected system info fields over TLS", func() {
			tlsServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", gomega.MatchRegexp(`^/v[0-9.]+/info$`)),
					ghttp.RespondWithJSONEncoded(http.StatusOK, map[string]any{
						"Name":            "test-docker",
						"ServerVersion":   "25.0.0",
						"OSType":          "linux",
						"OperatingSystem": "Alpine Linux",
						"Driver":          "btrfs",
					}),
				),
			)

			info, err := testClient.GetInfo()
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(info).To(gomega.HaveKeyWithValue("Name", "test-docker"))
			gomega.Expect(info).To(gomega.HaveKeyWithValue("ServerVersion", "25.0.0"))
			gomega.Expect(info).To(gomega.HaveKeyWithValue("OSType", "linux"))
			gomega.Expect(info).To(gomega.HaveKeyWithValue("OperatingSystem", "Alpine Linux"))
			gomega.Expect(info).To(gomega.HaveKeyWithValue("Driver", "btrfs"))
		})
	})

	ginkgo.Describe("NewClient", func() {
		ginkgo.It(
			"should successfully connect with TLS when DOCKER_TLS_VERIFY=1 and DOCKER_HOST points to TLS server",
			func() {
				tlsServer := ghttp.NewTLSServer()
				defer tlsServer.Close()

				tlsServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/_ping"),
						ghttp.RespondWith(http.StatusOK, "OK"),
					),
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", gomega.MatchRegexp(`^/v[0-9.]+/version$`)),
						ghttp.RespondWithJSONEncoded(http.StatusOK, map[string]any{
							"ApiVersion": "1.40",
							"Version":    "20.10.0",
						}),
					),
				)

				restore := withEnvVars(map[string]string{
					"DOCKER_TLS_VERIFY": "1",
					"DOCKER_HOST":       tlsServer.URL(),
				})
				defer restore()

				client := NewClient(ClientOptions{})
				gomega.Expect(client).NotTo(gomega.BeNil())
			},
		)

		ginkgo.It("should fail when TLS is required but server is HTTP-only", func() {
			httpServer := ghttp.NewServer()
			defer httpServer.Close()

			httpServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.RespondWith(http.StatusOK, "OK"),
				),
				ghttp.CombineHandlers(
					ghttp.RespondWith(http.StatusInternalServerError, "TLS connection failed"),
				),
			)

			restore := withEnvVars(map[string]string{
				"DOCKER_TLS_VERIFY": "1",
				"DOCKER_HOST":       httpServer.URL(),
			})
			defer restore()

			gomega.Expect(func() { NewClient(ClientOptions{}) }).ToNot(gomega.Panic())
		})

		ginkgo.It("should negotiate API version with TLS", func() {
			tlsServer := ghttp.NewTLSServer()
			defer tlsServer.Close()

			tlsServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", gomega.MatchRegexp(`^/v[0-9.]+/version$`)),
					ghttp.RespondWithJSONEncoded(http.StatusOK, map[string]any{
						"ApiVersion": "1.40",
						"Version":    "20.10.0",
					}),
				),
			)

			restore := withEnvVars(map[string]string{
				"DOCKER_TLS_VERIFY": "1",
				"DOCKER_HOST":       tlsServer.URL(),
			})
			defer restore()

			client := NewClient(ClientOptions{})
			gomega.Expect(client).NotTo(gomega.BeNil())
		})

		ginkgo.It("should use forced API version with TLS", func() {
			tlsServer := ghttp.NewTLSServer()
			defer tlsServer.Close()

			tlsServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/_ping"),
					ghttp.RespondWith(http.StatusOK, "OK"),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", gomega.MatchRegexp(`^/v[0-9.]+/version$`)),
					ghttp.RespondWithJSONEncoded(http.StatusOK, map[string]any{
						"ApiVersion": "1.40",
						"Version":    "20.10.0",
					}),
				),
			)

			restore := withEnvVars(map[string]string{
				"DOCKER_TLS_VERIFY":  "1",
				"DOCKER_HOST":        tlsServer.URL(),
				"DOCKER_API_VERSION": "1.40",
			})
			defer restore()

			client := NewClient(ClientOptions{})
			gomega.Expect(client).NotTo(gomega.BeNil())
		})

		ginkgo.It(
			"should handle invalid API version with TLS and fall back to negotiation",
			func() {
				tlsServer := ghttp.NewTLSServer()
				defer tlsServer.Close()

				tlsServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/_ping"),
						ghttp.RespondWith(http.StatusNotFound, "page not found"),
					),
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", gomega.MatchRegexp(`^/v[0-9.]+/version$`)),
						ghttp.RespondWithJSONEncoded(http.StatusOK, map[string]any{
							"ApiVersion": "1.40",
							"Version":    "20.10.0",
						}),
					),
				)

				restore := withEnvVars(map[string]string{
					"DOCKER_TLS_VERIFY":  "1",
					"DOCKER_HOST":        tlsServer.URL(),
					"DOCKER_API_VERSION": "1.99",
				})
				defer restore()

				client := NewClient(ClientOptions{})
				gomega.Expect(client).NotTo(gomega.BeNil())
			},
		)
	})
})

func TestStopAndRemoveContainer_ContainerStillExistsAfterStopping(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/stop") {
				w.WriteHeader(http.StatusNoContent)
			} else if r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/containers/") {
				w.WriteHeader(http.StatusNoContent)
			}
		}))
		defer server.Close()

		docker, _ := dockerClient.NewClientWithOpts(
			dockerClient.WithHost(server.URL),
			dockerClient.WithHTTPClient(server.Client()))

		// Create a mock container in running state.
		mockedContainer := MockContainer(
			WithContainerState(dockerContainer.State{Running: true}),
		)
		// Execute StopAndRemoveContainer and verify no error occurs.
		err := client{api: docker}.StopAndRemoveContainer(mockedContainer, time.Second)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})
}

func TestStopAndRemoveContainer_ContainerDoesNotExistAfterStopping(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/stop") {
				w.WriteHeader(http.StatusNoContent)
			} else if r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/containers/") {
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		docker, _ := dockerClient.NewClientWithOpts(
			dockerClient.WithHost(server.URL),
			dockerClient.WithHTTPClient(server.Client()))

		// Create a mock container in running state.
		mockedContainer := MockContainer(
			WithContainerState(dockerContainer.State{Running: true}),
		)
		// Execute StopAndRemoveContainer and verify no error occurs.
		err := client{api: docker}.StopAndRemoveContainer(mockedContainer, time.Second)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})
}

func TestStopContainer_StoppingFailsWithUnexpectedError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == pingPath {
				w.WriteHeader(http.StatusOK)

				return
			}

			if strings.Contains(r.URL.Path, "/version") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"ApiVersion": "1.40", "Version": "20.10.0"}`))

				return
			}

			if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/stop") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"message": "server error"}`))

				return
			}

			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		docker, _ := dockerClient.NewClientWithOpts(
			dockerClient.WithHost(server.URL),
			dockerClient.WithHTTPClient(server.Client()))

		// Create a mock container in running state.
		mockedContainer := MockContainer(
			WithContainerState(dockerContainer.State{Running: true}),
		)
		// Execute StopContainer and verify the error is propagated.
		err := client{api: docker}.StopContainer(mockedContainer, time.Second)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		expectedMsg := "failed to stop container: Error response from daemon: server error"
		if !strings.Contains(err.Error(), expectedMsg) {
			t.Fatalf("expected error to contain %q, got %q", expectedMsg, err.Error())
		}
	})
}

func TestStopAndRemoveContainer_RemovalFailsWithUnexpectedError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == pingPath {
				w.WriteHeader(http.StatusOK)

				return
			}

			if strings.Contains(r.URL.Path, "/version") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"ApiVersion": "1.40", "Version": "20.10.0"}`))

				return
			}

			if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/stop") {
				w.WriteHeader(http.StatusNoContent)

				return
			}

			if r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/containers/") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"message": "server error"}`))

				return
			}

			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		docker, _ := dockerClient.NewClientWithOpts(
			dockerClient.WithHost(server.URL),
			dockerClient.WithHTTPClient(server.Client()))

		// Create a mock container in running state.
		mockedContainer := MockContainer(
			WithContainerState(dockerContainer.State{Running: true}),
		)
		// Execute StopAndRemoveContainer and verify the removal error is propagated.
		err := client{api: docker}.StopAndRemoveContainer(mockedContainer, time.Second)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		expectedMsg := "failed to remove container: Error response from daemon: server error"
		if !strings.Contains(err.Error(), expectedMsg) {
			t.Fatalf("expected error to contain %q, got %q", expectedMsg, err.Error())
		}
	})
}

func TestStopContainer_ContainerFailsToStopWithinTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == pingPath {
				w.WriteHeader(http.StatusOK)

				return
			}

			if strings.Contains(r.URL.Path, "/version") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"ApiVersion": "1.40", "Version": "20.10.0"}`))

				return
			}

			if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/stop") {
				w.WriteHeader(http.StatusNoContent)

				return
			}

			if r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/containers/") {
				w.WriteHeader(http.StatusNoContent)

				return
			}

			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		docker, _ := dockerClient.NewClientWithOpts(
			dockerClient.WithHost(server.URL),
			dockerClient.WithHTTPClient(server.Client()))

		// Create a mock container in running state.
		mockedContainer := MockContainer(
			WithContainerState(dockerContainer.State{Running: true}),
		)
		// Capture logrus output for verification.
		resetLogrus, logbuf := captureLogrus(logrus.DebugLevel)
		defer resetLogrus()
		// Execute StopAndRemoveContainer with a realistic timeout.
		err := client{
			api: docker,
		}.StopAndRemoveContainer(
			mockedContainer,
			1*time.Second,
		)
		// Verify no error occurs, as removal should succeed.
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		// Verify log output includes expected message from container_source.go.
		if !strings.Contains(string(logbuf.Contents()), "Container removed successfully") {
			t.Fatalf(
				"expected log to contain 'Container removed successfully', got %q",
				string(logbuf.Contents()),
			)
		}
	})
}

func TestWaitForContainerHealthy_Timeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mockedContainer := MockContainer()
		cid := mockedContainer.ContainerInfo().ID

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == pingPath {
				w.WriteHeader(http.StatusOK)

				return
			}

			if strings.Contains(r.URL.Path, "/version") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"ApiVersion": "1.40", "Version": "20.10.0"}`))

				return
			}

			if strings.Contains(r.URL.Path, fmt.Sprintf("/containers/%s/json", cid)) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)

				response := dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						ID: cid,
						State: &dockerContainer.State{
							Status: "running",
							Health: &dockerContainer.Health{Status: "starting"},
						},
					},
					Config: &dockerContainer.Config{},
				}
				json.NewEncoder(w).Encode(response)

				return
			}

			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		docker, _ := dockerClient.NewClientWithOpts(
			dockerClient.WithHost(server.URL),
			dockerClient.WithHTTPClient(server.Client()))

		client := client{api: docker}

		err := client.WaitForContainerHealthy(types.ContainerID(cid), 0)
		if err == nil {
			t.Fatal("expected timeout error, got nil")
		}

		if !strings.Contains(err.Error(), "timeout") {
			t.Fatalf("expected error to contain 'timeout', got %q", err.Error())
		}
	})
}

// captureLogrus captures logrus output in a buffer for testing.
//
// Parameters:
//   - level: Log level to set for capturing output.
//
// Returns:
//   - func(): Function to restore original logrus settings.
//   - *gbytes.Buffer: Buffer containing captured log output.
func captureLogrus(level logrus.Level) (func(), *gbytes.Buffer) {
	logbuf := gbytes.NewBuffer()

	origOut := logrus.StandardLogger().Out

	logrus.SetOutput(logbuf)

	origLev := logrus.StandardLogger().Level

	logrus.SetLevel(level)

	return func() {
		logrus.SetOutput(origOut)
		logrus.SetLevel(origLev)
	}, logbuf
}

// havingRestartingState creates a Gomega matcher for container restarting state.
//
// Parameters:
//   - expected: Expected restarting state (true or false).
//
// Returns:
//   - gomegaTypes.GomegaMatcher: Matcher for verifying restarting state.
func havingRestartingState(expected bool) gomegaTypes.GomegaMatcher {
	return gomega.WithTransform(func(container types.Container) bool {
		return container.ContainerInfo().State.Restarting
	}, gomega.Equal(expected))
}

// havingRunningState creates a Gomega matcher for container running state.
//
// Parameters:
//   - expected: Expected running state (true or false).
//
// Returns:
//   - gomegaTypes.GomegaMatcher: Matcher for verifying running state.
func havingRunningState(expected bool) gomegaTypes.GomegaMatcher {
	return gomega.WithTransform(func(container types.Container) bool {
		return container.ContainerInfo().State.Running
	}, gomega.Equal(expected))
}

// withEnvVars sets environment variables and returns a restore function.
//
// Parameters:
//   - vars: Map of environment variables to set.
//
// Returns:
//   - func(): Function to restore original environment variables.
func withEnvVars(vars map[string]string) func() {
	type envState struct {
		value  string
		exists bool
	}

	original := make(map[string]envState)

	for k, v := range vars {
		orig, exists := os.LookupEnv(k)
		original[k] = envState{value: orig, exists: exists}
		os.Setenv(k, v)
	}

	return func() {
		for k, state := range original {
			if !state.exists {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, state.value)
			}
		}
	}
}
