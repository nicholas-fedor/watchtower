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
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/docker/docker/api/types/container"
	dockerImage "github.com/docker/docker/api/types/image"
	dockerNetwork "github.com/docker/docker/api/types/network"
	dockerClient "github.com/docker/docker/client"

	"github.com/nicholas-fedor/watchtower/pkg/filters"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

const (
	testContainerID = "test-container-id"
	testShortID     = "abc123def456"
)

var _ = ginkgo.Describe("ListSourceContainers", func() {
	var docker *dockerClient.Client
	var mockServer *ghttp.Server

	ginkgo.BeforeEach(func() {
		mockServer = ghttp.NewServer()
		var err error
		docker, err = dockerClient.NewClientWithOpts(
			dockerClient.WithHost(mockServer.URL()),
			dockerClient.WithHTTPClient(mockServer.HTTPTestServer.Client()))
		require.NoError(ginkgo.GinkgoT(), err)
	})

	ginkgo.AfterEach(func() {
		mockServer.Close()
	})

	// Helper function to verify filters in request
	verifyFilters := func(expectedStatuses []string) http.HandlerFunc {
		return ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", gomega.MatchRegexp("^/v[0-9.]+/containers/json$")),
			func(w http.ResponseWriter, r *http.Request) {
				filtersParam := r.URL.Query().Get("filters")
				var filters map[string]map[string]bool
				err := json.Unmarshal([]byte(filtersParam), &filters)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				statusMap, exists := filters["status"]
				if len(expectedStatuses) == 0 {
					gomega.Expect(exists).To(gomega.BeFalse())
				} else {
					gomega.Expect(exists).To(gomega.BeTrue())
					actualStatuses := make([]string, 0, len(statusMap))
					for status := range statusMap {
						actualStatuses = append(actualStatuses, status)
					}
					gomega.Expect(actualStatuses).To(gomega.ConsistOf(expectedStatuses))
				}

				// Return a running container for the test
				containers := []dockerContainer.Summary{
					{ID: testContainerID, Names: []string{"/test-container"}, State: "running"},
				}
				ghttp.RespondWithJSONEncoded(http.StatusOK, containers)(w, r)
			},
		)
	}

	// Helper to mock container and image inspect
	mockInspects := func(containerID string) []http.HandlerFunc {
		return []http.HandlerFunc{
			ghttp.CombineHandlers(
				ghttp.VerifyRequest(
					"GET",
					gomega.MatchRegexp(fmt.Sprintf("^/v[0-9.]+/containers/%s/json$", containerID)),
				),
				ghttp.RespondWithJSONEncoded(http.StatusOK, dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						ID:    containerID,
						Name:  "/test-container",
						Image: "test-image:latest",
						State: &dockerContainer.State{
							Status:  "running",
							Running: true,
						},
						HostConfig: &dockerContainer.HostConfig{},
					},
					Config: &dockerContainer.Config{
						Image: "test-image:latest",
					},
				}),
			),
			ghttp.CombineHandlers(
				ghttp.VerifyRequest(
					"GET",
					gomega.MatchRegexp("^/v[0-9.]+/images/test-image:latest/json$"),
				),
				ghttp.RespondWithJSONEncoded(http.StatusOK, dockerImage.InspectResponse{
					ID: "test-image-id",
				}),
			),
		}
	}

	ginkgo.When("no custom filter with default ClientOptions", func() {
		ginkgo.It("should list only running containers", func() {
			mockServer.AppendHandlers(
				verifyFilters([]string{"running"}),
			)
			mockServer.AppendHandlers(mockInspects(testContainerID)...)

			containers, err := ListSourceContainers(docker, ClientOptions{}, nil)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(containers).To(gomega.HaveLen(1))
		})
	})

	ginkgo.When("no custom filter with IncludeStopped=true", func() {
		ginkgo.It("should list running, created, and exited containers", func() {
			mockServer.AppendHandlers(
				verifyFilters([]string{"running", "created", "exited"}),
			)
			mockServer.AppendHandlers(mockInspects(testContainerID)...)

			containers, err := ListSourceContainers(
				docker,
				ClientOptions{IncludeStopped: true},
				nil,
			)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(containers).To(gomega.HaveLen(1))
		})
	})

	ginkgo.When("no custom filter with IncludeRestarting=true", func() {
		ginkgo.It("should list running and restarting containers", func() {
			mockServer.AppendHandlers(
				verifyFilters([]string{"running", "restarting"}),
			)
			mockServer.AppendHandlers(mockInspects(testContainerID)...)

			containers, err := ListSourceContainers(
				docker,
				ClientOptions{IncludeRestarting: true},
				nil,
			)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(containers).To(gomega.HaveLen(1))
		})
	})

	ginkgo.When("with custom filter (filters.NoFilter)", func() {
		ginkgo.It("should list containers and apply custom filter", func() {
			mockServer.AppendHandlers(
				verifyFilters([]string{"running"}),
			)
			mockServer.AppendHandlers(mockInspects(testContainerID)...)

			containers, err := ListSourceContainers(docker, ClientOptions{}, filters.NoFilter)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(containers).To(gomega.HaveLen(1))
		})
	})
})

var _ = ginkgo.Describe("buildListFilterArgs", func() {
	ginkgo.It("includes running status always", func() {
		opts := ClientOptions{}
		filterArgs := buildListFilterArgs(opts, false)
		statuses := filterArgs.Get("status")
		gomega.Expect(statuses).To(gomega.ContainElement("running"))
	})

	ginkgo.It("includes created and exited when IncludeStopped is true", func() {
		opts := ClientOptions{IncludeStopped: true}
		filterArgs := buildListFilterArgs(opts, false)
		statuses := filterArgs.Get("status")
		gomega.Expect(statuses).To(gomega.ContainElement("created"))
		gomega.Expect(statuses).To(gomega.ContainElement("exited"))
		gomega.Expect(statuses).To(gomega.ContainElement("running"))
	})

	ginkgo.It("does not include created and exited when IncludeStopped is false", func() {
		opts := ClientOptions{IncludeStopped: false}
		filterArgs := buildListFilterArgs(opts, false)
		statuses := filterArgs.Get("status")
		gomega.Expect(statuses).ToNot(gomega.ContainElement("created"))
		gomega.Expect(statuses).ToNot(gomega.ContainElement("exited"))
		gomega.Expect(statuses).To(gomega.ContainElement("running"))
	})

	ginkgo.It(
		"includes restarting when IncludeRestarting is true and isPodman is false",
		func() {
			opts := ClientOptions{IncludeRestarting: true}
			filterArgs := buildListFilterArgs(opts, false)
			statuses := filterArgs.Get("status")
			gomega.Expect(statuses).To(gomega.ContainElement("restarting"))
			gomega.Expect(statuses).To(gomega.ContainElement("running"))
		},
	)

	ginkgo.It("does not include restarting when IncludeRestarting is false", func() {
		opts := ClientOptions{IncludeRestarting: false}
		filterArgs := buildListFilterArgs(opts, false)
		statuses := filterArgs.Get("status")
		gomega.Expect(statuses).ToNot(gomega.ContainElement("restarting"))
		gomega.Expect(statuses).To(gomega.ContainElement("running"))
	})

	ginkgo.It(
		"does not include restarting when isPodman is true regardless of IncludeRestarting",
		func() {
			opts := ClientOptions{IncludeRestarting: true}
			filterArgs := buildListFilterArgs(opts, true)
			statuses := filterArgs.Get("status")
			gomega.Expect(statuses).ToNot(gomega.ContainElement("restarting"))
			gomega.Expect(statuses).To(gomega.ContainElement("running"))
		},
	)

	ginkgo.It(
		"includes all statuses when IncludeStopped and IncludeRestarting are true and isPodman is false",
		func() {
			opts := ClientOptions{IncludeStopped: true, IncludeRestarting: true}
			filterArgs := buildListFilterArgs(opts, false)
			statuses := filterArgs.Get("status")
			gomega.Expect(statuses).To(gomega.ContainElement("running"))
			gomega.Expect(statuses).To(gomega.ContainElement("created"))
			gomega.Expect(statuses).To(gomega.ContainElement("exited"))
			gomega.Expect(statuses).To(gomega.ContainElement("restarting"))
		},
	)

	ginkgo.It(
		"includes running, created, exited but not restarting when IncludeStopped is true, IncludeRestarting is true, and isPodman is true",
		func() {
			opts := ClientOptions{IncludeStopped: true, IncludeRestarting: true}
			filterArgs := buildListFilterArgs(opts, true)
			statuses := filterArgs.Get("status")
			gomega.Expect(statuses).To(gomega.ContainElement("running"))
			gomega.Expect(statuses).To(gomega.ContainElement("created"))
			gomega.Expect(statuses).To(gomega.ContainElement("exited"))
			gomega.Expect(statuses).ToNot(gomega.ContainElement("restarting"))
		},
	)
})

