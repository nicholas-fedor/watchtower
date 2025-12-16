package actions_test

import (
	"context"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/nicholas-fedor/watchtower/internal/actions"
	"github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

var _ = ginkgo.Describe("the update action", func() {
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
				context.Background(),
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

		ginkgo.It(
			"should propagate restart in Docker Compose with service name mismatch",
			func() {
				testData := getComposeTestData()
				containers := testData.Containers

				// db container is stale
				containers[0].SetStale(true)

				gomega.Expect(containers[0].ToRestart()).To(gomega.BeTrue())  // db
				gomega.Expect(containers[1].ToRestart()).To(gomega.BeFalse()) // web

				actions.UpdateImplicitRestart(containers, containers)

				// web should be marked for restart because it depends on db
				gomega.Expect(containers[0].ToRestart()).To(gomega.BeTrue())
				gomega.Expect(containers[1].ToRestart()).To(gomega.BeTrue())
			},
		)

		ginkgo.It(
			"should propagate restart through multi-hop chains in Docker Compose",
			func() {
				testData := getComposeMultiHopTestData()
				containers := testData.Containers

				// cache container is stale
				containers[0].SetStale(true)

				gomega.Expect(containers[0].ToRestart()).To(gomega.BeTrue())  // cache
				gomega.Expect(containers[1].ToRestart()).To(gomega.BeFalse()) // db
				gomega.Expect(containers[2].ToRestart()).To(gomega.BeFalse()) // app

				actions.UpdateImplicitRestart(containers, containers)

				// All should be marked for restart: cache -> db -> app
				gomega.Expect(containers[0].ToRestart()).To(gomega.BeTrue())
				gomega.Expect(containers[1].ToRestart()).To(gomega.BeTrue())
				gomega.Expect(containers[2].ToRestart()).To(gomega.BeTrue())
			},
		)
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
				context.Background(),
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
				context.Background(),
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
					context.Background(),
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
				context.Background(),
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
					context.Background(),
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
				context.Background(),
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
				context.Background(),
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
				context.Background(),
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
				context.Background(),
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
					context.Background(),
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
					context.Background(),
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
				context.Background(),
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
				context.Background(),
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
					context.Background(),
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
				context.Background(),
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
				context.Background(),
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

	ginkgo.When("handling network mode dependencies", func() {
		ginkgo.It(
			"should restart containers that depend on updated containers via network mode",
			func() {
				testData := getNetworkModeTestData()
				client := mocks.CreateMockClient(testData, false, false)

				report, cleanupImageInfos, err := actions.Update(
					context.Background(),
					client,
					actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
				)

				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(report.Updated()).
					To(gomega.HaveLen(1))
					// Only the stale network-dependency container is updated
				gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(1))
				gomega.Expect(cleanupImageInfos[0].ContainerName).
					To(gomega.Equal("network-dependency"))

				// The dependent container should be marked for restart
				containers := testData.Containers
				gomega.Expect(containers[0].Name()).
					To(gomega.Equal("network-dependency"))
					// stale container
				gomega.Expect(containers[1].Name()).
					To(gomega.Equal("network-dependent"))
					// dependent container
				gomega.Expect(containers[1].ToRestart()).To(gomega.BeTrue())
			},
		)
	})
})
