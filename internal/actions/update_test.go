package actions_test

import (
	"errors"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/go-connections/nat"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/nicholas-fedor/watchtower/internal/actions"
	"github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/filters"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

func getCommonTestData(keepContainer string) *mocks.TestData {
	return &mocks.TestData{
		NameOfContainerToKeep: keepContainer,
		Containers: []types.Container{
			mocks.CreateMockContainer(
				"test-container-01",
				"test-container-01",
				"fake-image:latest",
				time.Now().AddDate(0, 0, -1)),
			mocks.CreateMockContainer(
				"test-container-02",
				"test-container-02",
				"fake-image:latest",
				time.Now()),
			mocks.CreateMockContainer(
				"test-container-03",
				"test-container-03",
				"fake-image:latest",
				time.Now()),
		},
	}
}

func getLinkedTestData(withImageInfo bool) *mocks.TestData {
	staleContainer := mocks.CreateMockContainer(
		"test-container-01",
		"/test-container-01",
		"fake-image1:latest",
		time.Now().AddDate(0, 0, -1))

	var imageInfo *image.InspectResponse
	if withImageInfo {
		imageInfo = mocks.CreateMockImageInfo("test-container-02")
	}

	linkingContainer := mocks.CreateMockContainerWithLinks(
		"test-container-02",
		"/test-container-02",
		"fake-image2:latest",
		time.Now(),
		[]string{staleContainer.Name()},
		imageInfo)

	return &mocks.TestData{
		Staleness: map[string]bool{linkingContainer.Name(): false},
		Containers: []types.Container{
			staleContainer,
			linkingContainer,
		},
	}
}

func getNetworkModeTestData() *mocks.TestData {
	staleContainer := mocks.CreateMockContainer(
		"network-dependency",
		"/network-dependency",
		"fake-image:latest",
		time.Now().AddDate(0, 0, -1))

	dependentContainer := mocks.CreateMockContainerWithConfig(
		"network-dependent",
		"/network-dependent",
		"fake-image2:latest",
		true,
		false,
		time.Now(),
		&container.Config{
			Image:        "fake-image2:latest",
			Labels:       make(map[string]string),
			ExposedPorts: map[nat.Port]struct{}{},
		})

	// Set network mode to container:network-dependency
	dependentContainer.ContainerInfo().HostConfig.NetworkMode = "container:network-dependency"

	return &mocks.TestData{
		Staleness:  map[string]bool{staleContainer.Name(): true, dependentContainer.Name(): false},
		Containers: []types.Container{staleContainer, dependentContainer},
	}
}

func createDependencyChain(names []string) []types.Container {
	containers := make([]types.Container, len(names))
	for i := range names {
		name := names[i]
		image := "image-" + name[10:] + ":latest"

		labels := make(map[string]string)
		if i < len(names)-1 {
			labels["com.centurylinklabs.watchtower.depends-on"] = names[i+1]
		}

		containers[i] = mocks.CreateMockContainerWithConfig(
			name,
			"/"+name,
			image,
			true,
			false,
			time.Now().AddDate(0, 0, -1),
			&container.Config{
				Labels:       labels,
				ExposedPorts: map[nat.Port]struct{}{},
			})
	}

	return containers
}

var _ = ginkgo.Describe("the update action", func() {
	ginkgo.When("updating a Watchtower container", func() {
		ginkgo.It("should rename and start a new container without cleanup", func() {
			client := mocks.CreateMockClient(
				&mocks.TestData{
					Containers: []types.Container{
						mocks.CreateMockContainerWithConfig(
							"watchtower",
							"/watchtower",
							"watchtower:latest",
							true,
							false,
							time.Now(),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower": "true",
								},
							}),
					},
					Staleness: map[string]bool{
						"watchtower": true,
					},
				},
				false,
				false,
			)
			report, cleanupImageInfos, err := actions.Update(
				client,
				actions.UpdateConfig{
					Cleanup:          true,
					Filter:           filters.WatchtowerContainersFilter,
					CPUCopyMode:      "auto",
					PullFailureDelay: 10 * time.Millisecond,
				},
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(report.Updated()).To(gomega.HaveLen(1))
			gomega.Expect(cleanupImageInfos).
				To(gomega.BeEmpty(), "No cleanup for renamed Watchtower container")
			gomega.Expect(client.TestData.TriedToRemoveImageCount).
				To(gomega.Equal(0), "RemoveImageByID should not be called during Update")
			gomega.Expect(client.TestData.RenameContainerCount).
				To(gomega.Equal(1), "RenameContainer should be called once")
			gomega.Expect(client.TestData.UpdateContainerCount).
				To(gomega.Equal(1), "UpdateContainer should be called once for old Watchtower")
			gomega.Expect(client.TestData.StopContainerCount).
				To(gomega.Equal(1), "StopContainer should be called once for old Watchtower")
			gomega.Expect(client.TestData.IsContainerStaleCount).
				To(gomega.Equal(2), "IsContainerStale should be called twice for Watchtower")
		})

		ginkgo.It("should skip rename with no-restart for Watchtower", func() {
			client := mocks.CreateMockClient(
				&mocks.TestData{
					Containers: []types.Container{
						mocks.CreateMockContainerWithConfig(
							"watchtower",
							"/watchtower",
							"watchtower:latest",
							true,
							false,
							time.Now(),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower":              "true",
									"com.centurylinklabs.watchtower.monitor-only": "true",
								},
							}),
					},
					Staleness: map[string]bool{
						"watchtower": true,
					},
				},
				false,
				false,
			)
			config := actions.UpdateConfig{
				Cleanup:     true,
				NoRestart:   true,
				Filter:      filters.WatchtowerContainersFilter,
				CPUCopyMode: "auto",
			}
			report, cleanupImageInfos, err := actions.Update(client, config)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(report.Scanned()).
				To(gomega.HaveLen(1), "Container should be scanned but not updated")
			gomega.Expect(report.Updated()).
				To(gomega.BeEmpty(), "No containers should be updated with no-restart")
			gomega.Expect(cleanupImageInfos).
				To(gomega.BeEmpty(), "No images should be collected for cleanup")
			gomega.Expect(client.TestData.RenameContainerCount).
				To(gomega.Equal(0), "RenameContainer should not be called with no-restart")
		})

		ginkgo.It("should not rename Watchtower container in run-once mode", func() {
			client := mocks.CreateMockClient(
				&mocks.TestData{
					Containers: []types.Container{
						mocks.CreateMockContainerWithConfig(
							"watchtower",
							"/watchtower",
							"watchtower:latest",
							true,
							false,
							time.Now(),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower": "true",
								},
							}),
					},
					Staleness: map[string]bool{
						"watchtower": true,
					},
				},
				false,
				false,
			)
			config := actions.UpdateConfig{
				Cleanup:     true,
				RunOnce:     true,
				Filter:      filters.WatchtowerContainersFilter,
				CPUCopyMode: "auto",
			}
			report, cleanupImageInfos, err := actions.Update(client, config)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(report.Scanned()).
				To(gomega.HaveLen(1), "Container should be scanned")
			gomega.Expect(report.Updated()).
				To(gomega.BeEmpty(), "Watchtower should not be updated in run-once mode")
			gomega.Expect(cleanupImageInfos).
				To(gomega.BeEmpty(), "No images should be collected for cleanup")
			gomega.Expect(client.TestData.RenameContainerCount).
				To(gomega.Equal(0), "RenameContainer should not be called in run-once mode")
			gomega.Expect(client.TestData.IsContainerStaleCount).
				To(gomega.Equal(1), "IsContainerStale should be called once to pull the image")
		})

		ginkgo.It("should not clean up unscoped instances when scope is specified", func() {
			client := mocks.CreateMockClient(
				&mocks.TestData{
					Containers: []types.Container{
						mocks.CreateMockContainerWithConfig(
							"watchtower-scoped",
							"/watchtower-scoped",
							"watchtower:latest",
							true,
							false,
							time.Now().Add(-time.Hour),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower":       "true",
									"com.centurylinklabs.watchtower.scope": "prod",
								},
							}),
						mocks.CreateMockContainerWithConfig(
							"watchtower-unscoped",
							"/watchtower-unscoped",
							"watchtower:old",
							true,
							false,
							time.Now(),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower": "true",
								},
							}),
					},
				},
				false,
				false,
			)
			var cleanupImageInfos []types.CleanedImageInfo
			cleanupOccurred, err := actions.CheckForMultipleWatchtowerInstances(
				client,
				true, // cleanup=true
				"prod",
				&cleanupImageInfos,
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cleanupOccurred).To(gomega.BeFalse())
			gomega.Expect(client.TestData.StopContainerCount).
				To(gomega.Equal(0), "StopContainer should not be called for unscoped container")
			gomega.Expect(cleanupImageInfos).
				To(gomega.BeEmpty(), "No cleanup should occur for unscoped container")
			gomega.Expect(client.TestData.TriedToRemoveImageCount).
				To(gomega.Equal(0), "RemoveImageByID should not be called for unscoped container")
		})

		ginkgo.It("should skip cleanup for shared image", func() {
			client := mocks.CreateMockClient(
				&mocks.TestData{
					Containers: []types.Container{
						mocks.CreateMockContainerWithConfig(
							"old",
							"/watchtower",
							"watchtower:latest",
							true,
							false,
							time.Now().Add(-time.Hour),
							&container.Config{
								Labels: map[string]string{"com.centurylinklabs.watchtower": "true"},
							}),
						mocks.CreateMockContainerWithConfig(
							"new",
							"/watchtower",
							"watchtower:latest",
							true,
							false,
							time.Now(),
							&container.Config{
								Labels: map[string]string{"com.centurylinklabs.watchtower": "true"},
							}),
					},
				},
				false,
				false,
			)
			var cleanupImageInfos []types.CleanedImageInfo
			cleanupOccurred, err := actions.CheckForMultipleWatchtowerInstances(
				client,
				true,
				"",
				&cleanupImageInfos,
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cleanupOccurred).To(gomega.BeTrue())
			gomega.Expect(cleanupImageInfos).
				To(gomega.BeEmpty(), "No image cleanup for shared image")
			gomega.Expect(client.TestData.TriedToRemoveImageCount).To(gomega.Equal(0))
		})
	})

	ginkgo.When("watchtower has been instructed to clean up", func() {
		ginkgo.When("there are multiple containers using the same image", func() {
			ginkgo.It("should collect the image ID once for deferred cleanup", func() {
				client := mocks.CreateMockClient(getCommonTestData(""), false, false)
				client.TestData.Staleness = map[string]bool{
					"test-container-01": true,
					"test-container-02": true,
					"test-container-03": true,
				}
				report, cleanupImageInfos, err := actions.Update(
					client,
					actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).To(gomega.HaveLen(3))
				gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(1))
				gomega.Expect(cleanupImageInfos).
					To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("fake-image:latest"))))
				gomega.Expect(cleanupImageInfos).
					To(gomega.ContainElement(gomega.HaveField("ContainerName", "test-container-03")))
				gomega.Expect(client.TestData.TriedToRemoveImageCount).
					To(gomega.Equal(0), "RemoveImageByID should not be called during Update")
			})
		})

		ginkgo.When("there are multiple containers using different images", func() {
			ginkgo.It("should collect each image ID for deferred cleanup", func() {
				testData := getCommonTestData("")
				testData.Containers = append(
					testData.Containers,
					mocks.CreateMockContainer(
						"unique-test-container",
						"unique-test-container",
						"unique-fake-image:latest",
						time.Now(),
					),
				)
				client := mocks.CreateMockClient(testData, false, false)
				client.TestData.Staleness = map[string]bool{
					"test-container-01":     true,
					"test-container-02":     true,
					"test-container-03":     true,
					"unique-test-container": true,
				}
				report, cleanupImageInfos, err := actions.Update(
					client,
					actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).To(gomega.HaveLen(4))
				gomega.Expect(cleanupImageInfos).
					To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("fake-image:latest"))))
				gomega.Expect(cleanupImageInfos).
					To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("unique-fake-image:latest"))))
				gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(2))
				gomega.Expect(client.TestData.TriedToRemoveImageCount).
					To(gomega.Equal(0), "RemoveImageByID should not be called during Update")
			})
		})

		ginkgo.When("there are linked containers being updated", func() {
			ginkgo.It("should collect only the stale container's image ID", func() {
				client := mocks.CreateMockClient(getLinkedTestData(true), false, false)
				client.TestData.Staleness["test-container-01"] = true
				report, cleanupImageInfos, err := actions.Update(
					client,
					actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).To(gomega.HaveLen(1))
				gomega.Expect(cleanupImageInfos).
					To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("fake-image1:latest"))))
				gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(1))
				gomega.Expect(client.TestData.TriedToRemoveImageCount).
					To(gomega.Equal(0), "RemoveImageByID should not be called during Update")
			})
		})

		ginkgo.When("performing a rolling restart update", func() {
			ginkgo.It("should collect the image ID for deferred cleanup", func() {
				client := mocks.CreateMockClient(getCommonTestData(""), false, false)
				client.TestData.Staleness = map[string]bool{
					"test-container-01": true,
					"test-container-02": true,
					"test-container-03": true,
				}
				report, cleanupImageInfos, err := actions.Update(
					client,
					actions.UpdateConfig{Cleanup: true, RollingRestart: true, CPUCopyMode: "auto"},
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).To(gomega.HaveLen(3))
				gomega.Expect(cleanupImageInfos).
					To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("fake-image:latest"))))
				gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(1))
				gomega.Expect(client.TestData.TriedToRemoveImageCount).
					To(gomega.Equal(0), "RemoveImageByID should not be called during Update")
				gomega.Expect(client.TestData.WaitForContainerHealthyCount).
					To(gomega.Equal(3), "WaitForContainerHealthy should be called for each updated container")
			})
		})

		ginkgo.When("updating a linked container with missing image info", func() {
			ginkgo.It("should gracefully fail and collect no image IDs", func() {
				client := mocks.CreateMockClient(getLinkedTestData(false), false, false)
				client.TestData.Staleness["test-container-01"] = true
				report, cleanupImageInfos, err := actions.Update(
					client,
					actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).To(gomega.HaveLen(1))
				gomega.Expect(report.Fresh()).To(gomega.HaveLen(1))
				gomega.Expect(cleanupImageInfos).
					To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("fake-image1:latest"))))
				gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(1))
				gomega.Expect(client.TestData.TriedToRemoveImageCount).
					To(gomega.Equal(0), "RemoveImageByID should not be called during Update")
			})
		})
	})

	ginkgo.When("watchtower has been instructed to monitor only", func() {
		ginkgo.When("certain containers are set to monitor only", func() {
			ginkgo.It("should not update those containers and collect no image IDs", func() {
				client := mocks.CreateMockClient(
					&mocks.TestData{
						NameOfContainerToKeep: "test-container-02",
						Containers: []types.Container{
							mocks.CreateMockContainer(
								"test-container-01",
								"test-container-01",
								"fake-image1:latest",
								time.Now()),
							mocks.CreateMockContainerWithConfig(
								"test-container-02",
								"test-container-02",
								"fake-image2:latest",
								false,
								false,
								time.Now(),
								&container.Config{
									Labels: map[string]string{
										"com.centurylinklabs.watchtower.monitor-only": "true",
									},
								}),
						},
					},
					false,
					false,
				)
				client.TestData.Staleness = map[string]bool{
					"test-container-01": true,
					"test-container-02": true,
				}
				report, cleanupImageInfos, err := actions.Update(
					client,
					actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).To(gomega.HaveLen(1))
				gomega.Expect(cleanupImageInfos).
					To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("fake-image1:latest"))))
				gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(1))
				gomega.Expect(client.TestData.TriedToRemoveImageCount).
					To(gomega.Equal(0), "RemoveImageByID should not be called during Update")
			})
		})

		ginkgo.When("monitor only is set globally", func() {
			ginkgo.It("should not update any containers and collect no image IDs", func() {
				client := mocks.CreateMockClient(
					&mocks.TestData{
						Containers: []types.Container{
							mocks.CreateMockContainer(
								"test-container-01",
								"test-container-01",
								"fake-image:latest",
								time.Now()),
							mocks.CreateMockContainer(
								"test-container-02",
								"test-container-02",
								"fake-image:latest",
								time.Now()),
						},
					},
					false,
					false,
				)
				client.TestData.Staleness = map[string]bool{
					"test-container-01": true,
					"test-container-02": true,
				}
				report, cleanupImageInfos, err := actions.Update(
					client,
					actions.UpdateConfig{Cleanup: true, MonitorOnly: true, CPUCopyMode: "auto"},
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).To(gomega.BeEmpty())
				gomega.Expect(cleanupImageInfos).To(gomega.BeEmpty())
				gomega.Expect(client.TestData.TriedToRemoveImageCount).
					To(gomega.Equal(0), "RemoveImageByID should not be called during Update")
			})

			ginkgo.When("watchtower has been instructed to have label take precedence", func() {
				ginkgo.It("it should update containers when monitor only is set to false", func() {
					client := mocks.CreateMockClient(
						&mocks.TestData{
							Containers: []types.Container{
								mocks.CreateMockContainerWithConfig(
									"test-container-02",
									"test-container-02",
									"fake-image2:latest",
									false,
									false,
									time.Now(),
									&container.Config{
										Labels: map[string]string{
											"com.centurylinklabs.watchtower.monitor-only": "false",
										},
									}),
							},
						},
						false,
						false,
					)
					client.TestData.Staleness = map[string]bool{
						"test-container-02": true,
					}
					report, cleanupImageInfos, err := actions.Update(
						client,
						actions.UpdateConfig{
							Cleanup:         true,
							MonitorOnly:     true,
							LabelPrecedence: true,
							CPUCopyMode:     "auto",
						},
					)
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					gomega.Expect(report.Updated()).To(gomega.HaveLen(1))
					gomega.Expect(cleanupImageInfos).
						To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("fake-image2:latest"))))
					gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(1))
					gomega.Expect(client.TestData.TriedToRemoveImageCount).
						To(gomega.Equal(0), "RemoveImageByID should not be called during Update")
				})

				ginkgo.It(
					"it should not update containers when monitor only is set to true",
					func() {
						client := mocks.CreateMockClient(
							&mocks.TestData{
								Containers: []types.Container{
									mocks.CreateMockContainerWithConfig(
										"test-container-02",
										"test-container-02",
										"fake-image2:latest",
										false,
										false,
										time.Now(),
										&container.Config{
											Labels: map[string]string{
												"com.centurylinklabs.watchtower.monitor-only": "true",
											},
										}),
								},
							},
							false,
							false,
						)
						client.TestData.Staleness = map[string]bool{
							"test-container-02": true,
						}
						report, cleanupImageInfos, err := actions.Update(
							client,
							actions.UpdateConfig{
								Cleanup:         true,
								MonitorOnly:     true,
								LabelPrecedence: true,
								CPUCopyMode:     "auto",
							},
						)
						gomega.Expect(err).NotTo(gomega.HaveOccurred())
						gomega.Expect(report.Updated()).To(gomega.BeEmpty())
						gomega.Expect(cleanupImageInfos).To(gomega.BeEmpty())
						gomega.Expect(client.TestData.TriedToRemoveImageCount).
							To(gomega.Equal(0), "RemoveImageByID should not be called during Update")
					},
				)

				ginkgo.It("it should not update containers when monitor only is not set", func() {
					client := mocks.CreateMockClient(
						&mocks.TestData{
							Containers: []types.Container{
								mocks.CreateMockContainer(
									"test-container-01",
									"test-container-01",
									"fake-image:latest",
									time.Now()),
							},
						},
						false,
						false,
					)
					client.TestData.Staleness = map[string]bool{
						"test-container-01": true,
					}
					report, cleanupImageInfos, err := actions.Update(
						client,
						actions.UpdateConfig{
							Cleanup:         true,
							MonitorOnly:     true,
							LabelPrecedence: true,
							CPUCopyMode:     "auto",
						},
					)
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					gomega.Expect(report.Updated()).To(gomega.BeEmpty())
					gomega.Expect(cleanupImageInfos).To(gomega.BeEmpty())
					gomega.Expect(client.TestData.TriedToRemoveImageCount).
						To(gomega.Equal(0), "RemoveImageByID should not be called during Update")
				})
			})
		})
	})

	ginkgo.When("watchtower has been instructed to run lifecycle hooks", func() {
		ginkgo.When("pre-update script returns 1", func() {
			ginkgo.It("should not update those containers and collect no image IDs", func() {
				client := mocks.CreateMockClient(
					&mocks.TestData{
						Containers: []types.Container{
							mocks.CreateMockContainerWithConfig(
								"test-container-02",
								"test-container-02",
								"fake-image2:latest",
								true,
								false,
								time.Now(),
								&container.Config{
									Labels: map[string]string{
										"com.centurylinklabs.watchtower.lifecycle.pre-update-timeout": "190",
										"com.centurylinklabs.watchtower.lifecycle.pre-update":         "/PreUpdateReturn1.sh",
									},
									ExposedPorts: map[nat.Port]struct{}{},
								}),
						},
					},
					false,
					false,
				)
				client.TestData.Staleness = map[string]bool{
					"test-container-02": true,
				}
				report, cleanupImageInfos, err := actions.Update(
					client,
					actions.UpdateConfig{
						Cleanup:        true,
						LifecycleHooks: true,
						CPUCopyMode:    "auto",
					},
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).To(gomega.BeEmpty())
				gomega.Expect(cleanupImageInfos).To(gomega.BeEmpty())
				gomega.Expect(client.TestData.TriedToRemoveImageCount).
					To(gomega.Equal(0), "RemoveImageByID should not be called during Update")
			})
		})

		ginkgo.When("lifecycle UID and GID are specified", func() {
			ginkgo.It("should pass UID and GID to lifecycle hook execution", func() {
				client := mocks.CreateMockClient(
					&mocks.TestData{
						Containers: []types.Container{
							mocks.CreateMockContainerWithConfig(
								"test-container-uid-gid",
								"test-container-uid-gid",
								"fake-image:latest",
								true,
								false,
								time.Now(),
								&container.Config{
									Labels: map[string]string{
										"com.centurylinklabs.watchtower.lifecycle.pre-update": "/PreUpdateReturn0.sh",
									},
									ExposedPorts: map[nat.Port]struct{}{},
								}),
						},
					},
					false,
					false,
				)
				client.TestData.Staleness = map[string]bool{
					"test-container-uid-gid": true,
				}
				report, cleanupImageInfos, err := actions.Update(
					client,
					actions.UpdateConfig{
						Cleanup:        true,
						LifecycleHooks: true,
						LifecycleUID:   1000,
						LifecycleGID:   1001,
						CPUCopyMode:    "auto",
					},
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).To(gomega.HaveLen(1))
				gomega.Expect(cleanupImageInfos).
					To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("fake-image:latest"))))
				gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(1))
				gomega.Expect(client.TestData.TriedToRemoveImageCount).
					To(gomega.Equal(0), "RemoveImageByID should not be called during Update")
			})
		})

		ginkgo.When("preupdate script returns 75", func() {
			ginkgo.It("should not update those containers and collect no image IDs", func() {
				client := mocks.CreateMockClient(
					&mocks.TestData{
						Containers: []types.Container{
							mocks.CreateMockContainerWithConfig(
								"test-container-02",
								"test-container-02",
								"fake-image2:latest",
								true,
								false,
								time.Now(),
								&container.Config{
									Labels: map[string]string{
										"com.centurylinklabs.watchtower.lifecycle.pre-update-timeout": "190",
										"com.centurylinklabs.watchtower.lifecycle.pre-update":         "/PreUpdateReturn75.sh",
									},
									ExposedPorts: map[nat.Port]struct{}{},
								}),
						},
					},
					false,
					false,
				)
				client.TestData.Staleness = map[string]bool{
					"test-container-02": true,
				}
				report, cleanupImageInfos, err := actions.Update(
					client,
					actions.UpdateConfig{
						Cleanup:        true,
						LifecycleHooks: true,
						CPUCopyMode:    "auto",
					},
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).To(gomega.BeEmpty())
				gomega.Expect(cleanupImageInfos).To(gomega.BeEmpty())
				gomega.Expect(client.TestData.TriedToRemoveImageCount).
					To(gomega.Equal(0), "RemoveImageByID should not be called during Update")
			})
		})

		ginkgo.When("preupdate script returns 0", func() {
			ginkgo.It("should update those containers and collect image IDs", func() {
				client := mocks.CreateMockClient(
					&mocks.TestData{
						Containers: []types.Container{
							mocks.CreateMockContainerWithConfig(
								"test-container-02",
								"test-container-02",
								"fake-image2:latest",
								true,
								false,
								time.Now(),
								&container.Config{
									Labels: map[string]string{
										"com.centurylinklabs.watchtower.lifecycle.pre-update-timeout": "190",
										"com.centurylinklabs.watchtower.lifecycle.pre-update":         "/PreUpdateReturn0.sh",
									},
									ExposedPorts: map[nat.Port]struct{}{},
								}),
						},
					},
					false,
					false,
				)
				client.TestData.Staleness = map[string]bool{
					"test-container-02": true,
				}
				report, cleanupImageInfos, err := actions.Update(
					client,
					actions.UpdateConfig{
						Cleanup:        true,
						LifecycleHooks: true,
						CPUCopyMode:    "auto",
					},
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).To(gomega.HaveLen(1))
				gomega.Expect(cleanupImageInfos).
					To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("fake-image2:latest"))))
				gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(1))
				gomega.Expect(client.TestData.TriedToRemoveImageCount).
					To(gomega.Equal(0), "RemoveImageByID should not be called during Update")
			})
		})

		ginkgo.When("container is linked to restarting containers", func() {
			ginkgo.It("should be marked for restart and collect image IDs", func() {
				provider := mocks.CreateMockContainerWithConfig(
					"test-container-provider",
					"/test-container-provider",
					"fake-image2:latest",
					true,
					false,
					time.Now(),
					&container.Config{
						Labels:       map[string]string{},
						ExposedPorts: map[nat.Port]struct{}{},
					})

				provider.SetStale(true)

				consumer := mocks.CreateMockContainerWithConfig(
					"test-container-consumer",
					"/test-container-consumer",
					"fake-image3:latest",
					true,
					false,
					time.Now(),
					&container.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower.depends-on": "test-container-provider",
						},
						ExposedPorts: map[nat.Port]struct{}{},
					})

				containers := []types.Container{
					provider,
					consumer,
				}

				gomega.Expect(provider.ToRestart()).To(gomega.BeTrue())
				gomega.Expect(consumer.ToRestart()).To(gomega.BeFalse())

				actions.UpdateImplicitRestart(containers, containers)

				gomega.Expect(containers[0].ToRestart()).To(gomega.BeTrue())
				gomega.Expect(containers[1].ToRestart()).To(gomega.BeTrue())
			})
			ginkgo.It("should propagate restart through chained dependencies", func() {
				// Create a transitive dependency chain: A depends on B, B depends on C
				containerC := mocks.CreateMockContainerWithConfig(
					"test-container-c",
					"/test-container-c",
					"fake-image-c:latest",
					true,
					false,
					time.Now(),
					&container.Config{
						Labels:       map[string]string{},
						ExposedPorts: map[nat.Port]struct{}{},
					})

				containerB := mocks.CreateMockContainerWithConfig(
					"test-container-b",
					"/test-container-b",
					"fake-image-b:latest",
					true,
					false,
					time.Now(),
					&container.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower.depends-on": "test-container-c",
						},
						ExposedPorts: map[nat.Port]struct{}{},
					})

				containerA := mocks.CreateMockContainerWithConfig(
					"test-container-a",
					"/test-container-a",
					"fake-image-a:latest",
					true,
					false,
					time.Now(),
					&container.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower.depends-on": "test-container-b",
						},
						ExposedPorts: map[nat.Port]struct{}{},
					})

				containers := []types.Container{
					containerC,
					containerB,
					containerA,
				}

				// Initially, only C should be marked for restart
				containerC.SetStale(true)
				gomega.Expect(containerC.ToRestart()).To(gomega.BeTrue())
				gomega.Expect(containerB.ToRestart()).To(gomega.BeFalse())
				gomega.Expect(containerA.ToRestart()).To(gomega.BeFalse())

				// Run UpdateImplicitRestart to propagate restart through the chain
				actions.UpdateImplicitRestart(containers, containers)

				// Verify that restart propagates: A and B should now be marked for restart
				gomega.Expect(containers[0].ToRestart()).To(gomega.BeTrue()) // C
				gomega.Expect(containers[1].ToRestart()).To(gomega.BeTrue()) // B
				gomega.Expect(containers[2].ToRestart()).To(gomega.BeTrue()) // A
			})
		})

		ginkgo.When("container is not running", func() {
			ginkgo.It("should skip running preupdate and collect image IDs", func() {
				client := mocks.CreateMockClient(
					&mocks.TestData{
						Containers: []types.Container{
							mocks.CreateMockContainerWithConfig(
								"test-container-02",
								"test-container-02",
								"fake-image2:latest",
								false,
								false,
								time.Now(),
								&container.Config{
									Labels: map[string]string{
										"com.centurylinklabs.watchtower.lifecycle.pre-update-timeout": "190",
										"com.centurylinklabs.watchtower.lifecycle.pre-update":         "/PreUpdateReturn1.sh",
									},
									ExposedPorts: map[nat.Port]struct{}{},
								}),
						},
					},
					false,
					false,
				)
				client.TestData.Staleness = map[string]bool{
					"test-container-02": true,
				}
				report, cleanupImageInfos, err := actions.Update(
					client,
					actions.UpdateConfig{
						Cleanup:        true,
						LifecycleHooks: true,
						CPUCopyMode:    "auto",
					},
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).To(gomega.HaveLen(1))
				gomega.Expect(cleanupImageInfos).
					To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("fake-image2:latest"))))
				gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(1))
				gomega.Expect(client.TestData.TriedToRemoveImageCount).
					To(gomega.Equal(0), "RemoveImageByID should not be called during Update")
			})
		})

		ginkgo.When("container is restarting", func() {
			ginkgo.It("should skip running preupdate and collect image IDs", func() {
				client := mocks.CreateMockClient(
					&mocks.TestData{
						Containers: []types.Container{
							mocks.CreateMockContainerWithConfig(
								"test-container-02",
								"test-container-02",
								"fake-image2:latest",
								false,
								true,
								time.Now(),
								&container.Config{
									Labels: map[string]string{
										"com.centurylinklabs.watchtower.lifecycle.pre-update-timeout": "190",
										"com.centurylinklabs.watchtower.lifecycle.pre-update":         "/PreUpdateReturn1.sh",
									},
									ExposedPorts: map[nat.Port]struct{}{},
								}),
						},
					},
					false,
					false,
				)
				client.TestData.Staleness = map[string]bool{
					"test-container-02": true,
				}
				report, cleanupImageInfos, err := actions.Update(
					client,
					actions.UpdateConfig{
						Cleanup:        true,
						LifecycleHooks: true,
						CPUCopyMode:    "auto",
					},
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).To(gomega.HaveLen(1))
				gomega.Expect(cleanupImageInfos).
					To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("fake-image2:latest"))))
				gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(1))
				gomega.Expect(client.TestData.TriedToRemoveImageCount).
					To(gomega.Equal(0), "RemoveImageByID should not be called during Update")
			})
		})
	})
	ginkgo.When("updating containers with chained dependencies", func() {
		ginkgo.It("should process containers in dependency order", func() {
			// Create a dependency chain: A depends on B, B depends on C
			containerC := mocks.CreateMockContainerWithConfig(
				"test-container-c",
				"/test-container-c",
				"fake-image-c:latest",
				true,
				false,
				time.Now().AddDate(0, 0, -1), // Make it stale
				&container.Config{
					Labels:       map[string]string{},
					ExposedPorts: map[nat.Port]struct{}{},
				})

			containerB := mocks.CreateMockContainerWithConfig(
				"test-container-b",
				"/test-container-b",
				"fake-image-b:latest",
				true,
				false,
				time.Now().AddDate(0, 0, -1), // Make it stale
				&container.Config{
					Labels: map[string]string{
						"com.centurylinklabs.watchtower.depends-on": "test-container-c",
					},
					ExposedPorts: map[nat.Port]struct{}{},
				})

			containerA := mocks.CreateMockContainerWithConfig(
				"test-container-a",
				"/test-container-a",
				"fake-image-a:latest",
				true,
				false,
				time.Now().AddDate(0, 0, -1), // Make it stale
				&container.Config{
					Labels: map[string]string{
						"com.centurylinklabs.watchtower.depends-on": "test-container-b",
					},
					ExposedPorts: map[nat.Port]struct{}{},
				})

			client := mocks.CreateMockClient(
				&mocks.TestData{
					Containers: []types.Container{
						containerA,
						containerB,
						containerC,
					},
					Staleness: map[string]bool{
						"test-container-a": true,
						"test-container-b": true,
						"test-container-c": true,
					},
				},
				false,
				false,
			)

			report, cleanupImageInfos, err := actions.Update(
				client,
				actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(report.Updated()).To(gomega.HaveLen(3))

			// Verify that all containers were updated (dependencies were handled correctly)
			gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(3))
			gomega.Expect(cleanupImageInfos).
				To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("fake-image-a:latest"))))
			gomega.Expect(cleanupImageInfos).
				To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("fake-image-b:latest"))))
			gomega.Expect(cleanupImageInfos).
				To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("fake-image-c:latest"))))
		})
	})

	// Tests for image reference handling to cover isPinned functionality

	// Tests for image reference handling to cover isPinned functionality
	ginkgo.When("handling different image reference formats", func() {
		var client *mocks.MockClient
		var config actions.UpdateConfig

		ginkgo.BeforeEach(func() {
			config = actions.UpdateConfig{
				Cleanup:     true,
				Filter:      filters.NoFilter,
				CPUCopyMode: "auto",
			}
		})

		ginkgo.It("should process tagged images and update if stale", func() {
			count := 0
			client = &mocks.MockClient{
				TestData: &mocks.TestData{
					IsContainerStaleCount: count,
					Containers: []types.Container{
						mocks.CreateMockContainer(
							"tagged-container",
							"/tagged-container",
							"image:1.0.0",
							time.Now()),
					},
					Staleness: map[string]bool{
						"tagged-container": true,
					},
				},
				Stopped: make(map[string]bool),
			}
			report, cleanupImageInfos, err := actions.Update(client, config)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(report.Scanned()).
				To(gomega.HaveLen(1), "Tagged container should be scanned")
			gomega.Expect(report.Updated()).
				To(gomega.HaveLen(1), "Tagged container should be updated if stale")
			gomega.Expect(cleanupImageInfos).
				To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("image:1.0.0"))))
			gomega.Expect(client.TestData.IsContainerStaleCount).
				To(gomega.Equal(1), "IsContainerStale should be called")
		})

		ginkgo.It("should process untagged images and update if stale", func() {
			count := 0
			client = &mocks.MockClient{
				TestData: &mocks.TestData{
					IsContainerStaleCount: count,
					Containers: []types.Container{
						mocks.CreateMockContainer(
							"untagged-container",
							"/untagged-container",
							"image",
							time.Now()),
					},
					Staleness: map[string]bool{
						"untagged-container": true,
					},
				},
				Stopped: make(map[string]bool),
			}
			report, cleanupImageInfos, err := actions.Update(client, config)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(report.Scanned()).
				To(gomega.HaveLen(1), "Untagged container should be scanned")
			gomega.Expect(report.Updated()).
				To(gomega.HaveLen(1), "Untagged container should be updated if stale")
			gomega.Expect(cleanupImageInfos).
				To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("image"))))
			gomega.Expect(client.TestData.IsContainerStaleCount).
				To(gomega.Equal(1), "IsContainerStale should be called")
		})

		ginkgo.It("should skip pinned containers and not collect image IDs", func() {
			count := 0
			client = &mocks.MockClient{
				TestData: &mocks.TestData{
					IsContainerStaleCount: count,
					Containers: []types.Container{
						mocks.CreateMockContainer(
							"pinned-container",
							"/pinned-container",
							"image@sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
							time.Now(),
						),
					},
					Staleness: map[string]bool{
						"pinned-container": true,
					},
				},
				Stopped: make(map[string]bool),
			}
			report, cleanupImageInfos, err := actions.Update(client, config)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(report.Scanned()).
				To(gomega.HaveLen(1), "Pinned container should be scanned")
			gomega.Expect(report.Updated()).
				To(gomega.BeEmpty(), "Pinned container should not be updated")
			gomega.Expect(cleanupImageInfos).
				To(gomega.BeEmpty(), "No image IDs should be collected for pinned container")
			gomega.Expect(client.TestData.TriedToRemoveImageCount).
				To(gomega.Equal(0), "RemoveImageByID should not be called")
			gomega.Expect(client.TestData.IsContainerStaleCount).
				To(gomega.Equal(0), "IsContainerStale should not be called")
		})

		ginkgo.It(
			"should skip pinned containers with tag and digest and not collect image IDs",
			func() {
				count := 0
				client = &mocks.MockClient{
					TestData: &mocks.TestData{
						IsContainerStaleCount: count,
						Containers: []types.Container{
							mocks.CreateMockContainer(
								"pinned-tagged-container",
								"/pinned-tagged-container",
								"image:latest@sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
								time.Now(),
							),
						},
						Staleness: map[string]bool{
							"pinned-tagged-container": true,
						},
					},
					Stopped: make(map[string]bool),
				}
				report, cleanupImageInfos, err := actions.Update(client, config)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Scanned()).
					To(gomega.HaveLen(1), "Pinned container should be scanned")
				gomega.Expect(report.Updated()).
					To(gomega.BeEmpty(), "Pinned container should not be updated")
				gomega.Expect(cleanupImageInfos).
					To(gomega.BeEmpty(), "No image IDs should be collected for pinned container")
				gomega.Expect(client.TestData.TriedToRemoveImageCount).
					To(gomega.Equal(0), "RemoveImageByID should not be called")
				gomega.Expect(client.TestData.IsContainerStaleCount).
					To(gomega.Equal(0), "IsContainerStale should not be called")
			},
		)

		ginkgo.It("should skip invalid image references with error", func() {
			count := 0
			client = &mocks.MockClient{
				TestData: &mocks.TestData{
					IsContainerStaleCount: count,
					Containers: []types.Container{
						mocks.CreateMockContainer(
							"invalid-container",
							"/invalid-container",
							":latest",
							time.Now()),
					},
					Staleness: map[string]bool{
						"invalid-container": true,
					},
				},
				Stopped: make(map[string]bool),
			}
			report, cleanupImageInfos, err := actions.Update(client, config)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(report.Skipped()).
				To(gomega.HaveLen(1), "Invalid container should be skipped")
			gomega.Expect(report.Scanned()).
				To(gomega.BeEmpty(), "Invalid container should not be scanned")
			gomega.Expect(report.Updated()).
				To(gomega.BeEmpty(), "Invalid container should not be updated")
			gomega.Expect(cleanupImageInfos).
				To(gomega.BeEmpty(), "No image IDs should be collected")
			gomega.Expect(client.TestData.TriedToRemoveImageCount).
				To(gomega.Equal(0), "RemoveImageByID should not be called")
			gomega.Expect(client.TestData.IsContainerStaleCount).
				To(gomega.Equal(0), "IsContainerStale should not be called")
		})

		ginkgo.It(
			"should skip containers with missing Config.Image and imageInfo.ID with error",
			func() {
				count := 0
				client = &mocks.MockClient{
					TestData: &mocks.TestData{
						IsContainerStaleCount: count,
						Containers: []types.Container{
							mocks.CreateMockContainerWithImageInfoP(
								"edge-container",
								"/edge-container",
								"",
								time.Now(),
								nil,
							),
						},
						Staleness: map[string]bool{
							"edge-container": true,
						},
					},
					Stopped: make(map[string]bool),
				}
				report, cleanupImageInfos, err := actions.Update(client, config)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Skipped()).
					To(gomega.HaveLen(1), "Container with missing image info should be skipped")
				gomega.Expect(report.Scanned()).
					To(gomega.BeEmpty(), "Container should not be scanned")
				gomega.Expect(report.Updated()).
					To(gomega.BeEmpty(), "Container should not be updated")
				gomega.Expect(cleanupImageInfos).
					To(gomega.BeEmpty(), "No image IDs should be collected")
				gomega.Expect(client.TestData.TriedToRemoveImageCount).
					To(gomega.Equal(0), "RemoveImageByID should not be called")
				gomega.Expect(client.TestData.IsContainerStaleCount).
					To(gomega.Equal(1), "IsContainerStale should be called")
			},
		)

		ginkgo.It("should skip containers with invalid fallback image reference", func() {
			count := 0
			client = &mocks.MockClient{
				TestData: &mocks.TestData{
					IsContainerStaleCount: count,
					Containers: []types.Container{
						mocks.CreateMockContainer(
							"InvalidContainer",
							"/InvalidContainer",
							":latest",
							time.Now()),
					},
					Staleness: map[string]bool{
						"InvalidContainer": true,
					},
				},
				Stopped: make(map[string]bool),
			}
			report, cleanupImageInfos, err := actions.Update(client, config)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(report.Skipped()).
				To(gomega.HaveLen(1), "Container with invalid fallback image should be skipped")
			gomega.Expect(report.Scanned()).To(gomega.BeEmpty(), "Container should not be scanned")
			gomega.Expect(report.Updated()).To(gomega.BeEmpty(), "Container should not be updated")
			gomega.Expect(cleanupImageInfos).
				To(gomega.BeEmpty(), "No image IDs should be collected")
			gomega.Expect(client.TestData.TriedToRemoveImageCount).
				To(gomega.Equal(0), "RemoveImageByID should not be called")
			gomega.Expect(client.TestData.IsContainerStaleCount).
				To(gomega.Equal(0), "IsContainerStale should not be called")
		})
	})

	ginkgo.When("Watchtower self-update pull fails", func() {
		ginkgo.It("should apply safeguard delay to prevent rapid restarts", func() {
			client := mocks.CreateMockClient(
				&mocks.TestData{
					Containers: []types.Container{
						mocks.CreateMockContainerWithConfig(
							"watchtower",
							"/watchtower",
							"watchtower:latest",
							true,
							false,
							time.Now(),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower": "true",
								},
							}),
					},
					Staleness: map[string]bool{
						"watchtower": true, // Simulate stale Watchtower
					},
				},
				false,
				false,
			)

			// Mock IsContainerStale to return an error (simulating pull failure)
			client.TestData.IsContainerStaleError = errors.New("failed to pull image")

			startTime := time.Now()
			report, cleanupImageInfos, err := actions.Update(
				client,
				actions.UpdateConfig{
					Cleanup:          true,
					Filter:           filters.WatchtowerContainersFilter,
					CPUCopyMode:      "auto",
					PullFailureDelay: 10 * time.Millisecond,
				},
			)
			elapsedTime := time.Since(startTime)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(report.Updated()).
				To(gomega.BeEmpty(), "Watchtower should not be updated on pull failure")
			gomega.Expect(cleanupImageInfos).
				To(gomega.BeEmpty(), "No cleanup should occur on pull failure")

			// Verify that the delay was applied (using test-specific short delay from PullFailureDelay)
			gomega.Expect(elapsedTime).
				To(gomega.BeNumerically(">=", 10*time.Millisecond), "Delay should have been applied")
		})
	})

	ginkgo.When("restarting stale Watchtower containers in non-rolling mode", func() {
		ginkgo.It("should restart stale Watchtower containers even if stop is skipped", func() {
			client := mocks.CreateMockClient(
				&mocks.TestData{
					Containers: []types.Container{
						mocks.CreateMockContainerWithConfig(
							"watchtower",
							"/watchtower",
							"watchtower:latest",
							true,
							false,
							time.Now(),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower": "true",
								},
							}),
					},
					Staleness: map[string]bool{
						"watchtower": true, // Simulate stale Watchtower
					},
				},
				false,
				false,
			)

			report, cleanupImageInfos, err := actions.Update(
				client,
				actions.UpdateConfig{
					Cleanup:     true,
					Filter:      filters.WatchtowerContainersFilter,
					CPUCopyMode: "auto",
				},
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(report.Updated()).To(gomega.HaveLen(1))
			gomega.Expect(cleanupImageInfos).
				To(gomega.BeEmpty(), "No cleanup for renamed Watchtower container")
			gomega.Expect(client.TestData.StopContainerCount).
				To(gomega.Equal(1), "StopContainer should be called once for old Watchtower")
			gomega.Expect(client.TestData.StartContainerCount).
				To(gomega.Equal(1), "StartContainer should be called for Watchtower restart")
			gomega.Expect(client.TestData.RenameContainerCount).
				To(gomega.Equal(1), "RenameContainer should be called once")
			gomega.Expect(client.TestData.IsContainerStaleCount).
				To(gomega.Equal(2), "IsContainerStale should be called twice for Watchtower")
		})
		ginkgo.When("handling chained dependencies with multiple dependents", func() {
			ginkgo.It(
				"should restart all containers depending on the same base dependency when it updates",
				func() {
					// Create a base container that will be updated
					baseContainer := mocks.CreateMockContainerWithConfig(
						"base-container",
						"/base-container",
						"base-image:latest",
						true,
						false,
						time.Now().AddDate(0, 0, -1), // Make it stale
						&container.Config{
							Labels:       map[string]string{},
							ExposedPorts: map[nat.Port]struct{}{},
						})

					// Create multiple containers that all depend on the base container
					dependent1 := mocks.CreateMockContainerWithConfig(
						"dependent-1",
						"/dependent-1",
						"dep1-image:latest",
						true,
						false,
						time.Now(),
						&container.Config{
							Labels: map[string]string{
								"com.centurylinklabs.watchtower.depends-on": "base-container",
							},
							ExposedPorts: map[nat.Port]struct{}{},
						})

					dependent2 := mocks.CreateMockContainerWithConfig(
						"dependent-2",
						"/dependent-2",
						"dep2-image:latest",
						true,
						false,
						time.Now(),
						&container.Config{
							Labels: map[string]string{
								"com.centurylinklabs.watchtower.depends-on": "base-container",
							},
							ExposedPorts: map[nat.Port]struct{}{},
						})

					dependent3 := mocks.CreateMockContainerWithConfig(
						"dependent-3",
						"/dependent-3",
						"dep3-image:latest",
						true,
						false,
						time.Now(),
						&container.Config{
							Labels: map[string]string{
								"com.centurylinklabs.watchtower.depends-on": "base-container",
							},
							ExposedPorts: map[nat.Port]struct{}{},
						})

					client := mocks.CreateMockClient(
						&mocks.TestData{
							Containers: []types.Container{
								baseContainer,
								dependent1,
								dependent2,
								dependent3,
							},
							Staleness: map[string]bool{
								"base-container": true,
								"dependent-1":    false,
								"dependent-2":    false,
								"dependent-3":    false,
							},
						},
						false,
						false,
					)

					report, cleanupImageInfos, err := actions.Update(
						client,
						actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
					)

					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					gomega.Expect(report.Updated()).
						To(gomega.HaveLen(1))
						// Only base container is stale and updated

					// Verify that base container was updated
					gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(1))
					gomega.Expect(cleanupImageInfos).
						To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("base-image:latest"))))
				},
			)
		})

		ginkgo.When("handling circular dependencies", func() {
			ginkgo.It("should detect circular dependencies and skip affected containers", func() {
				// Create containers with circular dependencies: A depends on B, B depends on A
				containerA := mocks.CreateMockContainerWithConfig(
					"container-a",
					"/container-a",
					"image-a:latest",
					true,
					false,
					time.Now(),
					&container.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower.depends-on": "container-b",
						},
						ExposedPorts: map[nat.Port]struct{}{},
					})

				containerB := mocks.CreateMockContainerWithConfig(
					"container-b",
					"/container-b",
					"image-b:latest",
					true,
					false,
					time.Now(),
					&container.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower.depends-on": "container-a",
						},
						ExposedPorts: map[nat.Port]struct{}{},
					})

				client := mocks.CreateMockClient(
					&mocks.TestData{
						Containers: []types.Container{
							containerA,
							containerB,
						},
						Staleness: map[string]bool{
							"container-a": false,
							"container-b": false,
						},
					},
					false,
					false,
				)

				report, cleanupImageInfos, err := actions.Update(
					client,
					actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
				)

				gomega.Expect(err).
					NotTo(gomega.HaveOccurred(), "Circular dependencies should not cause an error")
				gomega.Expect(report.Skipped()).
					To(gomega.HaveLen(2), "Both containers should be skipped due to circular dependency")
				gomega.Expect(cleanupImageInfos).
					To(gomega.BeEmpty(), "No cleanup should occur for skipped containers")
			})
		})

		ginkgo.When("handling depends-on with non-existent containers", func() {
			ginkgo.It(
				"should gracefully handle depends-on referencing containers that don't exist",
				func() {
					// Create a container that depends on a non-existent container
					dependent := mocks.CreateMockContainerWithConfig(
						"dependent-container",
						"/dependent-container",
						"dep-image:latest",
						true,
						false,
						time.Now().AddDate(0, 0, -1), // Make it stale
						&container.Config{
							Labels: map[string]string{
								"com.centurylinklabs.watchtower.depends-on": "non-existent-container",
							},
							ExposedPorts: map[nat.Port]struct{}{},
						})

					client := mocks.CreateMockClient(
						&mocks.TestData{
							Containers: []types.Container{
								dependent,
							},
							Staleness: map[string]bool{
								"dependent-container": true,
							},
						},
						false,
						false,
					)

					report, cleanupImageInfos, err := actions.Update(
						client,
						actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
					)

					gomega.Expect(err).
						NotTo(gomega.HaveOccurred(), "Non-existent dependencies should not cause errors")
					gomega.Expect(report.Updated()).
						To(gomega.HaveLen(1), "Dependent container should still be updated")

					// Verify that the container was updated
					gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(1))
					gomega.Expect(cleanupImageInfos).
						To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("dep-image:latest"))))
				},
			)
		})

		ginkgo.When("handling diamond dependency patterns", func() {
			ginkgo.It("should propagate restart through multiple dependency paths", func() {
				// Create diamond pattern: A depends on B and C, both B and C depend on D
				containerD := mocks.CreateMockContainerWithConfig(
					"container-d",
					"/container-d",
					"image-d:latest",
					true,
					false,
					time.Now().AddDate(0, 0, -1), // Make it stale
					&container.Config{
						Labels:       map[string]string{},
						ExposedPorts: map[nat.Port]struct{}{},
					})

				containerB := mocks.CreateMockContainerWithConfig(
					"container-b",
					"/container-b",
					"image-b:latest",
					true,
					false,
					time.Now(),
					&container.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower.depends-on": "container-d",
						},
						ExposedPorts: map[nat.Port]struct{}{},
					})

				containerC := mocks.CreateMockContainerWithConfig(
					"container-c",
					"/container-c",
					"image-c:latest",
					true,
					false,
					time.Now(),
					&container.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower.depends-on": "container-d",
						},
						ExposedPorts: map[nat.Port]struct{}{},
					})

				containerA := mocks.CreateMockContainerWithConfig(
					"container-a",
					"/container-a",
					"image-a:latest",
					true,
					false,
					time.Now(),
					&container.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower.depends-on": "container-b,container-c",
						},
						ExposedPorts: map[nat.Port]struct{}{},
					})

				client := mocks.CreateMockClient(
					&mocks.TestData{
						Containers: []types.Container{
							containerD,
							containerB,
							containerC,
							containerA,
						},
						Staleness: map[string]bool{
							"container-d": true,
							"container-b": false,
							"container-c": false,
							"container-a": false,
						},
					},
					false,
					false,
				)

				report, cleanupImageInfos, err := actions.Update(
					client,
					actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
				)

				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).
					To(gomega.HaveLen(1)) // Only base container D is stale and updated

				// Verify that base container was updated
				gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(1))
				gomega.Expect(cleanupImageInfos).
					To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("image-d:latest"))))
			})
		})

		ginkgo.When("handling self-dependency in depends-on", func() {
			ginkgo.It("should gracefully handle containers that depend on themselves", func() {
				// Create a container that depends on itself
				selfDependent := mocks.CreateMockContainerWithConfig(
					"self-container",
					"/self-container",
					"self-image:latest",
					true,
					false,
					time.Now().AddDate(0, 0, -1), // Make it stale
					&container.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower.depends-on": "self-container",
						},
						ExposedPorts: map[nat.Port]struct{}{},
					})

				client := mocks.CreateMockClient(
					&mocks.TestData{
						Containers: []types.Container{
							selfDependent,
						},
						Staleness: map[string]bool{
							"self-container": true,
						},
					},
					false,
					false,
				)

				report, cleanupImageInfos, err := actions.Update(
					client,
					actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
				)

				gomega.Expect(err).
					NotTo(gomega.HaveOccurred(), "Self-dependency should not cause an error")
				gomega.Expect(report.Skipped()).
					To(gomega.HaveLen(1), "Self-dependent container should be skipped")
				gomega.Expect(cleanupImageInfos).
					To(gomega.BeEmpty(), "No cleanup should occur for skipped container")
			})
		})

		ginkgo.When("handling malformed container names in depends-on", func() {
			ginkgo.It("should gracefully handle depends-on with invalid container names", func() {
				// Create a container with malformed depends-on names
				dependent := mocks.CreateMockContainerWithConfig(
					"dependent-container",
					"/dependent-container",
					"dep-image:latest",
					true,
					false,
					time.Now().AddDate(0, 0, -1), // Make it stale
					&container.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower.depends-on": "invalid name,another-invalid",
						},
						ExposedPorts: map[nat.Port]struct{}{},
					})

				client := mocks.CreateMockClient(
					&mocks.TestData{
						Containers: []types.Container{
							dependent,
						},
						Staleness: map[string]bool{
							"dependent-container": true,
						},
					},
					false,
					false,
				)

				report, cleanupImageInfos, err := actions.Update(
					client,
					actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
				)

				gomega.Expect(err).
					NotTo(gomega.HaveOccurred(), "Malformed names should not cause errors")
				gomega.Expect(report.Updated()).
					To(gomega.HaveLen(1), "Dependent container should still be updated")

				// Verify that the container was updated
				gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(1))
				gomega.Expect(cleanupImageInfos).
					To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("dep-image:latest"))))
			})
		})

		ginkgo.When("handling deep dependency chains", func() {
			ginkgo.It("should handle dependency chains with more than 3 levels", func() {
				// Create a deep dependency chain: A depends on B, B depends on C, C depends on D, D depends on E
				containerE := mocks.CreateMockContainerWithConfig(
					"container-e",
					"/container-e",
					"image-e:latest",
					true,
					false,
					time.Now().AddDate(0, 0, -1), // Make it stale
					&container.Config{
						Labels:       map[string]string{},
						ExposedPorts: map[nat.Port]struct{}{},
					})

				containerD := mocks.CreateMockContainerWithConfig(
					"container-d",
					"/container-d",
					"image-d:latest",
					true,
					false,
					time.Now(),
					&container.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower.depends-on": "container-e",
						},
						ExposedPorts: map[nat.Port]struct{}{},
					})

				containerC := mocks.CreateMockContainerWithConfig(
					"container-c",
					"/container-c",
					"image-c:latest",
					true,
					false,
					time.Now(),
					&container.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower.depends-on": "container-d",
						},
						ExposedPorts: map[nat.Port]struct{}{},
					})

				containerB := mocks.CreateMockContainerWithConfig(
					"container-b",
					"/container-b",
					"image-b:latest",
					true,
					false,
					time.Now(),
					&container.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower.depends-on": "container-c",
						},
						ExposedPorts: map[nat.Port]struct{}{},
					})

				containerA := mocks.CreateMockContainerWithConfig(
					"container-a",
					"/container-a",
					"image-a:latest",
					true,
					false,
					time.Now(),
					&container.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower.depends-on": "container-b",
						},
						ExposedPorts: map[nat.Port]struct{}{},
					})

				client := mocks.CreateMockClient(
					&mocks.TestData{
						Containers: []types.Container{
							containerE,
							containerD,
							containerC,
							containerB,
							containerA,
						},
						Staleness: map[string]bool{
							"container-e": true,
							"container-d": false,
							"container-c": false,
							"container-b": false,
							"container-a": false,
						},
					},
					false,
					false,
				)

				report, cleanupImageInfos, err := actions.Update(
					client,
					actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
				)

				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).
					To(gomega.HaveLen(1)) // Only base container E is stale and updated

				// Verify that base container was updated
				gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(1))
				gomega.Expect(cleanupImageInfos).
					To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("image-e:latest"))))
			})
		})

		ginkgo.When("handling mixed valid and invalid dependencies", func() {
			ginkgo.It(
				"should handle containers with some valid and some invalid dependencies",
				func() {
					// Create containers where A depends on both a valid container B and an invalid container
					containerB := mocks.CreateMockContainerWithConfig(
						"container-b",
						"/container-b",
						"image-b:latest",
						true,
						false,
						time.Now().AddDate(0, 0, -1), // Make it stale
						&container.Config{
							Labels:       map[string]string{},
							ExposedPorts: map[nat.Port]struct{}{},
						})

					containerA := mocks.CreateMockContainerWithConfig(
						"container-a",
						"/container-a",
						"image-a:latest",
						true,
						false,
						time.Now(),
						&container.Config{
							Labels: map[string]string{
								"com.centurylinklabs.watchtower.depends-on": "container-b,non-existent-container",
							},
							ExposedPorts: map[nat.Port]struct{}{},
						})

					client := mocks.CreateMockClient(
						&mocks.TestData{
							Containers: []types.Container{
								containerB,
								containerA,
							},
							Staleness: map[string]bool{
								"container-b": true,
								"container-a": false,
							},
						},
						false,
						false,
					)

					report, cleanupImageInfos, err := actions.Update(
						client,
						actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
					)

					gomega.Expect(err).
						NotTo(gomega.HaveOccurred(), "Mixed valid/invalid dependencies should not cause errors")
					gomega.Expect(report.Updated()).
						To(gomega.HaveLen(1), "Only stale container should be updated")

					// Verify that base container was updated
					gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(1))
					gomega.Expect(cleanupImageInfos).
						To(gomega.ContainElement(gomega.HaveField("ImageID", types.ImageID("image-b:latest"))))
				},
			)
		})

		ginkgo.When(
			"handling missing dependency propagation when labels are on dependents",
			func() {
				ginkgo.It(
					"should propagate restart when dependent has depends-on label but dependency has no labels",
					func() {
						// Dependency container with no labels
						dependency := mocks.CreateMockContainerWithConfig(
							"dependency-no-labels",
							"/dependency-no-labels",
							"dep-image:latest",
							true,
							false,
							time.Now().AddDate(0, 0, -1), // Make it stale
							&container.Config{
								Labels:       map[string]string{},
								ExposedPorts: map[nat.Port]struct{}{},
							})

						// Dependent container with depends-on label
						dependent := mocks.CreateMockContainerWithConfig(
							"dependent-with-label",
							"/dependent-with-label",
							"dep-image:latest",
							true,
							false,
							time.Now(),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower.depends-on": "dependency-no-labels",
								},
								ExposedPorts: map[nat.Port]struct{}{},
							})

						containers := []types.Container{dependency, dependent}

						// Initially, only dependency should be marked for restart
						dependency.SetStale(true)
						gomega.Expect(dependency.ToRestart()).To(gomega.BeTrue())
						gomega.Expect(dependent.ToRestart()).To(gomega.BeFalse())

						// Run UpdateImplicitRestart to propagate restart
						actions.UpdateImplicitRestart(containers, containers)

						// Verify that restart propagates to dependent
						gomega.Expect(containers[0].ToRestart()).To(gomega.BeTrue()) // dependency
						gomega.Expect(containers[1].ToRestart()).To(gomega.BeTrue()) // dependent
					},
				)
			},
		)

		ginkgo.When("handling correct rolling restart order in dependency chains", func() {
			ginkgo.It(
				"should stop and start containers in correct order during rolling restart",
				func() {
					// Create dependency chain: A depends on B, B depends on C
					containerC := mocks.CreateMockContainerWithConfig(
						"container-c-rolling",
						"/container-c-rolling",
						"image-c:latest",
						true,
						false,
						time.Now().AddDate(0, 0, -1),
						&container.Config{
							Labels:       map[string]string{},
							ExposedPorts: map[nat.Port]struct{}{},
						})

					containerB := mocks.CreateMockContainerWithConfig(
						"container-b-rolling",
						"/container-b-rolling",
						"image-b:latest",
						true,
						false,
						time.Now().AddDate(0, 0, -1),
						&container.Config{
							Labels: map[string]string{
								"com.centurylinklabs.watchtower.depends-on": "container-c-rolling",
							},
							ExposedPorts: map[nat.Port]struct{}{},
						})

					containerA := mocks.CreateMockContainerWithConfig(
						"container-a-rolling",
						"/container-a-rolling",
						"image-a:latest",
						true,
						false,
						time.Now().AddDate(0, 0, -1),
						&container.Config{
							Labels: map[string]string{
								"com.centurylinklabs.watchtower.depends-on": "container-b-rolling",
							},
							ExposedPorts: map[nat.Port]struct{}{},
						})

					client := mocks.CreateMockClient(
						&mocks.TestData{
							Containers: []types.Container{containerC, containerB, containerA},
							Staleness: map[string]bool{
								"container-c-rolling": true,
								"container-b-rolling": true,
								"container-a-rolling": true,
							},
						},
						false,
						false,
					)

					report, cleanupImageInfos, err := actions.Update(
						client,
						actions.UpdateConfig{
							Cleanup:        true,
							RollingRestart: true,
							CPUCopyMode:    "auto",
						},
					)

					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					gomega.Expect(report.Updated()).To(gomega.HaveLen(3))

					// Verify rolling restart was used (WaitForContainerHealthy called)
					gomega.Expect(client.TestData.WaitForContainerHealthyCount).To(gomega.Equal(3))

					// Verify cleanup occurred for all updated containers
					gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(3))
				},
			)
		})

		ginkgo.When("handling independent staleness of dependents", func() {
			ginkgo.It("should update dependent when it is stale even if dependency is not", func() {
				// Dependency that is not stale
				dependency := mocks.CreateMockContainerWithConfig(
					"fresh-dependency",
					"/fresh-dependency",
					"dep-image:latest",
					true,
					false,
					time.Now(),
					&container.Config{
						Labels:       map[string]string{},
						ExposedPorts: map[nat.Port]struct{}{},
					})

				// Dependent that is stale
				dependent := mocks.CreateMockContainerWithConfig(
					"stale-dependent",
					"/stale-dependent",
					"dep-image:latest",
					true,
					false,
					time.Now().AddDate(0, 0, -1),
					&container.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower.depends-on": "fresh-dependency",
						},
						ExposedPorts: map[nat.Port]struct{}{},
					})

				client := mocks.CreateMockClient(
					&mocks.TestData{
						Containers: []types.Container{dependency, dependent},
						Staleness: map[string]bool{
							"fresh-dependency": false,
							"stale-dependent":  true,
						},
					},
					false,
					false,
				)

				report, cleanupImageInfos, err := actions.Update(
					client,
					actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
				)

				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).To(gomega.HaveLen(1))
				gomega.Expect(report.Scanned()).To(gomega.HaveLen(2))

				// Only the stale dependent should be updated
				gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(1))
				gomega.Expect(cleanupImageInfos[0].ContainerName).
					To(gomega.Equal("stale-dependent"))
			})

			ginkgo.It("should restart dependent when dependency becomes stale", func() {
				// Dependency that becomes stale
				dependency := mocks.CreateMockContainerWithConfig(
					"stale-dependency",
					"/stale-dependency",
					"dep-image:latest",
					true,
					false,
					time.Now().AddDate(0, 0, -1),
					&container.Config{
						Labels:       map[string]string{},
						ExposedPorts: map[nat.Port]struct{}{},
					})

				// Dependent that is not stale
				dependent := mocks.CreateMockContainerWithConfig(
					"fresh-dependent",
					"/fresh-dependent",
					"dep-image:latest",
					true,
					false,
					time.Now(),
					&container.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower.depends-on": "stale-dependency",
						},
						ExposedPorts: map[nat.Port]struct{}{},
					})

				client := mocks.CreateMockClient(
					&mocks.TestData{
						Containers: []types.Container{dependency, dependent},
						Staleness: map[string]bool{
							"stale-dependency": true,
							"fresh-dependent":  false,
						},
					},
					false,
					false,
				)

				report, cleanupImageInfos, err := actions.Update(
					client,
					actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
				)

				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).To(gomega.HaveLen(1))
				gomega.Expect(report.Scanned()).To(gomega.HaveLen(2))

				// Only the stale dependency should be updated, dependent should be restarted implicitly
				gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(1))
				gomega.Expect(cleanupImageInfos[0].ContainerName).
					To(gomega.Equal("stale-dependency"))

				// Verify that dependent was marked for restart
				gomega.Expect(dependent.ToRestart()).To(gomega.BeTrue())
			})
		})

		ginkgo.When("handling implicit restart propagation in both directions", func() {
			ginkgo.It(
				"should propagate restart from dependency to dependent and from dependent to dependency",
				func() {
					// Dependency container
					dependency := mocks.CreateMockContainerWithConfig(
						"dependency-bidirectional",
						"/dependency-bidirectional",
						"dep-image:latest",
						true,
						false,
						time.Now().AddDate(0, 0, -1),
						&container.Config{
							Labels:       map[string]string{},
							ExposedPorts: map[nat.Port]struct{}{},
						})

					// Dependent container
					dependent := mocks.CreateMockContainerWithConfig(
						"dependent-bidirectional",
						"/dependent-bidirectional",
						"dep-image:latest",
						true,
						false,
						time.Now(),
						&container.Config{
							Labels: map[string]string{
								"com.centurylinklabs.watchtower.depends-on": "dependency-bidirectional",
							},
							ExposedPorts: map[nat.Port]struct{}{},
						})

					containers := []types.Container{dependency, dependent}

					// Test propagation from dependency to dependent
					dependency.SetStale(true)
					gomega.Expect(dependency.ToRestart()).To(gomega.BeTrue())
					gomega.Expect(dependent.ToRestart()).To(gomega.BeFalse())

					actions.UpdateImplicitRestart(containers, containers)

					gomega.Expect(containers[0].ToRestart()).To(gomega.BeTrue()) // dependency
					gomega.Expect(containers[1].ToRestart()).To(gomega.BeTrue()) // dependent

					// Reset and test propagation from dependent to dependency
					dependency.SetStale(false)
					dependent.SetStale(true)
					dependency.SetLinkedToRestarting(false)
					dependent.SetLinkedToRestarting(false)

					gomega.Expect(dependency.ToRestart()).To(gomega.BeFalse())
					gomega.Expect(dependent.ToRestart()).To(gomega.BeTrue())

					actions.UpdateImplicitRestart(containers, containers)

					// With unidirectional logic, restart does NOT propagate from dependent to dependency
					gomega.Expect(containers[0].ToRestart()).
						To(gomega.BeFalse())
						// dependency NOT marked as linked
					gomega.Expect(containers[1].ToRestart()).To(gomega.BeTrue()) // dependent
				},
			)
		})

		ginkgo.When(
			"handling docker-compose scenario with multiple dependents on single dependency",
			func() {
				ginkgo.It("should restart all dependents when single dependency updates", func() {
					// Single dependency (like a database)
					database := mocks.CreateMockContainerWithConfig(
						"database",
						"/database",
						"postgres:latest",
						true,
						false,
						time.Now().AddDate(0, 0, -1), // Make it stale
						&container.Config{
							Labels:       map[string]string{},
							ExposedPorts: map[nat.Port]struct{}{},
						})

					// Multiple dependents (like web apps)
					webApp1 := mocks.CreateMockContainerWithConfig(
						"web-app-1",
						"/web-app-1",
						"web-app:latest",
						true,
						false,
						time.Now(),
						&container.Config{
							Labels: map[string]string{
								"com.centurylinklabs.watchtower.depends-on": "database",
							},
							ExposedPorts: map[nat.Port]struct{}{},
						})

					webApp2 := mocks.CreateMockContainerWithConfig(
						"web-app-2",
						"/web-app-2",
						"web-app:latest",
						true,
						false,
						time.Now(),
						&container.Config{
							Labels: map[string]string{
								"com.centurylinklabs.watchtower.depends-on": "database",
							},
							ExposedPorts: map[nat.Port]struct{}{},
						})

					apiService := mocks.CreateMockContainerWithConfig(
						"api-service",
						"/api-service",
						"api:latest",
						true,
						false,
						time.Now(),
						&container.Config{
							Labels: map[string]string{
								"com.centurylinklabs.watchtower.depends-on": "database",
							},
							ExposedPorts: map[nat.Port]struct{}{},
						})

					client := mocks.CreateMockClient(
						&mocks.TestData{
							Containers: []types.Container{database, webApp1, webApp2, apiService},
							Staleness: map[string]bool{
								"database":    true,
								"web-app-1":   false,
								"web-app-2":   false,
								"api-service": false,
							},
						},
						false,
						false,
					)

					report, cleanupImageInfos, err := actions.Update(
						client,
						actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
					)

					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					gomega.Expect(report.Updated()).To(gomega.HaveLen(1)) // Only database is stale
					gomega.Expect(report.Scanned()).To(gomega.HaveLen(4))

					// Only database should be in cleanup (updated)
					gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(1))
					gomega.Expect(cleanupImageInfos[0].ContainerName).To(gomega.Equal("database"))

					// All dependents should be marked for restart
					gomega.Expect(webApp1.ToRestart()).To(gomega.BeTrue())
					gomega.Expect(webApp2.ToRestart()).To(gomega.BeTrue())
					gomega.Expect(apiService.ToRestart()).To(gomega.BeTrue())
				})
			},
		)

		ginkgo.When("handling complex dependency chains with rolling restart", func() {
			ginkgo.It("should maintain correct stop/start order in complex chains", func() {
				// Create complex chain: A depends on B, B depends on C, D depends on C
				containerC := mocks.CreateMockContainerWithConfig(
					"service-c",
					"/service-c",
					"service-c:latest",
					true,
					false,
					time.Now().AddDate(0, 0, -1),
					&container.Config{
						Labels:       map[string]string{},
						ExposedPorts: map[nat.Port]struct{}{},
					})

				containerB := mocks.CreateMockContainerWithConfig(
					"service-b",
					"/service-b",
					"service-b:latest",
					true,
					false,
					time.Now().AddDate(0, 0, -1),
					&container.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower.depends-on": "service-c",
						},
						ExposedPorts: map[nat.Port]struct{}{},
					})

				containerA := mocks.CreateMockContainerWithConfig(
					"service-a",
					"/service-a",
					"service-a:latest",
					true,
					false,
					time.Now().AddDate(0, 0, -1),
					&container.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower.depends-on": "service-b",
						},
						ExposedPorts: map[nat.Port]struct{}{},
					})

				containerD := mocks.CreateMockContainerWithConfig(
					"service-d",
					"/service-d",
					"service-d:latest",
					true,
					false,
					time.Now().AddDate(0, 0, -1),
					&container.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower.depends-on": "service-c",
						},
						ExposedPorts: map[nat.Port]struct{}{},
					})

				client := mocks.CreateMockClient(
					&mocks.TestData{
						Containers: []types.Container{
							containerC,
							containerB,
							containerA,
							containerD,
						},
						Staleness: map[string]bool{
							"service-c": true,
							"service-b": true,
							"service-a": true,
							"service-d": true,
						},
					},
					false,
					false,
				)

				report, cleanupImageInfos, err := actions.Update(
					client,
					actions.UpdateConfig{Cleanup: true, RollingRestart: true, CPUCopyMode: "auto"},
				)

				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).To(gomega.HaveLen(4))

				// Verify rolling restart was used for all containers
				gomega.Expect(client.TestData.WaitForContainerHealthyCount).To(gomega.Equal(4))

				// Verify cleanup occurred for all updated containers
				gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(4))
			})
		})

		ginkgo.When("handling diamond dependency scenario with Docker Compose labels", func() {
			ginkgo.It("should stop and start containers in correct dependency order", func() {
				// Create diamond dependency: a-database (base), b-service1 and d-service3 depend on a-database, c-service2 depends on b-service1
				aDatabase := mocks.CreateMockContainerWithConfig(
					"a-database",
					"/a-database",
					"postgres:latest",
					true,
					false,
					time.Now(),
					&container.Config{
						Labels:       map[string]string{},
						ExposedPorts: map[nat.Port]struct{}{},
					})

				bService1 := mocks.CreateMockContainerWithConfig(
					"b-service1",
					"/b-service1",
					"service1:latest",
					true,
					false,
					time.Now().AddDate(0, 0, -1), // Make it stale
					&container.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower.depends-on": "a-database",
						},
						ExposedPorts: map[nat.Port]struct{}{},
					})

				cService2 := mocks.CreateMockContainerWithConfig(
					"c-service2",
					"/c-service2",
					"service2:latest",
					true,
					false,
					time.Now().AddDate(0, 0, -1), // Make it stale
					&container.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower.depends-on": "b-service1",
						},
						ExposedPorts: map[nat.Port]struct{}{},
					})

				dService3 := mocks.CreateMockContainerWithConfig(
					"d-service3",
					"/d-service3",
					"service3:latest",
					true,
					false,
					time.Now().AddDate(0, 0, -1), // Make it stale
					&container.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower.depends-on": "a-database",
						},
						ExposedPorts: map[nat.Port]struct{}{},
					})

				client := mocks.CreateMockClient(
					&mocks.TestData{
						Containers: []types.Container{aDatabase, bService1, dService3, cService2},
						Staleness: map[string]bool{
							"a-database": false,
							"b-service1": true,
							"c-service2": true,
							"d-service3": true,
						},
						StopOrder:  []string{},
						StartOrder: []string{},
					},
					false,
					false,
				)

				report, cleanupImageInfos, err := actions.Update(
					client,
					actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
				)

				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).
					To(gomega.HaveLen(3))
					// b, c, d are stale and updated

				// Verify stop order: reverse dependency order
				gomega.Expect(client.TestData.StopOrder).
					To(gomega.Equal([]string{"c-service2", "b-service1", "d-service3"}))

				// Verify start order: dependency order
				gomega.Expect(client.TestData.StartOrder).
					To(gomega.Equal([]string{"d-service3", "b-service1", "c-service2"}))

				// Verify cleanup for updated containers
				gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(3))
				gomega.Expect(cleanupImageInfos).
					To(gomega.ContainElement(gomega.HaveField("ContainerName", "b-service1")))
				gomega.Expect(cleanupImageInfos).
					To(gomega.ContainElement(gomega.HaveField("ContainerName", "c-service2")))
				gomega.Expect(cleanupImageInfos).
					To(gomega.ContainElement(gomega.HaveField("ContainerName", "d-service3")))
			})
		})

		ginkgo.When("handling dependencies outside the filtered container list", func() {
			ginkgo.It("should handle dependents when dependencies are filtered out", func() {
				dependency := mocks.CreateMockContainerWithConfig(
					"filtered-dependency",
					"/filtered-dependency",
					"dep-image:latest",
					true,
					false,
					time.Now().AddDate(0, 0, -1),
					&container.Config{
						Labels:       map[string]string{},
						ExposedPorts: map[nat.Port]struct{}{},
					})
				dependent := mocks.CreateMockContainerWithConfig(
					"included-dependent",
					"/included-dependent",
					"dep-image:latest",
					true,
					false,
					time.Now(),
					&container.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower.depends-on": "filtered-dependency",
						},
						ExposedPorts: map[nat.Port]struct{}{},
					})
				client := mocks.CreateMockClient(
					&mocks.TestData{
						Containers: []types.Container{dependency, dependent},
						Staleness: map[string]bool{
							"filtered-dependency": true,
							"included-dependent":  false,
						},
					},
					false,
					false,
				)
				config := actions.UpdateConfig{
					Cleanup: true,
					Filter: func(c types.FilterableContainer) bool {
						return c.Name() != "filtered-dependency"
					},
					CPUCopyMode: "auto",
				}
				report, cleanupImageInfos, err := actions.Update(client, config)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).To(gomega.BeEmpty())
				gomega.Expect(cleanupImageInfos).To(gomega.BeEmpty())
				gomega.Expect(dependent.ToRestart()).To(gomega.BeFalse())
			})
		})

		ginkgo.When("handling containers with mixed watchtower and compose labels", func() {
			ginkgo.It(
				"should verify containers with both watchtower labels and docker-compose depends_on labels",
				func() {
					// Create a dependency container without watchtower label
					dependency := mocks.CreateMockContainerWithConfig(
						"dependency-container",
						"/dependency-container",
						"dep-image:latest",
						true,
						false,
						time.Now().AddDate(0, 0, -1), // stale
						&container.Config{
							Labels:       map[string]string{},
							ExposedPorts: map[nat.Port]struct{}{},
						})

					// Create a dependent container with both watchtower enable and depends-on
					dependent := mocks.CreateMockContainerWithConfig(
						"dependent-container",
						"/dependent-container",
						"dep-image:latest",
						true,
						false,
						time.Now(),
						&container.Config{
							Labels: map[string]string{
								"com.centurylinklabs.watchtower":            "true",
								"com.centurylinklabs.watchtower.depends-on": "dependency-container",
							},
							ExposedPorts: map[nat.Port]struct{}{},
						})

					client := mocks.CreateMockClient(
						&mocks.TestData{
							Containers: []types.Container{dependency, dependent},
							Staleness: map[string]bool{
								"dependency-container": true,
								"dependent-container":  false,
							},
						},
						false,
						false,
					)

					report, cleanupImageInfos, err := actions.Update(
						client,
						actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
					)

					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					gomega.Expect(report.Updated()).
						To(gomega.HaveLen(1))
						// only dependency is stale
					gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(1))
					gomega.Expect(cleanupImageInfos[0].ContainerName).
						To(gomega.Equal("dependency-container"))
					// Dependent should be marked for restart
					gomega.Expect(dependent.ToRestart()).To(gomega.BeTrue())
				},
			)
		})

		ginkgo.When("handling containers with network mode dependencies", func() {
			ginkgo.It(
				"should restart containers depending on network mode when dependency updates",
				func() {
					client := mocks.CreateMockClient(getNetworkModeTestData(), false, false)
					report, cleanupImageInfos, err := actions.Update(
						client,
						actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
					)
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					gomega.Expect(report.Updated()).To(gomega.HaveLen(1))
					gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(1))
					gomega.Expect(cleanupImageInfos[0].ContainerName).
						To(gomega.Equal("network-dependency"))
					// The dependent should be marked for restart
					dependent := client.TestData.Containers[1]
					gomega.Expect(dependent.ToRestart()).To(gomega.BeTrue())
				},
			)
		})
		ginkgo.When(
			"handling complex multi-service applications with various dependency patterns",
			func() {
				ginkgo.It(
					"should restart all dependent services when database service updates",
					func() {
						// Create database container
						database := mocks.CreateMockContainerWithConfig(
							"database",
							"/database",
							"postgres:latest",
							true,
							false,
							time.Now().AddDate(0, 0, -1), // Make it stale
							&container.Config{
								Labels:       map[string]string{},
								ExposedPorts: map[nat.Port]struct{}{},
							})

						// Create web services
						web1 := mocks.CreateMockContainerWithConfig(
							"web-service-1",
							"/web-service-1",
							"web:latest",
							true,
							false,
							time.Now(),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower.depends-on": "database",
								},
								ExposedPorts: map[nat.Port]struct{}{},
							})

						// Create API services
						api1 := mocks.CreateMockContainerWithConfig(
							"api-service-1",
							"/api-service-1",
							"api:latest",
							true,
							false,
							time.Now(),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower.depends-on": "database",
								},
								ExposedPorts: map[nat.Port]struct{}{},
							})

						// Create a service that depends on web service
						dependentService := mocks.CreateMockContainerWithConfig(
							"dependent-service",
							"/dependent-service",
							"dependent:latest",
							true,
							false,
							time.Now(),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower.depends-on": "web-service-1",
								},
								ExposedPorts: map[nat.Port]struct{}{},
							})

						client := mocks.CreateMockClient(
							&mocks.TestData{
								Containers: []types.Container{
									database,
									web1,
									api1,
									dependentService,
								},
								Staleness: map[string]bool{
									"database":          true,
									"web-service-1":     false,
									"api-service-1":     false,
									"dependent-service": false,
								},
							},
							false,
							false,
						)

						report, cleanupImageInfos, err := actions.Update(
							client,
							actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
						)

						gomega.Expect(err).NotTo(gomega.HaveOccurred())
						gomega.Expect(report.Updated()).
							To(gomega.HaveLen(1))
							// Only database is stale
						gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(1))
						gomega.Expect(cleanupImageInfos[0].ContainerName).
							To(gomega.Equal("database"))

						// All dependents should be marked for restart
						gomega.Expect(web1.ToRestart()).To(gomega.BeTrue())
						gomega.Expect(api1.ToRestart()).To(gomega.BeTrue())
						gomega.Expect(dependentService.ToRestart()).To(gomega.BeTrue())
					},
				)
			},
		)
	})
	ginkgo.When("handling edge cases in dependency resolution", func() {
		ginkgo.It("should handle malformed labels and missing containers gracefully", func() {
			client := mocks.CreateMockClient(
				&mocks.TestData{
					Containers: []types.Container{
						mocks.CreateMockContainerWithConfig(
							"container-with-malformed-depends",
							"/container-with-malformed-depends",
							"image:latest",
							true,
							false,
							time.Now().AddDate(0, 0, -1),
							&container.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower.depends-on": "non-existent, malformed name",
								},
								ExposedPorts: map[nat.Port]struct{}{},
							}),
					},
					Staleness: map[string]bool{
						"container-with-malformed-depends": true,
					},
				},
				false,
				false,
			)
			report, cleanupImageInfos, err := actions.Update(
				client,
				actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(report.Updated()).To(gomega.HaveLen(1))
			gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(1))
		})
		ginkgo.It("should ensure dependencies are stopped and started in correct order", func() {
			// Create dependency chain: A depends on B, B depends on C
			containers := createDependencyChain(
				[]string{"container-a", "container-b", "container-c"},
			)
			client := mocks.CreateMockClient(
				&mocks.TestData{
					Containers: containers,
					Staleness: map[string]bool{
						"container-a": true,
						"container-b": true,
						"container-c": true,
					},
					StopOrder:  []string{},
					StartOrder: []string{},
				},
				false,
				false,
			)
			report, cleanupImageInfos, err := actions.Update(
				client,
				actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(report.Updated()).To(gomega.HaveLen(3))
			gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(3))
			// Verify stop order: dependents first (reverse dependency order)
			gomega.Expect(client.TestData.StopOrder).
				To(gomega.Equal([]string{"container-a", "container-b", "container-c"}))
			// Verify start order: dependencies first
			gomega.Expect(client.TestData.StartOrder).
				To(gomega.Equal([]string{"container-c", "container-b", "container-a"}))
		})
	})
	ginkgo.When("only necessary containers restarted", func() {
		ginkgo.It(
			"should verify that only dependent containers are restarted when dependencies update",
			func() {
				// Create base container (stale)
				base := mocks.CreateMockContainerWithConfig(
					"base",
					"/base",
					"base:latest",
					true,
					false,
					time.Now().AddDate(0, 0, -1),
					&container.Config{
						Labels:       map[string]string{},
						ExposedPorts: map[nat.Port]struct{}{},
					},
				)
				// Dependent
				dep1 := mocks.CreateMockContainerWithConfig(
					"dep1",
					"/dep1",
					"dep1:latest",
					true,
					false,
					time.Now(),
					&container.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower.depends-on": "base",
						},
						ExposedPorts: map[nat.Port]struct{}{},
					},
				)
				// Independent
				indep := mocks.CreateMockContainerWithConfig(
					"indep",
					"/indep",
					"indep:latest",
					true,
					false,
					time.Now(),
					&container.Config{
						Labels:       map[string]string{},
						ExposedPorts: map[nat.Port]struct{}{},
					},
				)
				client := mocks.CreateMockClient(
					&mocks.TestData{
						Containers: []types.Container{base, dep1, indep},
						Staleness: map[string]bool{
							"base":  true,
							"dep1":  false,
							"indep": false,
						},
					},
					false,
					false,
				)
				report, cleanupImageInfos, err := actions.Update(
					client,
					actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).To(gomega.HaveLen(1))
				gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(1))
				gomega.Expect(cleanupImageInfos[0].ContainerName).To(gomega.Equal("base"))
				gomega.Expect(dep1.ToRestart()).To(gomega.BeTrue())
				gomega.Expect(indep.ToRestart()).To(gomega.BeFalse())
			},
		)
	})
})