var _ = ginkgo.Describe("GetSourceContainer", func() {
	var docker *dockerClient.Client
	var mockServer *ghttp.Server

	ginkgo.BeforeEach(func() {
		mockServer = ghttp.NewServer()
		var err error
		docker, err = dockerClient.NewClientWithOpts(
			dockerClient.WithHost(mockServer.URL()),
			dockerClient.WithHTTPClient(mockServer.HTTPTestServer.Client()))
		require.NoError(ginkgo.GinkgoT(), err)
	})

	ginkgo.AfterEach(func() {
		mockServer.Close()
	})

	ginkgo.When("retrieving a running container successfully", func() {
		ginkgo.It("should return container with image info", func() {
			containerID := testContainerID
			mockServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest(
						"GET",
						gomega.MatchRegexp(
							fmt.Sprintf("^/v[0-9.]+/containers/%s/json$", containerID),
						),
					),
					ghttp.RespondWithJSONEncoded(http.StatusOK, dockerContainer.InspectResponse{
						ContainerJSONBase: &dockerContainer.ContainerJSONBase{
							ID:    containerID,
							Name:  "/test-watchtower",
							Image: "test-image:latest",
							State: &dockerContainer.State{
								Status:  "running",
								Running: true,
							},
							HostConfig: &dockerContainer.HostConfig{},
						},
						Config: &dockerContainer.Config{
							Image: "test-image:latest",
						},
					}),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest(
						"GET",
						gomega.MatchRegexp("^/v[0-9.]+/images/test-image:latest/json$"),
					),
					ghttp.RespondWithJSONEncoded(http.StatusOK, dockerImage.InspectResponse{
						ID: "test-image-id",
					}),
				),
			)

			container, err := GetSourceContainer(docker, types.ContainerID(containerID))
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(container).ToNot(gomega.BeNil())
			gomega.Expect(container.ID()).To(gomega.Equal(types.ContainerID(containerID)))
			gomega.Expect(container.Name()).To(gomega.Equal("test-watchtower"))
			gomega.Expect(container.ImageID()).To(gomega.Equal(types.ImageID("test-image-id")))
		})
	})

	ginkgo.When("retrieving a stopped container successfully", func() {
		ginkgo.It("should return container without image info when ImageInspect fails", func() {
			containerID := testContainerID
			mockServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest(
						"GET",
						gomega.MatchRegexp(
							fmt.Sprintf("^/v[0-9.]+/containers/%s/json$", containerID),
						),
					),
					ghttp.RespondWithJSONEncoded(http.StatusOK, dockerContainer.InspectResponse{
						ContainerJSONBase: &dockerContainer.ContainerJSONBase{
							ID:    containerID,
							Name:  "/test-watchtower",
							Image: "test-image:latest",
							State: &dockerContainer.State{
								Status:  "exited",
								Running: false,
							},
							HostConfig: &dockerContainer.HostConfig{},
						},
						Config: &dockerContainer.Config{
							Image: "test-image:latest",
						},
					}),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest(
						"GET",
						gomega.MatchRegexp("^/v[0-9.]+/images/test-image:latest/json$"),
					),
					ghttp.RespondWith(
						http.StatusInternalServerError,
						`{"message": "image not found"}`,
					),
				),
			)

			container, err := GetSourceContainer(docker, types.ContainerID(containerID))
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(container).ToNot(gomega.BeNil())
			gomega.Expect(container.ID()).To(gomega.Equal(types.ContainerID(containerID)))
			gomega.Expect(container.ImageID()).To(gomega.Equal(types.ImageID("")))
		})
	})

	ginkgo.When("container has network mode requiring resolution", func() {
		ginkgo.It("should resolve network container name", func() {
			containerID := testContainerID
			parentID := "parent-container-id"
			mockServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest(
						"GET",
						gomega.MatchRegexp(
							fmt.Sprintf("^/v[0-9.]+/containers/%s/json$", containerID),
						),
					),
					ghttp.RespondWithJSONEncoded(http.StatusOK, dockerContainer.InspectResponse{
						ContainerJSONBase: &dockerContainer.ContainerJSONBase{
							ID:    containerID,
							Name:  "/test-watchtower",
							Image: "test-image:latest",
							State: &dockerContainer.State{
								Status:  "running",
								Running: true,
							},
							HostConfig: &dockerContainer.HostConfig{
								NetworkMode: dockerContainer.NetworkMode("container:" + parentID),
							},
						},
						Config: &dockerContainer.Config{
							Image: "test-image:latest",
						},
					}),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest(
						"GET",
						gomega.MatchRegexp(fmt.Sprintf("^/v[0-9.]+/containers/%s/json$", parentID)),
					),
					ghttp.RespondWithJSONEncoded(http.StatusOK, dockerContainer.InspectResponse{
						ContainerJSONBase: &dockerContainer.ContainerJSONBase{
							ID:   parentID,
							Name: "/parent-container",
						},
					}),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest(
						"GET",
						gomega.MatchRegexp("^/v[0-9.]+/images/test-image:latest/json$"),
					),
					ghttp.RespondWithJSONEncoded(http.StatusOK, dockerImage.InspectResponse{
						ID: "test-image-id",
					}),
				),
			)

			container, err := GetSourceContainer(docker, types.ContainerID(containerID))
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(container).ToNot(gomega.BeNil())
			gomega.Expect(container.ContainerInfo().HostConfig.NetworkMode).
				To(gomega.Equal(dockerContainer.NetworkMode("container:/parent-container")))
		})
	})

	ginkgo.When("container ID is invalid", func() {
		ginkgo.It("should return error for 404 not found", func() {
			containerID := "invalid-container-id"
			mockServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest(
						"GET",
						gomega.MatchRegexp(
							fmt.Sprintf("^/v[0-9.]+/containers/%s/json$", containerID),
						),
					),
					ghttp.RespondWith(http.StatusNotFound, `{"message": "No such container"}`),
				),
			)

			container, err := GetSourceContainer(docker, types.ContainerID(containerID))
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to inspect container"))
			gomega.Expect(container).To(gomega.BeNil())
		})
	})

	ginkgo.When("API connection fails", func() {
		ginkgo.It("should return error for connection timeout", func() {
			containerID := testContainerID
			mockServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest(
						"GET",
						gomega.MatchRegexp(
							fmt.Sprintf("^/v[0-9.]+/containers/%s/json$", containerID),
						),
					),
					ghttp.RespondWith(http.StatusGatewayTimeout, ""),
				),
			)

			container, err := GetSourceContainer(docker, types.ContainerID(containerID))
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to inspect container"))
			gomega.Expect(container).To(gomega.BeNil())
		})
	})

	ginkgo.When("response is malformed", func() {
		ginkgo.It("should return error for invalid JSON", func() {
			containerID := testContainerID
			mockServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest(
						"GET",
						gomega.MatchRegexp(
							fmt.Sprintf("^/v[0-9.]+/containers/%s/json$", containerID),
						),
					),
					ghttp.RespondWith(http.StatusOK, `{"invalid": json}`),
				),
			)

			container, err := GetSourceContainer(docker, types.ContainerID(containerID))
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to inspect container"))
			gomega.Expect(container).To(gomega.BeNil())
		})
	})
})

