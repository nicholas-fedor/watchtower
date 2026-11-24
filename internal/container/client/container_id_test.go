package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"
	dockerImage "github.com/moby/moby/api/types/image"

	"github.com/nicholas-fedor/watchtower/internal/container"
	mockContainerClient "github.com/nicholas-fedor/watchtower/internal/container/client/mocks"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/logcapture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/modelfixture"
)

const testHostname = "test-hostname"

// Container IDs for the detection tests.
const (
	// selfID is Watchtower's own container.
	selfID = "2222222222222222222222222222222222222222222222222222222222222222"
	// otherID is another Watchtower container.
	otherID = "3333333333333333333333333333333333333333333333333333333333333333"
	// orchestratorID is the ephemeral orchestrator's ID.
	orchestratorID = "4444444444444444444444444444444444444444444444444444444444444444"
)

// writeProcFile adds a file with content at the absolute path to fsys, stored
// relative to the root as io/fs requires.
func writeProcFile(t *testing.T, fsys fstest.MapFS, path, content string) {
	t.Helper()

	fsys[strings.TrimPrefix(path, "/")] = &fstest.MapFile{Data: []byte(content)}
}

// cgroupFile is the cgroup file self container ID detection reads for this process.
func cgroupFile() string {
	return fmt.Sprintf("/proc/%d/cgroup", os.Getpid())
}

// TestGetContainerIDFromCgroupFile verifies that the container ID is read from
// this process's cgroup file, and that a missing file or one without an ID fails.
func TestGetContainerIDFromCgroupFile(t *testing.T) {
	t.Parallel()

	t.Run("should return the correct container ID", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name    string
			setup   func(t *testing.T, fsys fstest.MapFS)
			want    container.ContainerID
			wantErr error
		}{
			{name: "SuccessWithValidID", setup: func(t *testing.T, fsys fstest.MapFS) {
				t.Helper()

				writeProcFile(t, fsys, cgroupFile(),
					"11:perf_event:/docker/1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				)
			}, want: container.ContainerID(
				"1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			), wantErr: nil},
			{name: "FileNotReadable", setup: nil, want: container.ContainerID(""), wantErr: errReadCgroupFile},
			{name: "NoValidID", setup: func(t *testing.T, fsys fstest.MapFS) {
				t.Helper()

				writeProcFile(t, fsys, cgroupFile(), "11:perf_event:/user.slice\n10:cpu:/system.slice")
			}, want: container.ContainerID(""), wantErr: errExtractContainerID},
			{name: "EmptyFileContent", setup: func(t *testing.T, fsys fstest.MapFS) {
				t.Helper()

				writeProcFile(t, fsys, cgroupFile(), "")
			}, want: container.ContainerID(""), wantErr: errExtractContainerID},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				setup, want, wantErr := tt.setup, tt.want, tt.wantErr

				fsys := fstest.MapFS{}
				if setup != nil {
					setup(t, fsys)
				}

				got, err := GetContainerIDFromCgroupFile(newTestLogger(t), fsys)
				if wantErr == nil {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, wantErr)
				}

				assert.Equal(t, want, got)
			})
		}
	})
}

// Test_parseContainerIDFromCgroupString verifies that the container ID is found
// in single-line and multi-line cgroup content, and not in content without a
// Docker path or a full ID.
func Test_parseContainerIDFromCgroupString(t *testing.T) {
	t.Parallel()

	t.Run("should extract container ID from string", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name    string
			s       string
			want    container.ContainerID
			wantErr error
		}{
			{name: "ValidDockerContainerIDSingleLine", s: "11:perf_event:/docker/1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef", want: container.ContainerID(
				"1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			), wantErr: nil},
			{
				name: "ValidDockerContainerIDMultiLine",
				s:    "12:memory:/user.slice\n11:perf_event:/docker/abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567800\n10:cpu:/system.slice",
				want: container.ContainerID(
					"abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567800",
				),
				wantErr: nil,
			},
			{name: "NoDockerPatternMultiLine", s: "11:perf_event:/user.slice\n10:cpu:/system.slice", want: container.ContainerID(""), wantErr: errNoValidContainerID},
			{name: "EmptyString", s: "", want: container.ContainerID(""), wantErr: errNoValidContainerID},
			{name: "InvalidIDLength", s: "11:perf_event:/docker/12345678", want: container.ContainerID(""), wantErr: errNoValidContainerID},
			{name: "NonHexID", s: "11:perf_event:/docker/gggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggg", want: container.ContainerID(""), wantErr: errNoValidContainerID},
			{name: "ValidIDWithExtraLines", s: "11:perf_event:/docker/1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef\n10:cpu:/system.slice", want: container.ContainerID(
				"1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			), wantErr: nil},
			{name: "NoDockerPatternSingleLine", s: "11:perf_event:/user.slice", want: container.ContainerID(""), wantErr: errNoValidContainerID},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				s, want, wantErr := tt.s, tt.want, tt.wantErr

				got, err := parseContainerIDFromCgroupString(newTestLogger(t), s)
				if wantErr == nil {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, wantErr)
				}

				assert.Equal(t, want, got)
			})
		}
	})
}

// TestGetContainerIDFromMountinfo verifies that the container ID is read from
// the root or mount point of /proc/self/mountinfo, and that a missing file or
// one without an ID fails.
func TestGetContainerIDFromMountinfo(t *testing.T) {
	t.Parallel()

	t.Run("should return the correct container ID", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name    string
			setup   func(t *testing.T, fsys fstest.MapFS)
			want    container.ContainerID
			wantErr error
		}{
			{name: "SuccessWithValidIDInRoot", setup: func(t *testing.T, fsys fstest.MapFS) {
				t.Helper()

				writeProcFile(t, fsys, "/proc/self/mountinfo",
					"36 35 98:0 /var/lib/docker/containers/1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef/resolv.conf /etc/resolv.conf rw,noatime - btrfs /dev/sda1 rw",
				)
			}, want: container.ContainerID(
				"1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			), wantErr: nil},
			{name: "SuccessWithValidIDInMountPoint", setup: func(t *testing.T, fsys fstest.MapFS) {
				t.Helper()

				writeProcFile(t, fsys, "/proc/self/mountinfo",
					"36 35 98:0 /var/lib/docker/containers/abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567800/hostname /etc/hostname rw,noatime - btrfs /dev/sda1 rw",
				)
			}, want: container.ContainerID(
				"abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567800",
			), wantErr: nil},
			{name: "MountinfoFileNotReadable", setup: nil, want: container.ContainerID(""), wantErr: errReadMountinfoFile},
			{name: "NoValidIDInMountinfo", setup: func(t *testing.T, fsys fstest.MapFS) {
				t.Helper()

				writeProcFile(t, fsys, "/proc/self/mountinfo", "36 35 98:0 / rw,relatime shared:1 - overlay ")
			}, want: container.ContainerID(""), wantErr: errExtractContainerIDFromMountinfo},
			{name: "EmptyMountinfoFileContent", setup: func(t *testing.T, fsys fstest.MapFS) {
				t.Helper()

				writeProcFile(t, fsys, "/proc/self/mountinfo", "")
			}, want: container.ContainerID(""), wantErr: errExtractContainerIDFromMountinfo},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				setup, want, wantErr := tt.setup, tt.want, tt.wantErr

				fsys := fstest.MapFS{}
				if setup != nil {
					setup(t, fsys)
				}

				got, err := GetContainerIDFromMountinfo(newTestLogger(t), fsys)
				if wantErr == nil {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, wantErr)
				}

				assert.Equal(t, want, got)
			})
		}
	})
}

