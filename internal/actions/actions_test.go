package actions

import (
	"context"
	"errors"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	dockerContainer "github.com/docker/docker/api/types/container"

	mockActions "github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/filters"
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
			cleanedImages := []types.CleanedImageInfo{
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
			cleanedImages := []types.CleanedImageInfo{
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

			config := UpdateConfig{
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
			result, cleanupImages, err := executeUpdate(context.Background(), client, config)

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
	})

	ginkgo.Describe("sendNotifications", func() {
		ginkgo.It("should not send notifications when notifier is nil", func() {
			mockReport := mockTypes.NewMockReport(ginkgo.GinkgoT())
			sendNotifications(nil, false, false, mockReport, []types.CleanedImageInfo{})
			// No expectations since notifier is nil
		})

		ginkgo.It("should send grouped notification when split is false", func() {
			mockReport := mockTypes.NewMockReport(ginkgo.GinkgoT())
			notifier := mockTypes.NewMockNotifier(ginkgo.GinkgoT())
			notifier.EXPECT().SendNotification(mockReport).Return()
			sendNotifications(notifier, false, false, mockReport, []types.CleanedImageInfo{})
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

			sendNotifications(notifier, true, false, mockReport, []types.CleanedImageInfo{})
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
				notifier.EXPECT().SendNotification(mock.Anything).Return()

				sendSplitNotifications(notifier, true, mockReport, []types.CleanedImageInfo{})

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
			sendSplitNotifications(notifier, true, mockReport, []types.CleanedImageInfo{})
			notifier.AssertExpectations(ginkgo.GinkgoT())
		})
	})

	ginkgo.Describe("performImageCleanup", func() {
		ginkgo.It("should return empty slice when cleanup is disabled", func() {
			client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			cleanedImages := performImageCleanup(client, false, []types.CleanedImageInfo{})
			gomega.Expect(cleanedImages).To(gomega.BeEmpty())
		})

		ginkgo.It("should perform cleanup when cleanup is enabled", func() {
			client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			cleanedImages := performImageCleanup(client, true, []types.CleanedImageInfo{
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
	})
})