var _ = ginkgo.Describe("StopAndRemoveSourceContainer", func() {
	var docker *dockerClient.Client
	var mockServer *ghttp.Server

	ginkgo.BeforeEach(func() {
		mockServer = ghttp.NewServer()
		var err error
		docker, err = dockerClient.NewClientWithOpts(
			dockerClient.WithHost(mockServer.URL()),
			dockerClient.WithHTTPClient(mockServer.HTTPTestServer.Client()))
		require.NoError(ginkgo.GinkgoT(), err)
	})

	ginkgo.AfterEach(func() {
		mockServer.Close()
	})

	ginkgo.When(
		"stopping and removing a running container successfully with removeVolumes=false",
		func() {
			ginkgo.It("should stop and remove the container without error", func() {
				container := MockContainer(
					WithContainerState(dockerContainer.State{Running: true}),
				)
				cid := container.ContainerInfo().ID

				mockServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest(
							"POST",
							gomega.HaveSuffix(fmt.Sprintf("containers/%s/stop", cid)),
						),
						ghttp.RespondWith(http.StatusNoContent, nil),
					),
					ghttp.CombineHandlers(
						ghttp.VerifyRequest(
							"DELETE",
							gomega.MatchRegexp(fmt.Sprintf("^/v[0-9.]+/containers/%s$", cid)),
						),
						func(w http.ResponseWriter, r *http.Request) {
							// Verify query parameters
							gomega.Expect(r.URL.Query().Get("force")).To(gomega.Equal("1"))
							gomega.Expect(r.URL.Query().Get("v")).
								To(gomega.Equal(""))
								// removeVolumes=false
							w.WriteHeader(http.StatusNoContent)
						},
					),
				)

				err := StopAndRemoveSourceContainer(docker, container, 10*time.Second, false)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			})
		},
	)

	ginkgo.When(
		"stopping and removing a running container successfully with removeVolumes=true",
		func() {
			ginkgo.It("should stop and remove the container with volume removal", func() {
				container := MockContainer(
					WithContainerState(dockerContainer.State{Running: true}),
				)
				cid := container.ContainerInfo().ID

				mockServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest(
							"POST",
							gomega.HaveSuffix(fmt.Sprintf("containers/%s/stop", cid)),
						),
						ghttp.RespondWith(http.StatusNoContent, nil),
					),
					ghttp.CombineHandlers(
						ghttp.VerifyRequest(
							"DELETE",
							gomega.MatchRegexp(fmt.Sprintf("^/v[0-9.]+/containers/%s$", cid)),
						),
						func(w http.ResponseWriter, r *http.Request) {
							// Verify query parameters
							gomega.Expect(r.URL.Query().Get("force")).To(gomega.Equal("1"))
							gomega.Expect(r.URL.Query().Get("v")).
								To(gomega.Equal("1"))
								// removeVolumes=true
							w.WriteHeader(http.StatusNoContent)
						},
					),
				)

				err := StopAndRemoveSourceContainer(docker, container, 10*time.Second, true)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			})
		},
	)

	ginkgo.When("stopping fails", func() {
		ginkgo.It("should return an error without attempting removal", func() {
			container := MockContainer(
				WithContainerState(dockerContainer.State{Running: true}),
			)
			cid := container.ContainerInfo().ID

			mockServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest(
						"POST",
						gomega.HaveSuffix(fmt.Sprintf("containers/%s/stop", cid)),
					),
					ghttp.RespondWith(http.StatusInternalServerError, `{"message": "stop failed"}`),
				),
			)

			err := StopAndRemoveSourceContainer(docker, container, 10*time.Second, false)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to stop container"))
		})
	})

	ginkgo.When("removing fails", func() {
		ginkgo.It("should return an error after successful stop", func() {
			container := MockContainer(
				WithContainerState(dockerContainer.State{Running: true}),
			)
			cid := container.ContainerInfo().ID

			mockServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest(
						"POST",
						gomega.HaveSuffix(fmt.Sprintf("containers/%s/stop", cid)),
					),
					ghttp.RespondWith(http.StatusNoContent, nil),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest(
						"DELETE",
						gomega.MatchRegexp(fmt.Sprintf("^/v[0-9.]+/containers/%s$", cid)),
					),
					ghttp.RespondWith(
						http.StatusInternalServerError,
						`{"message": "remove failed"}`,
					),
				),
			)

			err := StopAndRemoveSourceContainer(docker, container, 10*time.Second, false)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to remove container"))
		})
	})

	ginkgo.When("container has AutoRemove enabled", func() {
		ginkgo.It("should stop but skip removal", func() {
			container := MockContainer(
				WithContainerState(dockerContainer.State{Running: true}),
				WithAutoRemove(true),
			)
			cid := container.ContainerInfo().ID

			mockServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest(
						"POST",
						gomega.HaveSuffix(fmt.Sprintf("containers/%s/stop", cid)),
					),
					ghttp.RespondWith(http.StatusNoContent, nil),
				),
			)

			err := StopAndRemoveSourceContainer(docker, container, 10*time.Second, true)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			// Should not have made a DELETE request since AutoRemove is true
			gomega.Expect(mockServer.ReceivedRequests()).To(gomega.HaveLen(1))
		})
	})

	ginkgo.When("container is not running", func() {
		ginkgo.It("should skip stop and remove successfully", func() {
			container := MockContainer(
				WithContainerState(dockerContainer.State{Running: false}),
			)
			cid := container.ContainerInfo().ID

			mockServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest(
						"DELETE",
						gomega.MatchRegexp(fmt.Sprintf("^/v[0-9.]+/containers/%s$", cid)),
					),
					func(w http.ResponseWriter, r *http.Request) {
						gomega.Expect(r.URL.Query().Get("force")).To(gomega.Equal("1"))
						gomega.Expect(r.URL.Query().Get("v")).To(gomega.Equal("1"))
						w.WriteHeader(http.StatusNoContent)
					},
				),
			)

			err := StopAndRemoveSourceContainer(docker, container, 10*time.Second, true)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})
	})

	ginkgo.When("timeout is specified", func() {
		ginkgo.It("should pass timeout to stop operation", func() {
			container := MockContainer(
				WithContainerState(dockerContainer.State{Running: true}),
			)
			cid := container.ContainerInfo().ID
			timeout := 30 * time.Second

			mockServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest(
						"POST",
						gomega.HaveSuffix(fmt.Sprintf("containers/%s/stop", cid)),
					),
					func(w http.ResponseWriter, r *http.Request) {
						query := r.URL.Query()
						signal := query.Get("signal")
						timeoutStr := query.Get("t")
						gomega.Expect(signal).To(gomega.Equal("SIGTERM"))
						gomega.Expect(timeoutStr).To(gomega.Equal("30"))
						w.WriteHeader(http.StatusNoContent)
					},
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest(
						"DELETE",
						gomega.MatchRegexp(fmt.Sprintf("^/v[0-9.]+/containers/%s$", cid)),
					),
					ghttp.RespondWith(http.StatusNoContent, nil),
				),
			)

			err := StopAndRemoveSourceContainer(docker, container, timeout, false)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})
	})

	ginkgo.When("container is not found during removal", func() {
		ginkgo.It("should succeed as container is already gone", func() {
			container := MockContainer(
				WithContainerState(dockerContainer.State{Running: true}),
			)
			cid := container.ContainerInfo().ID

			mockServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest(
						"POST",
						gomega.HaveSuffix(fmt.Sprintf("containers/%s/stop", cid)),
					),
					ghttp.RespondWith(http.StatusNoContent, nil),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest(
						"DELETE",
						gomega.MatchRegexp(fmt.Sprintf("^/v[0-9.]+/containers/%s$", cid)),
					),
					ghttp.RespondWith(http.StatusNotFound, `{"message": "No such container"}`),
				),
			)

			err := StopAndRemoveSourceContainer(docker, container, 10*time.Second, false)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})
	})

	ginkgo.When("API connection fails during stop", func() {
		ginkgo.It("should return error", func() {
			container := MockContainer(
				WithContainerState(dockerContainer.State{Running: true}),
			)
			cid := container.ContainerInfo().ID

			mockServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest(
						"POST",
						gomega.HaveSuffix(fmt.Sprintf("containers/%s/stop", cid)),
					),
					ghttp.RespondWith(http.StatusGatewayTimeout, ""),
				),
			)

			err := StopAndRemoveSourceContainer(docker, container, 10*time.Second, false)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to stop container"))
		})
	})
})