// Test_parseContainerIDFromMountinfo verifies that the container ID is found in
// the root or mount point fields of mountinfo content, and not in malformed
// lines.
func Test_parseContainerIDFromMountinfo(t *testing.T) {
	t.Parallel()

	t.Run("should extract container ID from mountinfo string", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name    string
			s       string
			want    container.ContainerID
			wantErr error
		}{
			{
				name: "ValidDockerContainerIDInRoot",
				s:    "36 35 98:0 /var/lib/docker/containers/1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef/resolv.conf /etc/resolv.conf rw,noatime - btrfs /dev/sda1 rw",
				want: container.ContainerID(
					"1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				),
				wantErr: nil,
			},
			{
				name: "ValidDockerContainerIDInMountPoint",
				s:    "36 35 98:0 /var/lib/docker/containers/abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567800/hostname /etc/hostname rw,noatime - btrfs /dev/sda1 rw",
				want: container.ContainerID(
					"abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567800",
				),
				wantErr: nil,
			},
			{
				name:    "NoValidMountinfoLine",
				s:       "36 35 98:0 / rw,relatime shared:1 - overlay rw,lowerdir=/var/lib/docker/overlay2/l/...,upperdir=/var/lib/docker/overlay2/...,workdir=/var/lib/docker/overlay2/...",
				want:    container.ContainerID(""),
				wantErr: errNoValidContainerID,
			},
			{name: "EmptyString", s: "", want: container.ContainerID(""), wantErr: errNoValidContainerID},
			{name: "InvalidMountinfoLine", s: "invalid line format", want: container.ContainerID(""), wantErr: errNoValidContainerID},
			{
				name: "ValidIDWithExtraLines",
				s:    "36 35 98:0 /tmp rw,relatime shared:1 - overlay rw,lowerdir=/var/lib/docker/overlay2/l/...,upperdir=/var/lib/docker/overlay2/...,workdir=/var/lib/docker/overlay2/...\n36 35 98:0 /var/lib/docker/containers/1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef/hosts /etc/hosts rw,noatime - btrfs /dev/sda1 rw\n36 35 98:0 /var rw,relatime shared:1 - overlay rw,lowerdir=/var/lib/docker/overlay2/l/...,upperdir=/var/lib/docker/overlay2/...,workdir=/var/lib/docker/overlay2/...",
				want: container.ContainerID(
					"1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				),
				wantErr: nil,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				s, want, wantErr := tt.s, tt.want, tt.wantErr

				got, err := parseContainerIDFromMountinfo(newTestLogger(t), s)
				if wantErr == nil {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, wantErr)
				}

				assert.Equal(t, want, got)
			})
		}
	})
}

// Test_extractContainerIDFromPath verifies that a full hexadecimal container ID
// is taken from a Docker cgroup path, and that other paths yield no ID.
func Test_extractContainerIDFromPath(t *testing.T) {
	t.Parallel()

	t.Run("should extract container ID from path", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name string
			path string
			want container.ContainerID
		}{
			{
				name: "ValidDockerPath",
				path: "/var/lib/docker/containers/1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef/resolv.conf",
				want: container.ContainerID("1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
			},
			{name: "NoDockerPath", path: "/sys/fs/cgroup", want: container.ContainerID("")},
			{name: "InvalidLength", path: "/docker/12345678", want: container.ContainerID("")},
			{name: "NonHexID", path: "/docker/gggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggg", want: container.ContainerID("")},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				path, want := tt.path, tt.want

				got := extractContainerIDFromPath(path)
				assert.Equal(t, want, got)
			})
		}
	})
}

