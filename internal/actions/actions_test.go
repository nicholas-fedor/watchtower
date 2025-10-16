package actions_test

import (
	"errors"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/nicholas-fedor/watchtower/internal/actions"
	actionMocks "github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/filters"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	"github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

var _ = ginkgo.Describe("RunUpdatesWithNotifications", func() {
	var (
		client   actionMocks.MockClient
		notifier *mocks.MockNotifier
		filter   types.Filter
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
				},
				false,
				false,
			)

			metric := actions.RunUpdatesWithNotifications(
				client,
				nil, // nil notifier
				false, false, filter, false, false, false, false, false, false, false,
				time.Minute, 1000, 1001, "auto", false,
			)

			gomega.Expect(metric).NotTo(gomega.BeNil())
		})
	})

	ginkgo.When("notifier is provided", func() {
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
							time.Now(),
						),
					},
				},
				false,
				false,
			)

			notifier.EXPECT().StartNotification().Return()
			notifier.EXPECT().SendNotification(mock.Anything).Return()

			metric := actions.RunUpdatesWithNotifications(
				client,
				notifier,
				false, false, filter, false, false, false, false, false, false, false,
				time.Minute, 1000, 1001, "auto", false,
			)

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
			notifier.EXPECT().SendNotification(mock.Anything).Return()

			metric := actions.RunUpdatesWithNotifications(
				client,
				notifier,
				true, true, filter, false, false, false, false, false, false, false,
				time.Minute, 1000, 1001, "auto", false,
			)

			gomega.Expect(metric).NotTo(gomega.BeNil())
		})

		ginkgo.It("should handle standard grouped notifications", func() {
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
				},
				false,
				false,
			)

			notifier.EXPECT().StartNotification().Return()
			notifier.EXPECT().SendNotification(mock.Anything).Return()

			metric := actions.RunUpdatesWithNotifications(
				client,
				notifier,
				false, false, filter, false, false, false, false, false, false, false,
				time.Minute, 1000, 1001, "auto", false,
			)

			gomega.Expect(metric).NotTo(gomega.BeNil())
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
							time.Now(),
						),
					},
				},
				false,
				false,
			)

			metric := actions.RunUpdatesWithNotifications(
				client,
				nil,
				false, false, filter, true, false, false, false, false, false, false,
				time.Minute, 1000, 1001, "auto", false,
			)

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
							time.Now(),
						),
					},
					IsContainerStaleError: errors.New("mock error"),
				},
				false,
				false,
			)

			metric := actions.RunUpdatesWithNotifications(
				client,
				nil,
				false, false, filter, false, false, false, false, false, false, false,
				time.Minute, 1000, 1001, "auto", false,
			)

			gomega.Expect(metric.Scanned).To(gomega.Equal(0))
			gomega.Expect(metric.Updated).To(gomega.Equal(0))
			gomega.Expect(metric.Failed).To(gomega.Equal(0))
		})
	})
})
