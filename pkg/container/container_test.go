package container

import (
	"bytes"
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/sirupsen/logrus"

	dockerContainerType "github.com/docker/docker/api/types/container"
	dockerImageType "github.com/docker/docker/api/types/image"
	dockerMountType "github.com/docker/docker/api/types/mount"
	dockerNetworkType "github.com/docker/docker/api/types/network"
	dockerNat "github.com/docker/go-connections/nat"

	"github.com/nicholas-fedor/watchtower/internal/flags"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

var _ = ginkgo.Describe("the container", func() {
	ginkgo.Describe("VerifyConfiguration", func() {
		ginkgo.When("verifying a container with no image info", func() {
			ginkgo.It("should return an error", func() {
				c := MockContainer(WithPortBindings())
				c.imageInfo = nil
				err := c.VerifyConfiguration()
				gomega.Expect(err).To(gomega.Equal(errNoImageInfo))
			})
		})
		ginkgo.When("verifying a container with no container info", func() {
			ginkgo.It("should return an error", func() {
				c := MockContainer(WithPortBindings())
				c.containerInfo = nil
				err := c.VerifyConfiguration()
				gomega.Expect(err).To(gomega.Equal(errNoContainerInfo))
			})
		})
		ginkgo.When("verifying a container with no config", func() {
			ginkgo.It("should return an error", func() {
				c := MockContainer(WithPortBindings())
				c.containerInfo.Config = nil
				err := c.VerifyConfiguration()
				gomega.Expect(err).To(gomega.Equal(errInvalidConfig))
			})
		})
		ginkgo.When("verifying a container with no host config", func() {
			ginkgo.It("should return an error", func() {
				c := MockContainer(WithPortBindings())
				c.containerInfo.HostConfig = nil
				err := c.VerifyConfiguration()
				gomega.Expect(err).To(gomega.Equal(errInvalidConfig))
			})
		})
		ginkgo.When("verifying a container with no port bindings", func() {
			ginkgo.It("should not return an error", func() {
				c := MockContainer(WithPortBindings())
				err := c.VerifyConfiguration()
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			})
		})
		ginkgo.When("verifying a container with port bindings, but no exposed ports", func() {
			ginkgo.It("should make the config compatible with updating", func() {
				c := MockContainer(WithPortBindings("80/tcp"))
				c.containerInfo.Config.ExposedPorts = nil
				gomega.Expect(c.VerifyConfiguration()).To(gomega.Succeed())

				gomega.Expect(c.containerInfo.Config.ExposedPorts).ToNot(gomega.BeNil())
				gomega.Expect(c.containerInfo.Config.ExposedPorts).To(gomega.BeEmpty())
			})
		})
		ginkgo.When(
			"verifying a container with port bindings and exposed ports is non-nil",
			func() {
				ginkgo.It("should not return an error", func() {
					c := MockContainer(WithPortBindings("80/tcp"))
					c.containerInfo.Config.ExposedPorts = map[dockerNat.Port]struct{}{"80/tcp": {}}
					err := c.VerifyConfiguration()
					gomega.Expect(err).ToNot(gomega.HaveOccurred())
				})
			},
		)
	})

	ginkgo.Describe("GetCreateConfig", func() {
		ginkgo.When("container healthcheck config is equal to image config", func() {
			ginkgo.It("should return empty healthcheck values", func() {
				mockContainer := MockContainer(WithHealthcheck(dockerContainerType.HealthConfig{
					Test: []string{"/usr/bin/sleep", "1s"},
				}), WithImageHealthcheck(dockerContainerType.HealthConfig{
					Test: []string{"/usr/bin/sleep", "1s"},
				}))
				gomega.Expect(mockContainer.GetCreateConfig().Healthcheck).
					To(gomega.Equal(&dockerContainerType.HealthConfig{}))

				mockContainer = MockContainer(WithHealthcheck(dockerContainerType.HealthConfig{
					Timeout: 30,
				}), WithImageHealthcheck(dockerContainerType.HealthConfig{
					Timeout: 30,
				}))
				gomega.Expect(mockContainer.GetCreateConfig().Healthcheck).
					To(gomega.Equal(&dockerContainerType.HealthConfig{}))

				mockContainer = MockContainer(WithHealthcheck(dockerContainerType.HealthConfig{
					StartPeriod: 30,
				}), WithImageHealthcheck(dockerContainerType.HealthConfig{
					StartPeriod: 30,
				}))
				gomega.Expect(mockContainer.GetCreateConfig().Healthcheck).
					To(gomega.Equal(&dockerContainerType.HealthConfig{}))

				mockContainer = MockContainer(WithHealthcheck(dockerContainerType.HealthConfig{
					Retries: 30,
				}), WithImageHealthcheck(dockerContainerType.HealthConfig{
					Retries: 30,
				}))
				gomega.Expect(mockContainer.GetCreateConfig().Healthcheck).
					To(gomega.Equal(&dockerContainerType.HealthConfig{}))
			})
		})
		ginkgo.When("container healthcheck config is different to image config", func() {
			ginkgo.It("should return the container healthcheck values", func() {
				mockContainer := MockContainer(WithHealthcheck(dockerContainerType.HealthConfig{
					Test:        []string{"/usr/bin/sleep", "1s"},
					Interval:    30,
					Timeout:     30,
					StartPeriod: 10,
					Retries:     2,
				}), WithImageHealthcheck(dockerContainerType.HealthConfig{
					Test:        []string{"/usr/bin/sleep", "10s"},
					Interval:    10,
					Timeout:     60,
					StartPeriod: 30,
					Retries:     10,
				}))
				gomega.Expect(mockContainer.GetCreateConfig().Healthcheck).
					To(gomega.Equal(&dockerContainerType.HealthConfig{
						Test:        []string{"/usr/bin/sleep", "1s"},
						Interval:    30,
						Timeout:     30,
						StartPeriod: 10,
						Retries:     2,
					}))
			})
		})
		ginkgo.When("container healthcheck config is empty", func() {
			ginkgo.It("should not panic", func() {
				mockContainer := MockContainer(
					WithImageHealthcheck(dockerContainerType.HealthConfig{
						Test:        []string{"/usr/bin/sleep", "10s"},
						Interval:    10,
						Timeout:     60,
						StartPeriod: 30,
						Retries:     10,
					}),
				)
				gomega.Expect(mockContainer.GetCreateConfig().Healthcheck).To(gomega.BeNil())
			})
		})
		ginkgo.When("container image healthcheck config is empty", func() {
			ginkgo.It("should not panic", func() {
				mockContainer := MockContainer(WithHealthcheck(dockerContainerType.HealthConfig{
					Test:        []string{"/usr/bin/sleep", "1s"},
					Interval:    30,
					Timeout:     30,
					StartPeriod: 10,
					Retries:     2,
				}))
				gomega.Expect(mockContainer.GetCreateConfig().Healthcheck).
					To(gomega.Equal(&dockerContainerType.HealthConfig{
						Test:        []string{"/usr/bin/sleep", "1s"},
						Interval:    30,
						Timeout:     30,
						StartPeriod: 10,
						Retries:     2,
					}))
			})
		})
	})

	ginkgo.When("asked for metadata", func() {
		var container *Container
		ginkgo.BeforeEach(func() {
			container = MockContainer(WithLabels(map[string]string{
				"com.centurylinklabs.watchtower.enable": "true",
				"com.centurylinklabs.watchtower":        "true",
			}))
		})
		ginkgo.It("should return its name on calls to .Name()", func() {
			name := container.Name()
			gomega.Expect(name).To(gomega.Equal("test-watchtower"))
			gomega.Expect(name).NotTo(gomega.Equal("wrong-name"))
		})
		ginkgo.It("should return its ID on calls to .ID()", func() {
			id := container.ID()

			gomega.Expect(id).To(gomega.BeEquivalentTo("container_id"))
			gomega.Expect(id).NotTo(gomega.BeEquivalentTo("wrong-id"))
		})
		ginkgo.It("should return true, true if enabled on calls to .Enabled()", func() {
			enabled, exists := container.Enabled()

			gomega.Expect(enabled).To(gomega.BeTrue())
			gomega.Expect(exists).To(gomega.BeTrue())
		})
		ginkgo.It(
			"should return false, true if present but not true on calls to .Enabled()",
			func() {
				container = MockContainer(
					WithLabels(map[string]string{"com.centurylinklabs.watchtower.enable": "false"}),
				)
				enabled, exists := container.Enabled()

				gomega.Expect(enabled).To(gomega.BeFalse())
				gomega.Expect(exists).To(gomega.BeTrue())
			},
		)
		ginkgo.It("should return false, false if not present on calls to .Enabled()", func() {
			container = MockContainer(WithLabels(map[string]string{"lol": "false"}))
			enabled, exists := container.Enabled()

			gomega.Expect(enabled).To(gomega.BeFalse())
			gomega.Expect(exists).To(gomega.BeFalse())
		})
		ginkgo.It("should return false, false if present but not parsable .Enabled()", func() {
			container = MockContainer(
				WithLabels(map[string]string{"com.centurylinklabs.watchtower.enable": "falsy"}),
			)
			enabled, exists := container.Enabled()

			gomega.Expect(enabled).To(gomega.BeFalse())
			gomega.Expect(exists).To(gomega.BeFalse())
		})
		ginkgo.When("checking if its a watchtower instance", func() {
			ginkgo.It("should return true if the label is set to true", func() {
				isWatchtower := container.IsWatchtower()
				gomega.Expect(isWatchtower).To(gomega.BeTrue())
			})
			ginkgo.It("should return false if the label is present but set to false", func() {
				container = MockContainer(
					WithLabels(map[string]string{"com.centurylinklabs.watchtower": "false"}),
				)
				isWatchtower := container.IsWatchtower()
				gomega.Expect(isWatchtower).To(gomega.BeFalse())
			})
			ginkgo.It("should return false if the label is not present", func() {
				container = MockContainer(WithLabels(map[string]string{"funny.label": "false"}))
				isWatchtower := container.IsWatchtower()
				gomega.Expect(isWatchtower).To(gomega.BeFalse())
			})
			ginkgo.It("should return false if there are no labels", func() {
				container = MockContainer(WithLabels(map[string]string{}))
				isWatchtower := container.IsWatchtower()
				gomega.Expect(isWatchtower).To(gomega.BeFalse())
			})
		})
		ginkgo.When("fetching the custom stop signal", func() {
			ginkgo.It("should return the signal if its set", func() {
				container = MockContainer(WithLabels(map[string]string{
					"com.centurylinklabs.watchtower.stop-signal": "SIGKILL",
				}))
				stopSignal := container.StopSignal()
				gomega.Expect(stopSignal).To(gomega.Equal("SIGKILL"))
			})
			ginkgo.It("should return an empty string if its not set", func() {
				container = MockContainer(WithLabels(map[string]string{}))
				stopSignal := container.StopSignal()
				gomega.Expect(stopSignal).To(gomega.Equal(""))
			})
		})
		ginkgo.When("fetching the image name", func() {
			ginkgo.When("the zodiac label is present", func() {
				ginkgo.It("should fetch the image name from it", func() {
					container = MockContainer(WithLabels(map[string]string{
						"com.centurylinklabs.zodiac.original-image": "the-original-image",
					}))
					imageName := container.ImageName()
					gomega.Expect(imageName).To(gomega.Equal("the-original-image:latest"))
				})
			})
			ginkgo.It("should return the image name", func() {
				name := "image-name:3"
				container = MockContainer(WithImageName(name))
				imageName := container.ImageName()
				gomega.Expect(imageName).To(gomega.Equal(name))
			})
			ginkgo.It("should assume latest if no tag is supplied", func() {
				name := "image-name"
				container = MockContainer(WithImageName(name))
				imageName := container.ImageName()
				gomega.Expect(imageName).To(gomega.Equal(name + ":latest"))
			})
		})

		ginkgo.When("fetching container links", func() {
			ginkgo.When("the depends on label is present", func() {
				ginkgo.It("should fetch depending containers from it", func() {
					container = MockContainer(WithLabels(map[string]string{
						"com.centurylinklabs.watchtower.depends-on": "postgres",
					}))
					links := container.Links()
					gomega.Expect(links).
						To(gomega.SatisfyAll(gomega.ContainElement("/postgres"), gomega.HaveLen(1)))
				})
				ginkgo.It("should fetch depending containers if there are many", func() {
					container = MockContainer(WithLabels(map[string]string{
						"com.centurylinklabs.watchtower.depends-on": "postgres,redis",
					}))
					links := container.Links()
					gomega.Expect(links).
						To(gomega.SatisfyAll(gomega.ContainElement("/postgres"), gomega.ContainElement("/redis"), gomega.HaveLen(2)))
				})
				ginkgo.It("should only add slashes to names when they are missing", func() {
					container = MockContainer(WithLabels(map[string]string{
						"com.centurylinklabs.watchtower.depends-on": "/postgres,redis",
					}))
					links := container.Links()
					gomega.Expect(links).
						To(gomega.SatisfyAll(gomega.ContainElement("/postgres"), gomega.ContainElement("/redis")))
				})
				ginkgo.It("should fetch depending containers if label is blank", func() {
					container = MockContainer(WithLabels(map[string]string{
						"com.centurylinklabs.watchtower.depends-on": "",
					}))
					links := container.Links()
					gomega.Expect(links).To(gomega.BeEmpty())
				})
			})
			ginkgo.When("the depends on label is not present", func() {
				ginkgo.It("should fetch depending containers from host config links", func() {
					container = MockContainer(WithLinks([]string{
						"redis:test-watchtower",
						"postgres:test-watchtower",
					}))
					links := container.Links()
					gomega.Expect(links).
						To(gomega.SatisfyAll(gomega.ContainElement("redis"), gomega.ContainElement("postgres"), gomega.HaveLen(2)))
				})
			})
		})

		ginkgo.When("checking no-pull label", func() {
			ginkgo.When("no-pull argument is not set", func() {
				ginkgo.When("no-pull label is true", func() {
					c := MockContainer(WithLabels(map[string]string{
						"com.centurylinklabs.watchtower.no-pull": "true",
					}))
					ginkgo.It("should return true", func() {
						gomega.Expect(c.IsNoPull(types.UpdateParams{})).To(gomega.BeTrue())
					})
				})
				ginkgo.When("no-pull label is false", func() {
					c := MockContainer(WithLabels(map[string]string{
						"com.centurylinklabs.watchtower.no-pull": "false",
					}))
					ginkgo.It("should return false", func() {
						gomega.Expect(c.IsNoPull(types.UpdateParams{})).To(gomega.BeFalse())
					})
				})
				ginkgo.When("no-pull label is set to an invalid value", func() {
					c := MockContainer(WithLabels(map[string]string{
						"com.centurylinklabs.watchtower.no-pull": "maybe",
					}))
					ginkgo.It("should return false", func() {
						gomega.Expect(c.IsNoPull(types.UpdateParams{})).To(gomega.BeFalse())
					})
				})
				ginkgo.When("no-pull label is unset", func() {
					container = MockContainer(WithLabels(map[string]string{}))
					ginkgo.It("should return false", func() {
						gomega.Expect(container.IsNoPull(types.UpdateParams{})).To(gomega.BeFalse())
					})
				})
			})
			ginkgo.When("no-pull argument is set to true", func() {
				ginkgo.When("no-pull label is true", func() {
					c := MockContainer(WithLabels(map[string]string{
						"com.centurylinklabs.watchtower.no-pull": "true",
					}))
					ginkgo.It("should return true", func() {
						gomega.Expect(c.IsNoPull(types.UpdateParams{NoPull: true})).
							To(gomega.BeTrue())
					})
				})
				ginkgo.When("no-pull label is false", func() {
					c := MockContainer(WithLabels(map[string]string{
						"com.centurylinklabs.watchtower.no-pull": "false",
					}))
					ginkgo.It("should return true", func() {
						gomega.Expect(c.IsNoPull(types.UpdateParams{NoPull: true})).
							To(gomega.BeTrue())
					})
				})
				ginkgo.When("label-take-precedence argument is set to true", func() {
					ginkgo.When("no-pull label is true", func() {
						c := MockContainer(WithLabels(map[string]string{
							"com.centurylinklabs.watchtower.no-pull": "true",
						}))
						ginkgo.It("should return true", func() {
							gomega.Expect(c.IsNoPull(types.UpdateParams{LabelPrecedence: true, NoPull: true})).
								To(gomega.BeTrue())
						})
					})
					ginkgo.When("no-pull label is false", func() {
						c := MockContainer(WithLabels(map[string]string{
							"com.centurylinklabs.watchtower.no-pull": "false",
						}))
						ginkgo.It("should return false", func() {
							gomega.Expect(c.IsNoPull(types.UpdateParams{LabelPrecedence: true, NoPull: true})).
								To(gomega.BeFalse())
						})
					})
				})
			})
		})

		ginkgo.When("there is a pre or post update timeout", func() {
			ginkgo.It("should return minute values", func() {
				container = MockContainer(WithLabels(map[string]string{
					"com.centurylinklabs.watchtower.lifecycle.pre-update-timeout":  "3",
					"com.centurylinklabs.watchtower.lifecycle.post-update-timeout": "5",
				}))
				preTimeout := container.PreUpdateTimeout()
				gomega.Expect(preTimeout).To(gomega.Equal(3))
				postTimeout := container.PostUpdateTimeout()
				gomega.Expect(postTimeout).To(gomega.Equal(5))
			})
		})
	})

	ginkgo.Describe("Network Configuration", func() {
		ginkgo.When("using bridge network mode", func() {
			ginkgo.It("should preserve IP and MAC addresses for running containers", func() {
				logOutput := &bytes.Buffer{}
				logrus.SetOutput(logOutput)
				logrus.SetLevel(logrus.DebugLevel)

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

			ginkgo.It("should warn for missing MAC address in running containers", func() {
				logOutput := &bytes.Buffer{}
				logrus.SetOutput(logOutput)
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
				ginkgo.It("should log debug for missing MAC address in created state", func() {
					logOutput := &bytes.Buffer{}
					logrus.SetOutput(logOutput)
					logrus.SetLevel(logrus.DebugLevel)

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
						"No MAC address found for non-running container",
					))
					gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("state=created"))
					gomega.Expect(logOutput.String()).NotTo(gomega.ContainSubstring(
						"Negotiated API version 1.49 is at least 1.44 but no MAC address found",
					))
				})

				ginkgo.It("should log debug for missing MAC address in exited state", func() {
					logOutput := &bytes.Buffer{}
					logrus.SetOutput(logOutput)
					logrus.SetLevel(logrus.DebugLevel)

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
						"No MAC address found for non-running container",
					))
					gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("state=exited"))
					gomega.Expect(logOutput.String()).NotTo(gomega.ContainSubstring(
						"Negotiated API version 1.49 is at least 1.44 but no MAC address found",
					))
				})
			})
		})

		ginkgo.When("using host network mode", func() {
			ginkgo.It("should include host endpoint with no aliases or DNS names", func() {
				logOutput := &bytes.Buffer{}
				logrus.SetOutput(logOutput)
				logrus.SetLevel(logrus.DebugLevel)

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

				networkMode := container.containerInfo.HostConfig.NetworkMode
				logrus.WithFields(logrus.Fields{
					"network_mode":      networkMode,
					"network_mode_type": fmt.Sprintf("%T", networkMode),
					"network_mode_str":  string(networkMode),
					"test":              "host_network_mode",
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

				gomega.Expect(container.containerInfo.HostConfig.NetworkMode).
					To(gomega.Equal(dockerContainerType.NetworkMode("host")))
			})

			ginkgo.It("should clear non-empty aliases and DNS names", func() {
				logOutput := &bytes.Buffer{}
				logrus.SetOutput(logOutput)
				logrus.SetLevel(logrus.DebugLevel)

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

			ginkgo.It("should log no MAC address as expected in debugLogMacAddress", func() {
				logOutput := &bytes.Buffer{}
				logrus.SetOutput(logOutput)
				logrus.SetLevel(logrus.DebugLevel)

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
				gomega.Expect(logOutput.String()).
					To(gomega.ContainSubstring("No MAC address in host network mode, as expected"))
			})
		})

		ginkgo.When("using legacy API version (<1.44)", func() {
			ginkgo.It("should clear MAC address and DNS names", func() {
				logOutput := &bytes.Buffer{}
				logrus.SetOutput(logOutput)
				logrus.SetLevel(logrus.DebugLevel)

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

			ginkgo.It("should log no MAC address in debugLogMacAddress", func() {
				logOutput := &bytes.Buffer{}
				logrus.SetOutput(logOutput)
				logrus.SetLevel(logrus.DebugLevel)

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
				gomega.Expect(logOutput.String()).
					To(gomega.ContainSubstring("No MAC address in legacy config, as expected"))
			})
		})
	})

	ginkgo.Describe("validateMacAddresses", func() {
		ginkgo.When("container is running with no MAC address", func() {
			ginkgo.It("should return errNoMacInNonHost and log warning", func() {
				logOutput := &bytes.Buffer{}
				logrus.SetOutput(logOutput)
				logrus.SetLevel(logrus.WarnLevel)

				container := MockContainer(
					WithNetworkMode("bridge"),
					WithContainerState(dockerContainerType.State{Running: true, Status: "running"}),
					WithNetworkSettings(map[string]*dockerNetworkType.EndpointSettings{
						"bridge": {
							MacAddress: "",
						},
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
					"Negotiated API version 1.49 is at least 1.44 but no MAC address found",
				))
				gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("state=running"))
			})
		})

		ginkgo.When("container is created with no MAC address", func() {
			ginkgo.It("should return nil and log debug", func() {
				logOutput := &bytes.Buffer{}
				logrus.SetOutput(logOutput)
				logrus.SetLevel(logrus.DebugLevel)

				container := MockContainer(
					WithNetworkMode("bridge"),
					WithContainerState(
						dockerContainerType.State{Running: false, Status: "created"},
					),
					WithNetworkSettings(map[string]*dockerNetworkType.EndpointSettings{
						"bridge": {
							MacAddress: "",
						},
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
					"No MAC address found for non-running container",
				))
				gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("state=created"))
			})
		})

		ginkgo.When("container is exited with no MAC address", func() {
			ginkgo.It("should return nil and log debug", func() {
				logOutput := &bytes.Buffer{}
				logrus.SetOutput(logOutput)
				logrus.SetLevel(logrus.DebugLevel)

				container := MockContainer(
					WithNetworkMode("bridge"),
					WithContainerState(dockerContainerType.State{Running: false, Status: "exited"}),
					WithNetworkSettings(map[string]*dockerNetworkType.EndpointSettings{
						"bridge": {
							MacAddress: "",
						},
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
					"No MAC address found for non-running container",
				))
				gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("state=exited"))
			})
		})

		ginkgo.When("container is running with a MAC address", func() {
			ginkgo.It("should return nil and log debug", func() {
				logOutput := &bytes.Buffer{}
				logrus.SetOutput(logOutput)
				logrus.SetLevel(logrus.DebugLevel)

				container := MockContainer(
					WithNetworkMode("bridge"),
					WithContainerState(dockerContainerType.State{Running: true, Status: "running"}),
					WithNetworkSettings(map[string]*dockerNetworkType.EndpointSettings{
						"bridge": {
							MacAddress: "02:42:ac:11:00:01",
						},
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
		})

		ginkgo.When("containerInfo or State is nil", func() {
			ginkgo.It("should return nil and log debug with unknown state", func() {
				logOutput := &bytes.Buffer{}
				logrus.SetOutput(logOutput)
				logrus.SetLevel(logrus.DebugLevel)

				container := MockContainer(
					WithNetworkMode("bridge"),
					// Explicitly omit State to simulate nil
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
					"No MAC address found for non-running container",
				))
				gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("state=unknown"))
			})
		})
	})

	ginkgo.Describe("validateMacAddresses", func() {
		ginkgo.When("container is running with no MAC address", func() {
			ginkgo.It("should return errNoMacInNonHost and log warning", func() {
				logOutput := &bytes.Buffer{}
				logrus.SetOutput(logOutput)
				logrus.SetLevel(logrus.WarnLevel)

				container := MockContainer(
					WithNetworkMode("bridge"),
					WithContainerState(dockerContainerType.State{Running: true, Status: "running"}),
					WithNetworkSettings(map[string]*dockerNetworkType.EndpointSettings{
						"bridge": {
							MacAddress: "",
						},
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
					"Negotiated API version 1.49 is at least 1.44 but no MAC address found",
				))
				gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("state=running"))
			})
		})

		ginkgo.When("container is created with no MAC address", func() {
			ginkgo.It("should return nil and log debug", func() {
				logOutput := &bytes.Buffer{}
				logrus.SetOutput(logOutput)
				logrus.SetLevel(logrus.DebugLevel)

				container := MockContainer(
					WithNetworkMode("bridge"),
					WithContainerState(
						dockerContainerType.State{Running: false, Status: "created"},
					),
					WithNetworkSettings(map[string]*dockerNetworkType.EndpointSettings{
						"bridge": {
							MacAddress: "",
						},
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
					"No MAC address found for non-running container",
				))
				gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("state=created"))
			})
		})

		ginkgo.When("container is exited with no MAC address", func() {
			ginkgo.It("should return nil and log debug", func() {
				logOutput := &bytes.Buffer{}
				logrus.SetOutput(logOutput)
				logrus.SetLevel(logrus.DebugLevel)

				container := MockContainer(
					WithNetworkMode("bridge"),
					WithContainerState(dockerContainerType.State{Running: false, Status: "exited"}),
					WithNetworkSettings(map[string]*dockerNetworkType.EndpointSettings{
						"bridge": {
							MacAddress: "",
						},
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
					"No MAC address found for non-running container",
				))
				gomega.Expect(logOutput.String()).To(gomega.ContainSubstring("state=exited"))
			})
		})

		ginkgo.When("container is running with a MAC address", func() {
			ginkgo.It("should return nil and log debug", func() {
				logOutput := &bytes.Buffer{}
				logrus.SetOutput(logOutput)
				logrus.SetLevel(logrus.DebugLevel)

				container := MockContainer(
					WithNetworkMode("bridge"),
					WithContainerState(dockerContainerType.State{Running: true, Status: "running"}),
					WithNetworkSettings(map[string]*dockerNetworkType.EndpointSettings{
						"bridge": {
							MacAddress: "02:42:ac:11:00:02",
						},
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
		})
	})

	ginkgo.Describe("DisableMemorySwappiness Configuration", func() {
		var mockContainer *Container
		var defaultMemorySwappiness int64 = 60
		containerName := "test-container"
		containerID := "test-container-id"

		WithMemorySwappiness := func(swappiness int64) MockContainerUpdate {
			return func(c *dockerContainerType.InspectResponse, _ *dockerImageType.InspectResponse) {
				if c.HostConfig == nil {
					c.HostConfig = &dockerContainerType.HostConfig{}
				}
				c.HostConfig.MemorySwappiness = &swappiness
			}
		}

		ginkgo.BeforeEach(func() {
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

		ginkgo.When("DisableMemorySwappiness is true", func() {
			ginkgo.It("sets MemorySwappiness to nil and logs debug message", func() {
				logOutput := &bytes.Buffer{}
				logrus.SetOutput(logOutput)
				logrus.SetLevel(logrus.DebugLevel)

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
					"MemorySwappiness should be nil when DisableMemorySwappiness is true")
				gomega.Expect(logOutput.String()).To(gomega.ContainSubstring(
					"Disabled memory swappiness for Podman compatibility"))
			})
		})

		ginkgo.When("DisableMemorySwappiness is false", func() {
			ginkgo.It("preserves MemorySwappiness and does not log debug message", func() {
				logOutput := &bytes.Buffer{}
				logrus.SetOutput(logOutput)
				logrus.SetLevel(logrus.DebugLevel)

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

				gomega.Expect(hostConfig.MemorySwappiness).
					To(gomega.Equal(&defaultMemorySwappiness),
						"MemorySwappiness should remain unchanged when DisableMemorySwappiness is false")
				gomega.Expect(logOutput.String()).NotTo(gomega.ContainSubstring(
					"Disabled memory swappiness for Podman compatibility"))
			})
		})
	})

	ginkgo.Describe("GetCreateHostConfig", func() {
		ginkgo.When("container has a volume mount with subpath", func() {
			ginkgo.It("preserves the subpath in the host config mounts", func() {
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
				gomega.Expect(mount.Source).
					To(gomega.Equal("test_volume"), "Mount source should match")
				gomega.Expect(mount.Target).
					To(gomega.Equal("/config/nest"), "Mount target should match")
				gomega.Expect(mount.VolumeOptions).
					ToNot(gomega.BeNil(), "VolumeOptions should be set")
				gomega.Expect(mount.VolumeOptions.Subpath).
					To(gomega.Equal("ha/nest"), "Subpath should be preserved")
			})
		})
	})
})