// TestGetContainerIDFromHostname verifies that the container ID is resolved from
// the Watchtower container whose hostname matches HOSTNAME, preferring one that
// is not an old instance, and that listing failures and missing matches fail.
//
// The test sets environment variables, so it does not run in parallel.
func TestGetContainerIDFromHostname(t *testing.T) {
	t.Run("should return the correct container ID or error", func(t *testing.T) {
		tests := []struct {
			name        string
			setup       func(t *testing.T, mockClient *mockContainerClient.MockContainerReader)
			want        container.ContainerID
			wantErrText string
		}{
			{name: "when a container with matching hostname is found", setup: func(t *testing.T, mockClient *mockContainerClient.MockContainerReader) {
				t.Helper()

				expectedID := container.ContainerID("hostname-container-id")
				hostname := testHostname

				// Set HOSTNAME environment variable
				t.Setenv("HOSTNAME", hostname)

				// Create a mock container with matching hostname
				mockContainer := modelfixture.Container(
					containerfixture.WithHostname(hostname),
					func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
						c.ID = string(expectedID)
					},
				)

				mockClient.EXPECT().ListContainers(context.Background()).Return([]*container.Container{mockContainer}, nil)
			}, want: container.ContainerID("hostname-container-id"), wantErrText: ""},
			{name: "when no container with matching hostname is found", setup: func(t *testing.T, mockClient *mockContainerClient.MockContainerReader) {
				t.Helper()

				hostname := testHostname
				differentHostname := "different-hostname"

				// Set HOSTNAME environment variable
				t.Setenv("HOSTNAME", hostname)

				// Create a mock container with different hostname
				mockContainer := modelfixture.Container(
					containerfixture.WithHostname(differentHostname),
					func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
						c.ID = "different-container-id"
					},
				)

				mockClient.EXPECT().ListContainers(context.Background()).Return([]*container.Container{mockContainer}, nil)
			}, want: container.ContainerID(""), wantErrText: "no container found with matching hostname"},
			{name: "when HOSTNAME environment variable is not set", setup: func(t *testing.T, mockClient *mockContainerClient.MockContainerReader) {
				t.Helper()

				// HOSTNAME is not set.
			}, want: container.ContainerID(""), wantErrText: "HOSTNAME environment variable is not set"},
			{name: "when container listing fails", setup: func(t *testing.T, mockClient *mockContainerClient.MockContainerReader) {
				t.Helper()

				hostname := testHostname

				// Set HOSTNAME environment variable
				t.Setenv("HOSTNAME", hostname)

				expectedError := errors.New("failed to list containers: connection refused")

				mockClient.EXPECT().ListContainers(context.Background()).Return(nil, expectedError)
			}, want: container.ContainerID(""), wantErrText: "failed to list containers: connection refused"},
			{name: "when container info is nil", setup: func(t *testing.T, mockClient *mockContainerClient.MockContainerReader) {
				t.Helper()

				hostname := testHostname

				// Set HOSTNAME environment variable
				t.Setenv("HOSTNAME", hostname)

				// Create containers: one with nil info, one with matching hostname
				containerNilInfo := container.NewContainer(nil, nil, nil)

				expectedID := container.ContainerID("matching-container-id")
				mockContainerMatching := modelfixture.Container(
					containerfixture.WithHostname(hostname),
					func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
						c.ID = string(expectedID)
					},
				)

				mockClient.EXPECT().
					ListContainers(context.Background()).
					Return([]*container.Container{containerNilInfo, mockContainerMatching}, nil)
			}, want: container.ContainerID("matching-container-id"), wantErrText: ""},
			{name: "when container config is nil", setup: func(t *testing.T, mockClient *mockContainerClient.MockContainerReader) {
				t.Helper()

				hostname := testHostname

				// Set HOSTNAME environment variable
				t.Setenv("HOSTNAME", hostname)

				// Create containers: one with nil config, one with matching hostname
				mockContainerNilConfig := modelfixture.Container(
					func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
						c.Config = nil
					},
				)

				expectedID := container.ContainerID("matching-container-id")
				mockContainerMatching := modelfixture.Container(
					containerfixture.WithHostname(hostname),
					func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
						c.ID = string(expectedID)
					},
				)

				mockClient.EXPECT().
					ListContainers(context.Background()).
					Return([]*container.Container{mockContainerNilConfig, mockContainerMatching}, nil)
			}, want: container.ContainerID("matching-container-id"), wantErrText: ""},
			{name: "when multiple containers exist but only one matches hostname", setup: func(t *testing.T, mockClient *mockContainerClient.MockContainerReader) {
				t.Helper()

				hostname := testHostname
				expectedID := container.ContainerID("matching-container-id")

				// Set HOSTNAME environment variable
				t.Setenv("HOSTNAME", hostname)

				// Create multiple containers: some with different hostnames, one matching
				mockContainer1 := modelfixture.Container(
					containerfixture.WithHostname("different-hostname-1"),
					func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
						c.ID = "container-1-id"
					},
				)

				mockContainer2 := modelfixture.Container(
					containerfixture.WithHostname("different-hostname-2"),
					func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
						c.ID = "container-2-id"
					},
				)

				mockContainerMatching := modelfixture.Container(
					containerfixture.WithHostname(hostname),
					func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
						c.ID = string(expectedID)
					},
				)

				mockContainer3 := modelfixture.Container(
					containerfixture.WithHostname("different-hostname-3"),
					func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
						c.ID = "container-3-id"
					},
				)

				mockClient.EXPECT().
					ListContainers(context.Background()).
					Return([]*container.Container{mockContainer1, mockContainer2, mockContainerMatching, mockContainer3}, nil)
			}, want: container.ContainerID("matching-container-id"), wantErrText: ""},
			{name: "when multiple containers share hostname but only one is Watchtower", setup: func(t *testing.T, mockClient *mockContainerClient.MockContainerReader) {
				t.Helper()

				hostname := testHostname

				// Set HOSTNAME environment variable
				t.Setenv("HOSTNAME", hostname)

				// Create a non-Watchtower container with matching hostname (first match)
				nonWatchtowerContainer := modelfixture.Container(
					containerfixture.WithHostname(hostname),
					func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
						c.ID = "non-watchtower-id"
					},
				)

				// Create a Watchtower container with matching hostname (second match)
				watchtowerContainer := modelfixture.Container(
					containerfixture.WithHostname(hostname),
					containerfixture.WithLabels(map[string]string{
						"com.centurylinklabs.watchtower": "true",
					}),
					func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
						c.ID = "watchtower-container-id"
					},
				)

				mockClient.EXPECT().
					ListContainers(context.Background()).
					Return([]*container.Container{nonWatchtowerContainer, watchtowerContainer}, nil)
			}, want: container.ContainerID("watchtower-container-id"), wantErrText: ""},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// Each case starts without HOSTNAME, and t.Setenv restores it.
				t.Setenv("HOSTNAME", "")
				require.NoError(t, os.Unsetenv("HOSTNAME"))

				mockClient := mockContainerClient.NewMockContainerReader(t)

				setup, want, wantErrText := tt.setup, tt.want, tt.wantErrText

				if setup != nil {
					setup(t, mockClient)
				}

				got, err := GetContainerIDFromHostname(newTestLogger(t), context.Background(), fstest.MapFS{}, mockClient)
				if wantErrText != "" {
					require.EqualError(t, err, wantErrText)
					assert.Empty(t, got)
				} else {
					require.NoError(t, err)
					assert.Equal(t, want, got)
				}
			})
		}
	})
}

