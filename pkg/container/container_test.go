package container

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/sirupsen/logrus"

	dockerContainer "github.com/docker/docker/api/types/container"
	dockerMount "github.com/docker/docker/api/types/mount"
	dockerNat "github.com/docker/go-connections/nat"

	"github.com/nicholas-fedor/watchtower/pkg/compose"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	mockTypes "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

// testContainerName is used for testing self-referencing dependency scenarios.
const testContainerName = "gluetun"

// webContainerName is used for testing filterSelfReferences scenarios.
const webContainerName = "web"

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

		ginkgo.It("removes port bindings with empty port string", func() {
			c := MockContainer(WithPortBindings("80/tcp"))
			c.containerInfo.HostConfig.PortBindings[""] = []dockerNat.PortBinding{}
			c.containerInfo.Config.ExposedPorts = map[dockerNat.Port]struct{}{
				"":       {},
				"80/tcp": {},
			}
			err := c.VerifyConfiguration()
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			_, exists := c.containerInfo.HostConfig.PortBindings[""]
			gomega.Expect(exists).To(gomega.BeFalse())
			_, exists = c.containerInfo.HostConfig.PortBindings["80/tcp"]
			gomega.Expect(exists).To(gomega.BeTrue())
			_, exists = c.containerInfo.Config.ExposedPorts[""]
			gomega.Expect(exists).To(gomega.BeFalse())
			_, exists = c.containerInfo.Config.ExposedPorts["80/tcp"]
			gomega.Expect(exists).To(gomega.BeTrue())
		})

		ginkgo.It("removes port bindings with empty port number", func() {
			c := MockContainer(WithPortBindings("80/tcp"))
			c.containerInfo.HostConfig.PortBindings["/tcp"] = []dockerNat.PortBinding{}
			c.containerInfo.Config.ExposedPorts = map[dockerNat.Port]struct{}{
				"/tcp": {},
			}
			err := c.VerifyConfiguration()
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			_, exists := c.containerInfo.HostConfig.PortBindings["/tcp"]
			gomega.Expect(exists).To(gomega.BeFalse())
			_, exists = c.containerInfo.Config.ExposedPorts["/tcp"]
			gomega.Expect(exists).To(gomega.BeFalse())
			_, exists = c.containerInfo.HostConfig.PortBindings["80/tcp"]
			gomega.Expect(exists).To(gomega.BeTrue())
		})

		ginkgo.It("preserves valid port bindings unchanged", func() {
			c := MockContainer(WithPortBindings("8080/tcp", "443/tcp"))
			c.containerInfo.Config.ExposedPorts = map[dockerNat.Port]struct{}{
				"8080/tcp": {},
				"443/tcp":  {},
			}
			err := c.VerifyConfiguration()
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(c.containerInfo.HostConfig.PortBindings).To(gomega.HaveLen(2))
			gomega.Expect(c.containerInfo.Config.ExposedPorts).To(gomega.HaveLen(2))
		})
	})

	ginkgo.Describe("Create Configuration", func() {
		ginkgo.Context("when container and image healthcheck configs are identical", func() {
			ginkgo.It("returns an empty healthcheck config", func() {
				tests := []dockerContainer.HealthConfig{
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
						To(gomega.Equal(&dockerContainer.HealthConfig{}))
				}
			})
		})

		ginkgo.It("returns container healthcheck when configs differ", func() {
			c := MockContainer(
				WithHealthcheck(dockerContainer.HealthConfig{
					Test:        []string{"/usr/bin/sleep", "1s"},
					Interval:    30,
					Timeout:     30,
					StartPeriod: 10,
					Retries:     2,
				}),
				WithImageHealthcheck(dockerContainer.HealthConfig{
					Test:        []string{"/usr/bin/sleep", "10s"},
					Interval:    10,
					Timeout:     60,
					StartPeriod: 30,
					Retries:     10,
				}),
			)
			gomega.Expect(c.GetCreateConfig().Healthcheck).
				To(gomega.Equal(&dockerContainer.HealthConfig{
					Test:        []string{"/usr/bin/sleep", "1s"},
					Interval:    30,
					Timeout:     30,
					StartPeriod: 10,
					Retries:     2,
				}))
		})

		ginkgo.It("handles empty container healthcheck config without panic", func() {
			c := MockContainer(WithImageHealthcheck(dockerContainer.HealthConfig{
				Test:        []string{"/usr/bin/sleep", "10s"},
				Interval:    10,
				Timeout:     60,
				StartPeriod: 30,
				Retries:     10,
			}))
			gomega.Expect(c.GetCreateConfig().Healthcheck).To(gomega.BeNil())
		})

		ginkgo.It("handles empty image healthcheck config without panic", func() {
			c := MockContainer(WithHealthcheck(dockerContainer.HealthConfig{
				Test:        []string{"/usr/bin/sleep", "1s"},
				Interval:    30,
				Timeout:     30,
				StartPeriod: 10,
				Retries:     2,
			}))
			gomega.Expect(c.GetCreateConfig().Healthcheck).
				To(gomega.Equal(&dockerContainer.HealthConfig{
					Test:        []string{"/usr/bin/sleep", "1s"},
					Interval:    30,
					Timeout:     30,
					StartPeriod: 10,
					Retries:     2,
				}))
		})

		ginkgo.Context("UTS mode hostname handling", func() {
			ginkgo.It("clears hostname when UTS mode is non-empty", func() {
				c := MockContainer(
					WithUTSMode("host"),
					WithHostname("test-hostname"),
				)
				config := c.GetCreateConfig()
				gomega.Expect(config.Hostname).
					To(gomega.Equal(""), "Hostname should be cleared when UTS mode is set")
			})

			ginkgo.It("preserves hostname when UTS mode is empty", func() {
				c := MockContainer(
					WithUTSMode(""),
					WithHostname("test-hostname"),
				)
				config := c.GetCreateConfig()
				gomega.Expect(config.Hostname).
					To(gomega.Equal("test-hostname"), "Hostname should be preserved when UTS mode is empty")
			})
		})

		ginkgo.It("returns minimal config when containerInfo is nil", func() {
			c := MockContainer(WithImageName("test-image"))
			c.containerInfo = nil
			config := c.GetCreateConfig()
			gomega.Expect(config.Image).To(gomega.Equal("unknown:latest"))
			gomega.Expect(config).To(gomega.Equal(&dockerContainer.Config{Image: "unknown:latest"}))
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
		})

		ginkgo.Context("fetching container links", func() {
			ginkgo.When("compose depends-on label is present", func() {
				ginkgo.It("returns single dependent container", func() {
					container = MockContainer(WithLabels(map[string]string{
						"com.docker.compose.depends_on": "postgres",
					}))
					links := container.Links(true)
					gomega.Expect(links).To(gomega.SatisfyAll(
						gomega.ContainElement("postgres"),
						gomega.HaveLen(1),
					))
				})

				ginkgo.It("returns multiple dependent containers", func() {
					container = MockContainer(WithLabels(map[string]string{
						"com.docker.compose.depends_on": "postgres,redis",
					}))
					links := container.Links(true)
					gomega.Expect(links).To(gomega.SatisfyAll(
						gomega.ContainElement("postgres"),
						gomega.ContainElement("redis"),
						gomega.HaveLen(2),
					))
				})

				ginkgo.It("trims whitespace from service names", func() {
					container = MockContainer(WithLabels(map[string]string{
						"com.docker.compose.depends_on": " postgres , redis ",
					}))
					links := container.Links(true)
					gomega.Expect(links).To(gomega.SatisfyAll(
						gomega.ContainElement("postgres"),
						gomega.ContainElement("redis"),
						gomega.HaveLen(2),
					))
				})

				ginkgo.It("normalizes container names with slashes", func() {
					container = MockContainer(WithLabels(map[string]string{
						compose.ComposeDependsOnLabel: "/postgres,/redis",
					}))
					links := container.Links(true)
					gomega.Expect(links).To(gomega.SatisfyAll(
						gomega.ContainElement("postgres"),
						gomega.ContainElement("redis"),
					))
				})

				ginkgo.It(
					"watchtower depends-on label takes precedence over compose depends_on",
					func() {
						container = MockContainer(WithLabels(map[string]string{
							"com.docker.compose.depends_on":             "postgres",
							"com.centurylinklabs.watchtower.depends-on": "redis",
						}))
						links := container.Links(true)
						gomega.Expect(links).To(gomega.SatisfyAll(
							gomega.ContainElement("redis"),
							gomega.Not(gomega.ContainElement("postgres")),
							gomega.HaveLen(1),
						))
					},
				)

				ginkgo.It("returns empty links for blank compose depends-on label", func() {
					container = MockContainer(WithLabels(map[string]string{
						"com.docker.compose.depends_on": "",
					}))
					gomega.Expect(container.Links(true)).To(gomega.BeEmpty())
				})

				ginkgo.It("parses colon-separated service:condition:required format", func() {
					container = MockContainer(WithLabels(map[string]string{
						"com.docker.compose.depends_on": "postgres:service_started:required,redis:service_healthy",
					}))
					links := container.Links(true)
					gomega.Expect(links).To(gomega.SatisfyAll(
						gomega.ContainElement("postgres"),
						gomega.ContainElement("redis"),
						gomega.HaveLen(2),
					))
				})
			})

			ginkgo.When("depends-on label is present", func() {
				ginkgo.It("returns single dependent container", func() {
					container = MockContainer(WithLabels(map[string]string{
						"com.centurylinklabs.watchtower.depends-on": "postgres",
					}))
					links := container.Links(true)
					gomega.Expect(links).To(gomega.SatisfyAll(
						gomega.ContainElement("postgres"),
						gomega.HaveLen(1),
					))
				})

				ginkgo.It("returns multiple dependent containers", func() {
					container = MockContainer(WithLabels(map[string]string{
						"com.centurylinklabs.watchtower.depends-on": "postgres,redis",
					}))
					links := container.Links(true)
					gomega.Expect(links).To(gomega.SatisfyAll(
						gomega.ContainElement("postgres"),
						gomega.ContainElement("redis"),
						gomega.HaveLen(2),
					))
				})

				ginkgo.It("normalizes container names with slashes", func() {
					container = MockContainer(WithLabels(map[string]string{
						"com.centurylinklabs.watchtower.depends-on": "/postgres,/redis",
					}))
					links := container.Links(true)
					gomega.Expect(links).To(gomega.SatisfyAll(
						gomega.ContainElement("postgres"),
						gomega.ContainElement("redis"),
					))
				})

				ginkgo.It("returns empty links for blank depends-on label", func() {
					container = MockContainer(WithLabels(map[string]string{
						"com.centurylinklabs.watchtower.depends-on": "",
					}))
					gomega.Expect(container.Links(true)).To(gomega.BeEmpty())
				})

				ginkgo.It(
					"does not prefix container names with project name for cross-project dependencies",
					func() {
						container = MockContainer(WithLabels(map[string]string{
							"com.docker.compose.project":                "myproject",
							"com.centurylinklabs.watchtower.depends-on": "otherproject-db,external-service",
						}))
						links := container.Links(true)
						gomega.Expect(links).To(gomega.SatisfyAll(
							gomega.ContainElement("otherproject-db"),
							gomega.ContainElement("external-service"),
							gomega.HaveLen(2),
						))
						gomega.Expect(links).
							To(gomega.Not(gomega.ContainElement("myproject-otherproject-db")))
						gomega.Expect(links).
							To(gomega.Not(gomega.ContainElement("myproject-external-service")))
					},
				)

				ginkgo.It("handles cross-project dependencies with single container", func() {
					container = MockContainer(WithLabels(map[string]string{
						"com.docker.compose.project":                "webapp",
						"com.centurylinklabs.watchtower.depends-on": "database",
					}))
					links := container.Links(true)
					gomega.Expect(links).To(gomega.SatisfyAll(
						gomega.ContainElement("database"),
						gomega.HaveLen(1),
					))
					gomega.Expect(links).To(gomega.Not(gomega.ContainElement("webapp-database")))
				})

				ginkgo.It("supports cross-project dependencies without project labels", func() {
					container = MockContainer(WithLabels(map[string]string{
						"com.centurylinklabs.watchtower.depends-on": "standalone-db,external-api",
					}))
					links := container.Links(true)
					gomega.Expect(links).To(gomega.SatisfyAll(
						gomega.ContainElement("standalone-db"),
						gomega.ContainElement("external-api"),
						gomega.HaveLen(2),
					))
				})

				ginkgo.It("handles self-referencing dependencies gracefully", func() {
					container = MockContainer(WithLabels(map[string]string{
						"com.centurylinklabs.watchtower.depends-on": "postgres,test-watchtower,redis",
					}))
					links := container.Links(true)
					gomega.Expect(links).To(gomega.SatisfyAll(
						gomega.ContainElement("postgres"),
						gomega.ContainElement("redis"),
						gomega.Not(gomega.ContainElement("test-watchtower")),
						gomega.HaveLen(2),
					))
				})

				ginkgo.It(
					"does not create self-reference when container name matches dependency",
					func() {
						container = MockContainer(WithLabels(map[string]string{
							"com.centurylinklabs.watchtower.depends-on": testContainerName,
						}))
						container.containerInfo.Name = testContainerName
						container.normalizedName = testContainerName

						links := container.Links(true)
						gomega.Expect(links).To(gomega.BeEmpty(),
							"Container with name matching dependency should not include self in links")
					},
				)

				ginkgo.It("filters self-references from compose depends-on label", func() {
					container = MockContainer(WithLabels(map[string]string{
						"com.docker.compose.depends_on": testContainerName,
					}))
					container.containerInfo.Name = testContainerName
					container.normalizedName = testContainerName

					links := container.Links(true)
					gomega.Expect(links).To(gomega.BeEmpty(),
						"Compose depends-on with self-reference should be filtered out")
				})

				ginkgo.DescribeTable(
					"parses malformed watchtower labels",
					func(label string, expected []string) {
						container = MockContainer(WithLabels(map[string]string{
							"com.centurylinklabs.watchtower.depends-on": label,
						}))
						links := container.Links(true)
						gomega.Expect(links).To(gomega.Equal(expected))
					},
					ginkgo.Entry("empty entries", ",postgres,,redis,", []string{"postgres", "redis"}),
					ginkgo.Entry("extra spaces", " postgres , redis ", []string{"postgres", "redis"}),
					ginkgo.Entry("empty string", "", []string{}),
					ginkgo.Entry("multiple dependencies", "postgres,redis,mysql", []string{"postgres", "redis", "mysql"}),
				)

				ginkgo.It("normalizes invalid container name dependencies", func() {
					container = MockContainer(WithLabels(map[string]string{
						"com.centurylinklabs.watchtower.depends-on": "postgres db, redis",
					}))
					links := container.Links(true)
					gomega.Expect(links).To(gomega.Equal([]string{"postgres db", "redis"}))
				})

				ginkgo.It("handles very long dependency lists", func() {
					var deps []string
					for i := 1; i <= 100; i++ {
						deps = append(deps, fmt.Sprintf("dep%d", i))
					}

					label := strings.Join(deps, ",")
					container = MockContainer(WithLabels(map[string]string{
						"com.centurylinklabs.watchtower.depends-on": label,
					}))
					links := container.Links(true)
					gomega.Expect(links).To(gomega.HaveLen(100))
					gomega.Expect(links).To(gomega.ContainElement("dep1"))
					gomega.Expect(links).To(gomega.ContainElement("dep50"))
					gomega.Expect(links).To(gomega.ContainElement("dep100"))
				})
			})

			ginkgo.Context("when UseComposeDependsOn is disabled", func() {
				ginkgo.It("returns empty links for compose depends-on label", func() {
					container = MockContainer(WithLabels(map[string]string{
						"com.docker.compose.depends_on": "postgres,redis",
					}))
					links := container.Links(false)
					gomega.Expect(links).To(gomega.BeEmpty(),
						"Compose depends-on should be ignored when disabled")
				})

				ginkgo.It("still returns links from watchtower depends-on label", func() {
					container = MockContainer(WithLabels(map[string]string{
						"com.docker.compose.depends_on":             "postgres",
						"com.centurylinklabs.watchtower.depends-on": "redis",
					}))
					links := container.Links(false)
					gomega.Expect(links).To(gomega.SatisfyAll(
						gomega.ContainElement("redis"),
						gomega.Not(gomega.ContainElement("postgres")),
						gomega.HaveLen(1),
					))
				})

				ginkgo.It("returns links from host config when compose is disabled", func() {
					container = MockContainer(WithLabels(map[string]string{
						"com.docker.compose.depends_on": "postgres",
					}), WithLinks([]string{
						"redis:test-watchtower",
					}))
					links := container.Links(false)
					gomega.Expect(links).To(gomega.SatisfyAll(
						gomega.ContainElement("redis"),
						gomega.Not(gomega.ContainElement("postgres")),
						gomega.HaveLen(1),
					))
				})
			})

			ginkgo.It("returns links from host config when depends-on labels are absent", func() {
				container = MockContainer(WithLinks([]string{
					"redis:test-watchtower",
					"postgres:test-watchtower",
				}))
				links := container.Links(true)
				gomega.Expect(links).To(gomega.SatisfyAll(
					gomega.ContainElement("redis"),
					gomega.ContainElement("postgres"),
					gomega.HaveLen(2),
				))
			})

			ginkgo.It("warns and skips invalid link format without colon", func() {
				logOutput := &bytes.Buffer{}
				originalOutput := logrus.StandardLogger().Out
				originalLevel := logrus.GetLevel()

				logrus.SetOutput(logOutput)
				logrus.SetLevel(logrus.WarnLevel)

				defer func() {
					logrus.SetOutput(originalOutput)
					logrus.SetLevel(originalLevel)
				}()

				container = MockContainer(WithLinks([]string{"invalidlink"}))
				links := container.Links(true)
				gomega.Expect(links).To(gomega.BeEmpty())
				gomega.Expect(logOutput.String()).
					To(gomega.ContainSubstring("Invalid link format in host config, expected 'name:alias'"))
			})

			ginkgo.It("warns and skips link with empty container name", func() {
				logOutput := &bytes.Buffer{}
				originalOutput := logrus.StandardLogger().Out
				originalLevel := logrus.GetLevel()

				logrus.SetOutput(logOutput)
				logrus.SetLevel(logrus.WarnLevel)

				defer func() {
					logrus.SetOutput(originalOutput)
					logrus.SetLevel(originalLevel)
				}()

				container = MockContainer(WithLinks([]string{":alias"}))
				links := container.Links(true)
				gomega.Expect(links).To(gomega.BeEmpty())
				gomega.Expect(logOutput.String()).
					To(gomega.ContainSubstring("Invalid link format in host config, missing container name"))
			})

			ginkgo.It("normalizes container names with leading slashes", func() {
				container = MockContainer(WithLinks([]string{"/redis:test-watchtower"}))
				links := container.Links(true)
				gomega.Expect(links).To(gomega.ContainElement("redis"))
			})

			ginkgo.It("does not prefix already prefixed container names", func() {
				container = MockContainer(
					WithLinks([]string{"myproject-redis:test-watchtower"}),
					WithLabels(map[string]string{"com.docker.compose.project": "myproject"}),
				)
				links := container.Links(true)
				gomega.Expect(links).To(gomega.ContainElement("myproject-redis"))
			})

			ginkgo.It("does not prefix when project name is empty", func() {
				container = MockContainer(WithLinks([]string{"redis:test-watchtower"}))
				links := container.Links(true)
				gomega.Expect(links).To(gomega.ContainElement("redis"))
			})

			ginkgo.It("includes network mode dependencies", func() {
				container = MockContainer(
					WithNetworkMode("container:other"),
					WithLabels(map[string]string{"com.docker.compose.project": "myproject"}),
				)
				links := container.Links(true)
				gomega.Expect(links).To(gomega.ContainElement("myproject-other"))
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

	ginkgo.Describe("ResolveContainerIdentifier", func() {
		ginkgo.It("returns service name when compose service label is present", func() {
			container := MockContainer(WithLabels(map[string]string{
				"com.docker.compose.service": "web-service",
			}))
			identifier := ResolveContainerIdentifier(container)
			gomega.Expect(identifier).To(gomega.Equal("web-service"))
		})

		ginkgo.It("returns container name when compose service label is absent", func() {
			container := MockContainer(WithLabels(map[string]string{
				"other.label": "value",
			}))
			identifier := ResolveContainerIdentifier(container)
			gomega.Expect(identifier).To(gomega.Equal("test-watchtower"))
		})

		ginkgo.It("returns container name when labels are empty", func() {
			container := MockContainer(WithLabels(map[string]string{}))
			identifier := ResolveContainerIdentifier(container)
			gomega.Expect(identifier).To(gomega.Equal("test-watchtower"))
		})

		ginkgo.It("returns container name when labels are nil", func() {
			container := MockContainer(WithLabels(nil))
			identifier := ResolveContainerIdentifier(container)
			gomega.Expect(identifier).To(gomega.Equal("test-watchtower"))
		})

		ginkgo.It("returns service name when compose service label has value", func() {
			container := MockContainer(WithLabels(map[string]string{
				"com.docker.compose.service": "api",
			}))
			identifier := ResolveContainerIdentifier(container)
			gomega.Expect(identifier).To(gomega.Equal("api"))
		})

		ginkgo.It("handles multiple labels with service name present", func() {
			container := MockContainer(WithLabels(map[string]string{
				"com.docker.compose.service":     "db-service",
				"com.docker.compose.project":     "myproject",
				"com.centurylinklabs.watchtower": "true",
			}))
			identifier := ResolveContainerIdentifier(container)
			gomega.Expect(identifier).To(gomega.Equal("myproject-db-service"))
		})

		ginkgo.It("returns container name when ContainerInfo returns nil", func() {
			container := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			container.EXPECT().ContainerInfo().Return(nil)
			container.EXPECT().Name().Return("test-watchtower")
			identifier := ResolveContainerIdentifier(container)
			gomega.Expect(identifier).To(gomega.Equal("test-watchtower"))
		})

		ginkgo.It("returns container name when ContainerInfo.Config is nil", func() {
			container := MockContainer(WithLabels(map[string]string{}))
			container.containerInfo.Config = nil
			identifier := ResolveContainerIdentifier(container)
			gomega.Expect(identifier).To(gomega.Equal("test-watchtower"))
		})

		ginkgo.It("returns container name for replica containers", func() {
			mockContainer := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			mockContainer.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
				Config: &dockerContainer.Config{
					Labels: map[string]string{
						"com.docker.compose.service":     "web",
						"com.docker.compose.project":     "myproject",
						"com.docker.compose.version":     "3.8",
						"com.docker.compose.config-hash": "abc123",
					},
				},
			})
			mockContainer.EXPECT().Name().Return("myproject-web-1")
			result := ResolveContainerIdentifier(mockContainer)
			gomega.Expect(result).To(gomega.Equal("myproject-web-1"))
		})

		ginkgo.DescribeTable("replica container identifiers",
			func(name string, labels map[string]string, expected, description string) {
				container := MockContainer(WithLabels(labels))
				container.containerInfo.Name = name
				container.normalizedName = name
				result := ResolveContainerIdentifier(container)
				gomega.Expect(result).To(gomega.Equal(expected), "Test case: %s", description)
			},
			ginkgo.Entry(
				"single replica returns unique name",
				"myproject-web-1",
				map[string]string{
					"com.docker.compose.service":          "web",
					"com.docker.compose.project":          "myproject",
					"com.docker.compose.container-number": "1",
				},
				"myproject-web-1",
				"Replica should return full name to ensure uniqueness",
			),
			ginkgo.Entry(
				"multiple replicas with different numbers",
				"myproject-web-2",
				map[string]string{
					"com.docker.compose.service":          "web",
					"com.docker.compose.project":          "myproject",
					"com.docker.compose.container-number": "2",
				},
				"myproject-web-2",
				"Each replica should have unique identifier",
			),
			ginkgo.Entry(
				"replica without container number label",
				"app-db-3",
				map[string]string{
					"com.docker.compose.service": "db",
					"com.docker.compose.project": "app",
				},
				"app-db-3",
				"Should return full name even without container number to avoid collisions",
			),
			ginkgo.Entry(
				"non-replica with container number",
				"mydb",
				map[string]string{
					"com.docker.compose.service":          "db",
					"com.docker.compose.project":          "my",
					"com.docker.compose.container-number": "1",
				},
				"my-db-1",
				"Non-replica should use project-service-number format",
			),
			ginkgo.Entry(
				"replica with missing service label",
				"myproject-orphan-1",
				map[string]string{
					"com.docker.compose.project": "myproject",
				},
				"myproject-orphan-1",
				"Container with project prefix but no service should return full name",
			),
			ginkgo.Entry(
				"container with service but no project",
				"unknown-web-2",
				map[string]string{
					"com.docker.compose.service": "web",
				},
				"web",
				"Non-replica container with service label returns service name",
			),
			ginkgo.Entry(
				"collision prevention for replicas",
				"test-app-1",
				map[string]string{
					"com.docker.compose.service":          "app",
					"com.docker.compose.project":          "test",
					"com.docker.compose.container-number": "1",
				},
				"test-app-1",
				"Should return unique name instead of potentially colliding 'test-app'",
			),
			ginkgo.Entry(
				"collision prevention for replicas with same number",
				"prod-api-1",
				map[string]string{
					"com.docker.compose.service":          "api",
					"com.docker.compose.project":          "prod",
					"com.docker.compose.container-number": "1",
				},
				"prod-api-1",
				"Multiple replicas with same number should still be unique",
			),
		)

		ginkgo.Describe("replica collision scenarios", func() {
			ginkgo.It("prevents collision between replicas without unique identifiers", func() {
				replica1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
				replica1.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							"com.docker.compose.service": "service",
							"com.docker.compose.project": "myapp",
						},
					},
				})
				replica1.EXPECT().Name().Return("myapp-service-1")

				replica2 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
				replica2.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							"com.docker.compose.service": "service",
							"com.docker.compose.project": "myapp",
						},
					},
				})
				replica2.EXPECT().Name().Return("myapp-service-2")

				id1 := ResolveContainerIdentifier(replica1)
				id2 := ResolveContainerIdentifier(replica2)

				gomega.Expect(id1).To(gomega.Equal("myapp-service-1"))
				gomega.Expect(id2).To(gomega.Equal("myapp-service-2"))
				gomega.Expect(id1).ToNot(gomega.Equal(id2))
			})

			ginkgo.It("handles replicas with and without container number labels", func() {
				replicaWithNumber := mockTypes.NewMockContainer(ginkgo.GinkgoT())
				replicaWithNumber.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							"com.docker.compose.service":          "worker",
							"com.docker.compose.project":          "queue",
							"com.docker.compose.container-number": "5",
						},
					},
				})
				replicaWithNumber.EXPECT().Name().Return("queue-worker-5")

				replicaWithoutNumber := mockTypes.NewMockContainer(ginkgo.GinkgoT())
				replicaWithoutNumber.EXPECT().
					ContainerInfo().
					Return(&dockerContainer.InspectResponse{
						Config: &dockerContainer.Config{
							Labels: map[string]string{
								"com.docker.compose.service": "worker",
								"com.docker.compose.project": "queue",
							},
						},
					})
				replicaWithoutNumber.EXPECT().Name().Return("queue-worker-6")

				idWith := ResolveContainerIdentifier(replicaWithNumber)
				idWithout := ResolveContainerIdentifier(replicaWithoutNumber)

				gomega.Expect(idWith).To(gomega.Equal("queue-worker-5"))
				gomega.Expect(idWithout).To(gomega.Equal("queue-worker-6"))
			})
		})

		ginkgo.Describe("filterSelfReferences", func() {
			ginkgo.It("filters out basic self-references from links", func() {
				links := []string{"db", webContainerName, "cache"}
				containerName := webContainerName
				result := filterSelfReferences(links, containerName)
				gomega.Expect(result).To(gomega.Equal([]string{"db", "cache"}))
			})

			ginkgo.It("filters out multiple self-references", func() {
				links := []string{"first", webContainerName, "second", webContainerName, "third"}
				containerName := webContainerName
				result := filterSelfReferences(links, containerName)
				gomega.Expect(result).To(gomega.Equal([]string{"first", "second", "third"}))
			})

			ginkgo.It("returns empty slice when all links are self-references", func() {
				links := []string{webContainerName}
				containerName := webContainerName
				result := filterSelfReferences(links, containerName)
				gomega.Expect(result).To(gomega.BeEmpty())
			})

			ginkgo.It("preserves order of non-self links", func() {
				links := []string{"db", webContainerName, "cache", webContainerName, "redis"}
				containerName := webContainerName
				result := filterSelfReferences(links, containerName)
				gomega.Expect(result).To(gomega.Equal([]string{"db", "cache", "redis"}))
			})

			ginkgo.It("returns original slice unchanged when no self-references exist", func() {
				links := []string{"db", "cache"}
				containerName := webContainerName
				result := filterSelfReferences(links, containerName)
				gomega.Expect(result).To(gomega.Equal([]string{"db", "cache"}))
			})
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

	ginkgo.Describe("Host Config Creation", func() {
		ginkgo.It("preserves volume mount subpath in host config", func() {
			volumeMount := dockerMount.Mount{
				Type:   dockerMount.TypeVolume,
				Source: "test_volume",
				Target: "/config/nest",
				VolumeOptions: &dockerMount.VolumeOptions{
					Subpath: "ha/nest",
				},
			}

			container := MockContainer(WithMounts([]dockerMount.Mount{volumeMount}))
			hostConfig := container.GetCreateHostConfig()

			gomega.Expect(hostConfig.Mounts).To(gomega.HaveLen(1), "Expected exactly one mount")
			mount := hostConfig.Mounts[0]
			gomega.Expect(mount.Type).
				To(gomega.Equal(dockerMount.TypeVolume), "Mount type should be volume")
			gomega.Expect(mount.Source).To(gomega.Equal("test_volume"), "Mount source should match")
			gomega.Expect(mount.Target).
				To(gomega.Equal("/config/nest"), "Mount target should match")
			gomega.Expect(mount.VolumeOptions).ToNot(gomega.BeNil(), "VolumeOptions should be set")
			gomega.Expect(mount.VolumeOptions.Subpath).
				To(gomega.Equal("ha/nest"), "Subpath should be preserved")
		})
	})
})