var _ = ginkgo.Describe("getNetworkConfig", func() {
	ginkgo.Context("with bridge network mode", func() {
		ginkgo.It("should return network config with processed endpoints", func() {
			container := MockContainer(
				WithNetworkMode("bridge"),
				WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
					"bridge": {
						NetworkID:  "bridge_network_id",
						MacAddress: "02:42:ac:11:00:02",
						IPAddress:  "172.17.0.2",
						Aliases:    []string{"container_id", "test-alias"},
						DNSNames:   []string{"test.example.com"},
						IPAMConfig: &dockerNetwork.EndpointIPAMConfig{
							IPv4Address: "172.17.0.2",
						},
					},
				}),
			)

			config := getNetworkConfig(container, "1.50")

			gomega.Expect(config).ToNot(gomega.BeNil())
			gomega.Expect(config.EndpointsConfig).To(gomega.HaveKey("bridge"))
			endpoint := config.EndpointsConfig["bridge"]
			gomega.Expect(endpoint.NetworkID).To(gomega.Equal("bridge_network_id"))
			gomega.Expect(endpoint.MacAddress).To(gomega.Equal("02:42:ac:11:00:02"))
			gomega.Expect(endpoint.IPAddress).To(gomega.Equal("172.17.0.2"))
			gomega.Expect(endpoint.DNSNames).To(gomega.ConsistOf("test.example.com"))
			gomega.Expect(endpoint.IPAMConfig).ToNot(gomega.BeNil())
			gomega.Expect(endpoint.IPAMConfig.IPv4Address).To(gomega.Equal("172.17.0.2"))
			// Aliases should be filtered to remove container short ID
			gomega.Expect(endpoint.Aliases).To(gomega.ConsistOf("test-alias"))
		})

		ginkgo.It("should handle multiple networks", func() {
			container := MockContainer(
				WithNetworks("bridge", "custom_network"),
			)

			config := getNetworkConfig(container, "1.50")

			gomega.Expect(config.EndpointsConfig).To(gomega.HaveLen(2))
			gomega.Expect(config.EndpointsConfig).To(gomega.HaveKey("bridge"))
			gomega.Expect(config.EndpointsConfig).To(gomega.HaveKey("custom_network"))
		})
	})

	ginkgo.Context("with host network mode", func() {
		ginkgo.It("should return network config with cleared host-specific settings", func() {
			container := MockContainer(
				WithNetworkMode("host"),
				WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
					"host": {
						MacAddress: "02:42:ac:11:00:02",
						IPAddress:  "192.168.1.100",
						Aliases:    []string{"container_id", "host-alias"},
						DNSNames:   []string{"host.example.com"},
						IPAMConfig: &dockerNetwork.EndpointIPAMConfig{
							IPv4Address: "192.168.1.100",
						},
					},
				}),
			)

			config := getNetworkConfig(container, "1.50")

			gomega.Expect(config.EndpointsConfig).To(gomega.HaveKey("host"))
			endpoint := config.EndpointsConfig["host"]
			gomega.Expect(endpoint.MacAddress).To(gomega.Equal(""))
			gomega.Expect(endpoint.IPAddress).To(gomega.Equal(""))
			gomega.Expect(endpoint.DNSNames).To(gomega.BeNil())
			gomega.Expect(endpoint.IPAMConfig).To(gomega.BeNil())
			gomega.Expect(endpoint.Aliases).To(gomega.BeNil())
		})
	})

	ginkgo.Context("with legacy client version (< 1.44)", func() {
		ginkgo.It("should clear MAC addresses and IP info", func() {
			container := MockContainer(
				WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
					"bridge": {
						MacAddress: "02:42:ac:11:00:02",
						IPAddress:  "172.17.0.2",
						DNSNames:   []string{"test.example.com"},
					},
				}),
			)

			config := getNetworkConfig(container, "1.40")

			endpoint := config.EndpointsConfig["bridge"]
			gomega.Expect(endpoint.MacAddress).To(gomega.Equal(""))
			gomega.Expect(endpoint.IPAddress).To(gomega.Equal(""))
			gomega.Expect(endpoint.DNSNames).To(gomega.BeNil())
		})
	})

	ginkgo.Context("with nil network settings", func() {
		ginkgo.It("should return empty network config", func() {
			container := MockContainer(
				func(container *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
					container.NetworkSettings = nil
				},
			)

			config := getNetworkConfig(container, "1.50")

			gomega.Expect(config).ToNot(gomega.BeNil())
			gomega.Expect(config.EndpointsConfig).To(gomega.BeEmpty())
		})
	})

	ginkgo.Context("with stopped container (exited state)", func() {
		ginkgo.It("should handle MAC address validation for non-running containers", func() {
			container := MockContainer(
				WithContainerState(dockerContainer.State{Running: false, Status: "exited"}),
				WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
					"bridge": {
						// No MAC address for stopped container - this is expected
					},
				}),
			)

			config := getNetworkConfig(container, "1.50")

			gomega.Expect(config).ToNot(gomega.BeNil())
			gomega.Expect(config.EndpointsConfig).To(gomega.HaveKey("bridge"))
		})
	})

	ginkgo.Context("with custom network settings", func() {
		ginkgo.It("should preserve custom network configurations", func() {
			container := MockContainer(
				WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
					"my_custom_network": {
						NetworkID:  "custom_net_id",
						MacAddress: "aa:bb:cc:dd:ee:ff",
						IPAddress:  "10.0.0.5",
						Aliases:    []string{"container_id", "custom-alias"},
						Links:      []string{"other_container:alias"},
					},
				}),
			)

			config := getNetworkConfig(container, "1.50")

			endpoint := config.EndpointsConfig["my_custom_network"]
			gomega.Expect(endpoint.NetworkID).To(gomega.Equal("custom_net_id"))
			gomega.Expect(endpoint.MacAddress).To(gomega.Equal("aa:bb:cc:dd:ee:ff"))
			gomega.Expect(endpoint.IPAddress).To(gomega.Equal("10.0.0.5"))
			gomega.Expect(endpoint.Aliases).To(gomega.ConsistOf("custom-alias"))
			gomega.Expect(endpoint.Links).To(gomega.ConsistOf("other_container:alias"))
		})
	})

	ginkgo.Context("with Podman-style client version", func() {
		ginkgo.It("should handle Podman API version strings", func() {
			container := MockContainer(
				WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
					"bridge": {
						MacAddress: "02:42:ac:11:00:02",
					},
				}),
			)

			// Test with Podman version format
			config := getNetworkConfig(container, "4.0.0")

			endpoint := config.EndpointsConfig["bridge"]
			// Should preserve MAC for modern versions
			gomega.Expect(endpoint.MacAddress).To(gomega.Equal("02:42:ac:11:00:02"))
		})
	})

	ginkgo.Context("with nil endpoint in network settings", func() {
		ginkgo.It("should skip nil endpoints", func() {
			container := MockContainer(
				WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
					"bridge":    nil,
					"valid_net": {NetworkID: "valid_id"},
				}),
			)

			config := getNetworkConfig(container, "1.50")

			gomega.Expect(config.EndpointsConfig).To(gomega.HaveKey("valid_net"))
			gomega.Expect(config.EndpointsConfig).ToNot(gomega.HaveKey("bridge"))
		})
	})
})

var _ = ginkgo.Describe("newEmptyNetworkConfig", func() {
	ginkgo.It("should create a non-nil network config", func() {
		config := newEmptyNetworkConfig()
		gomega.Expect(config).ToNot(gomega.BeNil())
	})

	ginkgo.It("should initialize EndpointsConfig as an empty map", func() {
		config := newEmptyNetworkConfig()
		gomega.Expect(config.EndpointsConfig).ToNot(gomega.BeNil())
		gomega.Expect(config.EndpointsConfig).To(gomega.BeEmpty())
		gomega.Expect(config.EndpointsConfig).
			To(gomega.Equal(make(map[string]*dockerNetwork.EndpointSettings)))
	})

	ginkgo.It("should return a properly structured NetworkingConfig", func() {
		config := newEmptyNetworkConfig()
		gomega.Expect(config).To(gomega.BeAssignableToTypeOf(&dockerNetwork.NetworkingConfig{}))
		gomega.Expect(config.EndpointsConfig).
			To(gomega.BeAssignableToTypeOf(make(map[string]*dockerNetwork.EndpointSettings)))
	})

	ginkgo.It("should be ready for use - can add endpoints to the map", func() {
		config := newEmptyNetworkConfig()
		endpoint := &dockerNetwork.EndpointSettings{
			NetworkID: "test-network",
		}
		config.EndpointsConfig["test"] = endpoint
		gomega.Expect(config.EndpointsConfig).To(gomega.HaveLen(1))
		gomega.Expect(config.EndpointsConfig["test"]).To(gomega.Equal(endpoint))
	})
})