// TestGetContainerIDFromHostname_ownContainer verifies how hostname matching
// tells apart several containers that share Watchtower's hostname. It follows
// the rules of the shared network namespace check and fails rather than guess.
//
// The test sets environment variables, so it does not run in parallel.
func TestGetContainerIDFromHostname_ownContainer(t *testing.T) {
	shared := containerfixture.WithHostname(testHostname)
	self := watchtowerContainer(selfID, "/watchtower", "", shared)
	other := watchtowerContainer(otherID, "/watchtower-b", "", shared)
	oldName := otherOldName()

	tests := []struct {
		name string
		// listed holds the containers the listing returns.
		listed []*container.Container
		// startTime makes /proc report when this container started.
		startTime bool
		// initOutside places PID 1 outside this container, as with a
		// shared PID namespace.
		initOutside bool
		wantID      container.ContainerID
		wantErr     error
	}{
		{
			// Picking another instance that shares an explicit hostname
			// would take over its scope.
			name:    "several Watchtower containers share the hostname",
			listed:  []*container.Container{other, self},
			wantErr: errAmbiguousOwnContainer,
		},
		{
			name: "several Watchtower containers told apart by their start",
			listed: []*container.Container{
				watchtowerContainer(otherID, "/watchtower-b", "", shared,
					startedAt(testProcessStarted.Add(-3*time.Second))),
				watchtowerContainer(selfID, "/watchtower", "", shared,
					startedAt(testProcessStarted.Add(500*time.Millisecond))),
			},
			startTime: true,
			wantID:    selfID,
		},
		{
			name: "several Watchtower containers started too close together",
			listed: []*container.Container{
				watchtowerContainer(otherID, "/watchtower-b", "", shared,
					startedAt(testProcessStarted.Add(time.Second))),
				watchtowerContainer(selfID, "/watchtower", "", shared,
					startedAt(testProcessStarted.Add(500*time.Millisecond))),
			},
			startTime: true,
			wantErr:   errAmbiguousOwnContainer,
		},
		{
			// PID 1 is the other instance's first process, so its start says
			// nothing about this container.
			name: "the other Watchtower container's start matches PID 1 outside this container",
			listed: []*container.Container{
				watchtowerContainer(otherID, "/watchtower-b", "", shared,
					startedAt(testProcessStarted.Add(500*time.Millisecond))),
				watchtowerContainer(selfID, "/watchtower", "", shared,
					startedAt(testProcessStarted.Add(-time.Hour))),
			},
			startTime:   true,
			initOutside: true,
			wantErr:     errAmbiguousOwnContainer,
		},
		{
			// An image without the Watchtower label falls back to every
			// container with the hostname, here sharing the network and PID
			// namespaces of a VPN container.
			name: "a container without the label started with PID 1 outside this container",
			listed: []*container.Container{
				modelfixture.Container(containerfixture.WithID(otherID), shared,
					startedAt(testProcessStarted.Add(500*time.Millisecond))),
				modelfixture.Container(containerfixture.WithID(selfID), shared,
					startedAt(testProcessStarted.Add(-time.Hour))),
			},
			startTime:   true,
			initOutside: true,
			wantErr:     errAmbiguousOwnContainer,
		},
		{
			// An orchestrator joins the network namespace Watchtower joins
			// and so reports the same hostname.
			name: "an ephemeral orchestrator shares the hostname",
			listed: []*container.Container{
				self,
				watchtowerContainer(orchestratorID, "/watchtower-orchestrator-"+selfID, "", shared,
					ephemeralOrchestrator()),
			},
			wantID: selfID,
		},
		{
			name: "a paused Watchtower container shares the hostname",
			listed: []*container.Container{
				watchtowerContainer(otherID, "/watchtower-b", "", shared, paused()),
				self,
			},
			wantID: selfID,
		},
		{
			// A rename-based self-update keeps the old container running
			// while its replacement starts.
			name:   "an old Watchtower container shares the hostname",
			listed: []*container.Container{self, watchtowerContainer(otherID, oldName, "", shared)},
			wantID: selfID,
		},
		{
			name: "several containers without the Watchtower label share the hostname",
			listed: []*container.Container{
				modelfixture.Container(containerfixture.WithID(otherID), shared),
				modelfixture.Container(containerfixture.WithID(selfID), shared),
			},
			wantErr: errAmbiguousOwnContainer,
		},
		{
			// A Watchtower image without the Watchtower label leaves only
			// the hostname to go by.
			name: "only a stopped Watchtower container has the Watchtower label",
			listed: []*container.Container{
				watchtowerContainer(otherID, "/watchtower-stopped", "", shared,
					containerfixture.WithContainerState(dockerContainer.State{Status: dockerContainer.StateExited})),
				modelfixture.Container(containerfixture.WithID(selfID), shared),
			},
			wantID: selfID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOSTNAME", testHostname)

			fsys := fstest.MapFS{}
			if tt.startTime {
				writeStartTime(t, fsys)
			}

			if tt.initOutside {
				writeMountNamespaces(t, fsys, hostMountNamespace)
			}

			client := mockContainerClient.NewMockContainerReader(t)
			client.EXPECT().ListContainers(mock.Anything).Return(tt.listed, nil).Once()

			id, err := GetContainerIDFromHostname(newTestLogger(t), t.Context(), fsys, client)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Empty(t, id)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantID, id)
		})
	}
}

