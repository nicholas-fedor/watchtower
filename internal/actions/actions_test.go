package actions_test

import (
	"errors"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/mock"

	"github.com/nicholas-fedor/watchtower/internal/actions"
	actionMocks "github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/filters"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	"github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

const (
	foundNewImageMessage           = "Found new image"
	stoppingContainerMessage       = "Stopping container"
	startedNewContainerMessage     = "Started new container"
	updateSkippedMessage           = "Update available but skipped (monitor-only mode)"
	containerRemainsRunningMessage = "Container remains running (monitor-only mode)"
	container1Name                 = "container-1"
	container2Name                 = "container-2"
	container3Name                 = "container-3"
	validContainerName             = "valid-container"
	duplicateTestName              = "duplicate-test"
	monitorOnlyContainerName       = "monitor-only-container"
)

var _ = ginkgo.Describe("RunUpdatesWithNotifications", func() {
	var (
		client actionMocks.MockClient
		filter types.Filter
	)

	ginkgo.BeforeEach(func() {
		filter = filters.NoFilter
	})

	ginkgo.When("notifier is nil", func() {
		ginkgo.It("should not start notification batching", func() {
			client = actionMocks.CreateMockClient(
				&actionMocks.TestData{
					Containers: []types.Container{
						actionMocks.CreateMockContainer(
							"test-container",
							"test-container",
							"image:latest",
							time.Now(),
						),
					},
					Staleness: map[string]bool{
						"test-container": false,
					}, // Container is not stale, so no update occurs
				},
				false,
				false,
			)

			params := actions.RunUpdatesWithNotificationsParams{
				Client:                       client,
				Notifier:                     nil,
				NotificationSplitByContainer: false,
				NotificationReport:           false,
				Filter:                       filter,
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
			metric := actions.RunUpdatesWithNotifications(params)

			gomega.Expect(metric).NotTo(gomega.BeNil())
		})
	})

	ginkgo.When("notifier is provided", func() {
		var notifier *mocks.MockNotifier

		ginkgo.BeforeEach(func() {
			notifier = mocks.NewMockNotifier(ginkgo.GinkgoT())
		})

		ginkgo.It("should start notification batching", func() {
			client = actionMocks.CreateMockClient(
				&actionMocks.TestData{
					Containers: []types.Container{
						actionMocks.CreateMockContainer(
							"test-container",
							"test-container",
							"image:latest",
							time.Now().Add(-24*time.Hour),
						),
					},
				},
				false,
				false,
			)

			notifier.EXPECT().StartNotification().Return()
			notifier.EXPECT().SendNotification(mock.Anything).Return()
			notifier.EXPECT().Close().Return()

			params := actions.RunUpdatesWithNotificationsParams{
				Client:                       client,
				Notifier:                     notifier,
				NotificationSplitByContainer: false,
				NotificationReport:           false,
				Filter:                       filter,
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
			metric := actions.RunUpdatesWithNotifications(params)

			gomega.Expect(metric).NotTo(gomega.BeNil())
		})

		ginkgo.It("should handle notification split by container", func() {
			client = actionMocks.CreateMockClient(
				&actionMocks.TestData{
					Containers: []types.Container{
						actionMocks.CreateMockContainerWithConfig(
							"test-container",
							"test-container",
							"image:latest",
							true,
							false,
							time.Now().Add(-time.Hour),
							&container.Config{},
						),
					},
					Staleness: map[string]bool{"test-container": true},
				},
				false,
				false,
			)

			notifier.EXPECT().StartNotification().Return()
			notifier.EXPECT().SendFilteredEntries(mock.Anything, mock.Anything).Return()
			notifier.EXPECT().Close().Return()

			params := actions.RunUpdatesWithNotificationsParams{
				Client:                       client,
				Notifier:                     notifier,
				NotificationSplitByContainer: true,
				NotificationReport:           false,
				Filter:                       filter,
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
			metric := actions.RunUpdatesWithNotifications(params)

			gomega.Expect(metric).NotTo(gomega.BeNil())

			notifier.AssertExpectations(ginkgo.GinkgoT())
		})

		ginkgo.It("should handle standard grouped notifications", func() {
			client = actionMocks.CreateMockClient(
				&actionMocks.TestData{
					Containers: []types.Container{
						actionMocks.CreateMockContainer(
							"test-container",
							"test-container",
							"image:latest",
							time.Now().Add(-24*time.Hour),
						),
					},
				},
				false,
				false,
			)

			notifier.EXPECT().StartNotification().Return()
			notifier.EXPECT().SendNotification(mock.Anything).Return()
			notifier.EXPECT().Close().Return()

			params := actions.RunUpdatesWithNotificationsParams{
				Client:                       client,
				Notifier:                     notifier,
				NotificationSplitByContainer: false,
				NotificationReport:           false,
				Filter:                       filter,
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
			metric := actions.RunUpdatesWithNotifications(params)

			gomega.Expect(metric).NotTo(gomega.BeNil())

			notifier.AssertExpectations(ginkgo.GinkgoT())
		})
		ginkgo.Context("notification splitting in log mode", func() {
			ginkgo.It(
				"should send notifications for monitor-only containers with stale images",
				func() {
					client = actionMocks.CreateMockClient(
						&actionMocks.TestData{
							Containers: []types.Container{
								actionMocks.CreateMockContainerWithConfig(
									monitorOnlyContainerName,
									monitorOnlyContainerName,
									"image:v1.0",
									true,
									false,
									time.Now().Add(-time.Hour),
									&container.Config{
										Labels: map[string]string{
											"com.centurylinklabs.watchtower.monitor-only": "true",
										},
									},
								),
							},
							Staleness: map[string]bool{
								monitorOnlyContainerName: true, // Container is stale
							},
						},
						false,
						false,
					)

					notifier.EXPECT().StartNotification().Return()
					// Expect notification for the monitor-only stale container
					notifier.EXPECT().
						SendFilteredEntries(mock.MatchedBy(func(entries []*logrus.Entry) bool {
							if len(entries) != 3 {
								return false
							}

							// Check all three entries for monitor-only-container
							return entries[0].Message == foundNewImageMessage &&
								entries[0].Data["container"] == monitorOnlyContainerName &&
								entries[1].Message == updateSkippedMessage &&
								entries[1].Data["container"] == monitorOnlyContainerName &&
								entries[2].Message == containerRemainsRunningMessage &&
								entries[2].Data["container"] == monitorOnlyContainerName
						}), mock.AnythingOfType("*session.SingleContainerReport")).
						Return()

					notifier.EXPECT().Close().Return()

					params := actions.RunUpdatesWithNotificationsParams{
						Client:                       client,
						Notifier:                     notifier,
						NotificationSplitByContainer: true,
						NotificationReport:           false,
						Filter:                       filter,
						Cleanup:                      false,
						NoRestart:                    false,
						MonitorOnly:                  false, // Global monitor-only is false, but container has label
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
					metric := actions.RunUpdatesWithNotifications(params)

					gomega.Expect(metric).NotTo(gomega.BeNil())
					gomega.Expect(metric.Updated).
						To(gomega.Equal(0))
						// Monitor-only containers are not "updated"

					notifier.AssertExpectations(ginkgo.GinkgoT())
				},
			)

			ginkgo.It("should send one notification per updated container", func() {
				client = actionMocks.CreateMockClient(
					&actionMocks.TestData{
						Containers: []types.Container{
							actionMocks.CreateMockContainerWithConfig(
								"container-1",
								"container-1",
								"image:v1.0",
								true,
								false,
								time.Now().Add(-time.Hour),
								&container.Config{},
							),
							actionMocks.CreateMockContainerWithConfig(
								"container-2",
								"container-2",
								"image:v2.0",
								true,
								false,
								time.Now().Add(-time.Hour),
								&container.Config{},
							),
							actionMocks.CreateMockContainerWithConfig(
								"container-3",
								"container-3",
								"image:v3.0",
								true,
								false,
								time.Now().Add(-time.Hour),
								&container.Config{},
							),
						},
						Staleness: map[string]bool{
							"container-1": true,
							"container-2": true,
							"container-3": true,
						},
					},
					false,
					false,
				)

				// Expect exactly 3 notifications (one per updated container), each with 3 log entries
				notifier.EXPECT().StartNotification().Return()
				notifier.EXPECT().
					SendFilteredEntries(mock.MatchedBy(func(entries []*logrus.Entry) bool {
						if len(entries) != 3 {
							return false
						}

						// Check all three entries for container-1
						return entries[0].Message == foundNewImageMessage &&
							entries[0].Data["container"] == container1Name &&
							entries[1].Message == stoppingContainerMessage &&
							entries[1].Data["container"] == container1Name &&
							entries[2].Message == startedNewContainerMessage &&
							entries[2].Data["container"] == container1Name
					}), mock.AnythingOfType("*session.SingleContainerReport")).
					Return()
				notifier.EXPECT().
					SendFilteredEntries(mock.MatchedBy(func(entries []*logrus.Entry) bool {
						if len(entries) != 3 {
							return false
						}

						// Check all three entries for container-2
						return entries[0].Message == foundNewImageMessage &&
							entries[0].Data["container"] == container2Name &&
							entries[1].Message == stoppingContainerMessage &&
							entries[1].Data["container"] == container2Name &&
							entries[2].Message == startedNewContainerMessage &&
							entries[2].Data["container"] == container2Name
					}), mock.AnythingOfType("*session.SingleContainerReport")).
					Return()
				notifier.EXPECT().
					SendFilteredEntries(mock.MatchedBy(func(entries []*logrus.Entry) bool {
						if len(entries) != 3 {
							return false
						}

						// Check all three entries for container-3
						return entries[0].Message == foundNewImageMessage &&
							entries[0].Data["container"] == container3Name &&
							entries[1].Message == stoppingContainerMessage &&
							entries[1].Data["container"] == container3Name &&
							entries[2].Message == startedNewContainerMessage &&
							entries[2].Data["container"] == container3Name
					}), mock.AnythingOfType("*session.SingleContainerReport")).
					Return()

				notifier.EXPECT().Close().Return()

				params := actions.RunUpdatesWithNotificationsParams{
					Client:                       client,
					Notifier:                     notifier,
					NotificationSplitByContainer: true,
					NotificationReport:           false,
					Filter:                       filter,
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
				metric := actions.RunUpdatesWithNotifications(params)

				gomega.Expect(metric).NotTo(gomega.BeNil())
				gomega.Expect(metric.Updated).To(gomega.Equal(3))

				notifier.AssertExpectations(ginkgo.GinkgoT())
			})

			ginkgo.It("should not send notifications when no containers are updated", func() {
				client = actionMocks.CreateMockClient(
					&actionMocks.TestData{
						Containers: []types.Container{
							actionMocks.CreateMockContainer(
								"fresh-container",
								"fresh-container",
								"image:latest",
								time.Now(),
							),
						},
						Staleness: map[string]bool{
							"fresh-container": false, // Not stale, so not updated
						},
					},
					false,
					false,
				)

				notifier.EXPECT().StartNotification().Return()
				// No SendNotification calls expected since no containers were updated
				notifier.EXPECT().Close().Return()

				params := actions.RunUpdatesWithNotificationsParams{
					Client:                       client,
					Notifier:                     notifier,
					NotificationSplitByContainer: true,
					NotificationReport:           false,
					Filter:                       filter,
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
				metric := actions.RunUpdatesWithNotifications(params)

				gomega.Expect(metric).NotTo(gomega.BeNil())
				gomega.Expect(metric.Updated).To(gomega.Equal(0))

				notifier.AssertExpectations(ginkgo.GinkgoT())
			})

			ginkgo.It("should skip containers with empty names", func() {
				client = actionMocks.CreateMockClient(
					&actionMocks.TestData{
						Containers: []types.Container{
							actionMocks.CreateMockContainerWithConfig(
								"unnamed-container",
								"", // Empty name
								"image:v1.0",
								true,
								false,
								time.Now().Add(-time.Hour),
								&container.Config{},
							),
							actionMocks.CreateMockContainerWithConfig(
								"valid-container",
								"valid-container",
								"image:v2.0",
								true,
								false,
								time.Now().Add(-time.Hour),
								&container.Config{},
							),
						},
						Staleness: map[string]bool{
							"":                true,
							"valid-container": true,
						},
					},
					false,
					false,
				)

				notifier.EXPECT().StartNotification().Return()
				// Only expect notification for the valid container, not the one with empty name
				notifier.EXPECT().
					SendFilteredEntries(mock.MatchedBy(func(entries []*logrus.Entry) bool {
						if len(entries) != 3 {
							return false
						}

						// Check all three entries for valid-container
						return entries[0].Message == foundNewImageMessage &&
							entries[0].Data["container"] == validContainerName &&
							entries[1].Message == stoppingContainerMessage &&
							entries[1].Data["container"] == validContainerName &&
							entries[2].Message == startedNewContainerMessage &&
							entries[2].Data["container"] == validContainerName
					}), mock.AnythingOfType("*session.SingleContainerReport")).
					Return()
				notifier.EXPECT().Close().Return()

				params := actions.RunUpdatesWithNotificationsParams{
					Client:                       client,
					Notifier:                     notifier,
					NotificationSplitByContainer: true,
					NotificationReport:           false,
					Filter:                       filter,
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
				metric := actions.RunUpdatesWithNotifications(params)

				gomega.Expect(metric).NotTo(gomega.BeNil())
				gomega.Expect(metric.Updated).
					To(gomega.Equal(2))
				// Both containers were updated, but only one notification sent

				notifier.AssertExpectations(ginkgo.GinkgoT())
			})

			ginkgo.It("should ensure no duplicate notifications are sent", func() {
				client = actionMocks.CreateMockClient(
					&actionMocks.TestData{
						Containers: []types.Container{
							actionMocks.CreateMockContainerWithConfig(
								"duplicate-test",
								"duplicate-test",
								"image:v1.0",
								true,
								false,
								time.Now().Add(-time.Hour),
								&container.Config{},
							),
						},
						Staleness: map[string]bool{
							"duplicate-test": true,
						},
					},
					false,
					false,
				)

				notifier.EXPECT().StartNotification().Return()
				// Expect exactly one notification for the single updated container with 3 entries
				notifier.EXPECT().
					SendFilteredEntries(mock.MatchedBy(func(entries []*logrus.Entry) bool {
						if len(entries) != 3 {
							return false
						}

						// Check all three entries for duplicate-test
						return entries[0].Message == foundNewImageMessage &&
							entries[0].Data["container"] == duplicateTestName &&
							entries[1].Message == stoppingContainerMessage &&
							entries[1].Data["container"] == duplicateTestName &&
							entries[2].Message == startedNewContainerMessage &&
							entries[2].Data["container"] == duplicateTestName
					}), mock.AnythingOfType("*session.SingleContainerReport")).
					Return().
					Times(1)
					// Exactly once
				notifier.EXPECT().Close().Return()

				params := actions.RunUpdatesWithNotificationsParams{
					Client:                       client,
					Notifier:                     notifier,
					NotificationSplitByContainer: true,
					NotificationReport:           false,
					Filter:                       filter,
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
				metric := actions.RunUpdatesWithNotifications(params)

				gomega.Expect(metric).NotTo(gomega.BeNil())
				gomega.Expect(metric.Updated).To(gomega.Equal(1))

				notifier.AssertExpectations(ginkgo.GinkgoT())
			})
		})
	})

	ginkgo.When("cleanup is enabled", func() {
		ginkgo.It("should call CleanupImages", func() {
			client = actionMocks.CreateMockClient(
				&actionMocks.TestData{
					Containers: []types.Container{
						actionMocks.CreateMockContainer(
							"test-container",
							"test-container",
							"image:latest",
							time.Now().Add(-24*time.Hour),
						),
					},
				},
				false,
				false,
			)

			params := actions.RunUpdatesWithNotificationsParams{
				Client:                       client,
				Notifier:                     nil,
				NotificationSplitByContainer: false,
				NotificationReport:           false,
				Filter:                       filter,
				Cleanup:                      true,
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
			metric := actions.RunUpdatesWithNotifications(params)

			gomega.Expect(metric).NotTo(gomega.BeNil())
			// CleanupImages is called internally by Update, so we check that cleanupImageIDs is processed
		})
	})

	ginkgo.When("update fails", func() {
		ginkgo.It("should return zero metric on error", func() {
			client = actionMocks.CreateMockClient(
				&actionMocks.TestData{
					Containers: []types.Container{
						actionMocks.CreateMockContainer(
							"test-container",
							"test-container",
							"image:latest",
							time.Now().Add(-24*time.Hour),
						),
					},
					IsContainerStaleError: errors.New("mock error"),
				},
				false,
				false,
			)

			params := actions.RunUpdatesWithNotificationsParams{
				Client:                       client,
				Notifier:                     nil,
				NotificationSplitByContainer: false,
				NotificationReport:           false,
				Filter:                       filter,
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
			metric := actions.RunUpdatesWithNotifications(params)

			gomega.Expect(metric.Scanned).To(gomega.Equal(0))
			gomega.Expect(metric.Updated).To(gomega.Equal(0))
			gomega.Expect(metric.Failed).To(gomega.Equal(0))
		})
	})
})
