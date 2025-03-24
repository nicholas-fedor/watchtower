package container

import (
	dockerContainerType "github.com/docker/docker/api/types/container"
	dockerNat "github.com/docker/go-connections/nat"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

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
		ginkgo.When("verifying a container with port bindings and exposed ports is non-nil", func() {
			ginkgo.It("should return an error", func() {
				c := MockContainer(WithPortBindings("80/tcp"))
				c.containerInfo.Config.ExposedPorts = map[dockerNat.Port]struct{}{"80/tcp": {}}
				err := c.VerifyConfiguration()
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			})
		})
	})
	ginkgo.Describe("GetCreateConfig", func() {
		ginkgo.When("container healthcheck config is equal to image config", func() {
			ginkgo.It("should return empty healthcheck values", func() {
				mockContainer := MockContainer(WithHealthcheck(dockerContainerType.HealthConfig{
					Test: []string{"/usr/bin/sleep", "1s"},
				}), WithImageHealthcheck(dockerContainerType.HealthConfig{
					Test: []string{"/usr/bin/sleep", "1s"},
				}))
				gomega.Expect(mockContainer.GetCreateConfig().Healthcheck).To(gomega.Equal(&dockerContainerType.HealthConfig{}))

				mockContainer = MockContainer(WithHealthcheck(dockerContainerType.HealthConfig{
					Timeout: 30,
				}), WithImageHealthcheck(dockerContainerType.HealthConfig{
					Timeout: 30,
				}))
				gomega.Expect(mockContainer.GetCreateConfig().Healthcheck).To(gomega.Equal(&dockerContainerType.HealthConfig{}))

				mockContainer = MockContainer(WithHealthcheck(dockerContainerType.HealthConfig{
					StartPeriod: 30,
				}), WithImageHealthcheck(dockerContainerType.HealthConfig{
					StartPeriod: 30,
				}))
				gomega.Expect(mockContainer.GetCreateConfig().Healthcheck).To(gomega.Equal(&dockerContainerType.HealthConfig{}))

				mockContainer = MockContainer(WithHealthcheck(dockerContainerType.HealthConfig{
					Retries: 30,
				}), WithImageHealthcheck(dockerContainerType.HealthConfig{
					Retries: 30,
				}))
				gomega.Expect(mockContainer.GetCreateConfig().Healthcheck).To(gomega.Equal(&dockerContainerType.HealthConfig{}))
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
				gomega.Expect(mockContainer.GetCreateConfig().Healthcheck).To(gomega.Equal(&dockerContainerType.HealthConfig{
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
				mockContainer := MockContainer(WithImageHealthcheck(dockerContainerType.HealthConfig{
					Test:        []string{"/usr/bin/sleep", "10s"},
					Interval:    10,
					Timeout:     60,
					StartPeriod: 30,
					Retries:     10,
				}))
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
				gomega.Expect(mockContainer.GetCreateConfig().Healthcheck).To(gomega.Equal(&dockerContainerType.HealthConfig{
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
		ginkgo.It("should return false, true if present but not true on calls to .Enabled()", func() {
			container = MockContainer(WithLabels(map[string]string{"com.centurylinklabs.watchtower.enable": "false"}))
			enabled, exists := container.Enabled()

			gomega.Expect(enabled).To(gomega.BeFalse())
			gomega.Expect(exists).To(gomega.BeTrue())
		})
		ginkgo.It("should return false, false if not present on calls to .Enabled()", func() {
			container = MockContainer(WithLabels(map[string]string{"lol": "false"}))
			enabled, exists := container.Enabled()

			gomega.Expect(enabled).To(gomega.BeFalse())
			gomega.Expect(exists).To(gomega.BeFalse())
		})
		ginkgo.It("should return false, false if present but not parsable .Enabled()", func() {
			container = MockContainer(WithLabels(map[string]string{"com.centurylinklabs.watchtower.enable": "falsy"}))
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
				container = MockContainer(WithLabels(map[string]string{"com.centurylinklabs.watchtower": "false"}))
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
					gomega.Expect(imageName).To(gomega.Equal(imageName))
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
					gomega.Expect(links).To(gomega.SatisfyAll(gomega.ContainElement("/postgres"), gomega.HaveLen(1)))
				})
				ginkgo.It("should fetch depending containers if there are many", func() {
					container = MockContainer(WithLabels(map[string]string{
						"com.centurylinklabs.watchtower.depends-on": "postgres,redis",
					}))
					links := container.Links()
					gomega.Expect(links).To(gomega.SatisfyAll(gomega.ContainElement("/postgres"), gomega.ContainElement("/redis"), gomega.HaveLen(2)))
				})
				ginkgo.It("should only add slashes to names when they are missing", func() {
					container = MockContainer(WithLabels(map[string]string{
						"com.centurylinklabs.watchtower.depends-on": "/postgres,redis",
					}))
					links := container.Links()
					gomega.Expect(links).To(gomega.SatisfyAll(gomega.ContainElement("/postgres"), gomega.ContainElement("/redis")))
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
					gomega.Expect(links).To(gomega.SatisfyAll(gomega.ContainElement("redis"), gomega.ContainElement("postgres"), gomega.HaveLen(2)))
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
						gomega.Expect(c.IsNoPull(types.UpdateParams{NoPull: true})).To(gomega.BeTrue())
					})
				})
				ginkgo.When("no-pull label is false", func() {
					c := MockContainer(WithLabels(map[string]string{
						"com.centurylinklabs.watchtower.no-pull": "false",
					}))
					ginkgo.It("should return true", func() {
						gomega.Expect(c.IsNoPull(types.UpdateParams{NoPull: true})).To(gomega.BeTrue())
					})
				})
				ginkgo.When("label-take-precedence argument is set to true", func() {
					ginkgo.When("no-pull label is true", func() {
						c := MockContainer(WithLabels(map[string]string{
							"com.centurylinklabs.watchtower.no-pull": "true",
						}))
						ginkgo.It("should return true", func() {
							gomega.Expect(c.IsNoPull(types.UpdateParams{LabelPrecedence: true, NoPull: true})).To(gomega.BeTrue())
						})
					})
					ginkgo.When("no-pull label is false", func() {
						c := MockContainer(WithLabels(map[string]string{
							"com.centurylinklabs.watchtower.no-pull": "false",
						}))
						ginkgo.It("should return false", func() {
							gomega.Expect(c.IsNoPull(types.UpdateParams{LabelPrecedence: true, NoPull: true})).To(gomega.BeFalse())
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
})