// TestGetCurrentContainerID verifies that self container ID detection tries the
// mountinfo file, then the cgroup file, then the hostname, and fails when all
// three fail.
//
// The test sets environment variables, so it does not run in parallel.
func TestGetCurrentContainerID(t *testing.T) {
	t.Run("should return the correct container ID or error", func(t *testing.T) {
		tests := []struct {
			name           string
			setup          func(t *testing.T, fsys fstest.MapFS, mockClient *mockContainerClient.MockContainerReader)
			expectedID     container.ContainerID
			expectError    bool
			errorSubstring string
		}{
			{name: "when cgroup v2 detection succeeds", setup: func(t *testing.T, fsys fstest.MapFS, mockClient *mockContainerClient.MockContainerReader) {
				t.Helper()

				expectedID := container.ContainerID(
					"1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				)

				// cgroup v2 detection succeeds and names a Watchtower container.
				writeProcFile(t, fsys, "/proc/self/mountinfo",
					"36 35 98:0 /var/lib/docker/containers/"+string(
						expectedID,
					)+"/resolv.conf /etc/resolv.conf rw,relatime shared:1 - overlay rw,lowerdir=/var/lib/docker/overlay2/l/...,upperdir=/var/lib/docker/overlay2/...,workdir=/var/lib/docker/overlay2/...,\n",
				)

				mockClient.EXPECT().GetCurrentWatchtowerContainer(context.Background(), expectedID).
					Return(watchtowerContainer(string(expectedID), "/watchtower", ""), nil).Once()
			}, expectedID: container.ContainerID("1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"), expectError: false, errorSubstring: ""},
			{name: "when cgroup v2 detection fails but cgroup v1 detection succeeds", setup: func(t *testing.T, fsys fstest.MapFS, mockClient *mockContainerClient.MockContainerReader) {
				t.Helper()

				expectedID := container.ContainerID(
					"abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
				)

				// No mountinfo file, so cgroup v2 detection fails and cgroup v1 detection succeeds.
				writeProcFile(t, fsys, cgroupFile(), "11:perf_event:/docker/"+string(expectedID)+"\n")
			}, expectedID: container.ContainerID(
				"abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			), expectError: false, errorSubstring: ""},
			{
				name: "when both cgroup v2 and cgroup v1 detection fail but hostname detection succeeds",
				setup: func(t *testing.T, fsys fstest.MapFS, mockClient *mockContainerClient.MockContainerReader) {
					t.Helper()

					expectedID := container.ContainerID("hostname-container-id")
					hostname := testHostname

					// Set HOSTNAME environment variable
					t.Setenv("HOSTNAME", hostname)

					// No mountinfo or cgroup file, so both cgroup detections fail.

					// Create a mock container with matching hostname
					mockContainer := modelfixture.Container(
						containerfixture.WithHostname(hostname),
						func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
							c.ID = string(expectedID)
						},
					)

					mockClient.EXPECT().
						ListContainers(context.Background()).
						Return([]*container.Container{mockContainer}, nil).
						Times(1)
				},
				expectedID:     container.ContainerID("hostname-container-id"),
				expectError:    false,
				errorSubstring: "",
			},
			{name: "when all detection methods fail", setup: func(t *testing.T, fsys fstest.MapFS, mockClient *mockContainerClient.MockContainerReader) {
				t.Helper()

				// No mountinfo or cgroup file, and HOSTNAME is not set, so hostname
				// detection fails without calling the client.
			}, expectedID: container.ContainerID(""), expectError: true, errorSubstring: "failed to detect current container ID"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// Each case starts without HOSTNAME, and t.Setenv restores it.
				t.Setenv("HOSTNAME", "")
				require.NoError(t, os.Unsetenv("HOSTNAME"))

				mockClient := mockContainerClient.NewMockContainerReader(t)

				setup, expectedID, expectError, errorSubstring := tt.setup, tt.expectedID, tt.expectError, tt.errorSubstring

				fsys := fstest.MapFS{}
				if setup != nil {
					setup(t, fsys, mockClient)
				}

				id, _, err := GetCurrentContainerID(newTestLogger(t), context.Background(), fsys, mockClient)
				if expectError {
					require.Error(t, err)
					assert.Empty(t, id)

					if errorSubstring != "" {
						assert.Contains(t, err.Error(), errorSubstring)
					}
				} else {
					require.NoError(t, err)
					assert.Equal(t, expectedID, id)
				}
			})
		}
	})
}

// watchtowerContainer returns a running Watchtower container with the given
// ID and name. A non-empty networkContainer makes it join that container's
// network namespace, and extra updates apply after the others.
func watchtowerContainer(id, name, networkContainer string, extra ...containerfixture.Update) *container.Container {
	updates := []containerfixture.Update{
		containerfixture.WithID(id),
		containerfixture.WithName(name),
		containerfixture.WithLabels(map[string]string{labels.Watchtower: "true"}),
	}
	if networkContainer != "" {
		updates = append(updates, containerfixture.WithNetworkMode("container:"+networkContainer))
	}

	return modelfixture.Container(append(updates, extra...)...)
}

// startedAt sets a running container's recorded start.
func startedAt(started time.Time) containerfixture.Update {
	return containerfixture.WithContainerState(dockerContainer.State{
		Running:   true,
		Status:    dockerContainer.StateRunning,
		StartedAt: started.UTC().Format(time.RFC3339Nano),
	})
}

// otherOldName returns the name a self-update gives the container with
// otherID.
func otherOldName() string {
	return "/" + watchtowerContainer(otherID, "", "").OldWatchtowerName()
}

// paused sets a paused state, which Docker reports as running too.
func paused() containerfixture.Update {
	return containerfixture.WithContainerState(dockerContainer.State{
		Running: true,
		Paused:  true,
		Status:  dockerContainer.StatePaused,
	})
}

// ephemeralOrchestrator labels a container as the ephemeral orchestrator of a
// self-update, which also carries the Watchtower image's label.
func ephemeralOrchestrator() containerfixture.Update {
	return containerfixture.WithLabels(map[string]string{
		labels.Watchtower:            "true",
		labels.EphemeralOrchestrator: "true",
	})
}

// Start ticks of 1/100 s since boot that writeStartTime writes, 5 s apart.
const (
	// testInitTicks is when PID 1 started.
	testInitTicks = 19322773
	// testOwnTicks is when this process started.
	testOwnTicks = testInitTicks + 500
)

// testProcessStarted is when the /proc files writeStartTime writes say this
// container started: 5 s before this package was initialized.
var testProcessStarted = initialized.Add(-5 * time.Second)

// Mount namespace links for the start time tests.
const (
	// containerMountNamespace is the mount namespace of this container.
	containerMountNamespace = "mnt:[4026532001]"
	// hostMountNamespace is the mount namespace of the host or of another
	// container.
	hostMountNamespace = "mnt:[4026531841]"
)

// writeMountNamespaces writes the mount namespace links of this process, in
// this container, and of PID 1, in first.
func writeMountNamespaces(t *testing.T, fsys fstest.MapFS, first string) {
	t.Helper()

	fsys["proc/self/ns/mnt"] = &fstest.MapFile{Data: []byte(containerMountNamespace), Mode: fs.ModeSymlink}
	fsys["proc/1/ns/mnt"] = &fstest.MapFile{Data: []byte(first), Mode: fs.ModeSymlink}
}

// statLine returns a /proc stat file line for a process that started ticks
// after boot. Its command name holds a space and a parenthesis.
func statLine(ticks int64) string {
	return fmt.Sprintf("1 (watch) tower) S 0 1 1 0 -1 4194560 0 0 0 0 0 0 0 0 20 0 1 0 %d 1234 5\n", ticks)
}

// writeStartTime writes the /proc files that place this container's start at
// testProcessStarted, with PID 1 in this container.
func writeStartTime(t *testing.T, fsys fstest.MapFS) {
	t.Helper()

	writeMountNamespaces(t, fsys, containerMountNamespace)
	writeProcFile(t, fsys, "/proc/self/stat", statLine(testOwnTicks))
	writeProcFile(t, fsys, "/proc/1/stat", statLine(testInitTicks))
}

// TestContainerStartedAt verifies that the start of this container's first
// process is derived from this package's initialization and the start ticks
// in /proc, and that missing or malformed files fail, as does a PID 1 outside
// this container's mount namespace.
func TestContainerStartedAt(t *testing.T) {
	t.Parallel()

	// started is when this package was initialized in the fixed-time cases.
	started := time.Date(2026, 11, 24, 12, 0, 5, 300_000_000, time.UTC)

	t.Run("moves back by the ticks between the two starts", func(t *testing.T) {
		t.Parallel()

		fsys := fstest.MapFS{}
		writeStartTime(t, fsys)

		got, err := containerStartedAt(fsys, started)
		require.NoError(t, err)
		assert.True(t, started.Add(-5*time.Second).Equal(got), "got %v", got)
	})

	t.Run("matches the right container whatever the boot time says", func(t *testing.T) {
		t.Parallel()

		// Docker recorded both starts. A clock step of 3.1 s after this
		// process started moves the boot time in /proc/stat, which would
		// place this container's start on the other container's.
		self := modelfixture.Container(startedAt(time.Date(2026, 11, 24, 12, 0, 0, 300_000_000, time.UTC)))
		other := modelfixture.Container(startedAt(time.Date(2026, 11, 24, 12, 0, 3, 400_000_000, time.UTC)))

		// bootTime is the boot time that matches started without a step.
		bootTime := started.Add(-testOwnTicks * (time.Second / userHZ))

		for _, step := range []time.Duration{0, 3100 * time.Millisecond} {
			t.Run(step.String(), func(t *testing.T) {
				t.Parallel()

				fsys := fstest.MapFS{}
				writeStartTime(t, fsys)
				writeProcFile(t, fsys, "/proc/stat", fmt.Sprintf("btime %d\n", bootTime.Add(step).Unix()))

				got, err := containerStartedAt(fsys, started)
				require.NoError(t, err)

				selfStarted, ok := self.StartedAt()
				require.True(t, ok)
				otherStarted, ok := other.StartedAt()
				require.True(t, ok)

				assert.LessOrEqual(t, got.Sub(selfStarted).Abs(), startTimeTolerance)
				assert.Greater(t, got.Sub(otherStarted).Abs(), startTimeTolerance)
			})
		}
	})

	t.Run("reads starts long after boot", func(t *testing.T) {
		t.Parallel()

		// 10000000000 ticks of 1/100 s are about 3.2 years.
		fsys := fstest.MapFS{}
		writeMountNamespaces(t, fsys, containerMountNamespace)
		writeProcFile(t, fsys, "/proc/self/stat", statLine(10000000500))
		writeProcFile(t, fsys, "/proc/1/stat", statLine(10000000000))

		got, err := containerStartedAt(fsys, started)
		require.NoError(t, err)
		assert.True(t, started.Add(-5*time.Second).Equal(got), "got %v", got)
	})

	t.Run("rejects a PID 1 outside this container", func(t *testing.T) {
		t.Parallel()

		// With the host's or another container's PID namespace, PID 1 is
		// that namespace's first process, whose start would match the
		// wrong container.
		fsys := fstest.MapFS{}
		writeStartTime(t, fsys)
		writeMountNamespaces(t, fsys, hostMountNamespace)

		_, err := containerStartedAt(fsys, started)
		require.ErrorIs(t, err, errInitOutsideContainer)
	})

	t.Run("rejects a PID 1 it cannot check", func(t *testing.T) {
		t.Parallel()

		fsys := fstest.MapFS{}
		writeStartTime(t, fsys)
		delete(fsys, "proc/1/ns/mnt")

		_, err := containerStartedAt(fsys, started)
		require.ErrorIs(t, err, fs.ErrNotExist)
	})

	// malformed holds stat file contents without a readable start time.
	malformed := []struct {
		name    string
		content string
		wantErr error
	}{
		{name: "no command name", content: "1 sh S 0", wantErr: errMalformedProcessStat},
		{name: "too few fields", content: "1 (sh) S 0 1 1", wantErr: errMalformedProcessStat},
		{
			name:    "malformed start ticks",
			content: "1 (sh) S 0 1 1 0 -1 4194560 0 0 0 0 0 0 0 0 20 0 1 0 later 1234 5",
			wantErr: errMalformedProcessStat,
		},
		{name: "missing", wantErr: fs.ErrNotExist},
	}

	for _, file := range []string{"/proc/self/stat", "/proc/1/stat"} {
		for _, tt := range malformed {
			t.Run(file+" "+tt.name, func(t *testing.T) {
				t.Parallel()

				fsys := fstest.MapFS{}
				writeStartTime(t, fsys)
				delete(fsys, strings.TrimPrefix(file, "/"))

				if tt.content != "" {
					writeProcFile(t, fsys, file, tt.content)
				}

				_, err := containerStartedAt(fsys, started)
				require.ErrorIs(t, err, tt.wantErr)
			})
		}
	}
}

// TestGetCurrentContainerID_sharedNetworkNamespace verifies how detection
// treats a mountinfo ID that names a container other than a Watchtower
// container. A Watchtower that joins another container's network namespace
// sees that container's hostname, hosts and resolv.conf mounts, so mountinfo
// names the network container.
func TestGetCurrentContainerID_sharedNetworkNamespace(t *testing.T) {
	const vpnID = "1111111111111111111111111111111111111111111111111111111111111111"

	vpn := modelfixture.Container(containerfixture.WithID(vpnID), containerfixture.WithName("/vpn"))
	self := watchtowerContainer(selfID, "/watchtower", "vpn")
	orchestrator := watchtowerContainer(orchestratorID, "/watchtower-orchestrator-"+selfID, "vpn",
		ephemeralOrchestrator())
	stopped := watchtowerContainer(otherID, "/watchtower-stopped", "vpn",
		containerfixture.WithContainerState(dockerContainer.State{Status: dockerContainer.StateExited}))
	pausedJoiner := watchtowerContainer(otherID, "/watchtower-paused", "vpn", paused())
	// restarting waits to restart, which Docker reports as running too.
	restarting := watchtowerContainer(otherID, "/watchtower-restarting", "vpn",
		containerfixture.WithContainerState(dockerContainer.State{
			Running:    true,
			Restarting: true,
			Status:     dockerContainer.StateRestarting,
		}))
	oldName := otherOldName()
	// old is a running Watchtower container that a self-update renamed.
	old := watchtowerContainer(otherID, oldName, "vpn")

	tests := []struct {
		name       string
		inspectErr error
		// listed holds the Watchtower containers the listing returns.
		listed  []*container.Container
		listErr error
		// startTime makes /proc report when this container started.
		startTime bool
		// initOutside places PID 1 outside this container, as with a
		// shared PID namespace.
		initOutside bool
		// cgroupID is the container ID the cgroup file names, empty for none.
		cgroupID string
		wantID   container.ContainerID
		// wantDetected reports whether detection returns the container it
		// read for wantID.
		wantDetected bool
		wantErr      error
	}{
		{
			name:         "the Watchtower container that joins the network container",
			listed:       []*container.Container{self, watchtowerContainer(otherID, "/watchtower-b", "")},
			wantID:       selfID,
			wantDetected: true,
		},
		{
			// The listing names the network container by its ID when it
			// cannot inspect it.
			name:         "a Watchtower container joins by the network container's ID",
			listed:       []*container.Container{watchtowerContainer(selfID, "/watchtower", vpnID)},
			wantID:       selfID,
			wantDetected: true,
		},
		{
			// A restarted old container is this one when it joins alone.
			name:         "only an old Watchtower container joins",
			listed:       []*container.Container{old},
			wantID:       otherID,
			wantDetected: true,
		},
		{
			// An old container restarted while its replacement runs must
			// not take the replacement's ID, or it would never retire.
			name: "an old Watchtower container restarted beside its replacement",
			listed: []*container.Container{
				watchtowerContainer(selfID, "/watchtower", "vpn", startedAt(testProcessStarted.Add(-time.Hour))),
				watchtowerContainer(otherID, oldName, "vpn", startedAt(testProcessStarted.Add(500*time.Millisecond))),
			},
			startTime:    true,
			wantID:       otherID,
			wantDetected: true,
		},
		{
			name: "a replacement started beside the old Watchtower container",
			listed: []*container.Container{
				watchtowerContainer(otherID, oldName, "vpn", startedAt(testProcessStarted.Add(-time.Hour))),
				watchtowerContainer(selfID, "/watchtower", "vpn", startedAt(testProcessStarted.Add(500*time.Millisecond))),
			},
			startTime:    true,
			wantID:       selfID,
			wantDetected: true,
		},
		{
			name: "several Watchtower containers told apart by their start",
			listed: []*container.Container{
				watchtowerContainer(otherID, "/watchtower-b", "vpn", startedAt(testProcessStarted.Add(-3*time.Second))),
				watchtowerContainer(selfID, "/watchtower-a", "vpn", startedAt(testProcessStarted.Add(500*time.Millisecond))),
			},
			startTime:    true,
			wantID:       selfID,
			wantDetected: true,
		},
		{
			name: "several Watchtower containers started too close together",
			listed: []*container.Container{
				watchtowerContainer(otherID, "/watchtower-b", "vpn", startedAt(testProcessStarted.Add(time.Second))),
				watchtowerContainer(selfID, "/watchtower-a", "vpn", startedAt(testProcessStarted.Add(500*time.Millisecond))),
			},
			startTime: true,
			wantErr:   errAmbiguousOwnContainer,
		},
		{
			// With the network container's PID namespace too, PID 1 is the
			// network container's first process, which started with the
			// other joiner.
			name: "a joiner started with PID 1 outside this container",
			listed: []*container.Container{
				watchtowerContainer(otherID, "/watchtower-b", "vpn", startedAt(testProcessStarted.Add(500*time.Millisecond))),
				watchtowerContainer(selfID, "/watchtower-a", "vpn", startedAt(testProcessStarted.Add(-time.Hour))),
			},
			startTime:   true,
			initOutside: true,
			wantErr:     errAmbiguousOwnContainer,
		},
		{
			// An orchestrator from the Watchtower image carries its label and
			// joins Watchtower's network namespace during a self-update.
			name:         "an ephemeral orchestrator joins as well",
			listed:       []*container.Container{self, orchestrator},
			wantID:       selfID,
			wantDetected: true,
		},
		{
			name:         "a paused Watchtower container joins as well",
			listed:       []*container.Container{self, pausedJoiner},
			wantID:       selfID,
			wantDetected: true,
		},
		{
			name:         "a Watchtower container waiting to restart joins as well",
			listed:       []*container.Container{self, restarting},
			wantID:       selfID,
			wantDetected: true,
		},
		{
			// The cgroup file is exact, so the containers are not listed.
			name:     "the cgroup file names this container",
			cgroupID: selfID,
			wantID:   selfID,
		},
		{
			// An image without the Watchtower label also reaches the cgroup
			// check, which then names the container detection already read.
			name:         "the cgroup file names the container mountinfo names",
			cgroupID:     vpnID,
			wantID:       vpnID,
			wantDetected: true,
		},
		{
			name:         "a stopped Watchtower container joins as well",
			listed:       []*container.Container{self, stopped},
			wantID:       selfID,
			wantDetected: true,
		},
		{
			// A rename-based self-update keeps the old container running
			// while its replacement starts.
			name:         "an old Watchtower container joins as well",
			listed:       []*container.Container{old, self},
			wantID:       selfID,
			wantDetected: true,
		},
		{
			name:   "no Watchtower container joins, as for an image without the label",
			listed: []*container.Container{watchtowerContainer(otherID, "/watchtower-b", "")},
			wantID: vpnID,
		},
		{
			// Every joiner reports the network container's hostname, so
			// hostname matching cannot tell them apart either and is skipped.
			name:    "several Watchtower containers join",
			listed:  []*container.Container{self, watchtowerContainer(otherID, "/watchtower-b", "vpn")},
			wantErr: errAmbiguousOwnContainer,
		},
		{
			name:       "the network container cannot be inspected",
			inspectErr: errors.New("daemon unavailable"),
			wantID:     vpnID,
		},
		{
			name:    "the containers cannot be listed",
			listErr: errors.New("daemon unavailable"),
			wantID:  vpnID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The several-joiners cases must fail without hostname matching,
			// which would succeed with this HOSTNAME.
			t.Setenv("HOSTNAME", "vpn-hostname")

			fsys := fstest.MapFS{}
			writeProcFile(t, fsys, "/proc/self/mountinfo",
				"36 35 98:0 /var/lib/docker/containers/"+vpnID+"/hostname /etc/hostname rw,relatime - ext4 /dev/sda1 rw\n")

			if tt.startTime {
				writeStartTime(t, fsys)
			}

			if tt.initOutside {
				writeMountNamespaces(t, fsys, hostMountNamespace)
			}

			if tt.cgroupID != "" {
				writeProcFile(t, fsys, cgroupFile(), "12:memory:/docker/"+tt.cgroupID+"\n")
			}

			client := mockContainerClient.NewMockContainerReader(t)

			switch {
			case tt.inspectErr != nil:
				client.EXPECT().GetCurrentWatchtowerContainer(mock.Anything, container.ContainerID(vpnID)).
					Return(nil, tt.inspectErr).Once()
			case tt.cgroupID != "":
				client.EXPECT().GetCurrentWatchtowerContainer(mock.Anything, container.ContainerID(vpnID)).
					Return(vpn, nil).Once()
			default:
				client.EXPECT().GetCurrentWatchtowerContainer(mock.Anything, container.ContainerID(vpnID)).
					Return(vpn, nil).Once()
				client.EXPECT().ListLabeledContainers(mock.Anything, labels.Watchtower).
					Return(tt.listed, tt.listErr).Once()
			}

			id, detected, err := GetCurrentContainerID(newTestLogger(t), t.Context(), fsys, client)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Empty(t, id)
				assert.Nil(t, detected)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantID, id)

			if tt.wantDetected {
				require.NotNil(t, detected)
				assert.Equal(t, tt.wantID, detected.ID())
			} else {
				assert.Nil(t, detected, "the network container is not Watchtower's own")
			}
		})
	}
}