var _ = ginkgo.Describe("processEndpoint", func() {
	ginkgo.Context("with modern API version (>= 1.44)", func() {
		clientVersion := "1.50"

		ginkgo.Context("and non-host network mode", func() {
			isHostNetwork := false

			ginkgo.It("should preserve MAC address, IP address, and DNS names", func() {
				sourceEndpoint := &dockerNetwork.EndpointSettings{
					NetworkID:  "bridge_network_id",
					MacAddress: "02:42:ac:11:00:02",
					IPAddress:  "172.17.0.2",
					DNSNames:   []string{"test.example.com"},
					Aliases:    []string{"container_id", "test-alias"},
					IPAMConfig: &dockerNetwork.EndpointIPAMConfig{
						IPv4Address: "172.17.0.2",
					},
				}
				containerID := types.ContainerID("container_id")

				result, err := processEndpoint(
					sourceEndpoint,
					containerID,
					clientVersion,
					isHostNetwork,
				)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				gomega.Expect(result.NetworkID).To(gomega.Equal("bridge_network_id"))
				gomega.Expect(result.MacAddress).To(gomega.Equal("02:42:ac:11:00:02"))
				gomega.Expect(result.IPAddress).To(gomega.Equal("172.17.0.2"))
				gomega.Expect(result.DNSNames).To(gomega.ConsistOf("test.example.com"))
				gomega.Expect(result.IPAMConfig).ToNot(gomega.BeNil())
				gomega.Expect(result.IPAMConfig.IPv4Address).To(gomega.Equal("172.17.0.2"))
				// Aliases should be filtered to remove container short ID
				gomega.Expect(result.Aliases).To(gomega.ConsistOf("test-alias"))
			})

			ginkgo.It("should handle multiple aliases correctly", func() {
				sourceEndpoint := &dockerNetwork.EndpointSettings{
					Aliases: []string{"container_id", "alias1", "alias2", "other_id"},
				}
				containerID := types.ContainerID("container_id")

				result, err := processEndpoint(
					sourceEndpoint,
					containerID,
					clientVersion,
					isHostNetwork,
				)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				gomega.Expect(result.Aliases).To(gomega.ConsistOf("alias1", "alias2", "other_id"))
			})

			ginkgo.It("should copy all IPAM config fields", func() {
				sourceEndpoint := &dockerNetwork.EndpointSettings{
					IPAMConfig: &dockerNetwork.EndpointIPAMConfig{
						IPv4Address:  "192.168.1.100",
						IPv6Address:  "2001:db8::1",
						LinkLocalIPs: []string{"169.254.1.1", "169.254.1.2"},
					},
				}
				containerID := types.ContainerID("container_id")

				result, err := processEndpoint(
					sourceEndpoint,
					containerID,
					clientVersion,
					isHostNetwork,
				)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				gomega.Expect(result.IPAMConfig).ToNot(gomega.BeNil())
				gomega.Expect(result.IPAMConfig.IPv4Address).To(gomega.Equal("192.168.1.100"))
				gomega.Expect(result.IPAMConfig.IPv6Address).To(gomega.Equal("2001:db8::1"))
				gomega.Expect(result.IPAMConfig.LinkLocalIPs).
					To(gomega.ConsistOf("169.254.1.1", "169.254.1.2"))
			})

			ginkgo.It("should preserve empty aliases list", func() {
				sourceEndpoint := &dockerNetwork.EndpointSettings{
					Aliases: []string{},
				}
				containerID := types.ContainerID("test_container_id")

				result, err := processEndpoint(
					sourceEndpoint,
					containerID,
					clientVersion,
					isHostNetwork,
				)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				gomega.Expect(result.Aliases).To(gomega.BeEmpty())
			})

			ginkgo.It("should preserve other endpoint fields", func() {
				sourceEndpoint := &dockerNetwork.EndpointSettings{
					Gateway:    "172.17.0.1",
					Links:      []string{"other_container:alias"},
					EndpointID: "endpoint_id",
				}
				containerID := types.ContainerID("test_container_id")

				result, err := processEndpoint(
					sourceEndpoint,
					containerID,
					clientVersion,
					isHostNetwork,
				)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				gomega.Expect(result.Gateway).To(gomega.Equal("172.17.0.1"))
				gomega.Expect(result.Links).To(gomega.ConsistOf("other_container:alias"))
				gomega.Expect(result.EndpointID).To(gomega.Equal("endpoint_id"))
			})
		})
	})

	ginkgo.Context("with legacy API version (< 1.44)", func() {
		clientVersion := "1.40"

		ginkgo.Context("and non-host network mode", func() {
			isHostNetwork := false

			ginkgo.It("should clear MAC address, IP address, and DNS names", func() {
				sourceEndpoint := &dockerNetwork.EndpointSettings{
					MacAddress: "02:42:ac:11:00:02",
					IPAddress:  "172.17.0.2",
					DNSNames:   []string{"test.example.com"},
					Aliases:    []string{"container_id", "test-alias"},
					IPAMConfig: &dockerNetwork.EndpointIPAMConfig{
						IPv4Address: "172.17.0.2",
					},
				}
				containerID := types.ContainerID("container_id")

				result, err := processEndpoint(
					sourceEndpoint,
					containerID,
					clientVersion,
					isHostNetwork,
				)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				gomega.Expect(result.MacAddress).To(gomega.Equal(""))
				gomega.Expect(result.IPAddress).To(gomega.Equal(""))
				gomega.Expect(result.DNSNames).To(gomega.BeNil())
				gomega.Expect(result.IPAMConfig).ToNot(gomega.BeNil())
				gomega.Expect(result.IPAMConfig.IPv4Address).To(gomega.Equal("172.17.0.2"))
				// Aliases should still be filtered
				gomega.Expect(result.Aliases).To(gomega.ConsistOf("test-alias"))
			})
		})
	})

	ginkgo.Context("with host network mode", func() {
		isHostNetwork := true

		ginkgo.It("should clear aliases regardless of API version", func() {
			sourceEndpoint := &dockerNetwork.EndpointSettings{
				Aliases: []string{"container_id", "test-alias"},
			}
			containerID := types.ContainerID("test_container_id")

			result, err := processEndpoint(sourceEndpoint, containerID, "1.50", isHostNetwork)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Expect(result.Aliases).To(gomega.BeNil())
		})

		ginkgo.It("should clear IPAM config regardless of API version", func() {
			sourceEndpoint := &dockerNetwork.EndpointSettings{
				IPAMConfig: &dockerNetwork.EndpointIPAMConfig{
					IPv4Address: "192.168.1.100",
				},
			}
			containerID := types.ContainerID("test_container_id")

			result, err := processEndpoint(sourceEndpoint, containerID, "1.50", isHostNetwork)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Expect(result.IPAMConfig).To(gomega.BeNil())
		})

		ginkgo.It("should clear MAC and IP addresses even with modern API", func() {
			sourceEndpoint := &dockerNetwork.EndpointSettings{
				MacAddress: "02:42:ac:11:00:02",
				IPAddress:  "192.168.1.100",
				DNSNames:   []string{"host.example.com"},
			}
			containerID := types.ContainerID("test_container_id")

			result, err := processEndpoint(sourceEndpoint, containerID, "1.50", isHostNetwork)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Expect(result.MacAddress).To(gomega.Equal(""))
			gomega.Expect(result.IPAddress).To(gomega.Equal(""))
			gomega.Expect(result.DNSNames).To(gomega.BeNil())
		})
	})

	ginkgo.Context("with edge cases", func() {
		ginkgo.It("should handle nil IPAM config gracefully", func() {
			sourceEndpoint := &dockerNetwork.EndpointSettings{
				IPAMConfig: nil,
			}
			containerID := types.ContainerID("test_container_id")

			result, err := processEndpoint(sourceEndpoint, containerID, "1.50", false)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Expect(result.IPAMConfig).To(gomega.BeNil())
		})

		ginkgo.It("should handle empty DNS names", func() {
			sourceEndpoint := &dockerNetwork.EndpointSettings{
				DNSNames: []string{},
			}
			containerID := types.ContainerID("test_container_id")

			result, err := processEndpoint(sourceEndpoint, containerID, "1.50", false)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Expect(result.DNSNames).To(gomega.BeEmpty())
		})

		ginkgo.Context("with nil source endpoint", func() {
			ginkgo.It("should return ErrNilSourceEndpoint when sourceEndpoint is nil", func() {
				containerID := types.ContainerID("test_container_id")

				result, err := processEndpoint(nil, containerID, "1.50", false)
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(err).To(gomega.MatchError(errNilSourceEndpoint))
				gomega.Expect(result).To(gomega.BeNil())
			})
		})
	})
})

