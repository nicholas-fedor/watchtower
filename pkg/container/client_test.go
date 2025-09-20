package container

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/ghttp"
	"github.com/sirupsen/logrus"

	dockerBackendType "github.com/docker/docker/api/types/backend"
	dockerContainerType "github.com/docker/docker/api/types/container"
	dockerImageType "github.com/docker/docker/api/types/image"
	dockerClient "github.com/docker/docker/client"
	gomegaTypes "github.com/onsi/gomega/types"

	"github.com/nicholas-fedor/watchtower/pkg/container/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/filters"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

var _ = ginkgo.Describe("the client", func() {
	var docker *dockerClient.Client
	var mockServer *ghttp.Server

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
				// Create a mock container in running state.
				mockContainer := MockContainer(
					WithContainerState(dockerContainerType.State{Running: true}),
				)
				cid := mockContainer.ContainerInfo().ID
				// Set up mock server handlers for stop and remove operations.
				mockServer.AppendHandlers(
					StopContainerHandler(cid, mocks.Found),         // Simulate successful stop
					mocks.RemoveContainerHandler(cid, mocks.Found), // Simulate successful removal
				)
				// Execute StopContainer and verify no error occurs.
				err := client{api: docker}.StopContainer(mockContainer, time.Second)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			})
		})

		ginkgo.When("the container does not exist after stopping", func() {
			ginkgo.It("should not cause an error", func() {
				// Create a mock container in running state.
				mockContainer := MockContainer(
					WithContainerState(dockerContainerType.State{Running: true}),
				)
				cid := mockContainer.ContainerInfo().ID
				// Set up mock server handlers for stop and removal.
				mockServer.AppendHandlers(
					StopContainerHandler(cid, mocks.Found),           // Simulate successful stop
					mocks.RemoveContainerHandler(cid, mocks.Missing), // Removal fails gracefully
				)
				// Execute StopContainer and verify no error occurs.
				err := client{api: docker}.StopContainer(mockContainer, time.Second)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			})
		})

		ginkgo.When("the container fails to stop within timeout", func() {
			ginkgo.It("should proceed with removal", func() {
				// Create a mock container in running state.
				mockContainer := MockContainer(
					WithContainerState(dockerContainerType.State{Running: true}),
				)
				cid := mockContainer.ContainerInfo().ID
				// Set up mock server handlers for stop and removal.
				mockServer.AppendHandlers(
					StopContainerHandler(
						cid,
						mocks.Found,
					), // Simulate successful stop attempt
					mocks.RemoveContainerHandler(cid, mocks.Found), // Simulate successful removal
				)
				// Capture logrus output for verification.
				resetLogrus, logbuf := captureLogrus(logrus.DebugLevel)
				defer resetLogrus()
				// Execute StopContainer with a short timeout to simulate failure to stop.
				err := client{
					api: docker,
				}.StopContainer(
					mockContainer,
					100*time.Millisecond,
				)
				// Verify no error occurs, as removal should succeed despite timeout.
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				// Verify log output includes expected message from container_source.go.
				gomega.Eventually(logbuf).Should(gbytes.Say("Container removed successfully"))
			})
		})

		ginkgo.When("stopping fails with an unexpected error", func() {
			ginkgo.It("should return an error", func() {
				// Create a mock container in running state.
				mockContainer := MockContainer(
					WithContainerState(dockerContainerType.State{Running: true}),
				)
				cid := mockContainer.ContainerInfo().ID
				// Set up mock server handler for stop failure.
				mockServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest(
							"POST",
							gomega.HaveSuffix(fmt.Sprintf("containers/%s/stop", cid)),
						),
						ghttp.RespondWith(http.StatusInternalServerError, "server error"),
					),
				)
				// Execute StopContainer and verify the error is propagated.
				err := client{api: docker}.StopContainer(mockContainer, time.Second)
				gomega.Expect(err).
					To(gomega.MatchError(gomega.ContainSubstring("failed to stop container: Error response from daemon: server error")))
			})
		})

		ginkgo.When("removal fails with an unexpected error", func() {
			ginkgo.It("should return an error", func() {
				// Create a mock container in running state.
				mockContainer := MockContainer(
					WithContainerState(dockerContainerType.State{Running: true}),
				)
				cid := mockContainer.ContainerInfo().ID
				// Set up mock server handlers for stop and removal failure.
				mockServer.AppendHandlers(
					StopContainerHandler(cid, mocks.Found), // Simulate successful stop
					ghttp.CombineHandlers( // Removal fails
						ghttp.VerifyRequest("DELETE", gomega.HaveSuffix(cid)),
						ghttp.RespondWith(http.StatusInternalServerError, "server error"),
					),
				)
				// Execute StopContainer and verify the removal error is propagated.
				err := client{api: docker}.StopContainer(mockContainer, time.Second)
				gomega.Expect(err).
					To(gomega.MatchError(gomega.ContainSubstring("failed to remove container: Error response from daemon: server error")))
			})
		})
	})

	// Test suite for listing containers with various filters and options.
	ginkgo.When("listing containers", func() {
		ginkgo.When("no filter is provided", func() {
			ginkgo.It("should return all available containers", func() {
				// Set up mock server to return running containers.
				mockServer.AppendHandlers(mocks.ListContainersHandler("running"))
				mockServer.AppendHandlers(
					mocks.GetContainerHandlers(&mocks.Watchtower, &mocks.Running)...)
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
				mockServer.AppendHandlers(mocks.ListContainersHandler("running"))
				mockServer.AppendHandlers(
					mocks.GetContainerHandlers(&mocks.Watchtower, &mocks.Running)...)
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
				mockServer.AppendHandlers(mocks.ListContainersHandler("running"))
				mockServer.AppendHandlers(
					mocks.GetContainerHandlers(&mocks.Watchtower, &mocks.Running)...)
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
					mocks.ListContainersHandler("running", "exited", "created"),
				)
				mockServer.AppendHandlers(
					mocks.GetContainerHandlers(
						&mocks.Stopped,
						&mocks.Watchtower,
						&mocks.Running,
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
				mockServer.AppendHandlers(mocks.ListContainersHandler("running", "restarting"))
				mockServer.AppendHandlers(
					mocks.GetContainerHandlers(
						&mocks.Watchtower,
						&mocks.Running,
						&mocks.Restarting,
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
				mockServer.AppendHandlers(mocks.ListContainersHandler("running"))
				mockServer.AppendHandlers(
					mocks.GetContainerHandlers(&mocks.Watchtower, &mocks.Running)...)
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

		ginkgo.When(`a container uses container network mode`, func() {
			ginkgo.When(`the network container can be resolved`, func() {
				ginkgo.It("should return the container name instead of the ID", func() {
					// Set up mock server for a container with network mode.
					consumerContainerRef := mocks.NetConsumerOK
					mockServer.AppendHandlers(mocks.GetContainerHandlers(&consumerContainerRef)...)
					client := client{
						api:           docker,
						ClientOptions: ClientOptions{},
					}
					// Execute GetContainer and verify network mode resolution.
					container, err := client.GetContainer(consumerContainerRef.ContainerID())
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					networkMode := container.ContainerInfo().HostConfig.NetworkMode
					gomega.Expect(networkMode.ConnectedContainer()).
						To(gomega.Equal(mocks.NetSupplierContainerName))
				})
			})

			ginkgo.When(`the network container cannot be resolved`, func() {
				ginkgo.It("should still return the container ID", func() {
					// Set up mock server for a container with invalid network supplier.
					consumerContainerRef := mocks.NetConsumerInvalidSupplier
					mockServer.AppendHandlers(mocks.GetContainerHandlers(&consumerContainerRef)...)
					client := client{
						api:           docker,
						ClientOptions: ClientOptions{},
					}
					// Execute GetContainer and verify fallback to container ID.
					container, err := client.GetContainer(consumerContainerRef.ContainerID())
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					networkMode := container.ContainerInfo().HostConfig.NetworkMode
					gomega.Expect(networkMode.ConnectedContainer()).
						To(gomega.Equal(mocks.NetSupplierNotFoundID))
				})
			})

			// Test suite for waiting for container health.
			ginkgo.Describe("WaitForContainerHealthy", func() {
				ginkgo.When("container has no health check", func() {
					ginkgo.It("should return immediately without error", func() {
						mockContainer := MockContainer()
						cid := mockContainer.ContainerInfo().ID
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
									dockerContainerType.InspectResponse{
										ContainerJSONBase: &dockerContainerType.ContainerJSONBase{
											ID:    cid,
											State: &dockerContainerType.State{Status: "running"},
										},
										Config: &dockerContainerType.Config{},
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
						mockContainer := MockContainer()
						cid := mockContainer.ContainerInfo().ID
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
									var response dockerContainerType.InspectResponse
									if callCount <= 2 { // First two calls return starting
										response = dockerContainerType.InspectResponse{
											ContainerJSONBase: &dockerContainerType.ContainerJSONBase{
												ID: cid,
												State: &dockerContainerType.State{
													Status: "running",
													Health: &dockerContainerType.Health{
														Status: "starting",
													},
												},
											},
											Config: &dockerContainerType.Config{},
										}
									} else { // Third call returns healthy
										response = dockerContainerType.InspectResponse{
											ContainerJSONBase: &dockerContainerType.ContainerJSONBase{
												ID: cid,
												State: &dockerContainerType.State{
													Status: "running",
													Health: &dockerContainerType.Health{
														Status: "healthy",
													},
												},
											},
											Config: &dockerContainerType.Config{},
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
									var response dockerContainerType.InspectResponse
									if callCount <= 2 { // First two calls return starting
										response = dockerContainerType.InspectResponse{
											ContainerJSONBase: &dockerContainerType.ContainerJSONBase{
												ID: cid,
												State: &dockerContainerType.State{
													Status: "running",
													Health: &dockerContainerType.Health{
														Status: "starting",
													},
												},
											},
											Config: &dockerContainerType.Config{},
										}
									} else { // Third call returns healthy
										response = dockerContainerType.InspectResponse{
											ContainerJSONBase: &dockerContainerType.ContainerJSONBase{
												ID: cid,
												State: &dockerContainerType.State{
													Status: "running",
													Health: &dockerContainerType.Health{
														Status: "healthy",
													},
												},
											},
											Config: &dockerContainerType.Config{},
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
									var response dockerContainerType.InspectResponse
									if callCount <= 2 { // First two calls return starting
										response = dockerContainerType.InspectResponse{
											ContainerJSONBase: &dockerContainerType.ContainerJSONBase{
												ID: cid,
												State: &dockerContainerType.State{
													Status: "running",
													Health: &dockerContainerType.Health{
														Status: "starting",
													},
												},
											},
											Config: &dockerContainerType.Config{},
										}
									} else { // Third call returns healthy
										response = dockerContainerType.InspectResponse{
											ContainerJSONBase: &dockerContainerType.ContainerJSONBase{
												ID: cid,
												State: &dockerContainerType.State{
													Status: "running",
													Health: &dockerContainerType.Health{
														Status: "healthy",
													},
												},
											},
											Config: &dockerContainerType.Config{},
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
						mockContainer := MockContainer()
						cid := mockContainer.ContainerInfo().ID
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
									dockerContainerType.InspectResponse{
										ContainerJSONBase: &dockerContainerType.ContainerJSONBase{
											ID: cid,
											State: &dockerContainerType.State{
												Status: "running",
												Health: &dockerContainerType.Health{
													Status: "unhealthy",
												},
											},
										},
										Config: &dockerContainerType.Config{},
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

				ginkgo.When("timeout is reached", func() {
					ginkgo.It("should return a timeout error", func() {
						mockContainer := MockContainer()
						cid := mockContainer.ContainerInfo().ID
						// Mock inspect response with starting status
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
									dockerContainerType.InspectResponse{
										ContainerJSONBase: &dockerContainerType.ContainerJSONBase{
											ID: cid,
											State: &dockerContainerType.State{
												Status: "running",
												Health: &dockerContainerType.Health{
													Status: "starting",
												},
											},
										},
										Config: &dockerContainerType.Config{},
									},
								),
							),
						)
						client := client{api: docker}
						err := client.WaitForContainerHealthy(
							types.ContainerID(cid),
							100*time.Millisecond,
						)
						gomega.Expect(err).To(gomega.HaveOccurred())
						gomega.Expect(err.Error()).To(gomega.ContainSubstring("timeout"))
					})
				})
			})
		})
	})

	// Test suite for executing commands in a container.
	ginkgo.Describe(`ExecuteCommand`, func() {
		ginkgo.When(`logging`, func() {
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
							dockerContainerType.InspectResponse{
								ContainerJSONBase: &dockerContainerType.ContainerJSONBase{
									ID:    string(containerID),
									Name:  "/test-container",
									Image: "test-image:latest",
									State: &dockerContainerType.State{
										Status: "running",
									},
									HostConfig: &dockerContainerType.HostConfig{},
								},
								Config: &dockerContainerType.Config{
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
							dockerImageType.InspectResponse{
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
						ghttp.VerifyJSONRepresenting(dockerContainerType.ExecOptions{
							User:   user,
							Detach: false,
							Tty:    true,
							Cmd: []string{
								"sh",
								"-c",
								cmd,
							},
							Env: []string{
								`WT_CONTAINER={"name":"test-container","id":"ex-cont-id","image_name":"test-image:latest","stop_signal":"SIGTERM","labels":{}}`,
							},
						}),
						ghttp.RespondWithJSONEncoded(
							http.StatusOK,
							dockerContainerType.CommitResponse{ID: execID},
						),
					),
					// Handler for ContainerExecStart
					ghttp.CombineHandlers(
						ghttp.VerifyRequest(
							"POST",
							gomega.MatchRegexp(fmt.Sprintf(`^/v[0-9.]+/exec/%v/start$`, execID)),
						),
						ghttp.VerifyJSONRepresenting(dockerContainerType.ExecStartOptions{
							Detach: false,
							Tty:    true,
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
							dockerBackendType.ExecInspect{
								ID:       execID,
								Running:  false,
								ExitCode: nil,
								ProcessConfig: &dockerBackendType.ExecProcessConfig{
									Entrypoint: "sh",
									Arguments:  []string{"-c", cmd},
									User:       user,
								},
								ContainerID: string(containerID),
							},
						),
					),
				)
				// Execute command and verify log output includes container id.
				_, err := client.ExecuteCommand(containerID, cmd, 1)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Eventually(logbuf).Should(gbytes.Say(`container_id=ex-cont-id`))
			})
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
})

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