// statErrorFs is a filesystem whose Stat fails with err for every path.
type statErrorFs struct {
	fs.FS

	// err is returned by every Stat call.
	err error
}

// Stat returns the configured error.
func (f statErrorFs) Stat(string) (fs.FileInfo, error) {
	return nil, f.err
}

// TestInContainer verifies that the container marker files decide whether
// Watchtower runs in a container, and that an unreadable marker counts as
// present.
func TestInContainer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// files are created before the check.
		files []string
		// statErr, when set, fails every Stat call.
		statErr error
		want    bool
	}{
		{name: "no marker", want: false},
		{name: "docker marker", files: []string{".dockerenv"}, want: true},
		{name: "podman marker", files: []string{"run/.containerenv"}, want: true},
		{name: "marker cannot be checked", statErr: fs.ErrPermission, want: true},
		{name: "markers do not exist", statErr: fs.ErrNotExist, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			files := fstest.MapFS{}
			for _, name := range tt.files {
				files[name] = &fstest.MapFile{}
			}

			var fsys fs.FS = files
			if tt.statErr != nil {
				fsys = statErrorFs{FS: files, err: tt.statErr}
			}

			assert.Equal(t, tt.want, InContainer(fsys))
		})
	}
}

// TestInContainer_WrappedNotExist verifies that a wrapped not-exist error
// still counts as a missing marker.
func TestInContainer_WrappedNotExist(t *testing.T) {
	t.Parallel()

	fsys := statErrorFs{FS: fstest.MapFS{}, err: errors.Join(errors.New("stat"), fs.ErrNotExist)}

	assert.False(t, InContainer(fsys))
}

