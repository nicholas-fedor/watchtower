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
			report, cleanupImageIDs, err := actions.Update(
				client,
				types.UpdateParams{
					Cleanup:          true,
					Filter:           filters.WatchtowerContainersFilter,
					PullFailureDelay: 10 * time.Millisecond, // Test-specific short delay
					CPUCopyMode:      "auto",
				},
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(report.Updated()).To(gomega.HaveLen(1))
			gomega.Expect(cleanupImageIDs).
				To(gomega.BeEmpty(), "No cleanup for renamed Watchtower container")
			gomega.Expect(client.TestData.TriedToRemoveImageCount).
				To(gomega.Equal(0), "RemoveImageByID should not be called during Update")
			gomega.Expect(client.TestData.RenameContainerCount).
				To(gomega.Equal(1), "RenameContainer should be called once")
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
			params := types.UpdateParams{
				Cleanup:     true,
				NoRestart:   true,
				Filter:      filters.WatchtowerContainersFilter,
				CPUCopyMode: "auto",
			}
			report, cleanupImageIDs, err := actions.Update(client, params)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(report.Scanned()).
				To(gomega.HaveLen(1), "Container should be scanned but not updated")
			gomega.Expect(report.Updated()).
				To(gomega.BeEmpty(), "No containers should be updated with no-restart")
			gomega.Expect(cleanupImageIDs).
				To(gomega.BeEmpty(), "No images should be collected for cleanup")
			gomega.Expect(client.TestData.RenameContainerCount).
				To(gomega.Equal(0), "RenameContainer should not be called with no-restart")
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
			cleanupImageIDs := make(map[types.ImageID]bool)
			err := actions.CheckForMultipleWatchtowerInstances(
				client,
				true, // cleanup=true
				"prod",
				cleanupImageIDs,
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(client.TestData.StopContainerCount).
				To(gomega.Equal(0), "StopContainer should not be called for unscoped container")
			gomega.Expect(cleanupImageIDs).
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
			cleanupImageIDs := make(map[types.ImageID]bool)
			err := actions.CheckForMultipleWatchtowerInstances(client, true, "", cleanupImageIDs)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty(), "No image cleanup for shared image")
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
				report, cleanupImageIDs, err := actions.Update(
					client,
					types.UpdateParams{Cleanup: true, CPUCopyMode: "auto"},
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).To(gomega.HaveLen(3))
				gomega.Expect(cleanupImageIDs).
					To(gomega.HaveKey(types.ImageID("fake-image:latest")))
				gomega.Expect(cleanupImageIDs).To(gomega.HaveLen(1))
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
				report, cleanupImageIDs, err := actions.Update(
					client,
					types.UpdateParams{Cleanup: true, CPUCopyMode: "auto"},
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).To(gomega.HaveLen(4))
				gomega.Expect(cleanupImageIDs).
					To(gomega.HaveKey(types.ImageID("fake-image:latest")))
				gomega.Expect(cleanupImageIDs).
					To(gomega.HaveKey(types.ImageID("unique-fake-image:latest")))
				gomega.Expect(cleanupImageIDs).To(gomega.HaveLen(2))
				gomega.Expect(client.TestData.TriedToRemoveImageCount).
					To(gomega.Equal(0), "RemoveImageByID should not be called during Update")
			})
		})

		ginkgo.When("there are linked containers being updated", func() {
			ginkgo.It("should collect only the stale container's image ID", func() {
				client := mocks.CreateMockClient(getLinkedTestData(true), false, false)
				client.TestData.Staleness["test-container-01"] = true
				report, cleanupImageIDs, err := actions.Update(
					client,
					types.UpdateParams{Cleanup: true, CPUCopyMode: "auto"},
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).To(gomega.HaveLen(1))
				gomega.Expect(cleanupImageIDs).
					To(gomega.HaveKey(types.ImageID("fake-image1:latest")))
				gomega.Expect(cleanupImageIDs).To(gomega.HaveLen(1))
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
				report, cleanupImageIDs, err := actions.Update(
					client,
					types.UpdateParams{Cleanup: true, RollingRestart: true, CPUCopyMode: "auto"},
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).To(gomega.HaveLen(3))
				gomega.Expect(cleanupImageIDs).
					To(gomega.HaveKey(types.ImageID("fake-image:latest")))
				gomega.Expect(cleanupImageIDs).To(gomega.HaveLen(1))
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
				report, cleanupImageIDs, err := actions.Update(
					client,
					types.UpdateParams{Cleanup: true, CPUCopyMode: "auto"},
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).To(gomega.HaveLen(1))
				gomega.Expect(report.Fresh()).To(gomega.HaveLen(1))
				gomega.Expect(cleanupImageIDs).
					To(gomega.HaveKey(types.ImageID("fake-image1:latest")))
				gomega.Expect(cleanupImageIDs).To(gomega.HaveLen(1))
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
				report, cleanupImageIDs, err := actions.Update(
					client,
					types.UpdateParams{Cleanup: true, CPUCopyMode: "auto"},
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).To(gomega.HaveLen(1))
				gomega.Expect(cleanupImageIDs).
					To(gomega.HaveKey(types.ImageID("fake-image1:latest")))
				gomega.Expect(cleanupImageIDs).To(gomega.HaveLen(1))
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
				report, cleanupImageIDs, err := actions.Update(
					client,
					types.UpdateParams{Cleanup: true, MonitorOnly: true, CPUCopyMode: "auto"},
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).To(gomega.BeEmpty())
				gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty())
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
					report, cleanupImageIDs, err := actions.Update(
						client,
						types.UpdateParams{
							Cleanup:         true,
							MonitorOnly:     true,
							LabelPrecedence: true,
							CPUCopyMode:     "auto",
						},
					)
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					gomega.Expect(report.Updated()).To(gomega.HaveLen(1))
					gomega.Expect(cleanupImageIDs).
						To(gomega.HaveKey(types.ImageID("fake-image2:latest")))
					gomega.Expect(cleanupImageIDs).To(gomega.HaveLen(1))
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
						report, cleanupImageIDs, err := actions.Update(
							client,
							types.UpdateParams{
								Cleanup:         true,
								MonitorOnly:     true,
								LabelPrecedence: true,
								CPUCopyMode:     "auto",
							},
						)
						gomega.Expect(err).NotTo(gomega.HaveOccurred())
						gomega.Expect(report.Updated()).To(gomega.BeEmpty())
						gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty())
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
					report, cleanupImageIDs, err := actions.Update(
						client,
						types.UpdateParams{
							Cleanup:         true,
							MonitorOnly:     true,
							LabelPrecedence: true,
							CPUCopyMode:     "auto",
						},
					)
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					gomega.Expect(report.Updated()).To(gomega.BeEmpty())
					gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty())
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
				report, cleanupImageIDs, err := actions.Update(
					client,
					types.UpdateParams{
						Cleanup:        true,
						LifecycleHooks: true,
						CPUCopyMode:    "auto",
					},
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).To(gomega.BeEmpty())
				gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty())
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
				report, cleanupImageIDs, err := actions.Update(
					client,
					types.UpdateParams{
						Cleanup:        true,
						LifecycleHooks: true,
						LifecycleUID:   1000,
						LifecycleGID:   1001,
						CPUCopyMode:    "auto",
					},
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).To(gomega.HaveLen(1))
				gomega.Expect(cleanupImageIDs).
					To(gomega.HaveKey(types.ImageID("fake-image:latest")))
				gomega.Expect(cleanupImageIDs).To(gomega.HaveLen(1))
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
				report, cleanupImageIDs, err := actions.Update(
					client,
					types.UpdateParams{
						Cleanup:        true,
						LifecycleHooks: true,
						CPUCopyMode:    "auto",
					},
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).To(gomega.BeEmpty())
				gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty())
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
				report, cleanupImageIDs, err := actions.Update(
					client,
					types.UpdateParams{
						Cleanup:        true,
						LifecycleHooks: true,
						CPUCopyMode:    "auto",
					},
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).To(gomega.HaveLen(1))
				gomega.Expect(cleanupImageIDs).
					To(gomega.HaveKey(types.ImageID("fake-image2:latest")))
				gomega.Expect(cleanupImageIDs).To(gomega.HaveLen(1))
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
				report, cleanupImageIDs, err := actions.Update(
					client,
					types.UpdateParams{
						Cleanup:        true,
						LifecycleHooks: true,
						CPUCopyMode:    "auto",
					},
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).To(gomega.HaveLen(1))
				gomega.Expect(cleanupImageIDs).
					To(gomega.HaveKey(types.ImageID("fake-image2:latest")))
				gomega.Expect(cleanupImageIDs).To(gomega.HaveLen(1))
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
				report, cleanupImageIDs, err := actions.Update(
					client,
					types.UpdateParams{
						Cleanup:        true,
						LifecycleHooks: true,
						CPUCopyMode:    "auto",
					},
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).To(gomega.HaveLen(1))
				gomega.Expect(cleanupImageIDs).
					To(gomega.HaveKey(types.ImageID("fake-image2:latest")))
				gomega.Expect(cleanupImageIDs).To(gomega.HaveLen(1))
				gomega.Expect(client.TestData.TriedToRemoveImageCount).
					To(gomega.Equal(0), "RemoveImageByID should not be called during Update")
			})
		})
	})

	// Tests for image reference handling to cover isPinned functionality
	ginkgo.When("handling different image reference formats", func() {
		var client *mocks.MockClient
		var params types.UpdateParams

		ginkgo.BeforeEach(func() {
			params = types.UpdateParams{
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
			report, cleanupImageIDs, err := actions.Update(client, params)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(report.Scanned()).
				To(gomega.HaveLen(1), "Tagged container should be scanned")
			gomega.Expect(report.Updated()).
				To(gomega.HaveLen(1), "Tagged container should be updated if stale")
			gomega.Expect(cleanupImageIDs).To(gomega.HaveKey(types.ImageID("image:1.0.0")))
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
			report, cleanupImageIDs, err := actions.Update(client, params)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(report.Scanned()).
				To(gomega.HaveLen(1), "Untagged container should be scanned")
			gomega.Expect(report.Updated()).
				To(gomega.HaveLen(1), "Untagged container should be updated if stale")
			gomega.Expect(cleanupImageIDs).To(gomega.HaveKey(types.ImageID("image")))
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
			report, cleanupImageIDs, err := actions.Update(client, params)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(report.Scanned()).
				To(gomega.HaveLen(1), "Pinned container should be scanned")
			gomega.Expect(report.Updated()).
				To(gomega.BeEmpty(), "Pinned container should not be updated")
			gomega.Expect(cleanupImageIDs).
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
				report, cleanupImageIDs, err := actions.Update(client, params)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Scanned()).
					To(gomega.HaveLen(1), "Pinned container should be scanned")
				gomega.Expect(report.Updated()).
					To(gomega.BeEmpty(), "Pinned container should not be updated")
				gomega.Expect(cleanupImageIDs).
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
			report, cleanupImageIDs, err := actions.Update(client, params)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(report.Skipped()).
				To(gomega.HaveLen(1), "Invalid container should be skipped")
			gomega.Expect(report.Scanned()).
				To(gomega.BeEmpty(), "Invalid container should not be scanned")
			gomega.Expect(report.Updated()).
				To(gomega.BeEmpty(), "Invalid container should not be updated")
			gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty(), "No image IDs should be collected")
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
				report, cleanupImageIDs, err := actions.Update(client, params)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Skipped()).
					To(gomega.HaveLen(1), "Container with missing image info should be skipped")
				gomega.Expect(report.Scanned()).
					To(gomega.BeEmpty(), "Container should not be scanned")
				gomega.Expect(report.Updated()).
					To(gomega.BeEmpty(), "Container should not be updated")
				gomega.Expect(cleanupImageIDs).
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
			report, cleanupImageIDs, err := actions.Update(client, params)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(report.Skipped()).
				To(gomega.HaveLen(1), "Container with invalid fallback image should be skipped")
			gomega.Expect(report.Scanned()).To(gomega.BeEmpty(), "Container should not be scanned")
			gomega.Expect(report.Updated()).To(gomega.BeEmpty(), "Container should not be updated")
			gomega.Expect(cleanupImageIDs).To(gomega.BeEmpty(), "No image IDs should be collected")
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
			report, cleanupImageIDs, err := actions.Update(
				client,
				types.UpdateParams{
					Cleanup:          true,
					Filter:           filters.WatchtowerContainersFilter,
					PullFailureDelay: 10 * time.Millisecond, // Test-specific very short delay
					CPUCopyMode:      "auto",
				},
			)
			elapsedTime := time.Since(startTime)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(report.Updated()).
				To(gomega.BeEmpty(), "Watchtower should not be updated on pull failure")
			gomega.Expect(cleanupImageIDs).
				To(gomega.BeEmpty(), "No cleanup should occur on pull failure")

			// Verify that the delay was applied (using test-specific short delay from PullFailureDelay)
			gomega.Expect(elapsedTime).
				To(gomega.BeNumerically(">=", 10*time.Millisecond), "Delay should have been applied")
		})
	})
})
