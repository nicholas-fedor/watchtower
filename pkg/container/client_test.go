package container

import (
	"encoding/json"
	"io"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"

	"github.com/nicholas-fedor/watchtower/internal/util"
	"github.com/nicholas-fedor/watchtower/pkg/container/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/filters"
	"github.com/nicholas-fedor/watchtower/pkg/types"

	"github.com/docker/docker/api/types/backend"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/ghttp"
	"github.com/sirupsen/logrus"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	gomegaTypes "github.com/onsi/gomega/types"

	"context"
	"net/http"
)

var _ = ginkgo.Describe("the client", func() {
	var docker *client.Client
	var mockServer *ghttp.Server
	ginkgo.BeforeEach(func() {
		mockServer = ghttp.NewServer()
		docker, _ = client.NewClientWithOpts(
			client.WithHost(mockServer.URL()),
			client.WithHTTPClient(mockServer.HTTPTestServer.Client()))
	})
	ginkgo.AfterEach(func() {
		mockServer.Close()
	})
	ginkgo.Describe("WarnOnHeadPullFailed", func() {
		containerUnknown := MockContainer(WithImageName("unknown.repo/prefix/imagename:latest"))
		containerKnown := MockContainer(WithImageName("docker.io/prefix/imagename:latest"))
		ginkgo.When(`warn on head failure is set to "always"`, func() {
			c := dockerClient{ClientOptions: ClientOptions{WarnOnHeadFailed: WarnAlways}}
			ginkgo.It("should always return true", func() {
				gomega.Expect(c.WarnOnHeadPullFailed(containerUnknown)).To(gomega.BeTrue())
				gomega.Expect(c.WarnOnHeadPullFailed(containerKnown)).To(gomega.BeTrue())
			})
		})
		ginkgo.When(`warn on head failure is set to "auto"`, func() {
			c := dockerClient{ClientOptions: ClientOptions{WarnOnHeadFailed: WarnAuto}}
			ginkgo.It("should return false for unknown repos", func() {
				gomega.Expect(c.WarnOnHeadPullFailed(containerUnknown)).To(gomega.BeFalse())
			})
			ginkgo.It("should return true for known repos", func() {
				gomega.Expect(c.WarnOnHeadPullFailed(containerKnown)).To(gomega.BeTrue())
			})
		})
		ginkgo.When(`warn on head failure is set to "never"`, func() {
			c := dockerClient{ClientOptions: ClientOptions{WarnOnHeadFailed: WarnNever}}
			ginkgo.It("should never return true", func() {
				gomega.Expect(c.WarnOnHeadPullFailed(containerUnknown)).To(gomega.BeFalse())
				gomega.Expect(c.WarnOnHeadPullFailed(containerKnown)).To(gomega.BeFalse())
			})
		})
	})
	ginkgo.When("pulling the latest image", func() {
		ginkgo.When("the image consist of a pinned hash", func() {
			ginkgo.It("should gracefully fail with a useful message", func() {
				c := dockerClient{}
				pinnedContainer := MockContainer(WithImageName("sha256:fa5269854a5e615e51a72b17ad3fd1e01268f278a6684c8ed3c5f0cdce3f230b"))
				err := c.PullImage(context.Background(), pinnedContainer)
				gomega.Expect(err).To(gomega.MatchError(`container uses a pinned image, and cannot be updated by watchtower`))
			})
		})
	})
	ginkgo.When("removing a running container", func() {
		ginkgo.When("the container still exists after stopping", func() {
			ginkgo.It("should attempt to remove the container", func() {
				mockContainer := MockContainer(WithContainerState(container.State{Running: true}))
				containerStopped := MockContainer(WithContainerState(container.State{Running: false}))
				cid := mockContainer.ContainerInfo().ID
				mockServer.AppendHandlers(
					mocks.KillContainerHandler(cid, mocks.Found),
					mocks.GetContainerHandler(cid, containerStopped.ContainerInfo()), // First wait: stopped
					mocks.RemoveContainerHandler(cid, mocks.Found),
					ghttp.CombineHandlers( // Second wait: timeout after removal
						ghttp.VerifyRequest("GET", gomega.HaveSuffix(cid+"/json")),
						ghttp.RespondWith(http.StatusNotFound, nil), // 404 treated as success
					),
				)
				err := dockerClient{api: docker}.StopContainer(mockContainer, time.Second)
				gomega.Expect(err).To(gomega.BeNil())
			})
		})
		ginkgo.When("the container does not exist after stopping", func() {
			ginkgo.It("should not cause an error", func() {
				mockContainer := MockContainer(WithContainerState(container.State{Running: true}))
				cid := mockContainer.ContainerInfo().ID
				mockServer.AppendHandlers(
					mocks.KillContainerHandler(cid, mocks.Found),
					ghttp.CombineHandlers( // First wait: already gone
						ghttp.VerifyRequest("GET", gomega.HaveSuffix(cid+"/json")),
						ghttp.RespondWith(http.StatusNotFound, nil), // 404 treated as success
					),
					mocks.RemoveContainerHandler(cid, mocks.Missing), // Removal fails gracefully
				)
				err := dockerClient{api: docker}.StopContainer(mockContainer, time.Second)
				gomega.Expect(err).To(gomega.BeNil())
			})
		})
		ginkgo.When("the container fails to stop within timeout", func() {
			ginkgo.It("should log a warning but proceed with removal", func() {
				mockContainer := MockContainer(WithContainerState(container.State{Running: true}))
				containerRunning := MockContainer(WithContainerState(container.State{Running: true}))
				cid := mockContainer.ContainerInfo().ID
				mockServer.AppendHandlers(
					mocks.KillContainerHandler(cid, mocks.Found),
					mocks.GetContainerHandler(cid, containerRunning.ContainerInfo()), // First wait: still running
					mocks.RemoveContainerHandler(cid, mocks.Found),
					ghttp.CombineHandlers( // Second wait: removed
						ghttp.VerifyRequest("GET", gomega.HaveSuffix(cid+"/json")),
						ghttp.RespondWith(http.StatusNotFound, nil),
					),
				)
				resetLogrus, logbuf := captureLogrus(logrus.WarnLevel)
				defer resetLogrus()
				err := dockerClient{api: docker}.StopContainer(mockContainer, 1*time.Millisecond) // Short timeout
				gomega.Expect(err).To(gomega.BeNil())
				gomega.Eventually(logbuf).Should(gbytes.Say(`Container %s \(%s\) did not stop within %v`, mockContainer.Name(), mockContainer.ID().ShortID(), 1*time.Millisecond))
			})
		})
		ginkgo.When("waiting for stop fails with an unexpected error", func() {
			ginkgo.It("should return an error", func() {
				mockContainer := MockContainer(WithContainerState(container.State{Running: true}))
				cid := mockContainer.ContainerInfo().ID
				mockServer.AppendHandlers(
					mocks.KillContainerHandler(cid, mocks.Found),
					ghttp.CombineHandlers( // First wait fails
						ghttp.VerifyRequest("GET", gomega.HaveSuffix(cid+"/json")),
						ghttp.RespondWith(http.StatusInternalServerError, "server error"),
					),
				)
				err := dockerClient{api: docker}.StopContainer(mockContainer, time.Second)
				gomega.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("failed to wait for container %s (%s) to stop", mockContainer.Name(), mockContainer.ID().ShortID())))
			})
		})
		ginkgo.When("waiting for removal fails with an unexpected error", func() {
			ginkgo.It("should return an error", func() {
				mockContainer := MockContainer(WithContainerState(container.State{Running: true}))
				containerStopped := MockContainer(WithContainerState(container.State{Running: false}))
				cid := mockContainer.ContainerInfo().ID
				mockServer.AppendHandlers(
					mocks.KillContainerHandler(cid, mocks.Found),
					mocks.GetContainerHandler(cid, containerStopped.ContainerInfo()), // First wait: stopped
					mocks.RemoveContainerHandler(cid, mocks.Found),
					ghttp.CombineHandlers( // Second wait fails
						ghttp.VerifyRequest("GET", gomega.HaveSuffix(cid+"/json")),
						ghttp.RespondWith(http.StatusInternalServerError, "server error"),
					),
				)
				err := dockerClient{api: docker}.StopContainer(mockContainer, time.Second)
				gomega.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("failed to confirm removal of container %s (%s)", mockContainer.Name(), mockContainer.ID().ShortID())))
			})
		})
	})
	ginkgo.When("removing a image", func() {
		ginkgo.When("debug logging is enabled", func() {
			ginkgo.It("should log removed and untagged images", func() {
				imageA := util.GenerateRandomSHA256()
				imageAParent := util.GenerateRandomSHA256()
				images := map[string][]string{imageA: {imageAParent}}
				mockServer.AppendHandlers(mocks.RemoveImageHandler(images))
				c := dockerClient{api: docker}
				resetLogrus, logbuf := captureLogrus(logrus.DebugLevel)
				defer resetLogrus()
				gomega.Expect(c.RemoveImageByID(types.ImageID(imageA))).To(gomega.Succeed())
				shortA := types.ImageID(imageA).ShortID()
				shortAParent := types.ImageID(imageAParent).ShortID()
				gomega.Eventually(logbuf).Should(gbytes.Say(`deleted="%v, %v" untagged="?%v"?`, shortA, shortAParent, shortA))
			})
		})
		ginkgo.When("image is not found", func() {
			ginkgo.It("should return an error", func() {
				image := util.GenerateRandomSHA256()
				mockServer.AppendHandlers(mocks.RemoveImageHandler(nil))
				c := dockerClient{api: docker}
				err := c.RemoveImageByID(types.ImageID(image))
				gomega.Expect(errdefs.IsNotFound(err)).To(gomega.BeTrue())
			})
		})
	})
	ginkgo.When("listing containers", func() {
		ginkgo.When("no filter is provided", func() {
			ginkgo.It("should return all available containers", func() {
				mockServer.AppendHandlers(mocks.ListContainersHandler("running"))
				mockServer.AppendHandlers(mocks.GetContainerHandlers(&mocks.Watchtower, &mocks.Running)...)
				client := dockerClient{
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
				mockServer.AppendHandlers(mocks.GetContainerHandlers(&mocks.Watchtower, &mocks.Running)...)
				filter := filters.FilterByNames([]string{"lollercoaster"}, filters.NoFilter)
				client := dockerClient{
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
				mockServer.AppendHandlers(mocks.GetContainerHandlers(&mocks.Watchtower, &mocks.Running)...)
				client := dockerClient{
					api:           docker,
					ClientOptions: ClientOptions{},
				}
				containers, err := client.ListContainers(filters.WatchtowerContainersFilter)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(containers).To(gomega.ConsistOf(withContainerImageName(gomega.Equal("nickfedor/watchtower:latest"))))
			})
		})
		ginkgo.When(`include stopped is enabled`, func() {
			ginkgo.It("should return both stopped and running containers", func() {
				mockServer.AppendHandlers(mocks.ListContainersHandler("running", "exited", "created"))
				mockServer.AppendHandlers(mocks.GetContainerHandlers(&mocks.Stopped, &mocks.Watchtower, &mocks.Running)...)
				client := dockerClient{
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
				mockServer.AppendHandlers(mocks.GetContainerHandlers(&mocks.Watchtower, &mocks.Running, &mocks.Restarting)...)
				client := dockerClient{
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
				mockServer.AppendHandlers(mocks.GetContainerHandlers(&mocks.Watchtower, &mocks.Running)...)
				client := dockerClient{
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
					client := dockerClient{
						api:           docker,
						ClientOptions: ClientOptions{},
					}
					container, err := client.GetContainer(consumerContainerRef.ContainerID())
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					networkMode := container.ContainerInfo().HostConfig.NetworkMode
					gomega.Expect(networkMode.ConnectedContainer()).To(gomega.Equal(mocks.NetSupplierContainerName))
				})
			})
			ginkgo.When(`the network container cannot be resolved`, func() {
				ginkgo.It("should still return the container ID", func() {
					consumerContainerRef := mocks.NetConsumerInvalidSupplier
					mockServer.AppendHandlers(mocks.GetContainerHandlers(&consumerContainerRef)...)
					client := dockerClient{
						api:           docker,
						ClientOptions: ClientOptions{},
					}
					container, err := client.GetContainer(consumerContainerRef.ContainerID())
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					networkMode := container.ContainerInfo().HostConfig.NetworkMode
					gomega.Expect(networkMode.ConnectedContainer()).To(gomega.Equal(mocks.NetSupplierNotFoundID))
				})
			})
		})
	})
	ginkgo.Describe(`GetNetworkConfig`, func() {
		ginkgo.When(`providing a container with network aliases`, func() {
			ginkgo.It(`should omit the container ID alias`, func() {
				client := dockerClient{api: docker, ClientOptions: ClientOptions{IncludeRestarting: false}}
				mockContainer := MockContainer(WithImageName("docker.io/prefix/imagename:latest"))
				aliases := []string{"One", "Two", mockContainer.ID().ShortID(), "Four"}
				endpoints := map[string]*network.EndpointSettings{
					`test`: {Aliases: aliases},
				}
				mockContainer.containerInfo.NetworkSettings = &container.NetworkSettings{Networks: endpoints}
				gomega.Expect(mockContainer.ContainerInfo().NetworkSettings.Networks[`test`].Aliases).To(gomega.Equal(aliases))
				gomega.Expect(client.GetNetworkConfig(mockContainer).EndpointsConfig[`test`].Aliases).To(gomega.Equal([]string{"One", "Two", "Four"}))
			})
		})
		ginkgo.When(`providing a container with a static MAC address`, func() {
			ginkgo.It(`should preserve the MAC address in the network config`, func() {
				client := dockerClient{api: docker, ClientOptions: ClientOptions{}}
				mockContainer := MockContainer(WithImageName("nginx:latest"))
				staticMac := "02:42:ac:11:00:02"
				endpoints := map[string]*network.EndpointSettings{
					"bridge": {MacAddress: staticMac, Aliases: []string{"app", mockContainer.ID().ShortID()}},
				}
				mockContainer.containerInfo.NetworkSettings = &container.NetworkSettings{Networks: endpoints}
				networkConfig := client.GetNetworkConfig(mockContainer)
				gomega.Expect(networkConfig.EndpointsConfig["bridge"].MacAddress).To(gomega.Equal(staticMac))
				gomega.Expect(networkConfig.EndpointsConfig["bridge"].Aliases).To(gomega.Equal([]string{"app"}))
			})
		})
		ginkgo.When(`providing a container with multiple networks and MAC addresses`, func() {
			ginkgo.It(`should preserve all MAC addresses`, func() {
				client := dockerClient{api: docker, ClientOptions: ClientOptions{}}
				mockContainer := MockContainer(WithImageName("qmcgaw/gluetun:latest"))
				endpoints := map[string]*network.EndpointSettings{
					"wt-contnet_default": {MacAddress: "02:42:ac:13:00:02", Aliases: []string{"producer"}},
					"extra_net":          {MacAddress: "02:42:ac:14:00:03", Aliases: []string{"extra"}},
				}
				mockContainer.containerInfo.NetworkSettings = &container.NetworkSettings{Networks: endpoints}
				networkConfig := client.GetNetworkConfig(mockContainer)
				gomega.Expect(networkConfig.EndpointsConfig["wt-contnet_default"].MacAddress).To(gomega.Equal("02:42:ac:13:00:02"))
				gomega.Expect(networkConfig.EndpointsConfig["extra_net"].MacAddress).To(gomega.Equal("02:42:ac:14:00:03"))
			})
		})
	})
	ginkgo.Describe(`StartContainer`, func() {
		ginkgo.When(`recreating a container with a static MAC address`, func() {
			ginkgo.It(`should apply the original MAC address to the new container`, func() {
				mockServer := ghttp.NewServer()
				docker, _ := client.NewClientWithOpts(
					client.WithHost(mockServer.URL()),
					client.WithHTTPClient(mockServer.HTTPTestServer.Client()),
				)
				client := dockerClient{api: docker}

				mockContainer := MockContainer(
					WithImageName("nginx:latest"),
					WithContainerState(container.State{Running: true}),
				)
				staticMac := "02:42:ac:11:00:02"
				endpoints := map[string]*network.EndpointSettings{
					"bridge": {MacAddress: staticMac},
				}
				mockContainer.containerInfo.NetworkSettings = &container.NetworkSettings{
					Networks: endpoints,
				}

				mockServer.AppendHandlers(
					// Handler for POST /containers/create
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("POST", gomega.HaveSuffix("/containers/create")),
						ghttp.VerifyContentType("application/json"),
						func(w http.ResponseWriter, req *http.Request) {
							body, err := io.ReadAll(req.Body)
							gomega.Expect(err).To(gomega.BeNil())

							type createConfig struct {
								NetworkingConfig struct {
									EndpointsConfig map[string]struct {
										MacAddress string `json:"MacAddress"`
									} `json:"EndpointsConfig"`
								} `json:"NetworkingConfig"`
							}
							var config createConfig
							err = json.Unmarshal(body, &config)
							gomega.Expect(err).To(gomega.BeNil())

							gomega.Expect(config.NetworkingConfig.EndpointsConfig).To(gomega.HaveKey("bridge"))
							gomega.Expect(config.NetworkingConfig.EndpointsConfig["bridge"].MacAddress).To(gomega.Equal(staticMac))

							ghttp.RespondWithJSONEncoded(http.StatusCreated, container.CreateResponse{ID: "new_container_id"})(w, req)
						},
					),
					// Handler for POST /networks/bridge/disconnect
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("POST", gomega.HaveSuffix("/networks/bridge/disconnect")),
						ghttp.RespondWith(http.StatusNoContent, nil),
					),
					// Handler for POST /networks/bridge/connect
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("POST", gomega.HaveSuffix("/networks/bridge/connect")),
						ghttp.VerifyContentType("application/json"),
						func(w http.ResponseWriter, req *http.Request) {
							body, err := io.ReadAll(req.Body)
							gomega.Expect(err).To(gomega.BeNil())

							type connectConfig struct {
								EndpointConfig struct {
									MacAddress string `json:"MacAddress"`
								} `json:"EndpointConfig"`
							}
							var config connectConfig
							err = json.Unmarshal(body, &config)
							gomega.Expect(err).To(gomega.BeNil())

							gomega.Expect(config.EndpointConfig.MacAddress).To(gomega.Equal(staticMac))

							ghttp.RespondWith(http.StatusNoContent, nil)(w, req)
						},
					),
					// Handler for POST /containers/new_container_id/start
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("POST", gomega.HaveSuffix("/containers/new_container_id/start")),
						ghttp.RespondWith(http.StatusNoContent, nil),
					),
				)

				newID, err := client.StartContainer(mockContainer)
				gomega.Expect(err).To(gomega.BeNil())
				gomega.Expect(newID).To(gomega.Equal(types.ContainerID("new_container_id")))
			})
		})
	})
	ginkgo.Describe(`ExecuteCommand`, func() {
		ginkgo.When(`logging`, func() {
			ginkgo.It("should include container id field", func() {
				client := dockerClient{
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
						ghttp.VerifyRequest("POST", gomega.HaveSuffix("containers/%v/exec", containerID)),
						ghttp.VerifyJSONRepresenting(container.ExecOptions{
							User:   user,
							Detach: false,
							Tty:    true,
							Cmd: []string{
								"sh",
								"-c",
								cmd,
							},
						}),
						ghttp.RespondWithJSONEncoded(http.StatusOK, container.CommitResponse{ID: execID}),
					),
					// API.ContainerExecStart
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("POST", gomega.HaveSuffix("exec/%v/start", execID)),
						ghttp.VerifyJSONRepresenting(container.ExecStartOptions{
							Detach: false,
							Tty:    true,
						}),
						ghttp.RespondWith(http.StatusOK, nil),
					),
					// API.ContainerExecInspect
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", gomega.HaveSuffix("exec/ex-exec-id/json")),
						ghttp.RespondWithJSONEncoded(http.StatusOK, backend.ExecInspect{
							ID:       execID,
							Running:  false,
							ExitCode: nil,
							ProcessConfig: &backend.ExecProcessConfig{
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
				gomega.Eventually(logbuf).Should(gbytes.Say(`containerID="?ex-cont-id"?`))
			})
		})
	})
})

// Capture logrus output in buffer
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

// Gomega matcher helpers
func withContainerImageName(matcher gomegaTypes.GomegaMatcher) gomegaTypes.GomegaMatcher {
	return gomega.WithTransform(containerImageName, matcher)
}

func containerImageName(container types.Container) string {
	return container.ImageName()
}

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
