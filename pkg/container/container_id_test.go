package container

import (
	"context"
	"errors"
	"os"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	dockerContainer "github.com/docker/docker/api/types/container"
	dockerImage "github.com/docker/docker/api/types/image"

	mockContainer "github.com/nicholas-fedor/watchtower/pkg/container/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	mockTypes "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

const testHostname = "test-hostname"

// Static error for test.
var (
	ErrMockedFileRead          = errors.New("mocked file read error")
	ErrMockedMountinfoFileRead = errors.New("mocked mountinfo file read error")
)

var _ = ginkgo.Describe("GetContainerIDFromCgroupFile", func() {
	var originalReadFileFunc func(string) ([]byte, error)

	ginkgo.BeforeEach(func() {
		originalReadFileFunc = ReadCgroupFunc
	})

	ginkgo.AfterEach(func() {
		ReadCgroupFunc = originalReadFileFunc
	})

	ginkgo.DescribeTable("should return the correct container ID",
		func(setup func(), want types.ContainerID, wantErr error) {
			if setup != nil {
				setup()
			}

			got, err := GetContainerIDFromCgroupFile()
			if wantErr == nil {
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			} else {
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(errors.Is(err, wantErr)).To(gomega.BeTrue())
			}

			gomega.Expect(got).To(gomega.Equal(want))
		},
		ginkgo.Entry("SuccessWithValidID", func() {
			ReadCgroupFunc = func(string) ([]byte, error) {
				return []byte(
					"11:perf_event:/docker/1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				), nil
			}
		}, types.ContainerID(
			"1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		), nil),
		ginkgo.Entry("FileNotReadable", func() {
			ReadCgroupFunc = func(string) ([]byte, error) {
				return nil, ErrMockedFileRead
			}
		}, types.ContainerID(""), errReadCgroupFile),
		ginkgo.Entry("NoValidID", func() {
			ReadCgroupFunc = func(string) ([]byte, error) {
				return []byte("11:perf_event:/user.slice\n10:cpu:/system.slice"), nil
			}
		}, types.ContainerID(""), errExtractContainerID),
		ginkgo.Entry("EmptyFileContent", func() {
			ReadCgroupFunc = func(string) ([]byte, error) {
				return []byte(""), nil
			}
		}, types.ContainerID(""), errExtractContainerID),
	)
})

var _ = ginkgo.Describe("ParseContainerIDFromCgroupString", func() {
	ginkgo.DescribeTable("should extract container ID from string",
		func(s string, want types.ContainerID, wantErr error) {
			got, err := ParseContainerIDFromCgroupString(s)
			if wantErr == nil {
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			} else {
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(errors.Is(err, wantErr)).To(gomega.BeTrue())
			}

			gomega.Expect(got).To(gomega.Equal(want))
		},
		ginkgo.Entry(
			"ValidDockerContainerIDSingleLine",
			"11:perf_event:/docker/1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			types.ContainerID(
				"1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			),
			nil,
		),
		ginkgo.Entry(
			"ValidDockerContainerIDMultiLine",
			"12:memory:/user.slice\n11:perf_event:/docker/abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567800\n10:cpu:/system.slice",
			types.ContainerID(
				"abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567800",
			),
			nil,
		),
		ginkgo.Entry("NoDockerPatternMultiLine",
			"11:perf_event:/user.slice\n10:cpu:/system.slice",
			types.ContainerID(""),
			errNoValidContainerID,
		),
		ginkgo.Entry("EmptyString",
			"",
			types.ContainerID(""),
			errNoValidContainerID,
		),
		ginkgo.Entry("InvalidIDLength",
			"11:perf_event:/docker/12345678",
			types.ContainerID(""),
			errNoValidContainerID,
		),
		ginkgo.Entry(
			"NonHexID",
			"11:perf_event:/docker/gggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggg",
			types.ContainerID(""),
			errNoValidContainerID,
		),
		ginkgo.Entry(
			"ValidIDWithExtraLines",
			"11:perf_event:/docker/1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef\n10:cpu:/system.slice",
			types.ContainerID(
				"1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			),
			nil,
		),
		ginkgo.Entry("NoDockerPatternSingleLine",
			"11:perf_event:/user.slice",
			types.ContainerID(""),
			errNoValidContainerID,
		),
	)
})

var _ = ginkgo.Describe("GetContainerIDFromMountinfo", func() {
	var originalReadMountinfoFunc func(string) ([]byte, error)

	ginkgo.BeforeEach(func() {
		originalReadMountinfoFunc = ReadMountinfoFunc
	})

	ginkgo.AfterEach(func() {
		ReadMountinfoFunc = originalReadMountinfoFunc
	})

	ginkgo.DescribeTable("should return the correct container ID",
		func(setup func(), want types.ContainerID, wantErr error) {
			if setup != nil {
				setup()
			}

			got, err := GetContainerIDFromMountinfo()
			if wantErr == nil {
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			} else {
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(errors.Is(err, wantErr)).To(gomega.BeTrue())
			}

			gomega.Expect(got).To(gomega.Equal(want))
		},
		ginkgo.Entry("SuccessWithValidIDInRoot", func() {
			ReadMountinfoFunc = func(string) ([]byte, error) {
				return []byte(
					"36 35 98:0 /var/lib/docker/containers/1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef/resolv.conf /etc/resolv.conf rw,noatime - btrfs /dev/sda1 rw",
				), nil
			}
		}, types.ContainerID(
			"1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		), nil),
		ginkgo.Entry("SuccessWithValidIDInMountPoint", func() {
			ReadMountinfoFunc = func(string) ([]byte, error) {
				return []byte(
					"36 35 98:0 /var/lib/docker/containers/abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567800/hostname /etc/hostname rw,noatime - btrfs /dev/sda1 rw",
				), nil
			}
		}, types.ContainerID(
			"abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567800",
		), nil),
		ginkgo.Entry("MountinfoFileNotReadable", func() {
			ReadMountinfoFunc = func(string) ([]byte, error) {
				return nil, ErrMockedMountinfoFileRead
			}
		}, types.ContainerID(""), errReadMountinfoFile),
		ginkgo.Entry("NoValidIDInMountinfo", func() {
			ReadMountinfoFunc = func(string) ([]byte, error) {
				return []byte("36 35 98:0 / rw,relatime shared:1 - overlay "), nil
			}
		}, types.ContainerID(""), errExtractContainerIDFromMountinfo),
		ginkgo.Entry("EmptyMountinfoFileContent", func() {
			ReadMountinfoFunc = func(string) ([]byte, error) {
				return []byte(""), nil
			}
		}, types.ContainerID(""), errExtractContainerIDFromMountinfo),
	)
})

var _ = ginkgo.Describe("ParseContainerIDFromMountinfo", func() {
	ginkgo.DescribeTable("should extract container ID from mountinfo string",
		func(s string, want types.ContainerID, wantErr error) {
			got, err := ParseContainerIDFromMountinfo(s)
			if wantErr == nil {
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			} else {
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(errors.Is(err, wantErr)).To(gomega.BeTrue())
			}

			gomega.Expect(got).To(gomega.Equal(want))
		},
		ginkgo.Entry(
			"ValidDockerContainerIDInRoot",
			"36 35 98:0 /var/lib/docker/containers/1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef/resolv.conf /etc/resolv.conf rw,noatime - btrfs /dev/sda1 rw",
			types.ContainerID(
				"1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			),
			nil,
		),
		ginkgo.Entry(
			"ValidDockerContainerIDInMountPoint",
			"36 35 98:0 /var/lib/docker/containers/abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567800/hostname /etc/hostname rw,noatime - btrfs /dev/sda1 rw",
			types.ContainerID(
				"abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567800",
			),
			nil,
		),
		ginkgo.Entry(
			"NoValidMountinfoLine",
			"36 35 98:0 / rw,relatime shared:1 - overlay rw,lowerdir=/var/lib/docker/overlay2/l/...,upperdir=/var/lib/docker/overlay2/...,workdir=/var/lib/docker/overlay2/...",
			types.ContainerID(""),
			errNoValidContainerID,
		),
		ginkgo.Entry("EmptyString",
			"",
			types.ContainerID(""),
			errNoValidContainerID,
		),
		ginkgo.Entry("InvalidMountinfoLine",
			"invalid line format",
			types.ContainerID(""),
			errNoValidContainerID,
		),
		ginkgo.Entry(
			"ValidIDWithExtraLines",
			"36 35 98:0 /tmp rw,relatime shared:1 - overlay rw,lowerdir=/var/lib/docker/overlay2/l/...,upperdir=/var/lib/docker/overlay2/...,workdir=/var/lib/docker/overlay2/...\n36 35 98:0 /var/lib/docker/containers/1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef/hosts /etc/hosts rw,noatime - btrfs /dev/sda1 rw\n36 35 98:0 /var rw,relatime shared:1 - overlay rw,lowerdir=/var/lib/docker/overlay2/l/...,upperdir=/var/lib/docker/overlay2/...,workdir=/var/lib/docker/overlay2/...",
			types.ContainerID(
				"1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			),
			nil,
		),
	)
})

var _ = ginkgo.Describe("ExtractContainerIDFromPath", func() {
	ginkgo.DescribeTable("should extract container ID from path",
		func(path string, want types.ContainerID) {
			got := ExtractContainerIDFromPath(path)
			gomega.Expect(got).To(gomega.Equal(want))
		},
		ginkgo.Entry(
			"ValidDockerPath",
			"/var/lib/docker/containers/1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef/resolv.conf",
			types.ContainerID("1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
		),
		ginkgo.Entry("NoDockerPath",
			"/sys/fs/cgroup",
			types.ContainerID(""),
		),
		ginkgo.Entry("InvalidLength",
			"/docker/12345678",
			types.ContainerID(""),
		),
		ginkgo.Entry("NonHexID",
			"/docker/gggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggg",
			types.ContainerID(""),
		),
	)
})

var _ = ginkgo.Describe("GetContainerIDFromHostname", func() {
	var mockClient *mockContainer.MockClient

	ginkgo.BeforeEach(func() {
		mockClient = mockContainer.NewMockClient(ginkgo.GinkgoT())
	})

	ginkgo.AfterEach(func() {
		// Reset HOSTNAME env var
		os.Unsetenv("HOSTNAME")
	})

	ginkgo.DescribeTable("should return the correct container ID or error",
		func(setup func(), want types.ContainerID, wantErr bool, errorSubstring string) {
			if setup != nil {
				setup()
			}

			got, err := GetContainerIDFromHostname(context.Background(), mockClient)
			if wantErr {
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(got).To(gomega.BeEmpty())

				if errorSubstring != "" {
					gomega.Expect(err.Error()).To(gomega.ContainSubstring(errorSubstring))
				}
			} else {
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				gomega.Expect(got).To(gomega.Equal(want))
			}
		},
		ginkgo.Entry("when a container with matching hostname is found", func() {
			expectedID := types.ContainerID("hostname-container-id")
			hostname := testHostname

			// Set HOSTNAME environment variable
			os.Setenv("HOSTNAME", hostname)

			// Create a mock container with matching hostname
			mockContainer := MockContainer(
				WithHostname(hostname),
				func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
					c.ID = string(expectedID)
				},
			)

			mockClient.EXPECT().ListContainers(context.Background()).Return([]types.Container{mockContainer}, nil)
		}, types.ContainerID("hostname-container-id"), false, ""),
		ginkgo.Entry("when no container with matching hostname is found", func() {
			hostname := testHostname
			differentHostname := "different-hostname"

			// Set HOSTNAME environment variable
			os.Setenv("HOSTNAME", hostname)

			// Create a mock container with different hostname
			mockContainer := MockContainer(
				WithHostname(differentHostname),
				func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
					c.ID = "different-container-id"
				},
			)

			mockClient.EXPECT().ListContainers(context.Background()).Return([]types.Container{mockContainer}, nil)
		}, types.ContainerID(""), true, "no container found with matching hostname"),
		ginkgo.Entry("when HOSTNAME environment variable is not set", func() {
			// HOSTNAME is not set (already unset in AfterEach)
		}, types.ContainerID(""), true, "HOSTNAME environment variable is not set"),
		ginkgo.Entry("when container listing fails", func() {
			hostname := testHostname

			// Set HOSTNAME environment variable
			os.Setenv("HOSTNAME", hostname)

			expectedError := errors.New("failed to list containers")

			mockClient.EXPECT().ListContainers(context.Background()).Return(nil, expectedError)
		}, types.ContainerID(""), true, "failed to list all containers"),
		ginkgo.Entry("when container info is nil", func() {
			hostname := testHostname

			// Set HOSTNAME environment variable
			os.Setenv("HOSTNAME", hostname)

			// Create containers: one with nil info, one with matching hostname
			mockContainerNilInfo := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			mockContainerNilInfo.EXPECT().ContainerInfo().Return(nil)

			expectedID := types.ContainerID("matching-container-id")
			mockContainerMatching := MockContainer(
				WithHostname(hostname),
				func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
					c.ID = string(expectedID)
				},
			)

			mockClient.EXPECT().
				ListContainers(context.Background()).
				Return([]types.Container{mockContainerNilInfo, mockContainerMatching}, nil)
		}, types.ContainerID("matching-container-id"), false, ""),
		ginkgo.Entry("when container config is nil", func() {
			hostname := testHostname

			// Set HOSTNAME environment variable
			os.Setenv("HOSTNAME", hostname)

			// Create containers: one with nil config, one with matching hostname
			mockContainerNilConfig := MockContainer(
				func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
					c.Config = nil
				},
			)

			expectedID := types.ContainerID("matching-container-id")
			mockContainerMatching := MockContainer(
				WithHostname(hostname),
				func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
					c.ID = string(expectedID)
				},
			)

			mockClient.EXPECT().
				ListContainers(context.Background()).
				Return([]types.Container{mockContainerNilConfig, mockContainerMatching}, nil)
		}, types.ContainerID("matching-container-id"), false, ""),
		ginkgo.Entry("when multiple containers exist but only one matches hostname", func() {
			hostname := testHostname
			expectedID := types.ContainerID("matching-container-id")

			// Set HOSTNAME environment variable
			os.Setenv("HOSTNAME", hostname)

			// Create multiple containers: some with different hostnames, one matching
			mockContainer1 := MockContainer(
				WithHostname("different-hostname-1"),
				func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
					c.ID = "container-1-id"
				},
			)

			mockContainer2 := MockContainer(
				WithHostname("different-hostname-2"),
				func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
					c.ID = "container-2-id"
				},
			)

			mockContainerMatching := MockContainer(
				WithHostname(hostname),
				func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
					c.ID = string(expectedID)
				},
			)

			mockContainer3 := MockContainer(
				WithHostname("different-hostname-3"),
				func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
					c.ID = "container-3-id"
				},
			)

			mockClient.EXPECT().
				ListContainers(context.Background()).
				Return([]types.Container{mockContainer1, mockContainer2, mockContainerMatching, mockContainer3}, nil)
		}, types.ContainerID("matching-container-id"), false, ""),
	)
})

var _ = ginkgo.Describe("GetCurrentContainerID", func() {
	var mockClient *mockContainer.MockClient

	ginkgo.BeforeEach(func() {
		mockClient = mockContainer.NewMockClient(ginkgo.GinkgoT())
	})

	ginkgo.AfterEach(func() {
		// Reset ReadCgroupFunc to original
		ReadCgroupFunc = os.ReadFile
		// Reset ReadMountinfoFunc to original
		ReadMountinfoFunc = os.ReadFile
		// Reset HOSTNAME env var
		os.Unsetenv("HOSTNAME")
	})

	ginkgo.DescribeTable(
		"should return the correct container ID or error",
		func(setup func(), expectedID types.ContainerID, expectError bool, errorSubstring string) {
			if setup != nil {
				setup()
			}

			id, err := GetCurrentContainerID(context.Background(), mockClient)
			if expectError {
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(id).To(gomega.BeEmpty())

				if errorSubstring != "" {
					gomega.Expect(err.Error()).To(gomega.ContainSubstring(errorSubstring))
				}
			} else {
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				gomega.Expect(id).To(gomega.Equal(expectedID))
			}
		},
		ginkgo.Entry("when cgroup v2 detection succeeds", func() {
			expectedID := types.ContainerID(
				"1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			)

			// Mock cgroup v2 detection to succeed
			ReadMountinfoFunc = func(string) ([]byte, error) {
				return []byte(
					"36 35 98:0 /var/lib/docker/containers/" + string(
						expectedID,
					) + "/resolv.conf /etc/resolv.conf rw,relatime shared:1 - overlay rw,lowerdir=/var/lib/docker/overlay2/l/...,upperdir=/var/lib/docker/overlay2/...,workdir=/var/lib/docker/overlay2/...,\n",
				), nil
			}
		}, types.ContainerID("1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"), false, ""),
		ginkgo.Entry("when cgroup v2 detection fails but cgroup v1 detection succeeds", func() {
			expectedID := types.ContainerID(
				"abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			)

			// Mock cgroup v2 detection to fail
			ReadMountinfoFunc = func(string) ([]byte, error) {
				return nil, errors.New("mountinfo read failed")
			}

			// Mock cgroup v1 detection to succeed
			ReadCgroupFunc = func(string) ([]byte, error) {
				return []byte("11:perf_event:/docker/" + string(expectedID) + "\n"), nil
			}
		}, types.ContainerID(
			"abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		), false, ""),
		ginkgo.Entry(
			"when both cgroup v2 and cgroup v1 detection fail but hostname detection succeeds",
			func() {
				expectedID := types.ContainerID("hostname-container-id")
				hostname := testHostname

				// Set HOSTNAME environment variable
				os.Setenv("HOSTNAME", hostname)

				// Mock cgroup v2 detection to fail
				ReadMountinfoFunc = func(string) ([]byte, error) {
					return nil, errors.New("mountinfo read failed")
				}

				// Mock cgroup v1 detection to fail
				ReadCgroupFunc = func(string) ([]byte, error) {
					return nil, errors.New("cgroup file not found")
				}

				// Create a mock container with matching hostname
				mockContainer := MockContainer(
					WithHostname(hostname),
					func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
						c.ID = string(expectedID)
					},
				)

				mockClient.EXPECT().
					ListContainers(context.Background()).
					Return([]types.Container{mockContainer}, nil).
					Times(1)
			},
			types.ContainerID("hostname-container-id"),
			false,
			"",
		),
		ginkgo.Entry("when all detection methods fail", func() {
			// Mock cgroup v2 detection to fail
			ReadMountinfoFunc = func(string) ([]byte, error) {
				return nil, errors.New("mountinfo read failed")
			}

			// Mock cgroup v1 detection to fail
			ReadCgroupFunc = func(string) ([]byte, error) {
				return nil, errors.New("cgroup file not found")
			}

			// HOSTNAME not set, so hostname detection will fail without calling client
		}, types.ContainerID(""), true, "failed to detect current container ID"),
	)
})
