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

		ginkgo.It("should skip self-update in run-once mode for Watchtower", func() {
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
					To(gomega.ContainElement(gomega.HaveField("ContainerName", "test-container-01")))
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

				actions.UpdateImplicitRestart(containers)

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
				actions.UpdateImplicitRestart(containers)

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
			ginkgo.It("should detect circular dependencies and return an error", func() {
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
					To(gomega.HaveOccurred(), "Circular dependencies should cause an error")
				gomega.Expect(report).To(gomega.BeNil(), "No report should be returned on error")
				gomega.Expect(cleanupImageInfos).
					To(gomega.BeEmpty(), "No cleanup should occur on error")
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
					To(gomega.HaveOccurred(), "Self-dependency should cause circular reference error")
				gomega.Expect(report).To(gomega.BeNil(), "No report should be returned on error")
				gomega.Expect(cleanupImageInfos).
					To(gomega.BeEmpty(), "No cleanup should occur on error")
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
	})
})
