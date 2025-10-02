package container

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/sirupsen/logrus"

	dockerContainerType "github.com/docker/docker/api/types/container"
	dockerImageType "github.com/docker/docker/api/types/image"
	dockerMountType "github.com/docker/docker/api/types/mount"
	dockerNetworkType "github.com/docker/docker/api/types/network"
	dockerNat "github.com/docker/go-connections/nat"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/nicholas-fedor/watchtower/internal/flags"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

var _ = ginkgo.Describe("Container", func() {
	ginkgo.Describe("Configuration Validation", func() {
		ginkgo.It("returns an error when image info is nil", func() {
			c := MockContainer(WithPortBindings())
			c.imageInfo = nil
			err := c.VerifyConfiguration()
			gomega.Expect(err).To(gomega.Equal(errNoImageInfo))
		})

		ginkgo.It("returns an error when container info is nil", func() {
			c := MockContainer(WithPortBindings())
			c.containerInfo = nil
			err := c.VerifyConfiguration()
			gomega.Expect(err).To(gomega.Equal(errNoContainerInfo))
		})

		ginkgo.It("returns an error when config is nil", func() {
			c := MockContainer(WithPortBindings())
			c.containerInfo.Config = nil
			err := c.VerifyConfiguration()
			gomega.Expect(err).To(gomega.Equal(errInvalidConfig))
		})

		ginkgo.It("returns an error when host config is nil", func() {
			c := MockContainer(WithPortBindings())
			c.containerInfo.HostConfig = nil
			err := c.VerifyConfiguration()
			gomega.Expect(err).To(gomega.Equal(errInvalidConfig))
		})

		ginkgo.It("succeeds with no port bindings", func() {
			c := MockContainer(WithPortBindings())
			err := c.VerifyConfiguration()
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.It("initializes exposed ports when nil with port bindings", func() {
			c := MockContainer(WithPortBindings("80/tcp"))
			c.containerInfo.Config.ExposedPorts = nil
			gomega.Expect(c.VerifyConfiguration()).To(gomega.Succeed())
			gomega.Expect(c.containerInfo.Config.ExposedPorts).ToNot(gomega.BeNil())
			gomega.Expect(c.containerInfo.Config.ExposedPorts).To(gomega.BeEmpty())
		})

		ginkgo.It("succeeds with non-nil exposed ports and port bindings", func() {
			c := MockContainer(WithPortBindings("80/tcp"))
			c.containerInfo.Config.ExposedPorts = map[dockerNat.Port]struct{}{"80/tcp": {}}
			err := c.VerifyConfiguration()
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})
	})

	ginkgo.Describe("Create Configuration", func() {
		ginkgo.Context("when container and image healthcheck configs are identical", func() {
			ginkgo.It("returns an empty healthcheck config", func() {
				tests := []dockerContainerType.HealthConfig{
					{Test: []string{"/usr/bin/sleep", "1s"}},
					{Timeout: 30},
					{StartPeriod: 30},
					{Retries: 30},
				}
				for _, healthConfig := range tests {
					c := MockContainer(
						WithHealthcheck(healthConfig),
						WithImageHealthcheck(healthConfig),
					)
					gomega.Expect(c.GetCreateConfig().Healthcheck).
						To(gomega.Equal(&dockerContainerType.HealthConfig{}))
				}
			})
		})

		ginkgo.It("returns container healthcheck when configs differ", func() {
			c := MockContainer(
				WithHealthcheck(dockerContainerType.HealthConfig{
					Test:        []string{"/usr/bin/sleep", "1s"},
					Interval:    30,
					Timeout:     30,
					StartPeriod: 10,
					Retries:     2,
				}),
				WithImageHealthcheck(dockerContainerType.HealthConfig{
					Test:        []string{"/usr/bin/sleep", "10s"},
					Interval:    10,
					Timeout:     60,
					StartPeriod: 30,
					Retries:     10,
				}),
			)
			gomega.Expect(c.GetCreateConfig().Healthcheck).
				To(gomega.Equal(&dockerContainerType.HealthConfig{
					Test:        []string{"/usr/bin/sleep", "1s"},
					Interval:    30,
					Timeout:     30,
					StartPeriod: 10,
					Retries:     2,
				}))
		})

		ginkgo.It("handles empty container healthcheck config without panic", func() {
			c := MockContainer(WithImageHealthcheck(dockerContainerType.HealthConfig{
				Test:        []string{"/usr/bin/sleep", "10s"},
				Interval:    10,
				Timeout:     60,
				StartPeriod: 30,
				Retries:     10,
			}))
			gomega.Expect(c.GetCreateConfig().Healthcheck).To(gomega.BeNil())
		})

		ginkgo.It("handles empty image healthcheck config without panic", func() {
			c := MockContainer(WithHealthcheck(dockerContainerType.HealthConfig{
				Test:        []string{"/usr/bin/sleep", "1s"},
				Interval:    30,
				Timeout:     30,
				StartPeriod: 10,
				Retries:     2,
			}))
			gomega.Expect(c.GetCreateConfig().Healthcheck).
				To(gomega.Equal(&dockerContainerType.HealthConfig{
					Test:        []string{"/usr/bin/sleep", "1s"},
					Interval:    30,
					Timeout:     30,
					StartPeriod: 10,
					Retries:     2,
				}))
		})
	})

	ginkgo.Describe("Metadata Retrieval", func() {
		var container *Container

		ginkgo.BeforeEach(func() {
			container = MockContainer(WithLabels(map[string]string{
				"com.centurylinklabs.watchtower.enable": "true",
				"com.centurylinklabs.watchtower":        "true",
			}))
		})

		ginkgo.It("returns correct container name", func() {
			name := container.Name()
			gomega.Expect(name).To(gomega.Equal("test-watchtower"))
			gomega.Expect(name).NotTo(gomega.Equal("wrong-name"))
		})

		ginkgo.It("returns correct container ID", func() {
			id := container.ID()
			gomega.Expect(id).To(gomega.BeEquivalentTo("container_id"))
			gomega.Expect(id).NotTo(gomega.BeEquivalentTo("wrong-id"))
		})

		ginkgo.It("returns true for enabled label when set to true", func() {
			enabled, exists := container.Enabled()
			gomega.Expect(enabled).To(gomega.BeTrue())
			gomega.Expect(exists).To(gomega.BeTrue())
		})

		ginkgo.It("returns false and true for enabled label when set to false", func() {
			container = MockContainer(WithLabels(map[string]string{
				"com.centurylinklabs.watchtower.enable": "false",
			}))
			enabled, exists := container.Enabled()
			gomega.Expect(enabled).To(gomega.BeFalse())
			gomega.Expect(exists).To(gomega.BeTrue())
		})

		ginkgo.It("returns false and false when enabled label is not set", func() {
			container = MockContainer(WithLabels(map[string]string{"lol": "false"}))
			enabled, exists := container.Enabled()
			gomega.Expect(enabled).To(gomega.BeFalse())
			gomega.Expect(exists).To(gomega.BeFalse())
		})

		ginkgo.It("returns false and false for invalid enabled label", func() {
			container = MockContainer(WithLabels(map[string]string{
				"com.centurylinklabs.watchtower.enable": "falsy",
			}))
			enabled, exists := container.Enabled()
			gomega.Expect(enabled).To(gomega.BeFalse())
			gomega.Expect(exists).To(gomega.BeFalse())
		})

		ginkgo.Context("checking Watchtower instance", func() {
			ginkgo.It("returns true when Watchtower label is true", func() {
				isWatchtower := container.IsWatchtower()
				gomega.Expect(isWatchtower).To(gomega.BeTrue())
			})

			ginkgo.It("returns false when Watchtower label is false", func() {
				container = MockContainer(WithLabels(map[string]string{
					"com.centurylinklabs.watchtower": "false",
				}))
				isWatchtower := container.IsWatchtower()
				gomega.Expect(isWatchtower).To(gomega.BeFalse())
			})

			ginkgo.It("returns false when Watchtower label is not set", func() {
				container = MockContainer(WithLabels(map[string]string{"funny.label": "false"}))
				isWatchtower := container.IsWatchtower()
				gomega.Expect(isWatchtower).To(gomega.BeFalse())
			})

			ginkgo.It("returns false when no labels are set", func() {
				container = MockContainer(WithLabels(map[string]string{}))
				isWatchtower := container.IsWatchtower()
				gomega.Expect(isWatchtower).To(gomega.BeFalse())
			})
		})

		ginkgo.Context("fetching stop signal", func() {
			ginkgo.It("returns signal when set", func() {
				container = MockContainer(WithLabels(map[string]string{
					"com.centurylinklabs.watchtower.stop-signal": "SIGKILL",
				}))
				gomega.Expect(container.StopSignal()).To(gomega.Equal("SIGKILL"))
			})

			ginkgo.It("returns SIGTERM when signal is not set", func() {
				container = MockContainer(WithLabels(map[string]string{}))
				gomega.Expect(container.StopSignal()).To(gomega.Equal("SIGTERM"))
			})
		})

		ginkgo.Context("fetching image name", func() {
			ginkgo.It("uses zodiac label when present", func() {
				container = MockContainer(WithLabels(map[string]string{
					"com.centurylinklabs.zodiac.original-image": "the-original-image",
				}))
				gomega.Expect(container.ImageName()).To(gomega.Equal("the-original-image:latest"))
			})

			ginkgo.It("returns image name from config", func() {
				name := "image-name:3"
				container = MockContainer(WithImageName(name))
				gomega.Expect(container.ImageName()).To(gomega.Equal(name))
			})

			ginkgo.It("appends latest tag when no tag is supplied", func() {
				name := "image-name"
				container = MockContainer(WithImageName(name))
				gomega.Expect(container.ImageName()).To(gomega.Equal(name + ":latest"))
			})
		})

		ginkgo.Context("fetching image ID", func() {
			ginkgo.It("returns image ID when imageInfo is available", func() {
				imageID := container.ImageID()
				gomega.Expect(imageID).To(gomega.Equal(types.ImageID("image_id")))
			})

			ginkgo.It("returns empty string for ImageID when imageInfo is nil", func() {
				container = MockContainer(WithPortBindings())
				container.imageInfo = nil
				imageID := container.ImageID()
				gomega.Expect(imageID).To(gomega.Equal(types.ImageID("")))
			})

			ginkgo.It("returns empty string for SafeImageID when imageInfo is nil", func() {
				container = MockContainer(WithPortBindings())
				container.imageInfo = nil
				imageID := container.SafeImageID()
				gomega.Expect(imageID).To(gomega.Equal(types.ImageID("")))
			})
		})

		ginkgo.Context("fetching container links", func() {
			ginkgo.When("depends-on label is present", func() {
				ginkgo.It("returns single dependent container", func() {
					container = MockContainer(WithLabels(map[string]string{
						"com.centurylinklabs.watchtower.depends-on": "postgres",
					}))
					links := container.Links()
					gomega.Expect(links).To(gomega.SatisfyAll(
						gomega.ContainElement("/postgres"),
						gomega.HaveLen(1),
					))
				})

				ginkgo.It("returns multiple dependent containers", func() {
					container = MockContainer(WithLabels(map[string]string{
						"com.centurylinklabs.watchtower.depends-on": "postgres,redis",
					}))
					links := container.Links()
					gomega.Expect(links).To(gomega.SatisfyAll(
						gomega.ContainElement("/postgres"),
						gomega.ContainElement("/redis"),
						gomega.HaveLen(2),
					))
				})

				ginkgo.It("normalizes container names with slashes", func() {
					container = MockContainer(WithLabels(map[string]string{
						"com.centurylinklabs.watchtower.depends-on": "/postgres,redis",
					}))
					links := container.Links()
					gomega.Expect(links).To(gomega.SatisfyAll(
						gomega.ContainElement("/postgres"),
						gomega.ContainElement("/redis"),
					))
				})

				ginkgo.It("returns empty links for blank depends-on label", func() {
					container = MockContainer(WithLabels(map[string]string{
						"com.centurylinklabs.watchtower.depends-on": "",
					}))
					gomega.Expect(container.Links()).To(gomega.BeEmpty())
				})
			})

			ginkgo.It("returns links from host config when depends-on label is absent", func() {
				container = MockContainer(WithLinks([]string{
					"redis:test-watchtower",
					"postgres:test-watchtower",
				}))
				links := container.Links()
				gomega.Expect(links).To(gomega.SatisfyAll(
					gomega.ContainElement("redis"),
					gomega.ContainElement("postgres"),
					gomega.HaveLen(2),
				))
			})
		})

		ginkgo.It("returns pre and post update timeout values", func() {
			container = MockContainer(WithLabels(map[string]string{
				"com.centurylinklabs.watchtower.lifecycle.pre-update-timeout":  "3",
				"com.centurylinklabs.watchtower.lifecycle.post-update-timeout": "5",
			}))
			gomega.Expect(container.PreUpdateTimeout()).To(gomega.Equal(3))
			gomega.Expect(container.PostUpdateTimeout()).To(gomega.Equal(5))
		})
	})

	ginkgo.Describe("No-Pull Configuration", func() {
		ginkgo.When("no-pull argument is not set", func() {
			ginkgo.It("returns true when no-pull label is true", func() {
				c := MockContainer(WithLabels(map[string]string{
					"com.centurylinklabs.watchtower.no-pull": "true",
				}))
				gomega.Expect(c.IsNoPull(types.UpdateParams{})).To(gomega.BeTrue())
			})

			ginkgo.It("returns false when no-pull label is false", func() {
				c := MockContainer(WithLabels(map[string]string{
					"com.centurylinklabs.watchtower.no-pull": "false",
				}))
				gomega.Expect(c.IsNoPull(types.UpdateParams{})).To(gomega.BeFalse())
			})

			ginkgo.It("returns false for invalid no-pull label", func() {
				c := MockContainer(WithLabels(map[string]string{
					"com.centurylinklabs.watchtower.no-pull": "maybe",
				}))
				gomega.Expect(c.IsNoPull(types.UpdateParams{})).To(gomega.BeFalse())
			})

			ginkgo.It("returns false when no-pull label is unset", func() {
				c := MockContainer(WithLabels(map[string]string{}))
				gomega.Expect(c.IsNoPull(types.UpdateParams{})).To(gomega.BeFalse())
			})
		})

		ginkgo.When("no-pull argument is true", func() {
			ginkgo.It("returns true when no-pull label is true", func() {
				c := MockContainer(WithLabels(map[string]string{
					"com.centurylinklabs.watchtower.no-pull": "true",
				}))
				gomega.Expect(c.IsNoPull(types.UpdateParams{NoPull: true})).To(gomega.BeTrue())
			})

			ginkgo.It("returns true when no-pull label is false", func() {
				c := MockContainer(WithLabels(map[string]string{
					"com.centurylinklabs.watchtower.no-pull": "false",
				}))
				gomega.Expect(c.IsNoPull(types.UpdateParams{NoPull: true})).To(gomega.BeTrue())
			})

			ginkgo.When("label-take-precedence is true", func() {
				ginkgo.It("returns true when no-pull label is true", func() {
					c := MockContainer(WithLabels(map[string]string{
						"com.centurylinklabs.watchtower.no-pull": "true",
					}))
					gomega.Expect(c.IsNoPull(types.UpdateParams{LabelPrecedence: true, NoPull: true})).
						To(gomega.BeTrue())
				})

				ginkgo.It("returns false when no-pull label is false", func() {
					c := MockContainer(WithLabels(map[string]string{
						"com.centurylinklabs.watchtower.no-pull": "false",
					}))
					gomega.Expect(c.IsNoPull(types.UpdateParams{LabelPrecedence: true, NoPull: true})).
						To(gomega.BeFalse())
				})
			})
		})
	})

	ginkgo.Describe("Network Configuration", func() {
		var logOutput *bytes.Buffer

		ginkgo.BeforeEach(func() {
			logOutput = &bytes.Buffer{}
			logrus.SetOutput(logOutput)
			logrus.SetLevel(logrus.DebugLevel)
		})

		ginkgo.Context("using bridge network mode", func() {
			ginkgo.It("preserves IP and MAC addresses for running containers", func() {
				container := MockContainer(
					WithNetworkMode("bridge"),
					WithContainerState(dockerContainerType.State{Running: true, Status: "running"}),
					WithNetworkSettings(map[string]*dockerNetworkType.EndpointSettings{
						"bridge": {
							IPAddress:  "172.17.0.2",
							MacAddress: "02:42:ac:11:00:02",
							Aliases:    nil,
							DNSNames:   nil,
						},
					}),
				)

				config := getNetworkConfig(container, "1.44")
				gomega.Expect(config.EndpointsConfig).To(gomega.HaveKey("bridge"))
				endpoint := config.EndpointsConfig["bridge"]
				gomega.Expect(endpoint.Aliases).To(gomega.BeEmpty(), "Aliases should not be set")
				gomega.Expect(endpoint.DNSNames).To(gomega.BeEmpty(), "DNSNames should not be set")
				gomega.Expect(endpoint.IPAddress).To(gomega.Equal("172.17.0.2"))
				gomega.Expect(endpoint.MacAddress).To(gomega.Equal("02:42:ac:11:00:02"))
				gomega.Expect(logOutput.String()).
					To(gomega.ContainSubstring("Found MAC address in config"))
			})

			ginkgo.It("logs warning for missing MAC address in running containers", func() {
				logrus.SetLevel(logrus.WarnLevel)
				container := MockContainer(
					WithNetworkMode("bridge"),
					WithContainerState(dockerContainerType.State{Running: true, Status: "running"}),
					WithNetworkSettings(map[string]*dockerNetworkType.EndpointSettings{
						"bridge": {
							IPAddress:  "172.17.0.2",
							MacAddress: "",
							Aliases:    nil,
							DNSNames:   nil,
						},
					}),
				)

				config := getNetworkConfig(container, "1.49")
				gomega.Expect(config.EndpointsConfig).To(gomega.HaveKey("bridge"))
				gomega.Expect(logOutput.String()).To(gomega.ContainSubstring(
					"Negotiated API version 1.49 is at least 1.44 but no MAC address found; preservation may not be supported",
				))
				gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("state=running"))
			})

			ginkgo.Context("for non-running containers", func() {
				ginkgo.It("logs debug message for missing MAC address in created state", func() {
					container := MockContainer(
						WithNetworkMode("bridge"),
						WithContainerState(
							dockerContainerType.State{Running: false, Status: "created"},
						),
						WithNetworkSettings(map[string]*dockerNetworkType.EndpointSettings{
							"bridge": {
								IPAddress:  "",
								MacAddress: "",
								Aliases:    nil,
								DNSNames:   nil,
							},
						}),
					)

					config := getNetworkConfig(container, "1.49")
					gomega.Expect(config.EndpointsConfig).To(gomega.HaveKey("bridge"))
					gomega.Expect(logOutput.String()).To(gomega.ContainSubstring(
						"No MAC address found for non-running container"))
					gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("state=created"))
					gomega.Expect(logOutput.String()).NotTo(gomega.ContainSubstring(
						"Negotiated API version 1.49 is at least 1.44 but no MAC address found"))
				})

				ginkgo.It("logs debug message for missing MAC address in exited state", func() {
					container := MockContainer(
						WithNetworkMode("bridge"),
						WithContainerState(
							dockerContainerType.State{Running: false, Status: "exited"},
						),
						WithNetworkSettings(map[string]*dockerNetworkType.EndpointSettings{
							"bridge": {
								IPAddress:  "",
								MacAddress: "",
								Aliases:    nil,
								DNSNames:   nil,
							},
						}),
					)

					config := getNetworkConfig(container, "1.49")
					gomega.Expect(config.EndpointsConfig).To(gomega.HaveKey("bridge"))
					gomega.Expect(logOutput.String()).To(gomega.ContainSubstring(
						"No MAC address found for non-running container"))
					gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("state=exited"))
					gomega.Expect(logOutput.String()).NotTo(gomega.ContainSubstring(
						"Negotiated API version 1.49 is at least 1.44 but no MAC address found"))
				})
			})
		})

		ginkgo.Context("using host network mode", func() {
			ginkgo.It("includes host endpoint with no aliases or DNS names", func() {
				container := MockContainer(
					WithNetworkMode("host"),
					WithContainerState(dockerContainerType.State{Running: true, Status: "running"}),
					WithNetworkSettings(map[string]*dockerNetworkType.EndpointSettings{
						"host": {
							IPAddress:  "",
							MacAddress: "",
							Aliases:    nil,
							DNSNames:   nil,
						},
					}),
				)

				logrus.WithFields(logrus.Fields{
					"network_mode": container.containerInfo.HostConfig.NetworkMode,
					"network_mode_type": fmt.Sprintf(
						"%T",
						container.containerInfo.HostConfig.NetworkMode,
					),
					"network_mode_str": string(container.containerInfo.HostConfig.NetworkMode),
					"test":             "host_network_mode",
				}).Debug("Test network mode check")

				config := getNetworkConfig(container, "1.44")
				gomega.Expect(config.EndpointsConfig).To(gomega.HaveKey("host"))
				endpoint := config.EndpointsConfig["host"]
				gomega.Expect(endpoint.Aliases).
					To(gomega.BeEmpty(), "Aliases should not be set for host mode")
				gomega.Expect(endpoint.DNSNames).
					To(gomega.BeEmpty(), "DNSNames should not be set for host mode")
				gomega.Expect(endpoint.IPAddress).To(gomega.BeEmpty())
				gomega.Expect(endpoint.MacAddress).To(gomega.BeEmpty())
				gomega.Expect(container.containerInfo.HostConfig.NetworkMode).To(gomega.Equal(
					dockerContainerType.NetworkMode("host")))
			})

			ginkgo.It("clears non-empty aliases and DNS names", func() {
				container := MockContainer(
					WithNetworkMode("host"),
					WithContainerState(dockerContainerType.State{Running: true, Status: "running"}),
					WithNetworkSettings(map[string]*dockerNetworkType.EndpointSettings{
						"host": {
							IPAddress:  "192.168.1.1",
							MacAddress: "02:42:ac:11:00:02",
							Aliases:    []string{"test-alias"},
							DNSNames:   []string{"test-dns"},
						},
					}),
				)

				config := getNetworkConfig(container, "1.44")
				gomega.Expect(config.EndpointsConfig).To(gomega.HaveKey("host"))
				endpoint := config.EndpointsConfig["host"]
				gomega.Expect(endpoint.Aliases).
					To(gomega.BeEmpty(), "Aliases should be cleared for host mode")
				gomega.Expect(endpoint.DNSNames).
					To(gomega.BeEmpty(), "DNSNames should be cleared for host mode")
				gomega.Expect(endpoint.IPAddress).
					To(gomega.BeEmpty(), "IPAddress should be cleared for host mode")
				gomega.Expect(endpoint.MacAddress).
					To(gomega.BeEmpty(), "MacAddress should be cleared for host mode")
			})

			ginkgo.It("logs no MAC address for host mode in debugLogMacAddress", func() {
				container := MockContainer(
					WithNetworkMode("host"),
					WithContainerState(dockerContainerType.State{Running: true, Status: "running"}),
					WithNetworkSettings(map[string]*dockerNetworkType.EndpointSettings{
						"host": {
							IPAddress:  "",
							MacAddress: "",
							Aliases:    nil,
							DNSNames:   nil,
						},
					}),
				)

				config := getNetworkConfig(container, "1.44")
				debugLogMacAddress(
					config,
					types.ContainerID("test-id"),
					"1.44",
					flags.DockerAPIMinVersion,
					true,
				)
				gomega.Expect(logOutput.String()).To(gomega.ContainSubstring(
					"No MAC address in host network mode, as expected"))
			})
		})

		ginkgo.Context("using legacy API version (< 1.44)", func() {
			ginkgo.It("clears MAC address and DNS names", func() {
				container := MockContainer(
					WithNetworkMode("bridge"),
					WithContainerState(dockerContainerType.State{Running: true, Status: "running"}),
					WithNetworkSettings(map[string]*dockerNetworkType.EndpointSettings{
						"bridge": {
							IPAddress:  "172.17.0.2",
							MacAddress: "02:42:ac:11:00:02",
							Aliases:    []string{"old-alias"},
							DNSNames:   []string{"old-dns"},
						},
					}),
				)

				config := getNetworkConfig(container, "1.43")
				gomega.Expect(config.EndpointsConfig).To(gomega.HaveKey("bridge"))
				endpoint := config.EndpointsConfig["bridge"]
				gomega.Expect(endpoint.IPAddress).
					To(gomega.BeEmpty(), "IPAddress should be cleared for legacy API")
				gomega.Expect(endpoint.MacAddress).
					To(gomega.BeEmpty(), "MAC address should be cleared for legacy API")
				gomega.Expect(endpoint.Aliases).
					To(gomega.ContainElement("old-alias"), "Aliases should be preserved")
				gomega.Expect(endpoint.DNSNames).
					To(gomega.BeEmpty(), "DNSNames should be cleared for legacy API")
			})

			ginkgo.It("logs no MAC address in debugLogMacAddress", func() {
				container := MockContainer(
					WithNetworkMode("bridge"),
					WithContainerState(dockerContainerType.State{Running: true, Status: "running"}),
					WithNetworkSettings(map[string]*dockerNetworkType.EndpointSettings{
						"bridge": {
							IPAddress:  "172.17.0.2",
							MacAddress: "02:42:ac:11:00:02",
						},
					}),
				)

				config := getNetworkConfig(container, "1.43")
				debugLogMacAddress(
					config,
					types.ContainerID("test-id"),
					"1.43",
					flags.DockerAPIMinVersion,
					false,
				)
				gomega.Expect(logOutput.String()).To(gomega.ContainSubstring(
					"No MAC address in legacy config, as expected"))
			})
		})

		ginkgo.Context("with multiple networks", func() {
			ginkgo.BeforeEach(func() {
				logrus.SetLevel(logrus.DebugLevel)
			})

			ginkgo.It(
				"preserves all networks and clears MAC addresses for legacy API (< 1.44)",
				func() {
					container := MockContainer(
						WithNetworkMode("bridge"),
						WithContainerState(
							dockerContainerType.State{Running: true, Status: "running"},
						),
						WithNetworks("network1", "network2"),
						WithImageName("test-image:latest"),
					)

					config := getNetworkConfig(container, "1.43")
					gomega.Expect(config.EndpointsConfig).
						To(gomega.HaveKey("network1"), "Should include first network")
					gomega.Expect(config.EndpointsConfig).
						To(gomega.HaveKey("network2"), "Should include second network")
					gomega.Expect(config.EndpointsConfig["network1"].MacAddress).
						To(gomega.BeEmpty(),
							"MAC address should be cleared for network1")
					gomega.Expect(config.EndpointsConfig["network2"].MacAddress).
						To(gomega.BeEmpty(),
							"MAC address should be cleared for network2")
					gomega.Expect(config.EndpointsConfig["network1"].Aliases).
						To(gomega.ContainElement("test-watchtower"),
							"Aliases should match mock setup for network1")
					gomega.Expect(config.EndpointsConfig["network2"].Aliases).
						To(gomega.ContainElement("test-watchtower"),
							"Aliases should match mock setup for network2")
					gomega.Expect(logOutput.String()).
						To(gomega.ContainSubstring("No MAC address in legacy config, as expected"))
				},
			)

			ginkgo.It(
				"preserves all networks with IP and MAC addresses for modern API (>= 1.44)",
				func() {
					container := MockContainer(
						WithNetworkMode("bridge"),
						WithContainerState(
							dockerContainerType.State{Running: true, Status: "running"},
						),
						WithNetworkSettings(map[string]*dockerNetworkType.EndpointSettings{
							"network1": {
								NetworkID:  "network_network1_id",
								IPAddress:  "172.17.0.2",
								MacAddress: "02:42:ac:11:00:02",
								Aliases:    []string{"test-watchtower"},
							},
							"network2": {
								NetworkID:  "network_network2_id",
								IPAddress:  "172.18.0.2",
								MacAddress: "02:42:ac:12:00:02",
								Aliases:    []string{"test-watchtower"},
							},
						}),
						WithImageName("test-image:latest"),
					)

					config := getNetworkConfig(container, "1.44")
					gomega.Expect(config.EndpointsConfig).
						To(gomega.HaveKey("network1"), "Should include first network")
					gomega.Expect(config.EndpointsConfig).
						To(gomega.HaveKey("network2"), "Should include second network")
					gomega.Expect(config.EndpointsConfig["network1"].IPAddress).
						To(gomega.Equal("172.17.0.2"),
							"IPAddress should be preserved for network1")
					gomega.Expect(config.EndpointsConfig["network1"].MacAddress).
						To(gomega.Equal("02:42:ac:11:00:02"),
							"MAC address should be preserved for network1")
					gomega.Expect(config.EndpointsConfig["network2"].IPAddress).
						To(gomega.Equal("172.18.0.2"),
							"IPAddress should be preserved for network2")
					gomega.Expect(config.EndpointsConfig["network2"].MacAddress).
						To(gomega.Equal("02:42:ac:12:00:02"),
							"MAC address should be preserved for network2")
					gomega.Expect(config.EndpointsConfig["network1"].Aliases).
						To(gomega.ContainElement("test-watchtower"),
							"Aliases should match mock setup for network1")
					gomega.Expect(config.EndpointsConfig["network2"].Aliases).
						To(gomega.ContainElement("test-watchtower"),
							"Aliases should match mock setup for network2")
					gomega.Expect(logOutput.String()).
						To(gomega.ContainSubstring("Found MAC address in config"))
				},
			)

			ginkgo.It("returns empty config when network settings are empty", func() {
				container := MockContainer(
					WithNetworkMode("bridge"),
					WithContainerState(dockerContainerType.State{Running: true, Status: "running"}),
					WithNetworkSettings(map[string]*dockerNetworkType.EndpointSettings{}),
					WithImageName("test-image:latest"),
				)

				config := getNetworkConfig(container, "1.44")
				gomega.Expect(config.EndpointsConfig).
					To(gomega.BeEmpty(), "Network config should have no endpoints")
				gomega.Expect(logOutput.String()).To(gomega.ContainSubstring(
					"Negotiated API version 1.44 is at least 1.44 but no MAC address found"))
				gomega.Expect(logOutput.String()).
					To(gomega.ContainSubstring("no MAC address found in non-host network config"))
			})

			ginkgo.It("returns empty config when network settings are nil", func() {
				container := MockContainer(
					WithNetworkMode("bridge"),
					WithContainerState(dockerContainerType.State{Running: true, Status: "running"}),
					WithImageName("test-image:latest"),
					func(c *dockerContainerType.InspectResponse, _ *dockerImageType.InspectResponse) {
						c.NetworkSettings = nil
					},
				)

				config := getNetworkConfig(container, "1.44")
				gomega.Expect(config.EndpointsConfig).
					To(gomega.BeEmpty(), "Network config should have no endpoints")
				gomega.Expect(logOutput.String()).
					To(gomega.ContainSubstring("No network settings available"))
			})
		})
	})

	ginkgo.Describe("MAC Address Validation", func() {
		var logOutput *bytes.Buffer

		ginkgo.BeforeEach(func() {
			logOutput = &bytes.Buffer{}
			logrus.SetOutput(logOutput)
			logrus.SetLevel(logrus.WarnLevel)
		})

		ginkgo.It(
			"returns error and logs warning for running container with no MAC address",
			func() {
				container := MockContainer(
					WithNetworkMode("bridge"),
					WithContainerState(dockerContainerType.State{Running: true, Status: "running"}),
					WithNetworkSettings(map[string]*dockerNetworkType.EndpointSettings{
						"bridge": {MacAddress: ""},
					}),
				)

				config := &dockerNetworkType.NetworkingConfig{
					EndpointsConfig: map[string]*dockerNetworkType.EndpointSettings{
						"bridge": {MacAddress: ""},
					},
				}

				err := validateMacAddresses(config, container.ID(), "1.49", false, container)
				gomega.Expect(err).To(gomega.Equal(errNoMacInNonHost))
				gomega.Expect(logOutput.String()).To(gomega.ContainSubstring(
					"Negotiated API version 1.49 is at least 1.44 but no MAC address found"))
				gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("state=running"))
			},
		)

		ginkgo.It("returns nil and logs debug for created container with no MAC address", func() {
			logrus.SetLevel(logrus.DebugLevel)
			container := MockContainer(
				WithNetworkMode("bridge"),
				WithContainerState(dockerContainerType.State{Running: false, Status: "created"}),
				WithNetworkSettings(map[string]*dockerNetworkType.EndpointSettings{
					"bridge": {MacAddress: ""},
				}),
			)

			config := &dockerNetworkType.NetworkingConfig{
				EndpointsConfig: map[string]*dockerNetworkType.EndpointSettings{
					"bridge": {MacAddress: ""},
				},
			}

			err := validateMacAddresses(config, container.ID(), "1.49", false, container)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(logOutput.String()).To(gomega.ContainSubstring(
				"No MAC address found for non-running container"))
			gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("state=created"))
		})

		ginkgo.It("returns nil and logs debug for exited container with no MAC address", func() {
			logrus.SetLevel(logrus.DebugLevel)
			container := MockContainer(
				WithNetworkMode("bridge"),
				WithContainerState(dockerContainerType.State{Running: false, Status: "exited"}),
				WithNetworkSettings(map[string]*dockerNetworkType.EndpointSettings{
					"bridge": {MacAddress: ""},
				}),
			)

			config := &dockerNetworkType.NetworkingConfig{
				EndpointsConfig: map[string]*dockerNetworkType.EndpointSettings{
					"bridge": {MacAddress: ""},
				},
			}

			err := validateMacAddresses(config, container.ID(), "1.49", false, container)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(logOutput.String()).To(gomega.ContainSubstring(
				"No MAC address found for non-running container"))
			gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("state=exited"))
		})

		ginkgo.It("returns nil and logs debug for running container with MAC address", func() {
			logrus.SetLevel(logrus.DebugLevel)
			container := MockContainer(
				WithNetworkMode("bridge"),
				WithContainerState(dockerContainerType.State{Running: true, Status: "running"}),
				WithNetworkSettings(map[string]*dockerNetworkType.EndpointSettings{
					"bridge": {MacAddress: "02:42:ac:11:00:02"},
				}),
			)

			config := &dockerNetworkType.NetworkingConfig{
				EndpointsConfig: map[string]*dockerNetworkType.EndpointSettings{
					"bridge": {MacAddress: "02:42:ac:11:00:02"},
				},
			}

			err := validateMacAddresses(config, container.ID(), "1.49", false, container)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(logOutput.String()).
				To(gomega.ContainSubstring("Found MAC address in config"))
			gomega.Expect(logOutput.String()).
				To(gomega.ContainSubstring("Verified MAC address presence"))
		})

		ginkgo.It("returns nil and logs debug for nil container state", func() {
			logrus.SetLevel(logrus.DebugLevel)
			container := MockContainer(
				WithNetworkMode("bridge"),
				func(c *dockerContainerType.InspectResponse, _ *dockerImageType.InspectResponse) {
					c.State = nil
				},
			)

			config := &dockerNetworkType.NetworkingConfig{
				EndpointsConfig: map[string]*dockerNetworkType.EndpointSettings{
					"bridge": {MacAddress: ""},
				},
			}

			err := validateMacAddresses(config, container.ID(), "1.49", false, container)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(logOutput.String()).To(gomega.ContainSubstring(
				"No MAC address found for non-running container"))
			gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("state=unknown"))
		})
	})

	ginkgo.Describe("Memory Swappiness Configuration", func() {
		var (
			logOutput               *bytes.Buffer
			mockContainer           *Container
			defaultMemorySwappiness int64 = 60
			containerName                 = "test-container"
			containerID                   = "test-container-id"
		)

		// WithMemorySwappiness configures the container's MemorySwappiness value.
		WithMemorySwappiness := func(swappiness int64) MockContainerUpdate {
			return func(c *dockerContainerType.InspectResponse, _ *dockerImageType.InspectResponse) {
				if c.HostConfig == nil {
					c.HostConfig = &dockerContainerType.HostConfig{}
				}
				c.HostConfig.MemorySwappiness = &swappiness
			}
		}

		ginkgo.BeforeEach(func() {
			logOutput = &bytes.Buffer{}
			logrus.SetOutput(logOutput)
			logrus.SetLevel(logrus.DebugLevel)
			mockContainer = MockContainer(WithMemorySwappiness(defaultMemorySwappiness))
			inspectResponse := dockerContainerType.InspectResponse{
				ContainerJSONBase: &dockerContainerType.ContainerJSONBase{
					ID:         containerID,
					Name:       "/" + containerName,
					HostConfig: mockContainer.GetCreateHostConfig(),
					State:      &dockerContainerType.State{Running: true},
				},
				Config: &dockerContainerType.Config{},
			}
			mockContainer.containerInfo = &inspectResponse
		})

		ginkgo.It("sets MemorySwappiness to nil when disabled", func() {
			clog := logrus.WithFields(logrus.Fields{
				"container": mockContainer.Name(),
				"id":        mockContainer.ID().ShortID(),
			})
			hostConfig := mockContainer.GetCreateHostConfig()
			disableMemorySwappiness := true

			if disableMemorySwappiness {
				hostConfig.MemorySwappiness = nil
				clog.Debug("Disabled memory swappiness for Podman compatibility")
			}

			gomega.Expect(hostConfig.MemorySwappiness).To(gomega.BeNil(),
				"MemorySwappiness should be nil when disabled")
			gomega.Expect(logOutput.String()).To(gomega.ContainSubstring(
				"Disabled memory swappiness for Podman compatibility"))
		})

		ginkgo.It("preserves MemorySwappiness when not disabled", func() {
			clog := logrus.WithFields(logrus.Fields{
				"container": mockContainer.Name(),
				"id":        mockContainer.ID().ShortID(),
			})
			hostConfig := mockContainer.GetCreateHostConfig()
			disableMemorySwappiness := false

			if disableMemorySwappiness {
				hostConfig.MemorySwappiness = nil
				clog.Debug("Disabled memory swappiness for Podman compatibility")
			}

			gomega.Expect(hostConfig.MemorySwappiness).To(gomega.Equal(&defaultMemorySwappiness),
				"MemorySwappiness should remain unchanged when not disabled")
			gomega.Expect(logOutput.String()).NotTo(gomega.ContainSubstring(
				"Disabled memory swappiness for Podman compatibility"))
		})
		ginkgo.Describe("CPU Copy Mode Configuration", func() {
			var (
				logOutput         *bytes.Buffer
				mockContainer     *Container
				defaultNanoCPUs   int64 = 1000000000 // 1 CPU
				defaultCPUShares  int64 = 1024
				defaultCPUQuota   int64 = 100000
				defaultCPUPeriod  int64 = 100000
				defaultCpusetCpus       = "0-1"
				defaultCpusetMems       = "0"
				containerName           = "test-container"
				containerID             = "test-container-id"
			)

			// WithCPUSettings configures the container's CPU settings.
			WithCPUSettings := func(nanoCPUs int64, cpuShares int64, cpuQuota int64, cpuPeriod int64, cpusetCpus string, cpusetMems string) MockContainerUpdate {
				return func(c *dockerContainerType.InspectResponse, _ *dockerImageType.InspectResponse) {
					if c.HostConfig == nil {
						c.HostConfig = &dockerContainerType.HostConfig{}
					}
					c.HostConfig.NanoCPUs = nanoCPUs
					c.HostConfig.CPUShares = cpuShares
					c.HostConfig.CPUQuota = cpuQuota
					c.HostConfig.CPUPeriod = cpuPeriod
					c.HostConfig.CpusetCpus = cpusetCpus
					c.HostConfig.CpusetMems = cpusetMems
				}
			}

			ginkgo.BeforeEach(func() {
				logOutput = &bytes.Buffer{}
				logrus.SetOutput(logOutput)
				logrus.SetLevel(logrus.DebugLevel)
				mockContainer = MockContainer(
					WithCPUSettings(
						defaultNanoCPUs,
						defaultCPUShares,
						defaultCPUQuota,
						defaultCPUPeriod,
						defaultCpusetCpus,
						defaultCpusetMems,
					),
				)
				inspectResponse := dockerContainerType.InspectResponse{
					ContainerJSONBase: &dockerContainerType.ContainerJSONBase{
						ID:         containerID,
						Name:       "/" + containerName,
						HostConfig: mockContainer.GetCreateHostConfig(),
						State:      &dockerContainerType.State{Running: true},
					},
					Config: &dockerContainerType.Config{},
				}
				mockContainer.containerInfo = &inspectResponse
			})

			ginkgo.It("strips all CPU settings when mode is 'none'", func() {
				clog := logrus.WithFields(logrus.Fields{
					"container": mockContainer.Name(),
					"id":        mockContainer.ID().ShortID(),
				})
				hostConfig := mockContainer.GetCreateHostConfig()

				handleCPUSettings(hostConfig, "none", false, clog)
				gomega.Expect(hostConfig.NanoCPUs).
					To(gomega.Equal(int64(0)), "NanoCPUs should be 0 when mode is 'none'")
				gomega.Expect(hostConfig.CPUShares).
					To(gomega.Equal(int64(0)), "CPUShares should be 0 when mode is 'none'")
				gomega.Expect(hostConfig.CPUQuota).
					To(gomega.Equal(int64(0)), "CPUQuota should be 0 when mode is 'none'")
				gomega.Expect(hostConfig.CPUPeriod).
					To(gomega.Equal(int64(0)), "CPUPeriod should be 0 when mode is 'none'")
				gomega.Expect(hostConfig.CpusetCpus).
					To(gomega.Equal(""), "CpusetCpus should be empty when mode is 'none'")
				gomega.Expect(hostConfig.CpusetMems).
					To(gomega.Equal(""), "CpusetMems should be empty when mode is 'none'")
				gomega.Expect(logOutput.String()).
					To(gomega.ContainSubstring("Stripped all CPU settings"))
			})

			ginkgo.It("preserves all CPU settings when mode is 'full'", func() {
				clog := logrus.WithFields(logrus.Fields{
					"container": mockContainer.Name(),
					"id":        mockContainer.ID().ShortID(),
				})
				hostConfig := mockContainer.GetCreateHostConfig()

				handleCPUSettings(hostConfig, "full", false, clog)
				gomega.Expect(hostConfig.NanoCPUs).
					To(gomega.Equal(defaultNanoCPUs), "NanoCPUs should remain unchanged when mode is 'full'")
				gomega.Expect(hostConfig.CPUShares).
					To(gomega.Equal(defaultCPUShares), "CPUShares should remain unchanged when mode is 'full'")
				gomega.Expect(hostConfig.CPUQuota).
					To(gomega.Equal(defaultCPUQuota), "CPUQuota should remain unchanged when mode is 'full'")
				gomega.Expect(hostConfig.CPUPeriod).
					To(gomega.Equal(defaultCPUPeriod), "CPUPeriod should remain unchanged when mode is 'full'")
				gomega.Expect(hostConfig.CpusetCpus).
					To(gomega.Equal(defaultCpusetCpus), "CpusetCpus should remain unchanged when mode is 'full'")
				gomega.Expect(hostConfig.CpusetMems).
					To(gomega.Equal(defaultCpusetMems), "CpusetMems should remain unchanged when mode is 'full'")
				gomega.Expect(logOutput.String()).
					To(gomega.ContainSubstring("Copied all CPU settings unchanged"))
			})

			ginkgo.It("filters NanoCPUs when mode is 'auto' and isPodman is true", func() {
				clog := logrus.WithFields(logrus.Fields{
					"container": mockContainer.Name(),
					"id":        mockContainer.ID().ShortID(),
				})
				hostConfig := mockContainer.GetCreateHostConfig()

				handleCPUSettings(hostConfig, "auto", true, clog)
				gomega.Expect(hostConfig.NanoCPUs).
					To(gomega.Equal(int64(0)), "NanoCPUs should be 0 when mode is 'auto' and isPodman is true")
				gomega.Expect(hostConfig.CPUShares).
					To(gomega.Equal(defaultCPUShares), "CPUShares should remain unchanged")
				gomega.Expect(hostConfig.CPUQuota).
					To(gomega.Equal(defaultCPUQuota), "CPUQuota should remain unchanged")
				gomega.Expect(hostConfig.CPUPeriod).
					To(gomega.Equal(defaultCPUPeriod), "CPUPeriod should remain unchanged")
				gomega.Expect(hostConfig.CpusetCpus).
					To(gomega.Equal(defaultCpusetCpus), "CpusetCpus should remain unchanged")
				gomega.Expect(hostConfig.CpusetMems).
					To(gomega.Equal(defaultCpusetMems), "CpusetMems should remain unchanged")
				gomega.Expect(logOutput.String()).
					To(gomega.ContainSubstring("Detected Podman, filtered NanoCPUs for compatibility"))
			})

			ginkgo.It(
				"preserves all CPU settings when mode is 'auto' and isPodman is false",
				func() {
					clog := logrus.WithFields(logrus.Fields{
						"container": mockContainer.Name(),
						"id":        mockContainer.ID().ShortID(),
					})
					hostConfig := mockContainer.GetCreateHostConfig()

					handleCPUSettings(hostConfig, "auto", false, clog)
					gomega.Expect(hostConfig.NanoCPUs).
						To(gomega.Equal(defaultNanoCPUs), "NanoCPUs should remain unchanged when mode is 'auto' and isPodman is false")
					gomega.Expect(hostConfig.CPUShares).
						To(gomega.Equal(defaultCPUShares), "CPUShares should remain unchanged")
					gomega.Expect(hostConfig.CPUQuota).
						To(gomega.Equal(defaultCPUQuota), "CPUQuota should remain unchanged")
					gomega.Expect(hostConfig.CPUPeriod).
						To(gomega.Equal(defaultCPUPeriod), "CPUPeriod should remain unchanged")
					gomega.Expect(hostConfig.CpusetCpus).
						To(gomega.Equal(defaultCpusetCpus), "CpusetCpus should remain unchanged")
					gomega.Expect(hostConfig.CpusetMems).
						To(gomega.Equal(defaultCpusetMems), "CpusetMems should remain unchanged")
					gomega.Expect(logOutput.String()).
						To(gomega.ContainSubstring("Detected Docker, copied all CPU settings"))
				},
			)

			ginkgo.It("defaults to 'full' mode for unknown CPU copy mode", func() {
				clog := logrus.WithFields(logrus.Fields{
					"container": mockContainer.Name(),
					"id":        mockContainer.ID().ShortID(),
				})
				hostConfig := mockContainer.GetCreateHostConfig()

				handleCPUSettings(hostConfig, "unknown", false, clog)
				gomega.Expect(hostConfig.NanoCPUs).
					To(gomega.Equal(defaultNanoCPUs), "NanoCPUs should remain unchanged for unknown mode")
				gomega.Expect(logOutput.String()).
					To(gomega.ContainSubstring("Unknown CPU copy mode, defaulting to full"))
			})
		})
	})

	ginkgo.Describe("Host Config Creation", func() {
		ginkgo.It("preserves volume mount subpath in host config", func() {
			volumeMount := dockerMountType.Mount{
				Type:   dockerMountType.TypeVolume,
				Source: "test_volume",
				Target: "/config/nest",
				VolumeOptions: &dockerMountType.VolumeOptions{
					Subpath: "ha/nest",
				},
			}

			container := MockContainer(WithMounts([]dockerMountType.Mount{volumeMount}))
			hostConfig := container.GetCreateHostConfig()

			gomega.Expect(hostConfig.Mounts).To(gomega.HaveLen(1), "Expected exactly one mount")
			mount := hostConfig.Mounts[0]
			gomega.Expect(mount.Type).
				To(gomega.Equal(dockerMountType.TypeVolume), "Mount type should be volume")
			gomega.Expect(mount.Source).To(gomega.Equal("test_volume"), "Mount source should match")
			gomega.Expect(mount.Target).
				To(gomega.Equal("/config/nest"), "Mount target should match")
			gomega.Expect(mount.VolumeOptions).ToNot(gomega.BeNil(), "VolumeOptions should be set")
			gomega.Expect(mount.VolumeOptions.Subpath).
				To(gomega.Equal("ha/nest"), "Subpath should be preserved")
		})
	})

	// Container Creation and Startup tests the StartTargetContainer function, covering
	// network configuration, memory swappiness, and error handling scenarios.
	ginkgo.Describe("Container Creation and Startup", func() {
		var (
			logOutput               *bytes.Buffer
			client                  *MockClient
			container               *Container
			networkConfig           *dockerNetworkType.NetworkingConfig
			defaultMemorySwappiness int64 = 60
		)

		ginkgo.BeforeEach(func() {
			logOutput = &bytes.Buffer{}
			logrus.SetOutput(logOutput)
			logrus.SetLevel(logrus.DebugLevel)

			client = &MockClient{}
			container = MockContainer(
				WithNetworkMode("bridge"),
				WithContainerState(dockerContainerType.State{Running: true, Status: "running"}),
				WithNetworkSettings(map[string]*dockerNetworkType.EndpointSettings{
					"bridge": {
						NetworkID:  "network_bridge_id",
						IPAddress:  "172.17.0.2",
						MacAddress: "02:42:ac:11:00:02",
						Aliases:    []string{"test-watchtower"},
					},
				}),
				func(c *dockerContainerType.InspectResponse, _ *dockerImageType.InspectResponse) {
					if c.HostConfig == nil {
						c.HostConfig = &dockerContainerType.HostConfig{}
					}
					c.HostConfig.MemorySwappiness = &defaultMemorySwappiness
				},
			)
			networkConfig = getNetworkConfig(container, "1.44")
		})

		ginkgo.It("disables memory swappiness when flag is true", func() {
			newID, err := StartTargetContainer(
				client,
				container,
				networkConfig,
				true,
				"1.44",
				flags.DockerAPIMinVersion,
				true,
				"auto",
				false,
			)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(newID).To(gomega.Equal(types.ContainerID("new_container_id")))
			gomega.Expect(logOutput.String()).
				To(gomega.ContainSubstring("Disabled memory swappiness for Podman compatibility"))
		})

		ginkgo.It("logs MAC address details", func() {
			newID, err := StartTargetContainer(
				client,
				container,
				networkConfig,
				true,
				"1.44",
				flags.DockerAPIMinVersion,
				false,
				"auto",
				false,
			)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(newID).To(gomega.Equal(types.ContainerID("new_container_id")))
			gomega.Expect(logOutput.String()).
				To(gomega.ContainSubstring("Found MAC address in config"))
		})

		ginkgo.It("uses full network config for API >= 1.44", func() {
			newID, err := StartTargetContainer(
				client,
				container,
				networkConfig,
				true,
				"1.44",
				flags.DockerAPIMinVersion,
				false,
				"auto",
				false,
			)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(newID).To(gomega.Equal(types.ContainerID("new_container_id")))
			gomega.Expect(logOutput.String()).
				To(gomega.ContainSubstring("Using full network config for API version >= 1.44 or single network"))
		})

		ginkgo.It("handles ContainerCreate failure", func() {
			createErr := errors.New("create failed")
			client.createFunc = func(_ context.Context, _ *dockerContainerType.Config, _ *dockerContainerType.HostConfig, _ *dockerNetworkType.NetworkingConfig, _ *ocispec.Platform, _ string) (dockerContainerType.CreateResponse, error) {
				return dockerContainerType.CreateResponse{}, createErr
			}
			newID, err := StartTargetContainer(
				client,
				container,
				networkConfig,
				true,
				"1.44",
				flags.DockerAPIMinVersion,
				false,
				"auto",
				false,
			)
			gomega.Expect(newID).To(gomega.BeEmpty())
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("create failed"))
			gomega.Expect(logOutput.String()).
				To(gomega.ContainSubstring("Failed to create new container"))
		})

		ginkgo.It("logs successful container creation", func() {
			newID, err := StartTargetContainer(
				client,
				container,
				networkConfig,
				true,
				"1.44",
				flags.DockerAPIMinVersion,
				false,
				"auto",
				false,
			)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(newID).To(gomega.Equal(types.ContainerID("new_container_id")))
			gomega.Expect(logOutput.String()).
				To(gomega.ContainSubstring("Created container successfully"))
		})

		ginkgo.It("skips starting stopped container when reviveStopped is false", func() {
			container = MockContainer(
				WithNetworkMode("bridge"),
				WithContainerState(dockerContainerType.State{Running: false, Status: "exited"}),
			)
			networkConfig = getNetworkConfig(container, "1.44")
			newID, err := StartTargetContainer(
				client,
				container,
				networkConfig,
				false,
				"1.44",
				flags.DockerAPIMinVersion,
				false,
				"auto",
				false,
			)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(newID).To(gomega.Equal(types.ContainerID("new_container_id")))
			gomega.Expect(logOutput.String()).
				To(gomega.ContainSubstring("Created container, not starting due to stopped state"))
		})

		ginkgo.It("handles ContainerStart failure", func() {
			startErr := errors.New("start failed")
			client.startFunc = func(_ context.Context, _ string, _ dockerContainerType.StartOptions) error {
				return startErr
			}
			newID, err := StartTargetContainer(
				client,
				container,
				networkConfig,
				true,
				"1.44",
				flags.DockerAPIMinVersion,
				false,
				"auto",
				false,
			)
			gomega.Expect(newID).To(gomega.Equal(types.ContainerID("new_container_id")))
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("start failed"))
			gomega.Expect(logOutput.String()).
				To(gomega.ContainSubstring("Failed to start new container"))
		})

		ginkgo.It("logs successful container start", func() {
			newID, err := StartTargetContainer(
				client,
				container,
				networkConfig,
				true,
				"1.44",
				flags.DockerAPIMinVersion,
				false,
				"auto",
				false,
			)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(newID).To(gomega.Equal(types.ContainerID("new_container_id")))
			gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("Started new container"))
		})

		ginkgo.It("attaches multiple networks for legacy API and handles success", func() {
			container = MockContainer(
				WithNetworkMode("bridge"),
				WithContainerState(dockerContainerType.State{Running: true, Status: "running"}),
				WithNetworks("network1", "network2"),
			)
			networkConfig = getNetworkConfig(container, "1.23")
			newID, err := StartTargetContainer(
				client,
				container,
				networkConfig,
				true,
				"1.23",
				flags.DockerAPIMinVersion,
				false,
				"auto",
				false,
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

		ginkgo.It("attaches multiple networks for legacy API and handles failure", func() {
			container = MockContainer(
				WithNetworkMode("bridge"),
				WithContainerState(dockerContainerType.State{Running: true, Status: "running"}),
				WithNetworks("network1", "network2"),
			)
			networkConfig = getNetworkConfig(container, "1.23")
			connectErr := errors.New("network connect failed")
			client.connectFunc = func(_ context.Context, _, _ string, _ *dockerNetworkType.EndpointSettings) error {
				return connectErr
			}
			client.removeFunc = func(_ context.Context, _ string, _ dockerContainerType.RemoveOptions) error {
				return nil
			}
			newID, err := StartTargetContainer(
				client,
				container,
				networkConfig,
				true,
				"1.23",
				flags.DockerAPIMinVersion,
				false,
				"auto",
				false,
			)
			gomega.Expect(newID).To(gomega.BeEmpty())
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("network connect failed"))
			gomega.Expect(logOutput.String()).
				To(gomega.ContainSubstring("Selected first network for container creation"))
			gomega.Expect(logOutput.String()).
				To(gomega.ContainSubstring("Attaching additional network to container"))
			gomega.Expect(logOutput.String()).
				To(gomega.ContainSubstring("Failed to attach additional network"))
		})
	})
})
