package container

import (
	"net/http"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/ghttp"
	"github.com/sirupsen/logrus"

	dockerBackendType "github.com/docker/docker/api/types/backend"
	dockerContainerType "github.com/docker/docker/api/types/container"
	dockerClient "github.com/docker/docker/client"
	gomegaTypes "github.com/onsi/gomega/types"

	"github.com/nicholas-fedor/watchtower/pkg/container/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/filters"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

var _ = ginkgo.Describe("the client", func() {
	var docker *dockerClient.Client
	var mockServer *ghttp.Server
	ginkgo.BeforeEach(func() {
		mockServer = ghttp.NewServer()
		docker, _ = dockerClient.NewClientWithOpts(
			dockerClient.WithHost(mockServer.URL()),
			dockerClient.WithHTTPClient(mockServer.HTTPTestServer.Client()))
	})
	ginkgo.AfterEach(func() {
		mockServer.Close()
	})
	ginkgo.When("removing a running container", func() {
		ginkgo.When("the container still exists after stopping", func() {
			ginkgo.It("should attempt to remove the container", func() {
				mockContainer := MockContainer(
					WithContainerState(dockerContainerType.State{Running: true}),
				)
				containerStopped := MockContainer(
					WithContainerState(dockerContainerType.State{Running: false}),
				)
				cid := mockContainer.ContainerInfo().ID
				mockServer.AppendHandlers(
					mocks.KillContainerHandler(cid, mocks.Found),
					mocks.GetContainerHandler(
						cid,
						containerStopped.ContainerInfo(),
					), // First wait: stopped
					mocks.RemoveContainerHandler(cid, mocks.Found),
					ghttp.CombineHandlers( // Second wait: timeout after removal
						ghttp.VerifyRequest("GET", gomega.HaveSuffix(cid+"/json")),
						ghttp.RespondWith(http.StatusNotFound, nil), // 404 treated as success
					),
				)
				err := client{api: docker}.StopContainer(mockContainer, time.Second)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			})
		})
		ginkgo.When("the container does not exist after stopping", func() {
			ginkgo.It("should not cause an error", func() {
				mockContainer := MockContainer(
					WithContainerState(dockerContainerType.State{Running: true}),
				)
				cid := mockContainer.ContainerInfo().ID
				mockServer.AppendHandlers(
					mocks.KillContainerHandler(cid, mocks.Found),
					ghttp.CombineHandlers( // First wait: already gone
						ghttp.VerifyRequest("GET", gomega.HaveSuffix(cid+"/json")),
						ghttp.RespondWith(http.StatusNotFound, nil), // 404 treated as success
					),
					mocks.RemoveContainerHandler(cid, mocks.Missing), // Removal fails gracefully
				)
				err := client{api: docker}.StopContainer(mockContainer, time.Second)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			})
		})
		ginkgo.When("the container fails to stop within timeout", func() {
			ginkgo.It("should log a debug message but proceed with removal", func() {
				mockContainer := MockContainer(
					WithContainerState(dockerContainerType.State{Running: true}),
				)
				containerRunning := MockContainer(
					WithContainerState(dockerContainerType.State{Running: true}),
				)
				cid := mockContainer.ContainerInfo().ID
				mockServer.AppendHandlers(
					mocks.KillContainerHandler(cid, mocks.Found),
					ghttp.CombineHandlers( // First wait: still running
						ghttp.VerifyRequest("GET", gomega.HaveSuffix(cid+"/json")),
						ghttp.RespondWithJSONEncoded(
							http.StatusOK,
							containerRunning.ContainerInfo(),
						),
						func(_ http.ResponseWriter, _ *http.Request) {
							time.Sleep(200 * time.Millisecond) // Simulate delay
						},
					),
					mocks.RemoveContainerHandler(cid, mocks.Found),
					ghttp.CombineHandlers( // Second wait: removed
						ghttp.VerifyRequest("GET", gomega.HaveSuffix(cid+"/json")),
						ghttp.RespondWith(http.StatusNotFound, nil),
					),
				)
				resetLogrus, logbuf := captureLogrus(logrus.DebugLevel)
				defer resetLogrus()
				err := client{
					api: docker,
				}.StopContainer(
					mockContainer,
					100*time.Millisecond,
				)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				gomega.Eventually(logbuf, 2*time.Second).
					Should(gbytes.Say(`Container did not stop within timeout.*container=%s.*id=%s.*timeout=%v`, mockContainer.Name(), mockContainer.ID().ShortID(), 100*time.Millisecond))
			})
		})
		ginkgo.When("waiting for stop fails with an unexpected error", func() {
			ginkgo.It("should return an error", func() {
				mockContainer := MockContainer(
					WithContainerState(dockerContainerType.State{Running: true}),
				)
				cid := mockContainer.ContainerInfo().ID
				mockServer.AppendHandlers(
					mocks.KillContainerHandler(cid, mocks.Found),
					ghttp.CombineHandlers( // First wait fails
						ghttp.VerifyRequest("GET", gomega.HaveSuffix(cid+"/json")),
						ghttp.RespondWith(http.StatusInternalServerError, "server error"),
					),
				)
				err := client{api: docker}.StopContainer(mockContainer, time.Second)
				gomega.Expect(err).
					To(gomega.MatchError(gomega.ContainSubstring("failed to inspect container: Error response from daemon: server error")))
			})
		})
		ginkgo.When("waiting for removal fails with an unexpected error", func() {
			ginkgo.It("should return an error", func() {
				mockContainer := MockContainer(
					WithContainerState(dockerContainerType.State{Running: true}),
				)
				containerStopped := MockContainer(
					WithContainerState(dockerContainerType.State{Running: false}),
				)
				cid := mockContainer.ContainerInfo().ID
				mockServer.AppendHandlers(
					mocks.KillContainerHandler(cid, mocks.Found),
					mocks.GetContainerHandler(
						cid,
						containerStopped.ContainerInfo(),
					), // First wait: stopped
					mocks.RemoveContainerHandler(cid, mocks.Found),
					ghttp.CombineHandlers( // Second wait fails
						ghttp.VerifyRequest("GET", gomega.HaveSuffix(cid+"/json")),
						ghttp.RespondWith(http.StatusInternalServerError, "server error"),
					),
				)
				err := client{api: docker}.StopContainer(mockContainer, time.Second)
				gomega.Expect(err).
					To(gomega.MatchError(gomega.ContainSubstring("failed to inspect container: Error response from daemon: server error")))
			})
		})
	})
	ginkgo.When("listing containers", func() {
		ginkgo.When("no filter is provided", func() {
			ginkgo.It("should return all available containers", func() {
				mockServer.AppendHandlers(mocks.ListContainersHandler("running"))
				mockServer.AppendHandlers(
					mocks.GetContainerHandlers(&mocks.Watchtower, &mocks.Running)...)
				client := client{
					api:           docker,
					ClientOptions: ClientOptions{},
				}
				containers, err := client.ListContainers(filters.NoFilter)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(containers).To(gomega.HaveLen(2))
			})
		})
		ginkgo.When("a filter matching nothing", func() {
			ginkgo.It("should return an empty array", func() {
				mockServer.AppendHandlers(mocks.ListContainersHandler("running"))
				mockServer.AppendHandlers(
					mocks.GetContainerHandlers(&mocks.Watchtower, &mocks.Running)...)
				filter := filters.FilterByNames([]string{"lollercoaster"}, filters.NoFilter)
				client := client{
					api:           docker,
					ClientOptions: ClientOptions{},
				}
				containers, err := client.ListContainers(filter)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(containers).To(gomega.BeEmpty())
			})
		})
		ginkgo.When("a watchtower filter is provided", func() {
			ginkgo.It("should return only the watchtower container", func() {
				mockServer.AppendHandlers(mocks.ListContainersHandler("running"))
				mockServer.AppendHandlers(
					mocks.GetContainerHandlers(&mocks.Watchtower, &mocks.Running)...)
				client := client{
					api:           docker,
					ClientOptions: ClientOptions{},
				}
				containers, err := client.ListContainers(filters.WatchtowerContainersFilter)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(containers).
					To(gomega.ConsistOf(withContainerImageName(gomega.Equal("nickfedor/watchtower:latest"))))
			})
		})
		ginkgo.When(`include stopped is enabled`, func() {
			ginkgo.It("should return both stopped and running containers", func() {
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
				containers, err := client.ListContainers(filters.NoFilter)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(containers).To(gomega.ContainElement(havingRunningState(false)))
			})
		})
		ginkgo.When(`include restarting is enabled`, func() {
			ginkgo.It("should return both restarting and running containers", func() {
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
				containers, err := client.ListContainers(filters.NoFilter)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(containers).To(gomega.ContainElement(havingRestartingState(true)))
			})
		})
		ginkgo.When(`include restarting is disabled`, func() {
			ginkgo.It("should not return restarting containers", func() {
				mockServer.AppendHandlers(mocks.ListContainersHandler("running"))
				mockServer.AppendHandlers(
					mocks.GetContainerHandlers(&mocks.Watchtower, &mocks.Running)...)
				client := client{
					api:           docker,
					ClientOptions: ClientOptions{IncludeRestarting: false},
				}
				containers, err := client.ListContainers(filters.NoFilter)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(containers).NotTo(gomega.ContainElement(havingRestartingState(true)))
			})
		})
		ginkgo.When(`a container uses container network mode`, func() {
			ginkgo.When(`the network container can be resolved`, func() {
				ginkgo.It("should return the container name instead of the ID", func() {
					consumerContainerRef := mocks.NetConsumerOK
					mockServer.AppendHandlers(mocks.GetContainerHandlers(&consumerContainerRef)...)
					client := client{
						api:           docker,
						ClientOptions: ClientOptions{},
					}
					container, err := client.GetContainer(consumerContainerRef.ContainerID())
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					networkMode := container.ContainerInfo().HostConfig.NetworkMode
					gomega.Expect(networkMode.ConnectedContainer()).
						To(gomega.Equal(mocks.NetSupplierContainerName))
				})
			})
			ginkgo.When(`the network container cannot be resolved`, func() {
				ginkgo.It("should still return the container ID", func() {
					consumerContainerRef := mocks.NetConsumerInvalidSupplier
					mockServer.AppendHandlers(mocks.GetContainerHandlers(&consumerContainerRef)...)
					client := client{
						api:           docker,
						ClientOptions: ClientOptions{},
					}
					container, err := client.GetContainer(consumerContainerRef.ContainerID())
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					networkMode := container.ContainerInfo().HostConfig.NetworkMode
					gomega.Expect(networkMode.ConnectedContainer()).
						To(gomega.Equal(mocks.NetSupplierNotFoundID))
				})
			})
		})
	})
	ginkgo.Describe(`ExecuteCommand`, func() {
		ginkgo.When(`logging`, func() {
			ginkgo.It("should include container id field", func() {
				client := client{
					api:           docker,
					ClientOptions: ClientOptions{},
				}
				// Capture logrus output in buffer
				resetLogrus, logbuf := captureLogrus(logrus.DebugLevel)
				defer resetLogrus()
				user := ""
				containerID := types.ContainerID("ex-cont-id")
				execID := "ex-exec-id"
				cmd := "exec-cmd"
				mockServer.AppendHandlers(
					// API.ContainerExecCreate
					ghttp.CombineHandlers(
						ghttp.VerifyRequest(
							"POST",
							gomega.HaveSuffix("containers/%v/exec", containerID),
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
						}),
						ghttp.RespondWithJSONEncoded(
							http.StatusOK,
							dockerContainerType.CommitResponse{ID: execID},
						),
					),
					// API.ContainerExecStart
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("POST", gomega.HaveSuffix("exec/%v/start", execID)),
						ghttp.VerifyJSONRepresenting(dockerContainerType.ExecStartOptions{
							Detach: false,
							Tty:    true,
						}),
						ghttp.RespondWith(http.StatusOK, nil),
					),
					// API.ContainerExecInspect
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", gomega.HaveSuffix("exec/ex-exec-id/json")),
						ghttp.RespondWithJSONEncoded(http.StatusOK, dockerBackendType.ExecInspect{
							ID:       execID,
							Running:  false,
							ExitCode: nil,
							ProcessConfig: &dockerBackendType.ExecProcessConfig{
								Entrypoint: "sh",
								Arguments:  []string{"-c", cmd},
								User:       user,
							},
							ContainerID: string(containerID),
						}),
					),
				)
				_, err := client.ExecuteCommand(containerID, cmd, 1)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				// Note: Since Execute requires opening up a raw TCP stream to the daemon for the output, this will fail
				// when using the mock API server. Regardless of the outcome, the log should include the container ID
				gomega.Eventually(logbuf).Should(gbytes.Say(`container_id=ex-cont-id`))
			})
		})
	})
	ginkgo.When("listing containers with 404 response", func() {
		ginkgo.It("should return empty list and log warning", func() {
			// Capture logrus output
			resetLogrus, logbuf := captureLogrus(logrus.WarnLevel)
			defer resetLogrus()

			// Setup mock server to return 404 for /containers/json
			mockServer.AppendHandlers(ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", gomega.MatchRegexp(`^/v[0-9.]+/containers/json$`)),
				ghttp.RespondWith(http.StatusNotFound, "page not found"),
			))

			// Create client instance
			client := client{api: docker, ClientOptions: ClientOptions{}}
			containers, err := client.ListContainers(filters.NoFilter)

			// Verify results
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(containers).To(gomega.BeEmpty())
			gomega.Eventually(logbuf).
				Should(gbytes.Say("Docker API returned 404 for container list"))
		})
	})
})

// Capture logrus output in buffer.
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

// Gomega matcher helpers.
func havingRestartingState(expected bool) gomegaTypes.GomegaMatcher {
	return gomega.WithTransform(func(container types.Container) bool {
		return container.ContainerInfo().State.Restarting
	}, gomega.Equal(expected))
}

func havingRunningState(expected bool) gomegaTypes.GomegaMatcher {
	return gomega.WithTransform(func(container types.Container) bool {
		return container.ContainerInfo().State.Running
	}, gomega.Equal(expected))
}
