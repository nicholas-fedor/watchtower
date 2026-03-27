package container

import (
	"bytes"
	"context"
	"errors"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/sirupsen/logrus"

	cerrdefs "github.com/containerd/errdefs"
	dockerContainer "github.com/docker/docker/api/types/container"
	dockerImage "github.com/docker/docker/api/types/image"
	dockerNetwork "github.com/docker/docker/api/types/network"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/nicholas-fedor/watchtower/internal/flags"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// WithCPUSettings configures CPU settings for the mock container.
func WithCPUSettings(nanoCPUs, cpuShares, cpuQuota, cpuPeriod int64, cpusetCpus, cpusetMems string) MockContainerUpdate {
	return func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
		if c.HostConfig == nil {
			c.HostConfig = &dockerContainer.HostConfig{}
		}

		c.HostConfig.NanoCPUs = nanoCPUs
		c.HostConfig.CPUShares = cpuShares
		c.HostConfig.CPUQuota = cpuQuota
		c.HostConfig.CPUPeriod = cpuPeriod
		c.HostConfig.CpusetCpus = cpusetCpus
		c.HostConfig.CpusetMems = cpusetMems
	}
}

var _ = ginkgo.Describe("Target Container Operations", func() {
	var logOutput *bytes.Buffer

	var (
		origOutput = logrus.StandardLogger().Out
		origLevel  = logrus.GetLevel()
	)

	ginkgo.BeforeEach(func() {
		logOutput = &bytes.Buffer{}
		logrus.SetOutput(logOutput)
		logrus.SetLevel(logrus.DebugLevel)
	})

	ginkgo.AfterEach(func() {
		logrus.SetOutput(origOutput)
		logrus.SetLevel(origLevel)
	})

	ginkgo.Describe("handleCPUSettings", func() {
		var (
			defaultNanoCPUs   int64 = 2000000000
			defaultCPUShares  int64 = 1024
			defaultCPUQuota   int64 = 100000
			defaultCPUPeriod  int64 = 100000
			defaultCpusetCpus       = "0-3"
			defaultCpusetMems       = "0"
		)

		var hostConfig *dockerContainer.HostConfig

		ginkgo.BeforeEach(func() {
			mockCont := MockContainer(
				WithCPUSettings(
					defaultNanoCPUs,
					defaultCPUShares,
					defaultCPUQuota,
					defaultCPUPeriod,
					defaultCpusetCpus,
					defaultCpusetMems,
				),
			)
			hostConfig = mockCont.GetCreateHostConfig()
		})

		ginkgo.It("should strip all CPU settings when mode is 'none'", func() {
			clog := logrus.WithField("test", "handleCPUSettings")
			handleCPUSettings(hostConfig, "none", false, clog)
			gomega.Expect(hostConfig.NanoCPUs).To(gomega.Equal(int64(0)))
			gomega.Expect(hostConfig.CPUShares).To(gomega.Equal(int64(0)))
			gomega.Expect(hostConfig.CPUQuota).To(gomega.Equal(int64(0)))
			gomega.Expect(hostConfig.CPUPeriod).To(gomega.Equal(int64(0)))
			gomega.Expect(hostConfig.CpusetCpus).To(gomega.BeEmpty())
			gomega.Expect(hostConfig.CpusetMems).To(gomega.BeEmpty())
			gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("Stripped all CPU settings"))
		})

		ginkgo.It("should preserve all CPU settings when mode is 'full'", func() {
			clog := logrus.WithField("test", "handleCPUSettings")
			handleCPUSettings(hostConfig, "full", false, clog)
			gomega.Expect(hostConfig.NanoCPUs).To(gomega.Equal(defaultNanoCPUs))
			gomega.Expect(hostConfig.CPUShares).To(gomega.Equal(defaultCPUShares))
			gomega.Expect(hostConfig.CPUQuota).To(gomega.Equal(defaultCPUQuota))
			gomega.Expect(hostConfig.CPUPeriod).To(gomega.Equal(defaultCPUPeriod))
			gomega.Expect(hostConfig.CpusetCpus).To(gomega.Equal(defaultCpusetCpus))
			gomega.Expect(hostConfig.CpusetMems).To(gomega.Equal(defaultCpusetMems))
			gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("Copied all CPU settings unchanged"))
		})

		ginkgo.It("should filter NanoCPUs when mode is 'auto' and isPodman is true", func() {
			clog := logrus.WithField("test", "handleCPUSettings")
			handleCPUSettings(hostConfig, "auto", true, clog)
			gomega.Expect(hostConfig.NanoCPUs).To(gomega.Equal(int64(0)))
			gomega.Expect(hostConfig.CPUShares).To(gomega.Equal(defaultCPUShares))
			gomega.Expect(hostConfig.CPUQuota).To(gomega.Equal(defaultCPUQuota))
			gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("Detected Podman, filtered NanoCPUs for compatibility"))
		})

		ginkgo.It("should preserve all CPU settings when mode is 'auto' and isPodman is false", func() {
			clog := logrus.WithField("test", "handleCPUSettings")
			handleCPUSettings(hostConfig, "auto", false, clog)
			gomega.Expect(hostConfig.NanoCPUs).To(gomega.Equal(defaultNanoCPUs))
			gomega.Expect(hostConfig.CPUShares).To(gomega.Equal(defaultCPUShares))
			gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("Detected Docker, copied all CPU settings"))
		})

		ginkgo.It("should default to 'full' mode for unknown CPU copy mode", func() {
			clog := logrus.WithField("test", "handleCPUSettings")
			handleCPUSettings(hostConfig, "unknown", false, clog)
			gomega.Expect(hostConfig.NanoCPUs).To(gomega.Equal(defaultNanoCPUs))
			gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("Unknown CPU copy mode, defaulting to full"))
		})
	})

	ginkgo.Describe("StartTargetContainer", func() {
		var (
			client        *MockClient
			mockCont      *Container
			networkConfig *dockerNetwork.NetworkingConfig
			defaultMem    int64 = 60
		)

		ginkgo.BeforeEach(func() {
			client = &MockClient{}
			mockCont = MockContainer(
				WithNetworkMode("bridge"),
				WithContainerState(dockerContainer.State{Running: true, Status: "running"}),
				WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
					"bridge": {
						NetworkID:  "network_bridge_id",
						IPAddress:  "172.17.0.2",
						MacAddress: "02:42:ac:11:00:02",
						Aliases:    []string{"test-watchtower"},
					},
				}),
				func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
					if c.HostConfig == nil {
						c.HostConfig = &dockerContainer.HostConfig{}
					}

					c.HostConfig.MemorySwappiness = &defaultMem
				},
			)
			networkConfig = getNetworkConfig(mockCont, "1.44")
		})

		ginkgo.It("should disable memory swappiness when flag is true", func() {
			newID, err := StartTargetContainer(
				context.Background(), client, mockCont, networkConfig,
				true, "1.44", flags.DockerAPIMinVersion, true, "auto", false,
			)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(newID).To(gomega.Equal(types.ContainerID("new_container_id")))
			gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("Disabled memory swappiness for Podman compatibility"))
		})

		ginkgo.It("should log MAC address details", func() {
			newID, err := StartTargetContainer(
				context.Background(), client, mockCont, networkConfig,
				true, "1.44", flags.DockerAPIMinVersion, false, "auto", false,
			)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(newID).To(gomega.Equal(types.ContainerID("new_container_id")))
			gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("Found MAC address in config"))
		})

		ginkgo.It("should use full network config for API >= 1.44", func() {
			newID, err := StartTargetContainer(
				context.Background(), client, mockCont, networkConfig,
				true, "1.44", flags.DockerAPIMinVersion, false, "auto", false,
			)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(newID).To(gomega.Equal(types.ContainerID("new_container_id")))
			gomega.Expect(logOutput.String()).
				To(gomega.ContainSubstring("Using full network config for API version >= 1.44 or single network"))
		})

		ginkgo.It("should handle ContainerCreate failure", func() {
			createErr := errors.New("create failed")
			client.createFunc = func(_ context.Context, _ *dockerContainer.Config, _ *dockerContainer.HostConfig, _ *dockerNetwork.NetworkingConfig, _ *ocispec.Platform, _ string) (dockerContainer.CreateResponse, error) {
				return dockerContainer.CreateResponse{}, createErr
			}
			newID, err := StartTargetContainer(
				context.Background(), client, mockCont, networkConfig,
				true, "1.44", flags.DockerAPIMinVersion, false, "auto", false,
			)
			gomega.Expect(newID).To(gomega.BeEmpty())
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("create failed"))
			gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("Failed to create new container"))
		})

		ginkgo.It("should log successful container creation", func() {
			newID, err := StartTargetContainer(
				context.Background(), client, mockCont, networkConfig,
				true, "1.44", flags.DockerAPIMinVersion, false, "auto", false,
			)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(newID).To(gomega.Equal(types.ContainerID("new_container_id")))
			gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("Created container successfully"))
		})

		ginkgo.It("should skip starting stopped container when reviveStopped is false", func() {
			mockCont = MockContainer(
				WithNetworkMode("bridge"),
				WithContainerState(dockerContainer.State{Running: false, Status: "exited"}),
			)
			networkConfig = getNetworkConfig(mockCont, "1.44")
			newID, err := StartTargetContainer(
				context.Background(), client, mockCont, networkConfig,
				false, "1.44", flags.DockerAPIMinVersion, false, "auto", false,
			)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(newID).To(gomega.Equal(types.ContainerID("new_container_id")))
			gomega.Expect(logOutput.String()).
				To(gomega.ContainSubstring("Created container, not starting due to stopped state"))
		})

		ginkgo.It("should handle ContainerStart failure", func() {
			startErr := errors.New("start failed")
			client.startFunc = func(_ context.Context, _ string, _ dockerContainer.StartOptions) error {
				return startErr
			}
			newID, err := StartTargetContainer(
				context.Background(), client, mockCont, networkConfig,
				true, "1.44", flags.DockerAPIMinVersion, false, "auto", false,
			)
			gomega.Expect(newID).To(gomega.Equal(types.ContainerID("new_container_id")))
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("start failed"))
			gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("Failed to start new container"))
		})

		ginkgo.It("should handle ContainerCreate Conflict error", func() {
			client.createFunc = func(_ context.Context, _ *dockerContainer.Config, _ *dockerContainer.HostConfig, _ *dockerNetwork.NetworkingConfig, _ *ocispec.Platform, _ string) (dockerContainer.CreateResponse, error) {
				return dockerContainer.CreateResponse{}, cerrdefs.ErrConflict
			}
			newID, err := StartTargetContainer(
				context.Background(), client, mockCont, networkConfig,
				true, "1.44", flags.DockerAPIMinVersion, false, "auto", false,
			)
			gomega.Expect(newID).To(gomega.BeEmpty())
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to create container"))
			gomega.Expect(cerrdefs.IsConflict(err)).To(gomega.BeTrue(),
				"wrapped error should be detectable via cerrdefs.IsConflict")
			gomega.Expect(errors.Is(err, cerrdefs.ErrConflict)).To(gomega.BeTrue(),
				"wrapped error should be detectable via errors.Is")
			gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("Failed to create new container"))
		})

		ginkgo.It("should handle ContainerCreate InvalidArgument error", func() {
			client.createFunc = func(_ context.Context, _ *dockerContainer.Config, _ *dockerContainer.HostConfig, _ *dockerNetwork.NetworkingConfig, _ *ocispec.Platform, _ string) (dockerContainer.CreateResponse, error) {
				return dockerContainer.CreateResponse{}, cerrdefs.ErrInvalidArgument
			}
			newID, err := StartTargetContainer(
				context.Background(), client, mockCont, networkConfig,
				true, "1.44", flags.DockerAPIMinVersion, false, "auto", false,
			)
			gomega.Expect(newID).To(gomega.BeEmpty())
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to create container"))
		})

		ginkgo.It("should handle ContainerStart Conflict error", func() {
			client.startFunc = func(_ context.Context, _ string, _ dockerContainer.StartOptions) error {
				return cerrdefs.ErrConflict
			}
			newID, err := StartTargetContainer(
				context.Background(), client, mockCont, networkConfig,
				true, "1.44", flags.DockerAPIMinVersion, false, "auto", false,
			)
			gomega.Expect(newID).To(gomega.Equal(types.ContainerID("new_container_id")))
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to start container"))
			gomega.Expect(cerrdefs.IsConflict(err)).To(gomega.BeTrue(),
				"wrapped error should be detectable via cerrdefs.IsConflict")
			gomega.Expect(errors.Is(err, cerrdefs.ErrConflict)).To(gomega.BeTrue(),
				"wrapped error should be detectable via errors.Is")
			gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("Failed to start new container"))
		})

		ginkgo.It("should handle ContainerStart NotFound error", func() {
			client.startFunc = func(_ context.Context, _ string, _ dockerContainer.StartOptions) error {
				return cerrdefs.ErrNotFound
			}
			newID, err := StartTargetContainer(
				context.Background(), client, mockCont, networkConfig,
				true, "1.44", flags.DockerAPIMinVersion, false, "auto", false,
			)
			gomega.Expect(newID).To(gomega.Equal(types.ContainerID("new_container_id")))
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to start container"))
		})

		ginkgo.It("should log successful container start", func() {
			newID, err := StartTargetContainer(
				context.Background(), client, mockCont, networkConfig,
				true, "1.44", flags.DockerAPIMinVersion, false, "auto", false,
			)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(newID).To(gomega.Equal(types.ContainerID("new_container_id")))
			gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("Started new container"))
			gomega.Expect(logOutput.String()).To(gomega.ContainSubstring(`new_id=new_container_id`))
		})

		ginkgo.It("should attach multiple networks for legacy API and handle success", func() {
			mockCont = MockContainer(
				WithNetworkMode("bridge"),
				WithContainerState(dockerContainer.State{Running: true, Status: "running"}),
				WithNetworks("network1", "network2"),
			)
			networkConfig = getNetworkConfig(mockCont, "1.23")
			newID, err := StartTargetContainer(
				context.Background(), client, mockCont, networkConfig,
				true, "1.23", flags.DockerAPIMinVersion, false, "auto", false,
			)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(newID).To(gomega.Equal(types.ContainerID("new_container_id")))
			gomega.Expect(logOutput.String()).
				To(gomega.ContainSubstring("Selected first network for container creation"))
			gomega.Expect(logOutput.String()).
				To(gomega.ContainSubstring("Attaching additional network to container"))
			gomega.Expect(logOutput.String()).
				To(gomega.ContainSubstring("Successfully attached additional network"))
		})

		ginkgo.It("should attach multiple networks for legacy API and handle failure", func() {
			mockCont = MockContainer(
				WithNetworkMode("bridge"),
				WithContainerState(dockerContainer.State{Running: true, Status: "running"}),
				WithNetworks("network1", "network2"),
			)
			networkConfig = getNetworkConfig(mockCont, "1.23")
			connectErr := errors.New("network connect failed")
			client.connectFunc = func(_ context.Context, _, _ string, _ *dockerNetwork.EndpointSettings) error {
				return connectErr
			}
			client.removeFunc = func(_ context.Context, _ string, _ dockerContainer.RemoveOptions) error {
				return nil
			}
			newID, err := StartTargetContainer(
				context.Background(), client, mockCont, networkConfig,
				true, "1.23", flags.DockerAPIMinVersion, false, "auto", false,
			)
			gomega.Expect(newID).To(gomega.BeEmpty())
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("network connect failed"))
			gomega.Expect(logOutput.String()).
				To(gomega.ContainSubstring("Failed to attach additional network"))
		})

		ginkgo.Describe("Context Propagation for Cleanup", func() {
			ginkgo.It("should perform cleanup with non-canceled context after rename failure", func() {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()

				client.renameFunc = func(_ context.Context, _, _ string) error {
					return errors.New("rename failed")
				}

				client.removeFunc = func(cleanupCtx context.Context, _ string, _ dockerContainer.RemoveOptions) error {
					client.removeFuncCalled.Store(true)

					select {
					case <-cleanupCtx.Done():
						ginkgo.Fail("Cleanup context should not be canceled")
					default:
					}

					return nil
				}

				newID, err := StartTargetContainer(
					ctx, client, mockCont, networkConfig,
					true, "1.44", flags.DockerAPIMinVersion, false, "auto", false,
				)
				gomega.Expect(newID).To(gomega.BeEmpty())
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(err.Error()).To(gomega.ContainSubstring("rename failed"))
				gomega.Expect(client.removeFuncCalled.Load()).To(gomega.BeTrue())
			})

			ginkgo.It("should perform cleanup with non-canceled context after network attachment failure", func() {
				mockCont = MockContainer(
					WithNetworkMode("bridge"),
					WithContainerState(dockerContainer.State{Running: true, Status: "running"}),
					WithNetworks("network1", "network2"),
				)
				networkConfig = getNetworkConfig(mockCont, "1.23")

				ctx, cancel := context.WithCancel(context.Background())
				cancel()

				client.connectFunc = func(_ context.Context, _, _ string, _ *dockerNetwork.EndpointSettings) error {
					return errors.New("network connect failed")
				}

				client.removeFunc = func(cleanupCtx context.Context, _ string, _ dockerContainer.RemoveOptions) error {
					client.removeFuncCalled.Store(true)

					select {
					case <-cleanupCtx.Done():
						ginkgo.Fail("Cleanup context should not be canceled")
					default:
					}

					return nil
				}

				newID, err := StartTargetContainer(
					ctx, client, mockCont, networkConfig,
					true, "1.23", flags.DockerAPIMinVersion, false, "auto", false,
				)
				gomega.Expect(newID).To(gomega.BeEmpty())
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(err.Error()).To(gomega.ContainSubstring("network connect failed"))
				gomega.Expect(client.removeFuncCalled.Load()).To(gomega.BeTrue())
			})
		})
	})

	ginkgo.Describe("RenameTargetContainer", func() {
		var client *MockClient

		ginkgo.BeforeEach(func() {
			client = &MockClient{}
		})

		ginkgo.It("should rename container successfully", func() {
			mockCont := MockContainer(WithName("old-name"), WithID("test-id"))
			err := RenameTargetContainer(context.Background(), client, mockCont, "new-name")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("Renamed container successfully"))
		})

		ginkgo.It("should return error when rename fails", func() {
			client.renameFunc = func(_ context.Context, _, _ string) error {
				return errors.New("rename failed")
			}
			mockCont := MockContainer(WithName("old-name"), WithID("test-id"))
			err := RenameTargetContainer(context.Background(), client, mockCont, "new-name")
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("rename failed"))
			gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("Failed to rename container"))
		})
	})

	ginkgo.Describe("attachNetworks", func() {
		var client *MockClient

		ginkgo.BeforeEach(func() {
			client = &MockClient{}
		})

		ginkgo.It("should attach additional networks successfully", func() {
			clog := logrus.WithField("test", "attachNetworks")
			fullConfig := &dockerNetwork.NetworkingConfig{
				EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
					"network1": {NetworkID: "net1"},
					"network2": {NetworkID: "net2"},
				},
			}
			initialConfig := &dockerNetwork.NetworkingConfig{
				EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
					"network1": {NetworkID: "net1"},
				},
			}

			err := attachNetworks(context.Background(), client, "container-id", fullConfig, initialConfig, clog)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("Attaching additional network to container"))
			gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("Successfully attached additional network"))
		})

		ginkgo.It("should return error when network connect fails", func() {
			client.connectFunc = func(_ context.Context, _, _ string, _ *dockerNetwork.EndpointSettings) error {
				return errors.New("connect failed")
			}
			clog := logrus.WithField("test", "attachNetworks")
			fullConfig := &dockerNetwork.NetworkingConfig{
				EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
					"network1": {NetworkID: "net1"},
					"network2": {NetworkID: "net2"},
				},
			}
			initialConfig := &dockerNetwork.NetworkingConfig{
				EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
					"network1": {NetworkID: "net1"},
				},
			}

			err := attachNetworks(context.Background(), client, "container-id", fullConfig, initialConfig, clog)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to attach network"))
			gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("Failed to attach additional network"))
		})

		ginkgo.It("should skip initial network and only attach additional ones", func() {
			clog := logrus.WithField("test", "attachNetworks")
			fullConfig := &dockerNetwork.NetworkingConfig{
				EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
					"network1": {NetworkID: "net1"},
				},
			}
			initialConfig := &dockerNetwork.NetworkingConfig{
				EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
					"network1": {NetworkID: "net1"},
				},
			}

			err := attachNetworks(context.Background(), client, "container-id", fullConfig, initialConfig, clog)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(logOutput.String()).ToNot(gomega.ContainSubstring("Attaching additional network"))
		})
	})
})