var _ = ginkgo.Describe("validateMacAddresses", func() {
	ginkgo.Context("with legacy API version (< 1.44)", func() {
		clientVersion := "1.40"

		ginkgo.Context("and non-host network mode", func() {
			isHostNetwork := false

			ginkgo.It("should succeed when no MAC address is present", func() {
				container := MockContainer(
					WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
						"bridge": {
							NetworkID: "bridge_network_id",
							IPAddress: "172.17.0.2",
						},
					}),
				)
				config := &dockerNetwork.NetworkingConfig{
					EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
						"bridge": {
							NetworkID: "bridge_network_id",
							IPAddress: "172.17.0.2",
						},
					},
				}

				err := validateMacAddresses(
					config,
					container.ID(),
					clientVersion,
					isHostNetwork,
					container,
				)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			})

			ginkgo.It("should return error when unexpected MAC address is present", func() {
				container := MockContainer(
					WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
						"bridge": {
							NetworkID:  "bridge_network_id",
							MacAddress: "02:42:ac:11:00:02",
							IPAddress:  "172.17.0.2",
						},
					}),
				)
				config := &dockerNetwork.NetworkingConfig{
					EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
						"bridge": {
							NetworkID:  "bridge_network_id",
							MacAddress: "02:42:ac:11:00:02",
							IPAddress:  "172.17.0.2",
						},
					},
				}

				err := validateMacAddresses(
					config,
					container.ID(),
					clientVersion,
					isHostNetwork,
					container,
				)
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(err).
					To(gomega.MatchError(gomega.ContainSubstring("unexpected MAC address in legacy config")))
				gomega.Expect(err).
					To(gomega.MatchError(gomega.ContainSubstring("API version 1.40")))
			})
		})
	})

	ginkgo.Context("with host network mode", func() {
		isHostNetwork := true

		ginkgo.It("should succeed when no MAC address is present", func() {
			container := MockContainer(
				WithNetworkMode("host"),
				WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
					"host": {
						NetworkID: "host_network_id",
						IPAddress: "192.168.1.100",
					},
				}),
			)
			config := &dockerNetwork.NetworkingConfig{
				EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
					"host": {
						NetworkID: "host_network_id",
						IPAddress: "192.168.1.100",
					},
				},
			}

			err := validateMacAddresses(config, container.ID(), "1.50", isHostNetwork, container)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.It("should return error when unexpected MAC address is present", func() {
			container := MockContainer(
				WithNetworkMode("host"),
				WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
					"host": {
						NetworkID:  "host_network_id",
						MacAddress: "02:42:ac:11:00:02",
						IPAddress:  "192.168.1.100",
					},
				}),
			)
			config := &dockerNetwork.NetworkingConfig{
				EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
					"host": {
						NetworkID:  "host_network_id",
						MacAddress: "02:42:ac:11:00:02",
						IPAddress:  "192.168.1.100",
					},
				},
			}

			err := validateMacAddresses(config, container.ID(), "1.50", isHostNetwork, container)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err).To(gomega.MatchError(errUnexpectedMacInHost))
		})
	})

	ginkgo.Context("with modern API version (>= 1.44)", func() {
		clientVersion := "1.50"

		ginkgo.Context("and non-host network mode", func() {
			isHostNetwork := false

			ginkgo.Context("and running container", func() {
				ginkgo.It("should succeed when MAC address is present", func() {
					container := MockContainer(
						WithContainerState(dockerContainer.State{Running: true, Status: "running"}),
						WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
							"bridge": {
								NetworkID:  "bridge_network_id",
								MacAddress: "02:42:ac:11:00:02",
								IPAddress:  "172.17.0.2",
							},
						}),
					)
					config := &dockerNetwork.NetworkingConfig{
						EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
							"bridge": {
								NetworkID:  "bridge_network_id",
								MacAddress: "02:42:ac:11:00:02",
								IPAddress:  "172.17.0.2",
							},
						},
					}

					err := validateMacAddresses(
						config,
						container.ID(),
						clientVersion,
						isHostNetwork,
						container,
					)
					gomega.Expect(err).ToNot(gomega.HaveOccurred())
				})

				ginkgo.It("should return error when MAC address is missing", func() {
					container := MockContainer(
						WithContainerState(dockerContainer.State{Running: true, Status: "running"}),
						WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
							"bridge": {
								NetworkID: "bridge_network_id",
								IPAddress: "172.17.0.2",
							},
						}),
					)
					config := &dockerNetwork.NetworkingConfig{
						EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
							"bridge": {
								NetworkID: "bridge_network_id",
								IPAddress: "172.17.0.2",
							},
						},
					}

					err := validateMacAddresses(
						config,
						container.ID(),
						clientVersion,
						isHostNetwork,
						container,
					)
					gomega.Expect(err).To(gomega.HaveOccurred())
					gomega.Expect(err).To(gomega.MatchError(errNoMacInNonHost))
				})

				ginkgo.It("should succeed with multiple networks all having MAC addresses", func() {
					container := MockContainer(
						WithContainerState(dockerContainer.State{Running: true, Status: "running"}),
						WithNetworks("bridge", "custom_network"),
						WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
							"bridge": {
								NetworkID:  "bridge_network_id",
								MacAddress: "02:42:ac:11:00:02",
								IPAddress:  "172.17.0.2",
							},
							"custom_network": {
								NetworkID:  "custom_network_id",
								MacAddress: "aa:bb:cc:dd:ee:ff",
								IPAddress:  "10.0.0.5",
							},
						}),
					)
					config := &dockerNetwork.NetworkingConfig{
						EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
							"bridge": {
								NetworkID:  "bridge_network_id",
								MacAddress: "02:42:ac:11:00:02",
								IPAddress:  "172.17.0.2",
							},
							"custom_network": {
								NetworkID:  "custom_network_id",
								MacAddress: "aa:bb:cc:dd:ee:ff",
								IPAddress:  "10.0.0.5",
							},
						},
					}

					err := validateMacAddresses(
						config,
						container.ID(),
						clientVersion,
						isHostNetwork,
						container,
					)
					gomega.Expect(err).ToNot(gomega.HaveOccurred())
				})

				ginkgo.It(
					"should succeed when at least one of multiple networks has MAC address",
					func() {
						container := MockContainer(
							WithContainerState(
								dockerContainer.State{Running: true, Status: "running"},
							),
							WithNetworks("bridge", "custom_network"),
							WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
								"bridge": {
									NetworkID:  "bridge_network_id",
									MacAddress: "02:42:ac:11:00:02",
									IPAddress:  "172.17.0.2",
								},
								"custom_network": {
									NetworkID: "custom_network_id",
									IPAddress: "10.0.0.5",
								},
							}),
						)
						config := &dockerNetwork.NetworkingConfig{
							EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
								"bridge": {
									NetworkID:  "bridge_network_id",
									MacAddress: "02:42:ac:11:00:02",
									IPAddress:  "172.17.0.2",
								},
								"custom_network": {
									NetworkID: "custom_network_id",
									IPAddress: "10.0.0.5",
								},
							},
						}

						err := validateMacAddresses(
							config,
							container.ID(),
							clientVersion,
							isHostNetwork,
							container,
						)
						gomega.Expect(err).ToNot(gomega.HaveOccurred())
					},
				)
			})

			ginkgo.Context("and non-running container", func() {
				ginkgo.It("should succeed when MAC address is missing (created state)", func() {
					container := MockContainer(
						WithContainerState(
							dockerContainer.State{Running: false, Status: "created"},
						),
						WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
							"bridge": {
								NetworkID: "bridge_network_id",
								IPAddress: "172.17.0.2",
							},
						}),
					)
					config := &dockerNetwork.NetworkingConfig{
						EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
							"bridge": {
								NetworkID: "bridge_network_id",
								IPAddress: "172.17.0.2",
							},
						},
					}

					err := validateMacAddresses(
						config,
						container.ID(),
						clientVersion,
						isHostNetwork,
						container,
					)
					gomega.Expect(err).ToNot(gomega.HaveOccurred())
				})

				ginkgo.It("should succeed when MAC address is missing (exited state)", func() {
					container := MockContainer(
						WithContainerState(dockerContainer.State{Running: false, Status: "exited"}),
						WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
							"bridge": {
								NetworkID: "bridge_network_id",
								IPAddress: "172.17.0.2",
							},
						}),
					)
					config := &dockerNetwork.NetworkingConfig{
						EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
							"bridge": {
								NetworkID: "bridge_network_id",
								IPAddress: "172.17.0.2",
							},
						},
					}

					err := validateMacAddresses(
						config,
						container.ID(),
						clientVersion,
						isHostNetwork,
						container,
					)
					gomega.Expect(err).ToNot(gomega.HaveOccurred())
				})
			})
		})
	})

	ginkgo.Context("with edge cases", func() {
		ginkgo.It("should handle empty network configuration", func() {
			container := MockContainer(
				WithContainerState(dockerContainer.State{Running: true, Status: "running"}),
			)
			config := &dockerNetwork.NetworkingConfig{
				EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{},
			}

			err := validateMacAddresses(config, container.ID(), "1.50", false, container)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err).To(gomega.MatchError(errNoMacInNonHost))
		})

		ginkgo.It("should handle nil network configuration", func() {
			container := MockContainer(
				WithContainerState(dockerContainer.State{Running: true, Status: "running"}),
			)

			err := validateMacAddresses(nil, container.ID(), "1.50", false, container)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err).To(gomega.MatchError(errNoMacInNonHost))
		})
	})
})

