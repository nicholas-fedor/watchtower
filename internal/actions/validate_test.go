package actions

import (
	"errors"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	mockContainer "github.com/nicholas-fedor/watchtower/pkg/container/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	mockContainerTypes "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

var _ = ginkgo.Describe("ValidateRollingRestartDependencies", func() {
	var (
		mockClient *mockContainer.MockClient
		filter     types.Filter
	)

	ginkgo.BeforeEach(func() {
		mockClient = mockContainer.NewMockClient(ginkgo.GinkgoT())
		// Create a simple filter that accepts all containers
		filter = func(types.FilterableContainer) bool { return true }
	})

	ginkgo.When("ListContainers fails", func() {
		ginkgo.It("should return wrapped error", func() {
			expectedErr := errors.New("list containers failed")
			mockClient.EXPECT().ListContainers(mock.Anything).Return(nil, expectedErr)

			err := ValidateRollingRestartDependencies(mockClient, filter)

			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err).
				To(gomega.MatchError(gomega.ContainSubstring("list containers failed")))
		})
	})

	ginkgo.When("no containers are found", func() {
		ginkgo.It("should return nil", func() {
			mockClient.EXPECT().ListContainers(mock.Anything).Return([]types.Container{}, nil)

			err := ValidateRollingRestartDependencies(mockClient, filter)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})
	})

	ginkgo.When("containers have no links", func() {
		ginkgo.It("should return nil", func() {
			mockContainer1 := mockContainerTypes.NewMockContainer(ginkgo.GinkgoT())
			mockContainer2 := mockContainerTypes.NewMockContainer(ginkgo.GinkgoT())

			mockContainer1.EXPECT().Links().Return([]string{})
			mockContainer2.EXPECT().Links().Return([]string{})

			mockClient.EXPECT().ListContainers(mock.Anything).Return([]types.Container{
				mockContainer1,
				mockContainer2,
			}, nil)

			err := ValidateRollingRestartDependencies(mockClient, filter)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})
	})

	ginkgo.When("a container has links", func() {
		ginkgo.It("should return error with container name and links", func() {
			mockContainer1 := mockContainerTypes.NewMockContainer(ginkgo.GinkgoT())
			mockContainer2 := mockContainerTypes.NewMockContainer(ginkgo.GinkgoT())

			mockContainer1.EXPECT().Links().Return([]string{})
			mockContainer2.EXPECT().Links().Return([]string{"db", "redis"})
			mockContainer2.EXPECT().Name().Return("web-container")

			mockClient.EXPECT().ListContainers(mock.Anything).Return([]types.Container{
				mockContainer1,
				mockContainer2,
			}, nil)

			err := ValidateRollingRestartDependencies(mockClient, filter)

			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring(`"web-container" depends on [db redis]`))
		})
	})
})
