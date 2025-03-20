package actions_test

import (
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/go-connections/nat"
	"github.com/nicholas-fedor/watchtower/internal/actions"
	"github.com/nicholas-fedor/watchtower/pkg/types"

	"github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
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
				"test-container-02",
				"test-container-02",
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
	ginkgo.When("watchtower has been instructed to clean up", func() {
		ginkgo.When("there are multiple containers using the same image", func() {
			ginkgo.It("should only try to remove the image once", func() {
				client := mocks.CreateMockClient(getCommonTestData(""), false, false)
				_, err := actions.Update(client, types.UpdateParams{Cleanup: true})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(client.TestData.TriedToRemoveImageCount).To(gomega.Equal(1))
			})
		})
		ginkgo.When("there are multiple containers using different images", func() {
			ginkgo.It("should try to remove each of them", func() {
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
				_, err := actions.Update(client, types.UpdateParams{Cleanup: true})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(client.TestData.TriedToRemoveImageCount).To(gomega.Equal(2))
			})
		})
		ginkgo.When("there are linked containers being updated", func() {
			ginkgo.It("should not try to remove their images", func() {
				client := mocks.CreateMockClient(getLinkedTestData(true), false, false)
				_, err := actions.Update(client, types.UpdateParams{Cleanup: true})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(client.TestData.TriedToRemoveImageCount).To(gomega.Equal(1))
			})
		})
		ginkgo.When("performing a rolling restart update", func() {
			ginkgo.It("should try to remove the image once", func() {
				client := mocks.CreateMockClient(getCommonTestData(""), false, false)
				_, err := actions.Update(client, types.UpdateParams{Cleanup: true, RollingRestart: true})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(client.TestData.TriedToRemoveImageCount).To(gomega.Equal(1))
			})
		})
		ginkgo.When("updating a linked container with missing image info", func() {
			ginkgo.It("should gracefully fail", func() {
				client := mocks.CreateMockClient(getLinkedTestData(false), false, false)

				report, err := actions.Update(client, types.UpdateParams{})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				// Note: Linked containers that were skipped for recreation is not counted in Failed
				// If this happens, an error is emitted to the logs, so a notification should still be sent.
				gomega.Expect(report.Updated()).To(gomega.HaveLen(1))
				gomega.Expect(report.Fresh()).To(gomega.HaveLen(1))
			})
		})
	})

	ginkgo.When("watchtower has been instructed to monitor only", func() {
		ginkgo.When("certain containers are set to monitor only", func() {
			ginkgo.It("should not update those containers", func() {
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
				_, err := actions.Update(client, types.UpdateParams{Cleanup: true})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(client.TestData.TriedToRemoveImageCount).To(gomega.Equal(1))
			})
		})

		ginkgo.When("monitor only is set globally", func() {
			ginkgo.It("should not update any containers", func() {
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
				_, err := actions.Update(client, types.UpdateParams{Cleanup: true, MonitorOnly: true})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(client.TestData.TriedToRemoveImageCount).To(gomega.Equal(0))
			})
			ginkgo.When("watchtower has been instructed to have label take precedence", func() {
				ginkgo.It("it should update containers when monitor only is set to false", func() {
					client := mocks.CreateMockClient(
						&mocks.TestData{
							// NameOfContainerToKeep: "test-container-02",
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
					_, err := actions.Update(client, types.UpdateParams{Cleanup: true, MonitorOnly: true, LabelPrecedence: true})
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					gomega.Expect(client.TestData.TriedToRemoveImageCount).To(gomega.Equal(1))
				})
				ginkgo.It("it should update not containers when monitor only is set to true", func() {
					client := mocks.CreateMockClient(
						&mocks.TestData{
							// NameOfContainerToKeep: "test-container-02",
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
					_, err := actions.Update(client, types.UpdateParams{Cleanup: true, MonitorOnly: true, LabelPrecedence: true})
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					gomega.Expect(client.TestData.TriedToRemoveImageCount).To(gomega.Equal(0))
				})
				ginkgo.It("it should update not containers when monitor only is not set", func() {
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
					_, err := actions.Update(client, types.UpdateParams{Cleanup: true, MonitorOnly: true, LabelPrecedence: true})
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					gomega.Expect(client.TestData.TriedToRemoveImageCount).To(gomega.Equal(0))
				})
			})
		})
	})

	ginkgo.When("watchtower has been instructed to run lifecycle hooks", func() {
		ginkgo.When("pre-update script returns 1", func() {
			ginkgo.It("should not update those containers", func() {
				client := mocks.CreateMockClient(
					&mocks.TestData{
						// NameOfContainerToKeep: "test-container-02",
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

				_, err := actions.Update(client, types.UpdateParams{Cleanup: true, LifecycleHooks: true})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(client.TestData.TriedToRemoveImageCount).To(gomega.Equal(0))
			})
		})

		ginkgo.When("preupdate script returns 75", func() {
			ginkgo.It("should not update those containers", func() {
				client := mocks.CreateMockClient(
					&mocks.TestData{
						// NameOfContainerToKeep: "test-container-02",
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
				_, err := actions.Update(client, types.UpdateParams{Cleanup: true, LifecycleHooks: true})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(client.TestData.TriedToRemoveImageCount).To(gomega.Equal(0))
			})
		})

		ginkgo.When("preupdate script returns 0", func() {
			ginkgo.It("should update those containers", func() {
				client := mocks.CreateMockClient(
					&mocks.TestData{
						// NameOfContainerToKeep: "test-container-02",
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
				_, err := actions.Update(client, types.UpdateParams{Cleanup: true, LifecycleHooks: true})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(client.TestData.TriedToRemoveImageCount).To(gomega.Equal(1))
			})
		})

		ginkgo.When("container is linked to restarting containers", func() {
			ginkgo.It("should be marked for restart", func() {
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
			ginkgo.It("skip running preupdate", func() {
				client := mocks.CreateMockClient(
					&mocks.TestData{
						// NameOfContainerToKeep: "test-container-02",
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
				_, err := actions.Update(client, types.UpdateParams{Cleanup: true, LifecycleHooks: true})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(client.TestData.TriedToRemoveImageCount).To(gomega.Equal(1))
			})
		})

		ginkgo.When("container is restarting", func() {
			ginkgo.It("skip running preupdate", func() {
				client := mocks.CreateMockClient(
					&mocks.TestData{
						// NameOfContainerToKeep: "test-container-02",
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
				_, err := actions.Update(client, types.UpdateParams{Cleanup: true, LifecycleHooks: true})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(client.TestData.TriedToRemoveImageCount).To(gomega.Equal(1))
			})
		})
	})
})