var _ = ginkgo.Describe("filterAliases", func() {
	ginkgo.It("removes container short ID from aliases", func() {
		shortID := testShortID
		aliases := []string{testShortID, "custom-alias", "another-alias"}
		result := filterAliases(aliases, shortID)
		gomega.Expect(result).To(gomega.ConsistOf("custom-alias", "another-alias"))
	})

	ginkgo.It("preserves all aliases when short ID is not present", func() {
		shortID := testShortID
		aliases := []string{"custom-alias", "another-alias", "third-alias"}
		result := filterAliases(aliases, shortID)
		gomega.Expect(result).To(gomega.ConsistOf("custom-alias", "another-alias", "third-alias"))
	})

	ginkgo.It("handles empty alias list", func() {
		shortID := testShortID
		aliases := []string{}
		result := filterAliases(aliases, shortID)
		gomega.Expect(result).To(gomega.BeEmpty())
	})

	ginkgo.It("handles nil alias list", func() {
		shortID := testShortID
		var aliases []string
		result := filterAliases(aliases, shortID)
		gomega.Expect(result).To(gomega.BeEmpty())
	})

	ginkgo.It("removes multiple instances of short ID", func() {
		shortID := testShortID
		aliases := []string{testShortID, "custom-alias", testShortID, "another-alias"}
		result := filterAliases(aliases, shortID)
		gomega.Expect(result).To(gomega.ConsistOf("custom-alias", "another-alias"))
	})

	ginkgo.It("returns empty list when all aliases are the short ID", func() {
		shortID := testShortID
		aliases := []string{testShortID, testShortID}
		result := filterAliases(aliases, shortID)
		gomega.Expect(result).To(gomega.BeEmpty())
	})

	ginkgo.It("does not remove partial matches", func() {
		shortID := testShortID
		aliases := []string{"abc123def4567", "abc123def45", testShortID, "custom-alias"}
		result := filterAliases(aliases, shortID)
		gomega.Expect(result).To(gomega.ConsistOf("abc123def4567", "abc123def45", "custom-alias"))
	})

	ginkgo.It("handles case-sensitive matching", func() {
		shortID := testShortID
		aliases := []string{"ABC123DEF456", testShortID, "Abc123Def456"}
		result := filterAliases(aliases, shortID)
		gomega.Expect(result).To(gomega.ConsistOf("ABC123DEF456", "Abc123Def456"))
	})

	ginkgo.It("preserves order of remaining aliases", func() {
		shortID := testShortID
		aliases := []string{"first", testShortID, "second", "third", testShortID}
		result := filterAliases(aliases, shortID)
		gomega.Expect(result).To(gomega.Equal([]string{"first", "second", "third"}))
	})
})

var _ = ginkgo.Describe("StopSourceContainer", func() {
	var docker *dockerClient.Client
	var mockServer *ghttp.Server

	ginkgo.BeforeEach(func() {
		mockServer = ghttp.NewServer()
		var err error
		docker, err = dockerClient.NewClientWithOpts(
			dockerClient.WithHost(mockServer.URL()),
			dockerClient.WithHTTPClient(mockServer.HTTPTestServer.Client()))
		require.NoError(ginkgo.GinkgoT(), err)
	})

	ginkgo.AfterEach(func() {
		mockServer.Close()
	})

	ginkgo.When("stopping a linked restarting container", func() {
		ginkgo.It("should log 'Stopping linked container' and succeed", func() {
			container := MockContainer()
			container.SetLinkedToRestarting(true)
			cid := container.ContainerInfo().ID

			mockServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest(
						"POST",
						gomega.HaveSuffix(fmt.Sprintf("containers/%s/stop", cid)),
					),
					ghttp.RespondWith(http.StatusNoContent, nil),
				),
			)

			resetLogrus, logbuf := captureLogrus(logrus.InfoLevel)
			defer resetLogrus()

			err := StopSourceContainer(docker, container, 10*time.Second)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Eventually(logbuf).Should(gbytes.Say("Stopping linked container"))
		})
	})

	ginkgo.When("stopping a running container successfully", func() {
		ginkgo.It("should stop the container without error", func() {
			container := MockContainer(
				WithContainerState(dockerContainer.State{Running: true}),
			)
			cid := container.ContainerInfo().ID

			mockServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest(
						"POST",
						gomega.HaveSuffix(fmt.Sprintf("containers/%s/stop", cid)),
					),
					ghttp.RespondWith(http.StatusNoContent, nil),
				),
			)

			err := StopSourceContainer(docker, container, 10*time.Second)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})
	})

	ginkgo.When("stopping a container that fails", func() {
		ginkgo.It("should return an error", func() {
			container := MockContainer(
				WithContainerState(dockerContainer.State{Running: true}),
			)
			cid := container.ContainerInfo().ID

			mockServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest(
						"POST",
						gomega.HaveSuffix(fmt.Sprintf("containers/%s/stop", cid)),
					),
					ghttp.RespondWith(http.StatusInternalServerError, `{"message": "stop failed"}`),
				),
			)

			err := StopSourceContainer(docker, container, 10*time.Second)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to stop container"))
		})
	})

	ginkgo.When("stopping a container with timeout parameter", func() {
		ginkgo.It("should pass the timeout to the Docker API", func() {
			container := MockContainer(
				WithContainerState(dockerContainer.State{Running: true}),
			)
			cid := container.ContainerInfo().ID
			timeout := 30 * time.Second

			mockServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest(
						"POST",
						gomega.HaveSuffix(fmt.Sprintf("containers/%s/stop", cid)),
					),
					func(w http.ResponseWriter, r *http.Request) {
						// Verify the timeout is in the query parameters
						query := r.URL.Query()
						signal := query.Get("signal")
						timeoutStr := query.Get("t")
						gomega.Expect(signal).To(gomega.Equal("SIGTERM"))
						gomega.Expect(timeoutStr).To(gomega.Equal("30"))
						w.WriteHeader(http.StatusNoContent)
					},
				),
			)

			err := StopSourceContainer(docker, container, timeout)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})
	})

	ginkgo.When("stopping a container that is not running", func() {
		ginkgo.It("should not attempt to stop and return no error", func() {
			container := MockContainer(
				WithContainerState(dockerContainer.State{Running: false}),
			)

			err := StopSourceContainer(docker, container, 10*time.Second)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(mockServer.ReceivedRequests()).To(gomega.BeEmpty())
		})
	})
})

