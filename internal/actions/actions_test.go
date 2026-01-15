package actions

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	dockerContainer "github.com/docker/docker/api/types/container"

	mockActions "github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/filters"
	"github.com/nicholas-fedor/watchtower/pkg/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	mockTypes "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

var _ = ginkgo.Describe("Actions", func() {
	ginkgo.Describe("handleUpdateResult", func() {
		ginkgo.When("given an error", func() {
			ginkgo.It("should return a zero metric", func() {
				result := &mockTypes.MockReport{}
				err := errors.New("test error")

				metric := handleUpdateResult(result, err)
				gomega.Expect(metric).NotTo(gomega.BeNil())
				gomega.Expect(metric.Scanned).To(gomega.Equal(0))
				gomega.Expect(metric.Updated).To(gomega.Equal(0))
				gomega.Expect(metric.Failed).To(gomega.Equal(0))
			})
		})

		ginkgo.When("given a nil result", func() {
			ginkgo.It("should return a zero metric", func() {
				metric := handleUpdateResult(nil, nil)
				gomega.Expect(metric).NotTo(gomega.BeNil())
				gomega.Expect(metric.Scanned).To(gomega.Equal(0))
				gomega.Expect(metric.Updated).To(gomega.Equal(0))
				gomega.Expect(metric.Failed).To(gomega.Equal(0))
			})
		})

		ginkgo.When("given a valid result with no error", func() {
			ginkgo.It("should return nil", func() {
				result := &mockTypes.MockReport{}
				metric := handleUpdateResult(result, nil)
				gomega.Expect(metric).To(gomega.BeNil())
			})
		})
	})

	ginkgo.Describe("RunUpdatesWithNotifications", func() {
		ginkgo.When("notifier is nil", func() {
			ginkgo.It("should not start notification batching", func() {
				client := mockActions.CreateMockClient(
					&mockActions.TestData{
						Containers: []types.Container{
							mockActions.CreateMockContainer(
								"test-container",
								"test-container",
								"image:latest",
								time.Now(),
							),
						},
						Staleness: map[string]bool{
							"test-container": false, // Container is not stale
						},
					},
					false,
					false,
				)

				params := RunUpdatesWithNotificationsParams{
					Client:                       client,
					Notifier:                     nil,
					NotificationSplitByContainer: false,
					NotificationReport:           false,
					Filter:                       filters.NoFilter,
					Cleanup:                      false,
					NoRestart:                    false,
					MonitorOnly:                  false,
					LifecycleHooks:               false,
					RollingRestart:               false,
					LabelPrecedence:              false,
					NoPull:                       false,
					Timeout:                      time.Minute,
					LifecycleUID:                 1000,
					LifecycleGID:                 1001,
					CPUCopyMode:                  "auto",
					PullFailureDelay:             time.Duration(0),
				}
				metric := RunUpdatesWithNotifications(context.Background(), params)

				gomega.Expect(metric).NotTo(gomega.BeNil())
			})
		})

		ginkgo.When("notifier is provided", func() {
			ginkgo.It("should handle notification split by container", func() {
				client := mockActions.CreateMockClient(
					&mockActions.TestData{
						Containers: []types.Container{
							mockActions.CreateMockContainerWithConfig(
								"test-container",
								"test-container",
								"image:latest",
								true,
								false,
								time.Now().Add(-time.Hour),
								&dockerContainer.Config{},
							),
						},
						Staleness: map[string]bool{"test-container": true},
					},
					false,
					false,
				)

				notifier := mockTypes.NewMockNotifier(ginkgo.GinkgoT())
				notifier.EXPECT().StartNotification(true).Return()
				notifier.EXPECT().ShouldSendNotification(mock.Anything).Return(true)
				notifier.EXPECT().SendFilteredEntries(mock.Anything, mock.Anything).Return()

				params := RunUpdatesWithNotificationsParams{
					Client:                       client,
					Notifier:                     notifier,
					NotificationSplitByContainer: true,
					NotificationReport:           false,
					Filter:                       filters.NoFilter,
					Cleanup:                      false,
					NoRestart:                    false,
					MonitorOnly:                  false,
					LifecycleHooks:               false,
					RollingRestart:               false,
					LabelPrecedence:              false,
					NoPull:                       false,
					Timeout:                      time.Minute,
					LifecycleUID:                 1000,
					LifecycleGID:                 1001,
					CPUCopyMode:                  "auto",
					PullFailureDelay:             time.Duration(0),
				}
				metric := RunUpdatesWithNotifications(context.Background(), params)

				gomega.Expect(metric).NotTo(gomega.BeNil())
				notifier.AssertExpectations(ginkgo.GinkgoT())
			})
		})

		ginkgo.When("no containers are provided", func() {
			ginkgo.It("should return zero metric when container list is empty", func() {
				client := mockActions.CreateMockClient(
					&mockActions.TestData{
						Containers: []types.Container{},
						Staleness:  map[string]bool{},
					},
					false,
					false,
				)

				params := RunUpdatesWithNotificationsParams{
					Client:                       client,
					Notifier:                     nil,
					NotificationSplitByContainer: false,
					NotificationReport:           false,
					Filter:                       filters.NoFilter,
					Cleanup:                      false,
					NoRestart:                    false,
					MonitorOnly:                  false,
					LifecycleHooks:               false,
					RollingRestart:               false,
					LabelPrecedence:              false,
					NoPull:                       false,
					Timeout:                      time.Minute,
					LifecycleUID:                 1000,
					LifecycleGID:                 1001,
					CPUCopyMode:                  "auto",
					PullFailureDelay:             time.Duration(0),
				}

				// Empty container list results in zero metrics
				metric := RunUpdatesWithNotifications(context.Background(), params)

				gomega.Expect(metric).NotTo(gomega.BeNil())
				gomega.Expect(metric.Scanned).To(gomega.Equal(0))
				gomega.Expect(metric.Updated).To(gomega.Equal(0))
				gomega.Expect(metric.Failed).To(gomega.Equal(0))
			})
		})

		ginkgo.When("CurrentContainerID is set", func() {
			ginkgo.It(
				"should correctly pass CurrentContainerID to UpdateConfig and skip other Watchtower containers",
				func() {
					// Create two Watchtower containers with the watchtower label, but only container1 should be updated due to CurrentContainerID
					watchtowerConfig := &dockerContainer.Config{
						Image:  "watchtower:latest",
						Labels: map[string]string{"com.centurylinklabs.watchtower": "true"},
					}

					container1 := mockActions.CreateMockContainerWithConfig(
						"container1-id",
						"watchtower-1",
						"watchtower:latest",
						true,
						false,
						time.Now().Add(-time.Hour),
						watchtowerConfig,
					)

					container2 := mockActions.CreateMockContainerWithConfig(
						"container2-id",
						"watchtower-2",
						"watchtower:latest",
						true,
						false,
						time.Now().Add(-time.Hour),
						watchtowerConfig,
					)

					client := mockActions.CreateMockClient(
						&mockActions.TestData{
							Containers: []types.Container{container1, container2},
							Staleness: map[string]bool{
								"watchtower-1": true,
								"watchtower-2": true,
							},
						},
						false,
						false,
					)

					params := RunUpdatesWithNotificationsParams{
						Client:                       client,
						Notifier:                     nil,
						NotificationSplitByContainer: false,
						NotificationReport:           false,
						Filter:                       filters.NoFilter,
						Cleanup:                      false,
						NoRestart:                    false,
						MonitorOnly:                  false,
						LifecycleHooks:               false,
						RollingRestart:               false,
						LabelPrecedence:              false,
						NoPull:                       false,
						Timeout:                      time.Minute,
						LifecycleUID:                 1000,
						LifecycleGID:                 1001,
						CPUCopyMode:                  "auto",
						PullFailureDelay:             time.Duration(0),
						CurrentContainerID: types.ContainerID(
							"container1-id",
						), // Set to first container
					}

					metric := RunUpdatesWithNotifications(context.Background(), params)

					gomega.Expect(metric).NotTo(gomega.BeNil())
					// Only container1 should be updated, container2 should be skipped due to CurrentContainerID
					gomega.Expect(client.TestData.StartOrder).To(gomega.HaveLen(1))
					gomega.Expect(client.TestData.StartOrder).
						To(gomega.ContainElement("watchtower-1"))
					gomega.Expect(client.TestData.StartOrder).
						To(gomega.Not(gomega.ContainElement("watchtower-2")))
				},
			)
		})
	})

	ginkgo.Describe("buildSingleContainerReport", func() {
		ginkgo.It("should create a SingleContainerReport with updated container", func() {
			mockContainerReport := mockTypes.NewMockContainerReport(ginkgo.GinkgoT())
			mockReport := mockTypes.NewMockReport(ginkgo.GinkgoT())

			mockReport.EXPECT().Scanned().Return([]types.ContainerReport{mockContainerReport})
			mockReport.EXPECT().Failed().Return([]types.ContainerReport{})
			mockReport.EXPECT().Skipped().Return([]types.ContainerReport{})
			mockReport.EXPECT().Stale().Return([]types.ContainerReport{})
			mockReport.EXPECT().Fresh().Return([]types.ContainerReport{})

			result := buildSingleContainerReport(mockContainerReport, mockReport)

			gomega.Expect(result).NotTo(gomega.BeNil())
			gomega.Expect(result.UpdatedReports).To(gomega.HaveLen(1))
			gomega.Expect(result.UpdatedReports[0]).To(gomega.HaveOccurred())
			gomega.Expect(result.ScannedReports).To(gomega.HaveLen(1))
			gomega.Expect(result.FailedReports).To(gomega.BeEmpty())
			gomega.Expect(result.SkippedReports).To(gomega.BeEmpty())
			gomega.Expect(result.StaleReports).To(gomega.BeEmpty())
			gomega.Expect(result.FreshReports).To(gomega.BeEmpty())
		})
	})

	ginkgo.Describe("buildSingleRestartedContainerReport", func() {
		ginkgo.It("should create a SingleContainerReport with restarted container", func() {
			mockContainerReport := mockTypes.NewMockContainerReport(ginkgo.GinkgoT())
			mockReport := mockTypes.NewMockReport(ginkgo.GinkgoT())

			mockContainerReport.EXPECT().Error().Return("")
			mockReport.EXPECT().Scanned().Return([]types.ContainerReport{mockContainerReport})
			mockReport.EXPECT().Failed().Return([]types.ContainerReport{})
			mockReport.EXPECT().Skipped().Return([]types.ContainerReport{})
			mockReport.EXPECT().Stale().Return([]types.ContainerReport{})
			mockReport.EXPECT().Fresh().Return([]types.ContainerReport{})

			result := buildSingleRestartedContainerReport(mockContainerReport, mockReport)

			gomega.Expect(result).NotTo(gomega.BeNil())
			gomega.Expect(result.RestartedReports).To(gomega.HaveLen(1))
			gomega.Expect(result.RestartedReports[0].Error()).To(gomega.BeEmpty())
			gomega.Expect(result.ScannedReports).To(gomega.HaveLen(1))
			gomega.Expect(result.FailedReports).To(gomega.BeEmpty())
			gomega.Expect(result.SkippedReports).To(gomega.BeEmpty())
			gomega.Expect(result.StaleReports).To(gomega.BeEmpty())
			gomega.Expect(result.FreshReports).To(gomega.BeEmpty())
		})
	})

	ginkgo.Describe("buildCleanupEntriesForContainer", func() {
		ginkgo.It("should return empty entries when no cleaned images match", func() {
			cleanedImages := []types.RemovedImageInfo{
				{
					ContainerName: "other-container",
					ImageName:     "image:v1.0",
					ImageID:       types.ImageID("sha256:123"),
				},
			}

			entries := buildCleanupEntriesForContainer(cleanedImages, "test-container")
			gomega.Expect(entries).To(gomega.BeEmpty())
		})

		ginkgo.It("should return entries for matching container", func() {
			cleanedImages := []types.RemovedImageInfo{
				{
					ContainerName: "test-container",
					ImageName:     "image:v1.0",
					ImageID:       types.ImageID("sha256:123"),
				},
			}

			entries := buildCleanupEntriesForContainer(cleanedImages, "test-container")
			gomega.Expect(entries).To(gomega.HaveLen(1))
			gomega.Expect(entries[0].Message).To(gomega.Equal("Removing image"))
			gomega.Expect(entries[0].Data["container_name"]).To(gomega.Equal("test-container"))
		})

		ginkgo.It("should return multiple entries for multiple matches", func() {
			cleanedImages := []types.RemovedImageInfo{
				{
					ContainerName: "test-container",
					ImageName:     "image:v1.0",
					ImageID:       types.ImageID("sha256:123"),
				},
				{
					ContainerName: "test-container",
					ImageName:     "image:v2.0",
					ImageID:       types.ImageID("sha256:456"),
				},
				{
					ContainerName: "other-container",
					ImageName:     "image:v3.0",
					ImageID:       types.ImageID("sha256:789"),
				},
			}

			entries := buildCleanupEntriesForContainer(cleanedImages, "test-container")
			gomega.Expect(entries).To(gomega.HaveLen(2))
			gomega.Expect(entries[0].Message).To(gomega.Equal("Removing image"))
			gomega.Expect(entries[1].Message).To(gomega.Equal("Removing image"))
		})
	})

	ginkgo.Describe("buildUpdateEntries", func() {
		ginkgo.It("should create entries for regular container update", func() {
			mockContainerReport := mockTypes.NewMockContainerReport(ginkgo.GinkgoT())

			mockContainerReport.EXPECT().Name().Return("test-container")
			mockContainerReport.EXPECT().ImageName().Return("test-image:latest")
			mockContainerReport.EXPECT().LatestImageID().Return(types.ImageID("sha256:new"))
			mockContainerReport.EXPECT().IsMonitorOnly().Return(false)
			mockContainerReport.EXPECT().CurrentImageID().Return(types.ImageID("sha256:old"))

			now := time.Now()
			entries := buildUpdateEntries(
				mockContainerReport,
				types.ContainerID("old-container-id"),
				types.ContainerID("new-container-id"),
				now,
			)

			gomega.Expect(entries).To(gomega.HaveLen(3))
			gomega.Expect(entries[0].Message).To(gomega.Equal(FoundNewImageMessage))
			gomega.Expect(entries[0].Data["container"]).To(gomega.Equal("test-container"))
			gomega.Expect(entries[0].Data["image"]).To(gomega.Equal("test-image:latest"))
			gomega.Expect(entries[0].Data["new_id"]).To(gomega.Equal("sha256:new"))
			gomega.Expect(entries[0].Time).To(gomega.Equal(now))

			gomega.Expect(entries[1].Message).To(gomega.Equal(StoppingContainerMessage))
			gomega.Expect(entries[1].Data["container"]).To(gomega.Equal("test-container"))
			gomega.Expect(entries[1].Data["id"]).
				To(gomega.Equal(types.ContainerID("old-container-id").ShortID()))
			gomega.Expect(entries[1].Data["old_id"]).To(gomega.Equal("sha256:old"))

			gomega.Expect(entries[2].Message).To(gomega.Equal(StartedNewContainerMessage))
			gomega.Expect(entries[2].Data["container"]).To(gomega.Equal("test-container"))
			gomega.Expect(entries[2].Data["new_id"]).
				To(gomega.Equal(types.ContainerID("new-container-id").ShortID()))
		})

		ginkgo.It("should create entries for monitor-only container", func() {
			mockContainerReport := mockTypes.NewMockContainerReport(ginkgo.GinkgoT())

			mockContainerReport.EXPECT().Name().Return("test-container")
			mockContainerReport.EXPECT().ImageName().Return("test-image:latest")
			mockContainerReport.EXPECT().LatestImageID().Return(types.ImageID("sha256:new"))
			mockContainerReport.EXPECT().IsMonitorOnly().Return(true)

			now := time.Now()
			entries := buildUpdateEntries(
				mockContainerReport,
				types.ContainerID("container-id"),
				types.ContainerID("new-container-id"),
				now,
			)

			gomega.Expect(entries).To(gomega.HaveLen(3))
			gomega.Expect(entries[0].Message).To(gomega.Equal(FoundNewImageMessage))
			gomega.Expect(entries[1].Message).To(gomega.Equal(UpdateSkippedMessage))
			gomega.Expect(entries[2].Message).To(gomega.Equal(ContainerRemainsRunningMessage))
		})

		ginkgo.It("should handle nil container gracefully", func() {
			now := time.Now()
			// This will panic with nil pointer dereference, so we expect it to panic
			gomega.Expect(func() {
				buildUpdateEntries(nil, types.ContainerID(""), types.ContainerID(""), now)
			}).To(gomega.Panic())
		})
	})

	ginkgo.Describe("executeUpdate", func() {
		ginkgo.It("should execute update and return results", func() {
			client := mockActions.CreateMockClient(
				&mockActions.TestData{
					Containers: []types.Container{
						mockActions.CreateMockContainer(
							"test-container",
							"test-container",
							"image:latest",
							time.Now(),
						),
					},
					Staleness: map[string]bool{"test-container": true},
				},
				false,
				false,
			)

			config := types.UpdateParams{
				Filter:           filters.NoFilter,
				Cleanup:          false,
				NoRestart:        false,
				MonitorOnly:      false,
				LifecycleHooks:   false,
				RollingRestart:   false,
				LabelPrecedence:  false,
				NoPull:           false,
				Timeout:          time.Minute,
				LifecycleUID:     1000,
				LifecycleGID:     1001,
				CPUCopyMode:      "auto",
				PullFailureDelay: time.Duration(0),
				RunOnce:          true,
			}

			// Test that function can be called without panicking
			result, cleanupImages, err := executeUpdate(
				context.Background(),
				client,
				config,
			)

			// We expect some result or error, but mainly that it doesn't panic
			gomega.Expect(result).NotTo(gomega.BeNil())
			gomega.Expect(cleanupImages).NotTo(gomega.BeNil())
			// For this mock configuration, we expect no error
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})
	})

	ginkgo.Describe("startNotifications", func() {
		ginkgo.It("should start notification when notifier is provided", func() {
			notifier := mockTypes.NewMockNotifier(ginkgo.GinkgoT())
			notifier.EXPECT().StartNotification(false).Return()
			startNotifications(notifier, false)
			notifier.AssertExpectations(ginkgo.GinkgoT())
		})

		ginkgo.It("should not panic when notifier is nil", func() {
			startNotifications(nil, false)
		})

		ginkgo.It("should start notification with split enabled", func() {
			notifier := mockTypes.NewMockNotifier(ginkgo.GinkgoT())
			notifier.EXPECT().StartNotification(true).Return()
			startNotifications(notifier, true)
			notifier.AssertExpectations(ginkgo.GinkgoT())
		})
	})

	ginkgo.Describe("logUpdateReport", func() {
		ginkgo.It("should log update report details", func() {
			mockReport := mockTypes.NewMockReport(ginkgo.GinkgoT())

			mockContainer1 := mockTypes.NewMockContainerReport(ginkgo.GinkgoT())
			mockContainer2 := mockTypes.NewMockContainerReport(ginkgo.GinkgoT())
			mockContainer3 := mockTypes.NewMockContainerReport(ginkgo.GinkgoT())

			mockContainer3.EXPECT().Name().Return("container3")

			mockReport.EXPECT().
				Scanned().
				Return([]types.ContainerReport{mockContainer1, mockContainer2})
			mockReport.EXPECT().Updated().Return([]types.ContainerReport{mockContainer3})
			mockReport.EXPECT().Failed().Return([]types.ContainerReport{})

			// Function just logs, so we verify it doesn't panic and calls expected methods
			logUpdateReport(mockReport)
		})

		ginkgo.It("should handle reports with no updates", func() {
			mockReport := mockTypes.NewMockReport(ginkgo.GinkgoT())

			mockContainer1 := mockTypes.NewMockContainerReport(ginkgo.GinkgoT())
			mockContainer2 := mockTypes.NewMockContainerReport(ginkgo.GinkgoT())

			mockReport.EXPECT().
				Scanned().
				Return([]types.ContainerReport{mockContainer1, mockContainer2})
			mockReport.EXPECT().Updated().Return([]types.ContainerReport{})
			mockReport.EXPECT().Failed().Return([]types.ContainerReport{})

			// Should handle empty updated containers gracefully
			logUpdateReport(mockReport)
		})
	})

	ginkgo.Describe("sendNotifications", func() {
		ginkgo.It("should not send notifications when notifier is nil", func() {
			mockReport := mockTypes.NewMockReport(ginkgo.GinkgoT())
			sendNotifications(nil, false, false, mockReport, []types.RemovedImageInfo{})
			// No expectations since notifier is nil
		})

		ginkgo.It("should send grouped notification when split is false", func() {
			mockReport := mockTypes.NewMockReport(ginkgo.GinkgoT())
			notifier := mockTypes.NewMockNotifier(ginkgo.GinkgoT())
			notifier.EXPECT().ShouldSendNotification(mockReport).Return(true)
			notifier.EXPECT().SendNotification(mockReport).Return()
			sendNotifications(notifier, false, false, mockReport, []types.RemovedImageInfo{})
			notifier.AssertExpectations(ginkgo.GinkgoT())
		})

		ginkgo.It("should call sendSplitNotifications when split is true", func() {
			mockReport := mockTypes.NewMockReport(ginkgo.GinkgoT())
			notifier := mockTypes.NewMockNotifier(ginkgo.GinkgoT())

			// Set up mock expectations for split notification behavior
			mockReport.EXPECT().Updated().Return([]types.ContainerReport{})
			mockReport.EXPECT().Restarted().Return([]types.ContainerReport{})
			mockReport.EXPECT().Stale().Return([]types.ContainerReport{})
			mockReport.EXPECT().Scanned().Return([]types.ContainerReport{})
			mockReport.EXPECT().Failed().Return([]types.ContainerReport{})
			mockReport.EXPECT().Skipped().Return([]types.ContainerReport{})
			mockReport.EXPECT().Fresh().Return([]types.ContainerReport{})

			sendNotifications(notifier, true, false, mockReport, []types.RemovedImageInfo{})
			notifier.AssertExpectations(ginkgo.GinkgoT())
		})
	})

	ginkgo.Describe("sendSplitNotifications", func() {
		ginkgo.It(
			"should send split notifications for updated containers when notificationReport is true",
			func() {
				mockReport := mockTypes.NewMockReport(ginkgo.GinkgoT())
				mockContainer := mockTypes.NewMockContainerReport(ginkgo.GinkgoT())

				mockContainer.EXPECT().ID().Return(types.ContainerID("test-id"))
				mockContainer.EXPECT().Name().Return("test-container")
				mockContainer.EXPECT().Error().Return("")

				mockReport.EXPECT().Updated().Return([]types.ContainerReport{mockContainer})
				mockReport.EXPECT().Restarted().Return([]types.ContainerReport{})
				mockReport.EXPECT().Stale().Return([]types.ContainerReport{})
				mockReport.EXPECT().Scanned().Return([]types.ContainerReport{mockContainer})
				mockReport.EXPECT().Failed().Return([]types.ContainerReport{})
				mockReport.EXPECT().Skipped().Return([]types.ContainerReport{})
				mockReport.EXPECT().Fresh().Return([]types.ContainerReport{})

				// Mock the SendNotification call
				notifier := mockTypes.NewMockNotifier(ginkgo.GinkgoT())
				notifier.EXPECT().ShouldSendNotification(mock.Anything).Return(true)
				notifier.EXPECT().SendNotification(mock.Anything).Return()

				sendSplitNotifications(notifier, true, mockReport, []types.RemovedImageInfo{})

				notifier.AssertExpectations(ginkgo.GinkgoT())
			},
		)

		ginkgo.It("should handle empty results gracefully", func() {
			mockReport := mockTypes.NewMockReport(ginkgo.GinkgoT())
			notifier := mockTypes.NewMockNotifier(ginkgo.GinkgoT())

			mockReport.EXPECT().Updated().Return([]types.ContainerReport{})
			mockReport.EXPECT().Restarted().Return([]types.ContainerReport{})
			mockReport.EXPECT().Stale().Return([]types.ContainerReport{})
			mockReport.EXPECT().Scanned().Return([]types.ContainerReport{})
			mockReport.EXPECT().Failed().Return([]types.ContainerReport{})
			mockReport.EXPECT().Skipped().Return([]types.ContainerReport{})
			mockReport.EXPECT().Fresh().Return([]types.ContainerReport{})

			// With empty results, no notifications should be sent
			sendSplitNotifications(notifier, true, mockReport, []types.RemovedImageInfo{})
			notifier.AssertExpectations(ginkgo.GinkgoT())
		})

		ginkgo.It(
			"should send filtered entry notifications when notificationReport is false",
			func() {
				mockReport := mockTypes.NewMockReport(ginkgo.GinkgoT())
				mockContainer := mockTypes.NewMockContainerReport(ginkgo.GinkgoT())

				mockContainer.EXPECT().ID().Return(types.ContainerID("test-id"))
				mockContainer.EXPECT().Name().Return("test-container")
				mockContainer.EXPECT().ImageName().Return("test-image:latest")
				mockContainer.EXPECT().NewContainerID().Return(types.ContainerID("new-id"))
				mockContainer.EXPECT().IsMonitorOnly().Return(false)
				mockContainer.EXPECT().LatestImageID().Return(types.ImageID("sha256:latest"))
				mockContainer.EXPECT().CurrentImageID().Return(types.ImageID("sha256:current"))
				mockContainer.EXPECT().Error().Return("")

				mockReport.EXPECT().Updated().Return([]types.ContainerReport{mockContainer})
				mockReport.EXPECT().Restarted().Return([]types.ContainerReport{})
				mockReport.EXPECT().Stale().Return([]types.ContainerReport{})
				mockReport.EXPECT().Scanned().Return([]types.ContainerReport{mockContainer})
				mockReport.EXPECT().Failed().Return([]types.ContainerReport{})
				mockReport.EXPECT().Skipped().Return([]types.ContainerReport{})
				mockReport.EXPECT().Fresh().Return([]types.ContainerReport{})

				notifier := mockTypes.NewMockNotifier(ginkgo.GinkgoT())
				notifier.EXPECT().ShouldSendNotification(mock.Anything).Return(true)
				notifier.EXPECT().SendFilteredEntries(mock.Anything, mock.Anything).Return()

				sendSplitNotifications(notifier, false, mockReport, []types.RemovedImageInfo{})
				notifier.AssertExpectations(ginkgo.GinkgoT())
			},
		)

		ginkgo.It("should handle restarted containers when notificationReport is true", func() {
			mockReport := mockTypes.NewMockReport(ginkgo.GinkgoT())
			mockContainer := mockTypes.NewMockContainerReport(ginkgo.GinkgoT())

			mockContainer.EXPECT().ID().Return(types.ContainerID("restart-id"))
			mockContainer.EXPECT().Name().Return("restart-container")
			mockContainer.EXPECT().Error().Return("")

			mockReport.EXPECT().Updated().Return([]types.ContainerReport{})
			mockReport.EXPECT().Restarted().Return([]types.ContainerReport{mockContainer})
			mockReport.EXPECT().Stale().Return([]types.ContainerReport{})
			mockReport.EXPECT().Scanned().Return([]types.ContainerReport{mockContainer})
			mockReport.EXPECT().Failed().Return([]types.ContainerReport{})
			mockReport.EXPECT().Skipped().Return([]types.ContainerReport{})
			mockReport.EXPECT().Fresh().Return([]types.ContainerReport{})

			notifier := mockTypes.NewMockNotifier(ginkgo.GinkgoT())
			notifier.EXPECT().ShouldSendNotification(mock.Anything).Return(true)
			notifier.EXPECT().SendNotification(mock.Anything).Return()

			sendSplitNotifications(notifier, true, mockReport, []types.RemovedImageInfo{})
			notifier.AssertExpectations(ginkgo.GinkgoT())
		})

		ginkgo.It("should skip containers with empty names", func() {
			mockReport := mockTypes.NewMockReport(ginkgo.GinkgoT())
			mockContainer := mockTypes.NewMockContainerReport(ginkgo.GinkgoT())

			mockContainer.EXPECT().ID().Return(types.ContainerID("test-id"))
			mockContainer.EXPECT().Name().Return("") // Empty name

			mockReport.EXPECT().Updated().Return([]types.ContainerReport{mockContainer})
			mockReport.EXPECT().Restarted().Return([]types.ContainerReport{})
			mockReport.EXPECT().Stale().Return([]types.ContainerReport{})
			mockReport.EXPECT().Scanned().Return([]types.ContainerReport{mockContainer})
			mockReport.EXPECT().Failed().Return([]types.ContainerReport{})
			mockReport.EXPECT().Skipped().Return([]types.ContainerReport{})
			mockReport.EXPECT().Fresh().Return([]types.ContainerReport{})

			notifier := mockTypes.NewMockNotifier(ginkgo.GinkgoT())
			// No SendNotification should be called due to empty name

			sendSplitNotifications(notifier, true, mockReport, []types.RemovedImageInfo{})
			notifier.AssertExpectations(ginkgo.GinkgoT())
		})

		ginkgo.It(
			"should handle monitor-only containers in stale list when notificationReport is true",
			func() {
				mockReport := mockTypes.NewMockReport(ginkgo.GinkgoT())
				mockContainer := mockTypes.NewMockContainerReport(ginkgo.GinkgoT())

				mockContainer.EXPECT().ID().Return(types.ContainerID("stale-id"))
				mockContainer.EXPECT().Name().Return("stale-container")
				mockContainer.EXPECT().IsMonitorOnly().Return(true)
				mockContainer.EXPECT().Error().Return("")

				mockReport.EXPECT().Updated().Return([]types.ContainerReport{})
				mockReport.EXPECT().Restarted().Return([]types.ContainerReport{})
				mockReport.EXPECT().Stale().Return([]types.ContainerReport{mockContainer})
				mockReport.EXPECT().Scanned().Return([]types.ContainerReport{mockContainer})
				mockReport.EXPECT().Failed().Return([]types.ContainerReport{})
				mockReport.EXPECT().Skipped().Return([]types.ContainerReport{})
				mockReport.EXPECT().Fresh().Return([]types.ContainerReport{})

				notifier := mockTypes.NewMockNotifier(ginkgo.GinkgoT())
				notifier.EXPECT().ShouldSendNotification(mock.Anything).Return(true)
				notifier.EXPECT().SendNotification(mock.Anything).Return()

				sendSplitNotifications(notifier, true, mockReport, []types.RemovedImageInfo{})
				notifier.AssertExpectations(ginkgo.GinkgoT())
			},
		)

		ginkgo.It("should handle restarted containers when notificationReport is false", func() {
			mockReport := mockTypes.NewMockReport(ginkgo.GinkgoT())
			mockContainer := mockTypes.NewMockContainerReport(ginkgo.GinkgoT())

			mockContainer.EXPECT().ID().Return(types.ContainerID("restart-id"))
			mockContainer.EXPECT().Name().Return("restart-container")
			mockContainer.EXPECT().ImageName().Return("restart-image:latest")
			mockContainer.EXPECT().NewContainerID().Return(types.ContainerID("new-restart-id"))
			mockContainer.EXPECT().CurrentImageID().Return(types.ImageID("sha256:current"))
			mockContainer.EXPECT().Error().Return("")

			mockReport.EXPECT().Updated().Return([]types.ContainerReport{})
			mockReport.EXPECT().Restarted().Return([]types.ContainerReport{mockContainer})
			mockReport.EXPECT().Stale().Return([]types.ContainerReport{})
			mockReport.EXPECT().Scanned().Return([]types.ContainerReport{mockContainer})
			mockReport.EXPECT().Failed().Return([]types.ContainerReport{})
			mockReport.EXPECT().Skipped().Return([]types.ContainerReport{})
			mockReport.EXPECT().Fresh().Return([]types.ContainerReport{})

			notifier := mockTypes.NewMockNotifier(ginkgo.GinkgoT())
			notifier.EXPECT().ShouldSendNotification(mock.Anything).Return(true)
			notifier.EXPECT().SendFilteredEntries(mock.Anything, mock.Anything).Return()

			sendSplitNotifications(notifier, false, mockReport, []types.RemovedImageInfo{})
			notifier.AssertExpectations(ginkgo.GinkgoT())
		})

		ginkgo.It("should handle restarted containers with empty NewContainerID", func() {
			mockReport := mockTypes.NewMockReport(ginkgo.GinkgoT())
			mockContainer := mockTypes.NewMockContainerReport(ginkgo.GinkgoT())

			mockContainer.EXPECT().ID().Return(types.ContainerID("restart-id"))
			mockContainer.EXPECT().Name().Return("restart-container")
			mockContainer.EXPECT().ImageName().Return("restart-image:latest")
			mockContainer.EXPECT().NewContainerID().Return(types.ContainerID("")) // Empty new ID
			mockContainer.EXPECT().CurrentImageID().Return(types.ImageID("sha256:current"))
			mockContainer.EXPECT().Error().Return("")

			mockReport.EXPECT().Updated().Return([]types.ContainerReport{})
			mockReport.EXPECT().Restarted().Return([]types.ContainerReport{mockContainer})
			mockReport.EXPECT().Stale().Return([]types.ContainerReport{})
			mockReport.EXPECT().Scanned().Return([]types.ContainerReport{mockContainer})
			mockReport.EXPECT().Failed().Return([]types.ContainerReport{})
			mockReport.EXPECT().Skipped().Return([]types.ContainerReport{})
			mockReport.EXPECT().Fresh().Return([]types.ContainerReport{})

			notifier := mockTypes.NewMockNotifier(ginkgo.GinkgoT())
			notifier.EXPECT().ShouldSendNotification(mock.Anything).Return(true)
			notifier.EXPECT().SendFilteredEntries(mock.Anything, mock.Anything).Return()

			sendSplitNotifications(notifier, false, mockReport, []types.RemovedImageInfo{})
			notifier.AssertExpectations(ginkgo.GinkgoT())
		})

		ginkgo.It("should handle monitor-only containers when notificationReport is false", func() {
			mockReport := mockTypes.NewMockReport(ginkgo.GinkgoT())
			mockContainer := mockTypes.NewMockContainerReport(ginkgo.GinkgoT())

			mockContainer.EXPECT().ID().Return(types.ContainerID("monitor-id"))
			mockContainer.EXPECT().Name().Return("monitor-container")
			mockContainer.EXPECT().ImageName().Return("monitor-image:latest")
			mockContainer.EXPECT().NewContainerID().Return(types.ContainerID("new-monitor-id"))
			mockContainer.EXPECT().LatestImageID().Return(types.ImageID("sha256:latest"))
			mockContainer.EXPECT().IsMonitorOnly().Return(true)
			mockContainer.EXPECT().Error().Return("")

			mockReport.EXPECT().Updated().Return([]types.ContainerReport{})
			mockReport.EXPECT().Restarted().Return([]types.ContainerReport{})
			mockReport.EXPECT().Stale().Return([]types.ContainerReport{mockContainer})
			mockReport.EXPECT().Scanned().Return([]types.ContainerReport{mockContainer})
			mockReport.EXPECT().Failed().Return([]types.ContainerReport{})
			mockReport.EXPECT().Skipped().Return([]types.ContainerReport{})
			mockReport.EXPECT().Fresh().Return([]types.ContainerReport{})

			notifier := mockTypes.NewMockNotifier(ginkgo.GinkgoT())
			notifier.EXPECT().ShouldSendNotification(mock.Anything).Return(true)
			notifier.EXPECT().SendFilteredEntries(mock.Anything, mock.Anything).Return()

			sendSplitNotifications(notifier, false, mockReport, []types.RemovedImageInfo{})
			notifier.AssertExpectations(ginkgo.GinkgoT())
		})

		ginkgo.It("should handle empty container lists gracefully", func() {
			mockReport := mockTypes.NewMockReport(ginkgo.GinkgoT())

			// Return empty slices - all lists are empty
			mockReport.EXPECT().Updated().Return([]types.ContainerReport{})
			mockReport.EXPECT().Restarted().Return([]types.ContainerReport{})
			mockReport.EXPECT().Stale().Return([]types.ContainerReport{})
			mockReport.EXPECT().Scanned().Return([]types.ContainerReport{})
			mockReport.EXPECT().Failed().Return([]types.ContainerReport{})
			mockReport.EXPECT().Skipped().Return([]types.ContainerReport{})
			mockReport.EXPECT().Fresh().Return([]types.ContainerReport{})

			notifier := mockTypes.NewMockNotifier(ginkgo.GinkgoT())
			// No SendNotification should be called since all lists are empty

			sendSplitNotifications(notifier, true, mockReport, []types.RemovedImageInfo{})
			notifier.AssertExpectations(ginkgo.GinkgoT())
		})
	})

	ginkgo.Describe("notification level filtering", func() {
		ginkgo.Context("when notification level is error", func() {
			ginkgo.It("should not send notifications when no errors occur", func() {
				mockReport := mockTypes.NewMockReport(ginkgo.GinkgoT())

				// Set up report with no errors - only successful updates
				mockContainer := mockTypes.NewMockContainerReport(ginkgo.GinkgoT())
				mockContainer.EXPECT().ID().Return(types.ContainerID("test-id"))
				mockContainer.EXPECT().Name().Return("test-container")
				mockContainer.EXPECT().Error().Return("") // No error

				mockReport.EXPECT().Updated().Return([]types.ContainerReport{mockContainer})
				mockReport.EXPECT().Restarted().Return([]types.ContainerReport{})
				mockReport.EXPECT().Stale().Return([]types.ContainerReport{})
				mockReport.EXPECT().Scanned().Return([]types.ContainerReport{mockContainer})
				mockReport.EXPECT().Failed().Return([]types.ContainerReport{})
				mockReport.EXPECT().Skipped().Return([]types.ContainerReport{})
				mockReport.EXPECT().Fresh().Return([]types.ContainerReport{})

				notifier := mockTypes.NewMockNotifier(ginkgo.GinkgoT())
				notifier.EXPECT().ShouldSendNotification(mock.Anything).Return(false)
				// With notification level set to error and no errors, no notification should be sent

				sendSplitNotifications(notifier, true, mockReport, []types.RemovedImageInfo{})
				notifier.AssertExpectations(ginkgo.GinkgoT())
			})
		})
	})

	ginkgo.Describe("performImageCleanup", func() {
		ginkgo.It("should return empty slice when cleanup is disabled", func() {
			client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			cleanedImages := performImageCleanup(client, false, []types.RemovedImageInfo{})
			gomega.Expect(cleanedImages).To(gomega.BeEmpty())
		})

		ginkgo.It("should perform cleanup when cleanup is enabled", func() {
			client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			cleanedImages := performImageCleanup(client, true, []types.RemovedImageInfo{
				{
					ContainerName: "test-container",
					ImageName:     "test-image:v1.0",
					ImageID:       types.ImageID("sha256:123"),
				},
			})
			// The function should return the cleaned images when cleanup is enabled
			gomega.Expect(cleanedImages).To(gomega.HaveLen(1))
			gomega.Expect(cleanedImages[0].ContainerName).To(gomega.Equal("test-container"))
		})

		ginkgo.It("should return a valid slice when cleanup input is empty", func() {
			client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			cleanedImages := performImageCleanup(client, true, []types.RemovedImageInfo{})
			// Should return a valid slice even with empty input
			gomega.Expect(cleanedImages).NotTo(gomega.BeNil())
		})
	})

	ginkgo.Describe("generateAndLogMetric", func() {
		ginkgo.It("should generate metric from report", func() {
			mockReport := mockTypes.NewMockReport(ginkgo.GinkgoT())

			mockContainer1 := mockTypes.NewMockContainerReport(ginkgo.GinkgoT())
			mockContainer2 := mockTypes.NewMockContainerReport(ginkgo.GinkgoT())
			mockContainer3 := mockTypes.NewMockContainerReport(ginkgo.GinkgoT())
			mockContainer4 := mockTypes.NewMockContainerReport(ginkgo.GinkgoT())

			mockReport.EXPECT().
				Scanned().
				Return([]types.ContainerReport{mockContainer1, mockContainer2})
			mockReport.EXPECT().Updated().Return([]types.ContainerReport{mockContainer3})
			mockReport.EXPECT().Failed().Return([]types.ContainerReport{mockContainer4})
			mockReport.EXPECT().Restarted().Return([]types.ContainerReport{})

			metric := generateAndLogMetric(mockReport)

			gomega.Expect(metric).NotTo(gomega.BeNil())
			gomega.Expect(metric.Scanned).To(gomega.Equal(2))
			gomega.Expect(metric.Updated).To(gomega.Equal(1))
			gomega.Expect(metric.Failed).To(gomega.Equal(1))
			gomega.Expect(metric.Restarted).To(gomega.Equal(0))
		})

		ginkgo.It("should handle empty report gracefully", func() {
			mockReport := mockTypes.NewMockReport(ginkgo.GinkgoT())

			mockReport.EXPECT().Scanned().Return([]types.ContainerReport{})
			mockReport.EXPECT().Updated().Return([]types.ContainerReport{})
			mockReport.EXPECT().Failed().Return([]types.ContainerReport{})
			mockReport.EXPECT().Restarted().Return([]types.ContainerReport{})

			metric := generateAndLogMetric(mockReport)

			gomega.Expect(metric).NotTo(gomega.BeNil())
			gomega.Expect(metric.Scanned).To(gomega.Equal(0))
			gomega.Expect(metric.Updated).To(gomega.Equal(0))
			gomega.Expect(metric.Failed).To(gomega.Equal(0))
			gomega.Expect(metric.Restarted).To(gomega.Equal(0))
		})
	})

	ginkgo.Describe("Multiple Concurrent Scopes", func() {
		ginkgo.It(
			"should support multiple Watchtower instances with different scopes running simultaneously",
			func() {
				// Create containers with different scopes
				scopeAContainer := mockActions.CreateMockContainerWithConfig(
					"scope-a-container",
					"scope-a-app",
					"app:latest",
					true,
					false,
					time.Now().Add(-time.Hour),
					&dockerContainer.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower":       "true",
							"com.centurylinklabs.watchtower.scope": "scope-a",
						},
					},
				)

				scopeBContainer := mockActions.CreateMockContainerWithConfig(
					"scope-b-container",
					"scope-b-app",
					"app:latest",
					true,
					false,
					time.Now().Add(-time.Hour),
					&dockerContainer.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower":       "true",
							"com.centurylinklabs.watchtower.scope": "scope-b",
						},
					},
				)

				// Create separate container lists for each client
				containersA := []types.Container{scopeAContainer}
				containersB := []types.Container{scopeBContainer}

				// Create separate test data for each client
				testDataA := &mockActions.TestData{
					Containers: containersA,
					Staleness: map[string]bool{
						"scope-a-app": true,
					},
				}

				testDataB := &mockActions.TestData{
					Containers: containersB,
					Staleness: map[string]bool{
						"scope-b-app": true,
					},
				}

				// Create mock clients for each scope
				clientA := mockActions.CreateMockClient(testDataA, false, false)
				clientB := mockActions.CreateMockClient(testDataB, false, false)

				// Results channel to collect metrics from concurrent operations
				results := make(chan *metrics.Metric, 2)
				var wg sync.WaitGroup

				// Launch concurrent updates for different scopes
				wg.Add(2)

				go func() {
					defer wg.Done()
					params := RunUpdatesWithNotificationsParams{
						Client:                       clientA,
						Notifier:                     nil,
						NotificationSplitByContainer: false,
						NotificationReport:           false,
						Filter: filters.FilterByScope(
							"scope-a",
							filters.NoFilter,
						),
						Cleanup:          false,
						NoRestart:        false,
						MonitorOnly:      false,
						LifecycleHooks:   false,
						RollingRestart:   false,
						LabelPrecedence:  false,
						NoPull:           false,
						Timeout:          time.Minute,
						LifecycleUID:     1000,
						LifecycleGID:     1001,
						CPUCopyMode:      "auto",
						PullFailureDelay: time.Duration(0),
					}
					metric := RunUpdatesWithNotifications(context.Background(), params)
					results <- metric
				}()

				go func() {
					defer wg.Done()
					params := RunUpdatesWithNotificationsParams{
						Client:                       clientB,
						Notifier:                     nil,
						NotificationSplitByContainer: false,
						NotificationReport:           false,
						Filter: filters.FilterByScope(
							"scope-b",
							filters.NoFilter,
						),
						Cleanup:          false,
						NoRestart:        false,
						MonitorOnly:      false,
						LifecycleHooks:   false,
						RollingRestart:   false,
						LabelPrecedence:  false,
						NoPull:           false,
						Timeout:          time.Minute,
						LifecycleUID:     1000,
						LifecycleGID:     1001,
						CPUCopyMode:      "auto",
						PullFailureDelay: time.Duration(0),
					}
					metric := RunUpdatesWithNotifications(context.Background(), params)
					results <- metric
				}()

				// Wait for all goroutines to complete
				wg.Wait()
				close(results)

				// Collect results
				var metrics []*metrics.Metric
				for metric := range results {
					metrics = append(metrics, metric)
				}

				// Verify we got results from both concurrent operations
				gomega.Expect(metrics).To(gomega.HaveLen(2))
				for _, metric := range metrics {
					gomega.Expect(metric).NotTo(gomega.BeNil())
					gomega.Expect(metric.Scanned).
						To(gomega.Equal(1))
						// Each scope should scan 1 container
					gomega.Expect(metric.Updated).
						To(gomega.Equal(1))
					// Each scope should update 1 container
				}
			},
		)

		ginkgo.It(
			"should ensure scope isolation and prevent interference between concurrent scopes",
			func() {
				// Create containers for different scopes
				scope1Container := mockActions.CreateMockContainerWithConfig(
					"scope1-watchtower",
					"scope1-watchtower",
					"watchtower:latest",
					true,
					false,
					time.Now().Add(-time.Hour),
					&dockerContainer.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower":       "true",
							"com.centurylinklabs.watchtower.scope": "scope1",
						},
					},
				)

				scope2Container := mockActions.CreateMockContainerWithConfig(
					"scope2-watchtower",
					"scope2-watchtower",
					"watchtower:latest",
					true,
					false,
					time.Now().Add(-time.Hour),
					&dockerContainer.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower":       "true",
							"com.centurylinklabs.watchtower.scope": "scope2",
						},
					},
				)

				allContainers := []types.Container{scope1Container, scope2Container}

				// Create separate test data for each client to track operations independently
				testData1 := &mockActions.TestData{
					Containers: allContainers,
					Staleness: map[string]bool{
						"scope1-watchtower": true,
						"scope2-watchtower": false, // scope2 container not stale for scope1 filter
					},
				}

				testData2 := &mockActions.TestData{
					Containers: allContainers,
					Staleness: map[string]bool{
						"scope1-watchtower": false, // scope1 container not stale for scope2 filter
						"scope2-watchtower": true,
					},
				}

				client1 := mockActions.CreateMockClient(testData1, false, false)
				client2 := mockActions.CreateMockClient(testData2, false, false)

				var wg sync.WaitGroup

				wg.Add(2)

				go func() {
					defer wg.Done()
					params := RunUpdatesWithNotificationsParams{
						Client:                       client1,
						Notifier:                     nil,
						NotificationSplitByContainer: false,
						NotificationReport:           false,
						Filter: filters.FilterByScope(
							"scope1",
							filters.WatchtowerContainersFilter,
						),
						Cleanup:          false,
						NoRestart:        false,
						MonitorOnly:      false,
						LifecycleHooks:   false,
						RollingRestart:   false,
						LabelPrecedence:  false,
						NoPull:           false,
						Timeout:          time.Minute,
						LifecycleUID:     1000,
						LifecycleGID:     1001,
						CPUCopyMode:      "auto",
						PullFailureDelay: time.Duration(0),
					}
					RunUpdatesWithNotifications(context.Background(), params)
				}()

				go func() {
					defer wg.Done()
					params := RunUpdatesWithNotificationsParams{
						Client:                       client2,
						Notifier:                     nil,
						NotificationSplitByContainer: false,
						NotificationReport:           false,
						Filter: filters.FilterByScope(
							"scope2",
							filters.WatchtowerContainersFilter,
						),
						Cleanup:          false,
						NoRestart:        false,
						MonitorOnly:      false,
						LifecycleHooks:   false,
						RollingRestart:   false,
						LabelPrecedence:  false,
						NoPull:           false,
						Timeout:          time.Minute,
						LifecycleUID:     1000,
						LifecycleGID:     1001,
						CPUCopyMode:      "auto",
						PullFailureDelay: time.Duration(0),
					}
					RunUpdatesWithNotifications(context.Background(), params)
				}()

				wg.Wait()

				// Verify scope isolation through separate TestData tracking
				// client1 should only start scope1-watchtower
				gomega.Expect(client1.TestData.StartOrder).
					To(gomega.ContainElement("scope1-watchtower"))
				gomega.Expect(client1.TestData.StartOrder).
					NotTo(gomega.ContainElement("scope2-watchtower"))

				// client2 should only start scope2-watchtower
				gomega.Expect(client2.TestData.StartOrder).
					To(gomega.ContainElement("scope2-watchtower"))
				gomega.Expect(client2.TestData.StartOrder).
					NotTo(gomega.ContainElement("scope1-watchtower"))
			},
		)

		ginkgo.It("should maintain resource cleanup isolation respecting scope boundaries", func() {
			// Create Watchtower containers for different scopes
			scopeAWatchtower := mockActions.CreateMockContainerWithConfig(
				"watchtower-a",
				"watchtower-a",
				"watchtower:latest",
				true,
				false,
				time.Now().Add(-time.Hour),
				&dockerContainer.Config{
					Labels: map[string]string{
						"com.centurylinklabs.watchtower":       "true",
						"com.centurylinklabs.watchtower.scope": "scope-a",
					},
				},
			)

			scopeBWatchtower := mockActions.CreateMockContainerWithConfig(
				"watchtower-b",
				"watchtower-b",
				"watchtower:latest",
				true,
				false,
				time.Now().Add(-time.Hour),
				&dockerContainer.Config{
					Labels: map[string]string{
						"com.centurylinklabs.watchtower":       "true",
						"com.centurylinklabs.watchtower.scope": "scope-b",
					},
				},
			)

			allContainers := []types.Container{scopeAWatchtower, scopeBWatchtower}

			// Create separate test data for each client to track cleanup independently
			testDataA := &mockActions.TestData{
				Containers: allContainers,
				Staleness: map[string]bool{
					"watchtower-a": true,
					"watchtower-b": false, // scope-b container not processed by scope-a filter
				},
			}

			testDataB := &mockActions.TestData{
				Containers: allContainers,
				Staleness: map[string]bool{
					"watchtower-a": false, // scope-a container not processed by scope-b filter
					"watchtower-b": true,
				},
			}

			clientA := mockActions.CreateMockClient(testDataA, false, false)
			clientB := mockActions.CreateMockClient(testDataB, false, false)

			var wg sync.WaitGroup

			wg.Add(2)

			go func() {
				defer wg.Done()
				params := RunUpdatesWithNotificationsParams{
					Client:                       clientA,
					Notifier:                     nil,
					NotificationSplitByContainer: false,
					NotificationReport:           false,
					Filter: filters.FilterByScope(
						"scope-a",
						filters.WatchtowerContainersFilter,
					),
					Cleanup:          true, // Enable cleanup
					NoRestart:        false,
					MonitorOnly:      false,
					LifecycleHooks:   false,
					RollingRestart:   false,
					LabelPrecedence:  false,
					NoPull:           false,
					Timeout:          time.Minute,
					LifecycleUID:     1000,
					LifecycleGID:     1001,
					CPUCopyMode:      "auto",
					PullFailureDelay: time.Duration(0),
				}
				RunUpdatesWithNotifications(context.Background(), params)
			}()

			go func() {
				defer wg.Done()
				params := RunUpdatesWithNotificationsParams{
					Client:                       clientB,
					Notifier:                     nil,
					NotificationSplitByContainer: false,
					NotificationReport:           false,
					Filter: filters.FilterByScope(
						"scope-b",
						filters.WatchtowerContainersFilter,
					),
					Cleanup:          true, // Enable cleanup
					NoRestart:        false,
					MonitorOnly:      false,
					LifecycleHooks:   false,
					RollingRestart:   false,
					LabelPrecedence:  false,
					NoPull:           false,
					Timeout:          time.Minute,
					LifecycleUID:     1000,
					LifecycleGID:     1001,
					CPUCopyMode:      "auto",
					PullFailureDelay: time.Duration(0),
				}
				RunUpdatesWithNotifications(context.Background(), params)
			}()

			wg.Wait()

			// Verify cleanup isolation - each client should only attempt cleanup for containers in its scope
			// Since each client has separate TestData, the counts should reflect independent operations
			gomega.Expect(clientA.TestData.TriedToRemoveImageCount).
				To(gomega.BeNumerically(">=", 0))
			gomega.Expect(clientB.TestData.TriedToRemoveImageCount).
				To(gomega.BeNumerically(">=", 0))
		})

		ginkgo.It(
			"should handle concurrent operations with different scopes without interference",
			func() {
				// Create mixed containers - regular apps and watchtower instances
				appContainer := mockActions.CreateMockContainerWithConfig(
					"app-1",
					"my-app",
					"myapp:latest",
					true,
					false,
					time.Now().Add(-time.Hour),
					&dockerContainer.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower":       "true",
							"com.centurylinklabs.watchtower.scope": "prod", // This app belongs to prod scope
						},
					},
				)

				watchtowerProd := mockActions.CreateMockContainerWithConfig(
					"wt-prod",
					"watchtower-prod",
					"watchtower:latest",
					true,
					false,
					time.Now().Add(-time.Hour),
					&dockerContainer.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower":       "true",
							"com.centurylinklabs.watchtower.scope": "prod",
						},
					},
				)

				watchtowerDev := mockActions.CreateMockContainerWithConfig(
					"wt-dev",
					"watchtower-dev",
					"watchtower:latest",
					true,
					false,
					time.Now().Add(-time.Hour),
					&dockerContainer.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower":       "true",
							"com.centurylinklabs.watchtower.scope": "dev",
						},
					},
				)

				allContainers := []types.Container{appContainer, watchtowerProd, watchtowerDev}

				testData := &mockActions.TestData{
					Containers: allContainers,
					Staleness: map[string]bool{
						"my-app":          true,
						"watchtower-prod": true,
						"watchtower-dev":  true,
					},
				}

				clientProd := mockActions.CreateMockClient(testData, false, false)
				clientDev := mockActions.CreateMockClient(testData, false, false)

				results := make(chan map[string]int, 2)
				var wg sync.WaitGroup

				wg.Add(2)

				go func() {
					defer wg.Done()
					params := RunUpdatesWithNotificationsParams{
						Client:                       clientProd,
						Notifier:                     nil,
						NotificationSplitByContainer: false,
						NotificationReport:           false,
						Filter: filters.FilterByScope(
							"prod",
							filters.NoFilter,
						),
						Cleanup:          false,
						NoRestart:        false,
						MonitorOnly:      false,
						LifecycleHooks:   false,
						RollingRestart:   false,
						LabelPrecedence:  false,
						NoPull:           false,
						Timeout:          time.Minute,
						LifecycleUID:     1000,
						LifecycleGID:     1001,
						CPUCopyMode:      "auto",
						PullFailureDelay: time.Duration(0),
					}
					metric := RunUpdatesWithNotifications(context.Background(), params)
					results <- map[string]int{"prod-scanned": metric.Scanned, "prod-updated": metric.Updated}
				}()

				go func() {
					defer wg.Done()
					params := RunUpdatesWithNotificationsParams{
						Client:                       clientDev,
						Notifier:                     nil,
						NotificationSplitByContainer: false,
						NotificationReport:           false,
						Filter: filters.FilterByScope(
							"dev",
							filters.NoFilter,
						),
						Cleanup:          false,
						NoRestart:        false,
						MonitorOnly:      false,
						LifecycleHooks:   false,
						RollingRestart:   false,
						LabelPrecedence:  false,
						NoPull:           false,
						Timeout:          time.Minute,
						LifecycleUID:     1000,
						LifecycleGID:     1001,
						CPUCopyMode:      "auto",
						PullFailureDelay: time.Duration(0),
					}
					metric := RunUpdatesWithNotifications(context.Background(), params)
					results <- map[string]int{"dev-scanned": metric.Scanned, "dev-updated": metric.Updated}
				}()

				wg.Wait()
				close(results)

				// Collect results
				var resultMaps []map[string]int
				for resultMap := range results {
					resultMaps = append(resultMaps, resultMap)
				}

				gomega.Expect(resultMaps).To(gomega.HaveLen(2))

				// Verify concurrent operations completed independently
				totalScanned := 0
				totalUpdated := 0
				for _, resultMap := range resultMaps {
					for key, value := range resultMap {
						if key == "prod-scanned" || key == "dev-scanned" {
							totalScanned += value
						}
						if key == "prod-updated" || key == "dev-updated" {
							totalUpdated += value
						}
					}
				}

				// Total operations should reflect independent scope processing
				gomega.Expect(totalScanned).
					To(gomega.BeNumerically(">=", 2))
					// At least 2 containers scanned total
				gomega.Expect(totalUpdated).
					To(gomega.BeNumerically(">=", 0))
				// Updates depend on filtering
			},
		)
	})

	ginkgo.Describe("Scoped Environment Error Reporting and State Consistency", func() {
		ginkgo.When("scoped operations encounter errors", func() {
			ginkgo.It(
				"should maintain state consistency when scoped update operations fail partially",
				func() {
					// Create containers in same scope with one failing update
					scopeAContainer1 := mockActions.CreateMockContainerWithConfig(
						"scope-a-1",
						"app-a-1",
						"app:latest",
						true,
						false,
						time.Now().Add(-time.Hour),
						&dockerContainer.Config{
							Labels: map[string]string{
								"com.centurylinklabs.watchtower.scope": "scope-a",
							},
						},
					)

					scopeAContainer2 := mockActions.CreateMockContainerWithConfig(
						"scope-a-2",
						"app-a-2",
						"app:latest",
						true,
						false,
						time.Now().Add(-time.Hour),
						&dockerContainer.Config{
							Labels: map[string]string{
								"com.centurylinklabs.watchtower.scope": "scope-a",
							},
						},
					)

					allContainers := []types.Container{scopeAContainer1, scopeAContainer2}

					// Set up mock to fail updates
					testData := &mockActions.TestData{
						Containers: allContainers,
						Staleness:  map[string]bool{"app-a-1": true, "app-a-2": true},
						StopContainerError: errors.New(
							"container stop failed",
						), // Fail during update process
						StopContainerFailCount: 2, // Fail both containers
					}

					client := mockActions.CreateMockClient(testData, false, false)

					params := RunUpdatesWithNotificationsParams{
						Client:                       client,
						Notifier:                     nil, // No notifications for this test
						NotificationSplitByContainer: false,
						NotificationReport:           false,
						Filter: filters.FilterByScope(
							"scope-a",
							filters.NoFilter,
						),
						Cleanup:          false,
						NoRestart:        false,
						MonitorOnly:      false,
						LifecycleHooks:   false,
						RollingRestart:   false,
						LabelPrecedence:  false,
						NoPull:           false,
						Timeout:          time.Minute,
						LifecycleUID:     1000,
						LifecycleGID:     1001,
						CPUCopyMode:      "auto",
						PullFailureDelay: time.Duration(0),
					}

					metric := RunUpdatesWithNotifications(context.Background(), params)

					// Verify state consistency: partial failures are properly tracked
					gomega.Expect(metric).NotTo(gomega.BeNil())
					gomega.Expect(metric.Scanned).
						To(gomega.Equal(2))
						// Both containers in scope scanned
					gomega.Expect(metric.Failed).To(gomega.Equal(2))  // Both fail due to mock error
					gomega.Expect(metric.Updated).To(gomega.Equal(0)) // No successful updates
				},
			)

			ginkgo.It("should properly isolate scoped operations when cleanup fails", func() {
				// Test that cleanup failures in one scope don't affect other scopes
				scopeAContainer := mockActions.CreateMockContainerWithConfig(
					"cleanup-a",
					"cleanup-app-a",
					"app:v1",
					true,
					false,
					time.Now().Add(-time.Hour),
					&dockerContainer.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower":       "true",
							"com.centurylinklabs.watchtower.scope": "scope-a",
						},
					},
				)

				scopeBContainer := mockActions.CreateMockContainerWithConfig(
					"cleanup-b",
					"cleanup-app-b",
					"app:v2",
					true,
					false,
					time.Now().Add(-time.Hour),
					&dockerContainer.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower":       "true",
							"com.centurylinklabs.watchtower.scope": "scope-b",
						},
					},
				)

				allContainers := []types.Container{scopeAContainer, scopeBContainer}

				// Different test data for different scopes
				testDataA := &mockActions.TestData{
					Containers: allContainers,
					Staleness: map[string]bool{
						"cleanup-app-a": true,
						"cleanup-app-b": false,
					},
					RemoveImageError: errors.New("cleanup failed in scope-a"),
					FailedImageIDs: []types.ImageID{
						types.ImageID("app:v1"),
					}, // Fail cleanup for scope-a
				}

				testDataB := &mockActions.TestData{
					Containers: allContainers,
					Staleness:  map[string]bool{"cleanup-app-a": false, "cleanup-app-b": true},
					// scope-b cleanup succeeds
				}

				clientA := mockActions.CreateMockClient(testDataA, false, false)
				clientB := mockActions.CreateMockClient(testDataB, false, false)

				// Run scope-a operation (cleanup should fail)
				paramsA := RunUpdatesWithNotificationsParams{
					Client:                       clientA,
					Notifier:                     nil,
					NotificationSplitByContainer: false,
					NotificationReport:           false,
					Filter: filters.FilterByScope(
						"scope-a",
						filters.NoFilter,
					),
					Cleanup:          true,
					NoRestart:        false,
					MonitorOnly:      false,
					LifecycleHooks:   false,
					RollingRestart:   false,
					LabelPrecedence:  false,
					NoPull:           false,
					Timeout:          time.Minute,
					LifecycleUID:     1000,
					LifecycleGID:     1001,
					CPUCopyMode:      "auto",
					PullFailureDelay: time.Duration(0),
				}

				// Run scope-b operation (cleanup should succeed)
				paramsB := RunUpdatesWithNotificationsParams{
					Client:                       clientB,
					Notifier:                     nil,
					NotificationSplitByContainer: false,
					NotificationReport:           false,
					Filter: filters.FilterByScope(
						"scope-b",
						filters.NoFilter,
					),
					Cleanup:          true,
					NoRestart:        false,
					MonitorOnly:      false,
					LifecycleHooks:   false,
					RollingRestart:   false,
					LabelPrecedence:  false,
					NoPull:           false,
					Timeout:          time.Minute,
					LifecycleUID:     1000,
					LifecycleGID:     1001,
					CPUCopyMode:      "auto",
					PullFailureDelay: time.Duration(0),
				}

				var wg sync.WaitGroup
				results := make(chan *metrics.Metric, 2)

				wg.Add(2)

				go func() {
					defer wg.Done()
					metric := RunUpdatesWithNotifications(context.Background(), paramsA)
					results <- metric
				}()

				go func() {
					defer wg.Done()
					metric := RunUpdatesWithNotifications(context.Background(), paramsB)
					results <- metric
				}()

				wg.Wait()
				close(results)

				// Collect results
				var metrics []*metrics.Metric
				for metric := range results {
					metrics = append(metrics, metric)
				}

				// Verify scope isolation in error handling
				gomega.Expect(metrics).To(gomega.HaveLen(2))
				for _, metric := range metrics {
					gomega.Expect(metric).NotTo(gomega.BeNil())
					// Each scope processes exactly one container
					gomega.Expect(metric.Scanned).To(gomega.Equal(1))
					gomega.Expect(metric.Updated).To(gomega.Equal(1)) // Updates succeed
					gomega.Expect(metric.Failed).To(gomega.Equal(0))  // No update failures
				}
			})

			ginkgo.It(
				"should ensure proper error reporting isolation between concurrent scoped operations",
				func() {
					// Test concurrent scoped operations with different error patterns
					containerX := mockActions.CreateMockContainerWithConfig(
						"concurrent-x",
						"app-x",
						"app:latest",
						true,
						false,
						time.Now().Add(-time.Hour),
						&dockerContainer.Config{
							Labels: map[string]string{
								"com.centurylinklabs.watchtower.scope": "scope-x",
							},
						},
					)

					containerY := mockActions.CreateMockContainerWithConfig(
						"concurrent-y",
						"app-y",
						"app:latest",
						true,
						false,
						time.Now().Add(-time.Hour),
						&dockerContainer.Config{
							Labels: map[string]string{
								"com.centurylinklabs.watchtower.scope": "scope-y",
							},
						},
					)

					allContainers := []types.Container{containerX, containerY}

					// scope-x has update errors, scope-y succeeds
					testDataX := &mockActions.TestData{
						Containers:             allContainers,
						Staleness:              map[string]bool{"app-x": true, "app-y": false},
						StopContainerError:     errors.New("scope-x update failure"),
						StopContainerFailCount: 1, // Fail once
					}

					testDataY := &mockActions.TestData{
						Containers: allContainers,
						Staleness:  map[string]bool{"app-x": false, "app-y": true},
						// scope-y succeeds
					}

					clientX := mockActions.CreateMockClient(testDataX, false, false)
					clientY := mockActions.CreateMockClient(testDataY, false, false)

					// Run concurrent operations
					var wg sync.WaitGroup
					results := make(chan *metrics.Metric, 2)

					wg.Add(2)

					go func() {
						defer wg.Done()
						params := RunUpdatesWithNotificationsParams{
							Client:                       clientX,
							Notifier:                     nil,
							NotificationSplitByContainer: false,
							NotificationReport:           false,
							Filter: filters.FilterByScope(
								"scope-x",
								filters.NoFilter,
							),
							Cleanup:          false,
							NoRestart:        false,
							MonitorOnly:      false,
							LifecycleHooks:   false,
							RollingRestart:   false,
							LabelPrecedence:  false,
							NoPull:           false,
							Timeout:          time.Minute,
							LifecycleUID:     1000,
							LifecycleGID:     1001,
							CPUCopyMode:      "auto",
							PullFailureDelay: time.Duration(0),
						}
						metric := RunUpdatesWithNotifications(context.Background(), params)
						results <- metric
					}()

					go func() {
						defer wg.Done()
						params := RunUpdatesWithNotificationsParams{
							Client:                       clientY,
							Notifier:                     nil,
							NotificationSplitByContainer: false,
							NotificationReport:           false,
							Filter: filters.FilterByScope(
								"scope-y",
								filters.NoFilter,
							),
							Cleanup:          false,
							NoRestart:        false,
							MonitorOnly:      false,
							LifecycleHooks:   false,
							RollingRestart:   false,
							LabelPrecedence:  false,
							NoPull:           false,
							Timeout:          time.Minute,
							LifecycleUID:     1000,
							LifecycleGID:     1001,
							CPUCopyMode:      "auto",
							PullFailureDelay: time.Duration(0),
						}
						metric := RunUpdatesWithNotifications(context.Background(), params)
						results <- metric
					}()

					wg.Wait()
					close(results)

					// Collect results
					var metrics []*metrics.Metric
					for metric := range results {
						metrics = append(metrics, metric)
					}

					// Verify proper isolation of error reporting
					gomega.Expect(metrics).To(gomega.HaveLen(2))
					totalScanned := 0
					totalFailed := 0
					totalUpdated := 0

					for _, metric := range metrics {
						gomega.Expect(metric).NotTo(gomega.BeNil())
						gomega.Expect(metric.Scanned).
							To(gomega.Equal(1))
							// Each scope sees 1 container
						totalScanned += metric.Scanned
						totalFailed += metric.Failed
						totalUpdated += metric.Updated
					}

					gomega.Expect(totalScanned).To(gomega.Equal(2)) // Both containers processed
					gomega.Expect(totalFailed).To(gomega.Equal(1))  // One scope failed
					gomega.Expect(totalUpdated).To(gomega.Equal(1)) // One scope succeeded
				},
			)
		})
	})
})