// TestGetCurrentContainerID_FailureLogsAtDebug verifies that failing to find
// the container ID is not logged as an error. A Watchtower binary on the host
// has no container ID, and callers report a missing ID themselves. It sets
// environment variables, so it runs sequentially.
func TestGetCurrentContainerID_FailureLogsAtDebug(t *testing.T) {
	t.Setenv("HOSTNAME", "")

	var out bytes.Buffer

	log := zerolog.New(&out).Level(zerolog.TraceLevel)

	id, _, err := GetCurrentContainerID(&log, t.Context(), fstest.MapFS{}, nil)
	require.Error(t, err)
	assert.Empty(t, id)
	assert.NotContains(t, out.String(), `"level":"error"`)
	assert.NotContains(t, out.String(), `"level":"warn"`)

	// Find the summary record and check its level.
	assert.Equal(t, []string{"debug"}, logcapture.Levels(t, out.String(), "All container ID detection methods failed"))
}

// TestParseContainerIDFromMountinfo_Layouts verifies that the container ID is
// read from the mount roots Docker and Podman use, and that paths which only
// resemble them are ignored.
func TestParseContainerIDFromMountinfo_Layouts(t *testing.T) {
	t.Parallel()

	const id = "867e908db86764ddc4898108f1ffc69c7cdaa903079cfe71d27d3f78278be03c"

	tests := []struct {
		name string
		root string
		want container.ContainerID
	}{
		{name: "Docker", root: "/var/lib/docker/containers/" + id + "/hostname", want: id},
		{name: "rootless Podman", root: "/containers/overlay-containers/" + id + "/userdata/hostname", want: id},
		{
			name: "rootful Podman",
			root: "/var/lib/containers/storage/overlay-containers/" + id + "/userdata/hostname",
			want: id,
		},
		{name: "a directory that only ends in containers", root: "/srv/notcontainers/" + id + "/hostname"},
		{name: "a prefixed overlay directory", root: "/srv/my-overlay-containers/" + id + "/hostname"},
		{name: "a longer hexadecimal directory name", root: "/containers/overlay-containers/" + id + "a/userdata/hostname"},
		{name: "an ID that ends the path", root: "/var/lib/docker/containers/" + id, want: id},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mountinfo := "1234 24 0:52 " + tt.root + " /etc/hostname rw,relatime - ext4 /dev/sda1 rw\n"

			got, err := parseContainerIDFromMountinfo(newTestLogger(t), mountinfo)
			if tt.want == "" {
				require.ErrorIs(t, err, errNoValidContainerID)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestParseContainerIDFromCgroupString_Layouts verifies the container ID is
// read from cgroup paths under both the cgroupfs and the systemd cgroup
// drivers, for Docker and Podman, and not from paths without a full ID.
func TestParseContainerIDFromCgroupString_Layouts(t *testing.T) {
	t.Parallel()

	const id = "3f4e2b1a9c8d7e6f5a4b3c2d1e0f9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f"

	tests := []struct {
		name   string
		cgroup string
		want   container.ContainerID
	}{
		{name: "cgroupfs driver", cgroup: "12:memory:/docker/" + id, want: id},
		{
			// Docker running inside a Docker container nests the inner
			// container's cgroup under the outer one's.
			name:   "nested Docker",
			cgroup: "12:memory:/docker/" + strings.Repeat("a", 64) + "/docker/" + id,
			want:   id,
		},
		{name: "a parent cgroup named like a container", cgroup: "12:memory:/docker/" + id + "/app"},
		{name: "an ID longer than a full ID", cgroup: "12:memory:/docker/" + id + "ab"},
		{name: "systemd driver on cgroup v1", cgroup: "1:name=systemd:/system.slice/docker-" + id + ".scope", want: id},
		{name: "systemd driver on cgroup v2", cgroup: "0::/system.slice/docker-" + id + ".scope", want: id},
		{name: "Podman under systemd", cgroup: "0::/machine.slice/libpod-" + id + ".scope", want: id},
		{
			name:   "Podman under systemd with a nested container cgroup",
			cgroup: "0::/machine.slice/libpod-" + id + ".scope/container",
			want:   id,
		},
		{name: "namespaced cgroup v2", cgroup: "0::/"},
		{name: "scope without a full ID", cgroup: "0::/system.slice/docker-compose-app.scope"},
		{name: "short ID in a scope", cgroup: "0::/system.slice/docker-" + id[:12] + ".scope"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseContainerIDFromCgroupString(newTestLogger(t), tt.cgroup)
			if tt.want == "" {
				require.ErrorIs(t, err, errNoValidContainerID)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