var _ = ginkgo.Describe("debugLogMacAddress", func() {
	ginkgo.Context("with legacy API version (< minimum supported version)", func() {
		minSupportedVersion := "1.44"

		ginkgo.Context("and MAC address present", func() {
			ginkgo.It("should log 'Unexpected MAC address in legacy config'", func() {
				config := &dockerNetwork.NetworkingConfig{
					EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
						"bridge": {MacAddress: "02:42:ac:11:00:02"},
					},
				}
				containerID := types.ContainerID("test-container")

				resetLogrus, logbuf := captureLogrus(logrus.DebugLevel)
				defer resetLogrus()

				debugLogMacAddress(config, containerID, "1.40", minSupportedVersion, false)

				gomega.Eventually(logbuf).
					Should(gbytes.Say("Unexpected MAC address in legacy config"))
			})
		})

		ginkgo.Context("and no MAC address present", func() {
			ginkgo.It("should log 'No MAC address in legacy config, Docker will handle'", func() {
				config := &dockerNetwork.NetworkingConfig{
					EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
						"bridge": {NetworkID: "bridge_network_id"},
					},
				}
				containerID := types.ContainerID("test-container")

				resetLogrus, logbuf := captureLogrus(logrus.DebugLevel)
				defer resetLogrus()

				debugLogMacAddress(config, containerID, "1.40", minSupportedVersion, false)

				gomega.Eventually(logbuf).
					Should(gbytes.Say("No MAC address in legacy config, Docker will handle"))
			})
		})

		ginkgo.Context("and host network mode", func() {
			ginkgo.It(
				"should log 'Unexpected MAC address in legacy config' when MAC present",
				func() {
					config := &dockerNetwork.NetworkingConfig{
						EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
							"host": {MacAddress: "02:42:ac:11:00:02"},
						},
					}
					containerID := types.ContainerID("test-container")

					resetLogrus, logbuf := captureLogrus(logrus.DebugLevel)
					defer resetLogrus()

					debugLogMacAddress(config, containerID, "1.40", minSupportedVersion, true)

					gomega.Eventually(logbuf).
						Should(gbytes.Say("Unexpected MAC address in legacy config"))
				},
			)
		})
	})

	ginkgo.Context("with API version < 1.44 and not host network", func() {
		minSupportedVersion := "1.39" // Lower than 1.40 so first case doesn't match

		ginkgo.Context("and MAC address present", func() {
			ginkgo.It("should log 'Unexpected MAC address in legacy config'", func() {
				config := &dockerNetwork.NetworkingConfig{
					EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
						"bridge": {MacAddress: "02:42:ac:11:00:02"},
					},
				}
				containerID := types.ContainerID("test-container")

				resetLogrus, logbuf := captureLogrus(logrus.DebugLevel)
				defer resetLogrus()

				debugLogMacAddress(config, containerID, "1.40", minSupportedVersion, false)

				gomega.Eventually(logbuf).
					Should(gbytes.Say("Unexpected MAC address in legacy config"))
			})
		})

		ginkgo.Context("and no MAC address present", func() {
			ginkgo.It("should log 'No MAC address in legacy config, as expected'", func() {
				config := &dockerNetwork.NetworkingConfig{
					EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
						"bridge": {NetworkID: "bridge_network_id"},
					},
				}
				containerID := types.ContainerID("test-container")

				resetLogrus, logbuf := captureLogrus(logrus.DebugLevel)
				defer resetLogrus()

				debugLogMacAddress(config, containerID, "1.40", minSupportedVersion, false)

				gomega.Eventually(logbuf).
					Should(gbytes.Say("No MAC address in legacy config, as expected"))
			})
		})
	})

	ginkgo.Context("with modern API version (>= 1.44)", func() {
		minSupportedVersion := "1.44"

		ginkgo.Context("and MAC address present", func() {
			ginkgo.It("should log 'Verified MAC address configuration'", func() {
				config := &dockerNetwork.NetworkingConfig{
					EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
						"bridge": {MacAddress: "02:42:ac:11:00:02"},
					},
				}
				containerID := types.ContainerID("test-container")

				resetLogrus, logbuf := captureLogrus(logrus.DebugLevel)
				defer resetLogrus()

				debugLogMacAddress(config, containerID, "1.50", minSupportedVersion, false)

				gomega.Eventually(logbuf).Should(gbytes.Say("Verified MAC address configuration"))
			})

			ginkgo.It("should log MAC address details for each network", func() {
				config := &dockerNetwork.NetworkingConfig{
					EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
						"bridge":         {MacAddress: "02:42:ac:11:00:02"},
						"custom_network": {MacAddress: "aa:bb:cc:dd:ee:ff"},
					},
				}
				containerID := types.ContainerID("test-container")

				resetLogrus, logbuf := captureLogrus(logrus.DebugLevel)
				defer resetLogrus()

				debugLogMacAddress(config, containerID, "1.50", minSupportedVersion, false)

				gomega.Eventually(logbuf).Should(gbytes.Say("Found MAC address in config"))
				gomega.Eventually(logbuf).Should(gbytes.Say("network"))
				gomega.Eventually(logbuf).Should(gbytes.Say("mac_address"))
			})
		})

		ginkgo.Context("and no MAC address present", func() {
			ginkgo.Context("and non-host network mode", func() {
				ginkgo.It("should log 'No MAC address found in config'", func() {
					config := &dockerNetwork.NetworkingConfig{
						EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
							"bridge": {NetworkID: "bridge_network_id"},
						},
					}
					containerID := types.ContainerID("test-container")

					resetLogrus, logbuf := captureLogrus(logrus.DebugLevel)
					defer resetLogrus()

					debugLogMacAddress(config, containerID, "1.50", minSupportedVersion, false)

					gomega.Eventually(logbuf).Should(gbytes.Say("No MAC address found in config"))
				})
			})

			ginkgo.Context("and host network mode", func() {
				ginkgo.It("should log 'No MAC address in host network mode, as expected'", func() {
					config := &dockerNetwork.NetworkingConfig{
						EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
							"host": {NetworkID: "host_network_id"},
						},
					}
					containerID := types.ContainerID("test-container")

					resetLogrus, logbuf := captureLogrus(logrus.DebugLevel)
					defer resetLogrus()

					debugLogMacAddress(config, containerID, "1.50", minSupportedVersion, true)

					gomega.Eventually(logbuf).
						Should(gbytes.Say("No MAC address in host network mode, as expected"))
				})
			})
		})
	})

	ginkgo.Context("with nil network config", func() {
		ginkgo.It("should execute without panicking and log appropriate message", func() {
			containerID := types.ContainerID("test-container")

			resetLogrus, logbuf := captureLogrus(logrus.DebugLevel)
			defer resetLogrus()

			gomega.Expect(func() {
				debugLogMacAddress(nil, containerID, "1.50", "1.44", false)
			}).ToNot(gomega.Panic())

			// Should log that no MAC address was found
			gomega.Eventually(logbuf).Should(gbytes.Say("No MAC address found in config"))
		})
	})

	ginkgo.Context("with empty endpoints config", func() {
		ginkgo.It("should log appropriate message for non-host network", func() {
			config := &dockerNetwork.NetworkingConfig{
				EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{},
			}
			containerID := types.ContainerID("test-container")

			resetLogrus, logbuf := captureLogrus(logrus.DebugLevel)
			defer resetLogrus()

			debugLogMacAddress(config, containerID, "1.50", "1.44", false)

			gomega.Eventually(logbuf).Should(gbytes.Say("No MAC address found in config"))
		})
	})

	ginkgo.Context("with multiple networks", func() {
		ginkgo.It("should check all networks for MAC addresses", func() {
			config := &dockerNetwork.NetworkingConfig{
				EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
					"bridge":         {MacAddress: "02:42:ac:11:00:02"},
					"custom_network": {NetworkID: "custom_net_id"},
				},
			}
			containerID := types.ContainerID("test-container")

			resetLogrus, logbuf := captureLogrus(logrus.DebugLevel)
			defer resetLogrus()

			debugLogMacAddress(config, containerID, "1.50", "1.44", false)

			gomega.Eventually(logbuf).Should(gbytes.Say("Found MAC address in config"))
			gomega.Eventually(logbuf).Should(gbytes.Say("Verified MAC address configuration"))
		})
	})

	ginkgo.Context("with different client version formats", func() {
		ginkgo.It("should handle Podman-style version strings", func() {
			config := &dockerNetwork.NetworkingConfig{
				EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
					"bridge": {MacAddress: "02:42:ac:11:00:02"},
				},
			}
			containerID := types.ContainerID("test-container")

			resetLogrus, logbuf := captureLogrus(logrus.DebugLevel)
			defer resetLogrus()

			debugLogMacAddress(config, containerID, "4.0.0", "1.44", false)

			gomega.Eventually(logbuf).Should(gbytes.Say("Verified MAC address configuration"))
		})

		ginkgo.It("should handle version strings with patch levels", func() {
			config := &dockerNetwork.NetworkingConfig{
				EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
					"bridge": {MacAddress: "02:42:ac:11:00:02"},
				},
			}
			containerID := types.ContainerID("test-container")

			resetLogrus, logbuf := captureLogrus(logrus.DebugLevel)
			defer resetLogrus()

			debugLogMacAddress(config, containerID, "1.45.0", "1.44", false)

			gomega.Eventually(logbuf).Should(gbytes.Say("Verified MAC address configuration"))
		})
	})
})

var _ = ginkgo.Describe("IsWatchtowerParent", func() {
	ginkgo.It("should return false for empty chain", func() {
		result := IsWatchtowerParent(types.ContainerID("test-id"), "")
		gomega.Expect(result).To(gomega.BeFalse())
	})

	ginkgo.It("should return false for empty ID", func() {
		result := IsWatchtowerParent(types.ContainerID(""), "id1,id2")
		gomega.Expect(result).To(gomega.BeFalse())
	})

	ginkgo.It("should return true when ID is at start of chain", func() {
		result := IsWatchtowerParent(types.ContainerID("test-id"), "test-id,id2,id3")
		gomega.Expect(result).To(gomega.BeTrue())
	})

	ginkgo.It("should return true when ID is in middle of chain", func() {
		result := IsWatchtowerParent(types.ContainerID("id2"), "id1,id2,id3")
		gomega.Expect(result).To(gomega.BeTrue())
	})

	ginkgo.It("should return true when ID is at end of chain", func() {
		result := IsWatchtowerParent(types.ContainerID("id3"), "id1,id2,id3")
		gomega.Expect(result).To(gomega.BeTrue())
	})

	ginkgo.It("should return true for single ID chain", func() {
		result := IsWatchtowerParent(types.ContainerID("test-id"), "test-id")
		gomega.Expect(result).To(gomega.BeTrue())
	})

	ginkgo.It("should return false when ID is not in chain", func() {
		result := IsWatchtowerParent(types.ContainerID("test-id"), "id1,id2,id3")
		gomega.Expect(result).To(gomega.BeFalse())
	})

	ginkgo.It("should handle chains with spaces around commas", func() {
		result := IsWatchtowerParent(types.ContainerID("id2"), "id1, id2 , id3")
		gomega.Expect(result).To(gomega.BeTrue())
	})
})
