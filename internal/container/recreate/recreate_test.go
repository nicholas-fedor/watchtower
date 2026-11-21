package recreate

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	cerrdefs "github.com/containerd/errdefs"
	dockerContainer "github.com/moby/moby/api/types/container"
	dockerImage "github.com/moby/moby/api/types/image"
	dockerMount "github.com/moby/moby/api/types/mount"
	dockerNetwork "github.com/moby/moby/api/types/network"
	dockerClient "github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	mockContainer "github.com/nicholas-fedor/watchtower/internal/container/mocks"
	"github.com/nicholas-fedor/watchtower/internal/container/network"
	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/logcapture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/modelfixture"
)

// WithCPUSettings configures CPU settings for the mock container.
func WithCPUSettings(nanoCPUs, cpuShares, cpuQuota, cpuPeriod int64, cpusetCpus, cpusetMems string) containerfixture.Update {
	return func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
		if c.HostConfig == nil {
			c.HostConfig = &dockerContainer.HostConfig{}
		}

		c.HostConfig.NanoCPUs = nanoCPUs
		c.HostConfig.CPUShares = cpuShares
		c.HostConfig.CPUQuota = cpuQuota
		c.HostConfig.CPUPeriod = cpuPeriod
		c.HostConfig.CpusetCpus = cpusetCpus
		c.HostConfig.CpusetMems = cpusetMems
	}
}

// expectCreateAndRename expects the target container to be created as
// testTargetID and renamed to the source container's name.
//
// Parameters:
//   - api: Mocked Docker API.
//   - source: Source container whose name the target takes.
func expectCreateAndRename(api *mockContainer.MockAPIClient, source *container.Container) {
	api.EXPECT().ContainerCreate(mock.Anything, mock.Anything).
		Return(dockerClient.ContainerCreateResult{ID: testTargetID}, nil).Once()
	api.EXPECT().
		ContainerRename(mock.Anything, testTargetID, dockerClient.ContainerRenameOptions{NewName: source.Name()}).
		Return(dockerClient.ContainerRenameResult{}, nil).Once()
}

// TestHandleCPUSettings verifies how each CPU copy mode carries CPU settings
// from the source container to its replacement.
func TestHandleCPUSettings(t *testing.T) {
	t.Parallel()

	var (
		defaultNanoCPUs   int64 = 2000000000
		defaultCPUShares  int64 = 1024
		defaultCPUQuota   int64 = 100000
		defaultCPUPeriod  int64 = 100000
		defaultCpusetCpus       = "0-3"
		defaultCpusetMems       = "0"
	)

	// newHostConfig returns the create host config of a container with
	// every CPU setting populated.
	newHostConfig := func() *dockerContainer.HostConfig {
		return modelfixture.Container(
			WithCPUSettings(
				defaultNanoCPUs,
				defaultCPUShares,
				defaultCPUQuota,
				defaultCPUPeriod,
				defaultCpusetCpus,
				defaultCpusetMems,
			),
		).GetCreateHostConfig()
	}

	t.Run("should strip all CPU settings when mode is 'none'", func(t *testing.T) {
		t.Parallel()

		log, logOutput := logcapture.New(zerolog.DebugLevel)
		hostConfig := newHostConfig()

		handleCPUSettings(hostConfig, "none", false, log)
		assert.Equal(t, int64(0), hostConfig.NanoCPUs)
		assert.Equal(t, int64(0), hostConfig.CPUShares)
		assert.Equal(t, int64(0), hostConfig.CPUQuota)
		assert.Equal(t, int64(0), hostConfig.CPUPeriod)
		assert.Empty(t, hostConfig.CpusetCpus)
		assert.Empty(t, hostConfig.CpusetMems)
		assert.Contains(t, logOutput.String(), "Stripped all CPU settings")
	})

	t.Run("should preserve all CPU settings when mode is 'full'", func(t *testing.T) {
		t.Parallel()

		log, logOutput := logcapture.New(zerolog.DebugLevel)
		hostConfig := newHostConfig()

		handleCPUSettings(hostConfig, "full", false, log)
		assert.Equal(t, defaultNanoCPUs, hostConfig.NanoCPUs)
		assert.Equal(t, defaultCPUShares, hostConfig.CPUShares)
		assert.Equal(t, defaultCPUQuota, hostConfig.CPUQuota)
		assert.Equal(t, defaultCPUPeriod, hostConfig.CPUPeriod)
		assert.Equal(t, defaultCpusetCpus, hostConfig.CpusetCpus)
		assert.Equal(t, defaultCpusetMems, hostConfig.CpusetMems)
		assert.Contains(t, logOutput.String(), "Copied all CPU settings unchanged")
	})

	t.Run("should filter NanoCPUs when mode is 'auto' and isPodman is true", func(t *testing.T) {
		t.Parallel()

		log, logOutput := logcapture.New(zerolog.DebugLevel)
		hostConfig := newHostConfig()

		handleCPUSettings(hostConfig, "auto", true, log)
		assert.Equal(t, int64(0), hostConfig.NanoCPUs)
		assert.Equal(t, defaultCPUShares, hostConfig.CPUShares)
		assert.Equal(t, defaultCPUQuota, hostConfig.CPUQuota)
		assert.Contains(t, logOutput.String(), "Detected Podman, filtered NanoCPUs for compatibility")
	})

	t.Run("should preserve all CPU settings when mode is 'auto' and isPodman is false", func(t *testing.T) {
		t.Parallel()

		log, logOutput := logcapture.New(zerolog.DebugLevel)
		hostConfig := newHostConfig()

		handleCPUSettings(hostConfig, "auto", false, log)
		assert.Equal(t, defaultNanoCPUs, hostConfig.NanoCPUs)
		assert.Equal(t, defaultCPUShares, hostConfig.CPUShares)
		assert.Contains(t, logOutput.String(), "Detected Docker, copied all CPU settings")
	})

	t.Run("should default to 'full' mode for unknown CPU copy mode", func(t *testing.T) {
		t.Parallel()

		log, logOutput := logcapture.New(zerolog.DebugLevel)
		hostConfig := newHostConfig()

		handleCPUSettings(hostConfig, "unknown", false, log)
		assert.Equal(t, defaultNanoCPUs, hostConfig.NanoCPUs)
		assert.Contains(t, logOutput.String(), "Unknown CPU copy mode, defaulting to full")
	})
}

// TestCreateTargetContainer verifies how a replacement container is created
// from the source container's configuration, and how a failed create cleans up.
func TestCreateTargetContainer(t *testing.T) {
	t.Parallel()

	t.Run("should disable memory swappiness when flag is true", func(t *testing.T) {
		t.Parallel()

		log, logOutput := logcapture.New(zerolog.DebugLevel)
		source := newRunningSource()
		api := mockContainer.NewMockAPIClient(t)

		var created dockerClient.ContainerCreateOptions

		api.EXPECT().ContainerCreate(mock.Anything, mock.Anything).RunAndReturn(
			func(_ context.Context, options dockerClient.ContainerCreateOptions) (dockerClient.ContainerCreateResult, error) {
				created = options

				return dockerClient.ContainerCreateResult{ID: testTargetID}, nil
			},
		).Once()
		api.EXPECT().
			ContainerRename(mock.Anything, testTargetID, dockerClient.ContainerRenameOptions{NewName: source.Name()}).
			Return(dockerClient.ContainerRenameResult{}, nil).Once()

		newID, err := CreateTarget(log, t.Context(), api, source, network.FromContainer(log, source, "1.44"),
			"1.44", true, "auto", false,
		)
		require.NoError(t, err)
		assert.Equal(t, container.ContainerID(testTargetID), newID)
		require.NotNil(t, created.HostConfig)
		assert.Nil(t, created.HostConfig.MemorySwappiness)
		assert.Contains(t, logOutput.String(), "Disabled memory swappiness for Podman compatibility")
	})
	t.Run("should log MAC address details once", func(t *testing.T) {
		t.Parallel()

		log, logOutput := logcapture.New(zerolog.DebugLevel)
		source := newRunningSource()
		api := mockContainer.NewMockAPIClient(t)
		expectCreateAndRename(api, source)

		newID, err := CreateTarget(log, t.Context(), api, source, network.FromContainer(log, source, "1.44"),
			"1.44", false, "auto", false,
		)
		require.NoError(t, err)
		assert.Equal(t, container.ContainerID(testTargetID), newID)
		assert.Contains(t, logOutput.String(), "MAC address validation passed")
		assert.Equal(t, 1, strings.Count(logOutput.String(), "mac_address="),
			"the network config's MAC check runs once per recreation")
	})
	t.Run("should use full network config for API >= 1.44", func(t *testing.T) {
		t.Parallel()

		log, logOutput := logcapture.New(zerolog.DebugLevel)
		source := newRunningSource()
		api := mockContainer.NewMockAPIClient(t)
		expectCreateAndRename(api, source)

		newID, err := CreateTarget(log, t.Context(), api, source, network.FromContainer(log, source, "1.44"),
			"1.44", false, "auto", false,
		)
		require.NoError(t, err)
		assert.Equal(t, container.ContainerID(testTargetID), newID)
		assert.Contains(t, logOutput.String(),
			"Using full network config for API version >= 1.44 or single network")
	})
	// An ordinary recreation of a container rebound earlier mounts its kept
	// volumes anonymously again.
	t.Run("should keep a rebound container's image and anonymous volumes", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name      string
			rebinding bool
			image     string
			// label is the Zodiac original-image label, empty for none.
			label string
			// recorded reports whether an earlier rebind kept the volume
			// by name.
			recorded bool
			binds    []string
		}{
			{
				name:      "rebound container",
				rebinding: true,
				image:     "sha256:running",
				label:     "example/app:latest",
				binds:     []string{"anonymous-data:/data:nocopy"},
			},
			{name: "other container", rebinding: false, image: "example/app:latest", label: "", binds: nil},
			{
				name:      "container rebound earlier",
				rebinding: false,
				recorded:  true,
				image:     "example/app:latest",
				label:     "",
				binds:     []string{},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				updates := []containerfixture.Update{
					containerfixture.WithImageName("example/app:latest"),
					containerfixture.WithImageID("sha256:running"),
					containerfixture.WithInspectMounts([]dockerContainer.MountPoint{
						{Type: dockerMount.TypeVolume, Name: "anonymous-data", Destination: "/data", RW: true},
					}),
				}
				if tt.recorded {
					updates = append(updates,
						containerfixture.WithBinds([]string{"anonymous-data:/data:nocopy"}),
						containerfixture.WithLabels(map[string]string{
							labels.KeptVolumes: `[{"destination": "/data", "volume": "anonymous-data", "form": "volume"}]`,
						}),
					)
				}

				log, _ := logcapture.New(zerolog.DebugLevel)
				source := modelfixture.Container(updates...)
				source.SetRebinding(tt.rebinding)

				// The strict mock fails when a container that is not rebound
				// inspects its image reference.
				api := mockContainer.NewMockAPIClient(t)
				if tt.rebinding {
					api.EXPECT().ImageInspect(mock.Anything, "example/app:latest").
						Return(dockerClient.ImageInspectResult{ID: "sha256:newer"}, nil).Once()
				}

				var created dockerClient.ContainerCreateOptions

				api.EXPECT().ContainerCreate(mock.Anything, mock.Anything).RunAndReturn(
					func(_ context.Context, options dockerClient.ContainerCreateOptions) (dockerClient.ContainerCreateResult, error) {
						created = options

						return dockerClient.ContainerCreateResult{ID: testTargetID}, nil
					},
				).Once()
				api.EXPECT().
					ContainerRename(mock.Anything, testTargetID, dockerClient.ContainerRenameOptions{NewName: source.Name()}).
					Return(dockerClient.ContainerRenameResult{}, nil).Once()

				_, err := CreateTarget(log, t.Context(), api, source, network.FromContainer(log, source, "1.44"),
					"1.44", false, "auto", false,
				)
				require.NoError(t, err)
				require.NotNil(t, created.Config)
				require.NotNil(t, created.HostConfig)
				assert.Equal(t, tt.image, created.Config.Image)
				assert.Equal(t, tt.label, created.Config.Labels[labels.ZodiacOriginalImage])
				assert.Equal(t, tt.binds, created.HostConfig.Binds)
				assert.Equal(t, tt.rebinding, created.Config.Labels[labels.KeptVolumes] != "")
			})
		}
	})
	t.Run("should handle ContainerCreate failure", func(t *testing.T) {
		t.Parallel()

		log, logOutput := logcapture.New(zerolog.DebugLevel)
		source := newRunningSource()
		api := mockContainer.NewMockAPIClient(t)
		api.EXPECT().ContainerCreate(mock.Anything, mock.Anything).
			Return(dockerClient.ContainerCreateResult{}, errors.New("create failed")).Once()

		newID, err := CreateTarget(log, t.Context(), api, source, network.FromContainer(log, source, "1.44"),
			"1.44", false, "auto", false,
		)
		assert.Empty(t, newID)
		require.ErrorContains(t, err, "create failed")
		assert.Contains(t, logOutput.String(), "Failed to create new container")
	})
	t.Run("should log successful container creation", func(t *testing.T) {
		t.Parallel()

		log, logOutput := logcapture.New(zerolog.DebugLevel)
		source := newRunningSource()
		api := mockContainer.NewMockAPIClient(t)
		expectCreateAndRename(api, source)

		newID, err := CreateTarget(log, t.Context(), api, source, network.FromContainer(log, source, "1.44"),
			"1.44", false, "auto", false,
		)
		require.NoError(t, err)
		assert.Equal(t, container.ContainerID(testTargetID), newID)
		assert.Contains(t, logOutput.String(), "Created container successfully")
	})
	t.Run("should handle ContainerCreate Conflict error", func(t *testing.T) {
		t.Parallel()

		log, logOutput := logcapture.New(zerolog.DebugLevel)
		source := newRunningSource()
		api := mockContainer.NewMockAPIClient(t)
		api.EXPECT().ContainerCreate(mock.Anything, mock.Anything).
			Return(dockerClient.ContainerCreateResult{}, cerrdefs.ErrConflict).Once()

		newID, err := CreateTarget(log, t.Context(), api, source, network.FromContainer(log, source, "1.44"),
			"1.44", false, "auto", false,
		)
		assert.Empty(t, newID)
		require.ErrorContains(t, err, "failed to create container")
		assert.True(t, cerrdefs.IsConflict(err), "wrapped error should be detectable via cerrdefs.IsConflict")
		require.ErrorIs(t, err, cerrdefs.ErrConflict, "wrapped error should be detectable via errors.Is")
		assert.Contains(t, logOutput.String(), "Failed to create new container")
	})
	t.Run("should handle ContainerCreate InvalidArgument error", func(t *testing.T) {
		t.Parallel()

		log, _ := logcapture.New(zerolog.DebugLevel)
		source := newRunningSource()
		api := mockContainer.NewMockAPIClient(t)
		api.EXPECT().ContainerCreate(mock.Anything, mock.Anything).
			Return(dockerClient.ContainerCreateResult{}, cerrdefs.ErrInvalidArgument).Once()

		newID, err := CreateTarget(log, t.Context(), api, source, network.FromContainer(log, source, "1.44"),
			"1.44", false, "auto", false,
		)
		assert.Empty(t, newID)
		require.ErrorContains(t, err, "failed to create container")
	})
	t.Run("should attach multiple networks for legacy API and handle success", func(t *testing.T) {
		t.Parallel()

		log, logOutput := logcapture.New(zerolog.DebugLevel)
		source := newMultiNetworkSource()
		api := mockContainer.NewMockAPIClient(t)
		expectCreateAndRename(api, source)
		api.EXPECT().NetworkConnect(mock.Anything, mock.Anything, mock.MatchedBy(
			func(options dockerClient.NetworkConnectOptions) bool { return options.Container == testTargetID },
		)).Return(dockerClient.NetworkConnectResult{}, nil).Once()

		newID, err := CreateTarget(log, t.Context(), api, source, network.FromContainer(log, source, "1.23"),
			"1.23", false, "auto", false,
		)
		require.NoError(t, err)
		assert.Equal(t, container.ContainerID(testTargetID), newID)
		assert.Contains(t, logOutput.String(), "Selected first network for container creation")
		assert.Contains(t, logOutput.String(), "Attaching additional network to container")
		assert.Contains(t, logOutput.String(), "Successfully attached additional network")
	})
	t.Run("should attach multiple networks for legacy API and handle failure", func(t *testing.T) {
		t.Parallel()

		log, logOutput := logcapture.New(zerolog.DebugLevel)
		source := newMultiNetworkSource()
		api := mockContainer.NewMockAPIClient(t)
		expectCreateAndRename(api, source)
		api.EXPECT().NetworkConnect(mock.Anything, mock.Anything, mock.Anything).
			Return(dockerClient.NetworkConnectResult{}, errors.New("network connect failed")).Once()
		expectForceRemove(api)

		newID, err := CreateTarget(log, t.Context(), api, source, network.FromContainer(log, source, "1.23"),
			"1.23", false, "auto", false,
		)
		assert.Empty(t, newID)
		require.ErrorContains(t, err, "network connect failed")
		assert.Contains(t, logOutput.String(), "Failed to attach additional network")
	})
	t.Run("Context Propagation for Cleanup", func(t *testing.T) {
		t.Parallel()

		t.Run("should perform cleanup with non-canceled context after rename failure", func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			log, _ := logcapture.New(zerolog.DebugLevel)
			source := newRunningSource()
			api := mockContainer.NewMockAPIClient(t)
			api.EXPECT().ContainerCreate(mock.Anything, mock.Anything).
				Return(dockerClient.ContainerCreateResult{ID: testTargetID}, nil).Once()
			api.EXPECT().ContainerRename(mock.Anything, testTargetID, mock.Anything).
				Return(dockerClient.ContainerRenameResult{}, errors.New("rename failed")).Once()
			expectForceRemove(api)

			newID, err := CreateTarget(log, ctx, api, source, network.FromContainer(log, source, "1.44"),
				"1.44", false, "auto", false,
			)
			assert.Empty(t, newID)
			require.ErrorContains(t, err, "rename failed")
		})

		t.Run("should perform cleanup with non-canceled context after network attachment failure", func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			log, _ := logcapture.New(zerolog.DebugLevel)
			source := newMultiNetworkSource()
			api := mockContainer.NewMockAPIClient(t)
			expectCreateAndRename(api, source)
			api.EXPECT().NetworkConnect(mock.Anything, mock.Anything, mock.Anything).
				Return(dockerClient.NetworkConnectResult{}, errors.New("network connect failed")).Once()
			expectForceRemove(api)

			newID, err := CreateTarget(log, ctx, api, source, network.FromContainer(log, source, "1.23"),
				"1.23", false, "auto", false,
			)
			assert.Empty(t, newID)
			require.ErrorContains(t, err, "network connect failed")
		})
	})
}

// TestAttachNetworks verifies that a replacement container is connected to the
// source container's additional networks.
func TestAttachNetworks(t *testing.T) {
	t.Parallel()

	// networks returns a networking config with an endpoint for each
	// named network, whose ID is the name with a "-id" suffix.
	networks := func(names ...string) *dockerNetwork.NetworkingConfig {
		config := &dockerNetwork.NetworkingConfig{EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{}}
		for _, name := range names {
			config.EndpointsConfig[name] = &dockerNetwork.EndpointSettings{NetworkID: name + "-id"}
		}

		return config
	}

	t.Run("should attach additional networks successfully", func(t *testing.T) {
		t.Parallel()

		log, logOutput := logcapture.New(zerolog.DebugLevel)
		api := mockContainer.NewMockAPIClient(t)
		api.EXPECT().NetworkConnect(mock.Anything, "network2", dockerClient.NetworkConnectOptions{
			Container:      "container-id",
			EndpointConfig: &dockerNetwork.EndpointSettings{NetworkID: "network2-id"},
		}).Return(dockerClient.NetworkConnectResult{}, nil).Once()

		err := attachNetworks(t.Context(), api, "container-id", networks("network1", "network2"), networks("network1"), log)
		require.NoError(t, err)
		assert.Contains(t, logOutput.String(), "Attaching additional network to container")
		assert.Contains(t, logOutput.String(), "Successfully attached additional network")
	})

	t.Run("should return error when network connect fails", func(t *testing.T) {
		t.Parallel()

		log, logOutput := logcapture.New(zerolog.DebugLevel)
		api := mockContainer.NewMockAPIClient(t)
		api.EXPECT().NetworkConnect(mock.Anything, "network2", mock.Anything).
			Return(dockerClient.NetworkConnectResult{}, errors.New("connect failed")).Once()

		err := attachNetworks(t.Context(), api, "container-id", networks("network1", "network2"), networks("network1"), log)
		require.ErrorContains(t, err, "failed to attach network")
		assert.Contains(t, logOutput.String(), "Failed to attach additional network")
	})

	t.Run("should skip initial network and only attach additional ones", func(t *testing.T) {
		t.Parallel()

		log, logOutput := logcapture.New(zerolog.DebugLevel)

		err := attachNetworks(t.Context(), mockContainer.NewMockAPIClient(t), "container-id",
			networks("network1"), networks("network1"), log)
		require.NoError(t, err)
		assert.NotContains(t, logOutput.String(), "Attaching additional network")
	})
}

// TestSyncComposeImageLabel verifies that the Compose image label is pointed at
// the replacement container's new image.
func TestSyncComposeImageLabel(t *testing.T) {
	t.Parallel()

	const (
		oldImageID = "sha256:old-index"
		newImageID = "sha256:new-index"
		oldAmd64   = "sha256:old-amd64"
		newAmd64   = "sha256:new-amd64"
		imageRef   = "example/app:nightly"
	)

	// amd64Manifest builds an available linux/amd64 image manifest summary.
	amd64Manifest := func(id string) dockerImage.ManifestSummary {
		return dockerImage.ManifestSummary{
			ID:        id,
			Kind:      dockerImage.ManifestKindImage,
			Available: true,
			ImageData: &dockerImage.ImageProperties{
				Platform: ocispec.Platform{OS: "linux", Architecture: "amd64"},
			},
		}
	}

	// composeContainer builds a source container running oldImageID with
	// the given Compose image label.
	composeContainer := func(label string, extra ...containerfixture.Update) *container.Container {
		updates := append([]containerfixture.Update{
			containerfixture.WithImageName(imageRef),
			containerfixture.WithLabels(map[string]string{
				"com.docker.compose.project": "stack",
				"com.docker.compose.image":   label,
			}),
			func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
				c.Image = oldImageID
			},
		}, extra...)

		return modelfixture.Container(updates...)
	}

	// labelAPI is a mocked Docker API that records the config sent to
	// ContainerCreate and the image references inspected.
	type labelAPI struct {
		*mockContainer.MockAPIClient

		// created is the config the target container was created with.
		created *dockerContainer.Config
		// inspected lists the image references inspected, in order.
		inspected []string
	}

	// newLabelAPI returns a labelAPI that creates and renames any target
	// container and serves the old and new images, with their manifests
	// when the inspect asks for them. inspectErr, when set, fails every
	// image inspect instead.
	newLabelAPI := func(t *testing.T, inspectErr error) *labelAPI {
		t.Helper()

		api := &labelAPI{MockAPIClient: mockContainer.NewMockAPIClient(t)}
		images := map[string]dockerImage.InspectResponse{
			imageRef: {
				ID:        newImageID,
				Manifests: []dockerImage.ManifestSummary{amd64Manifest(newAmd64)},
			},
			oldImageID: {
				ID:        oldImageID,
				Manifests: []dockerImage.ManifestSummary{amd64Manifest(oldAmd64)},
			},
		}
		inspect := func(_ context.Context, ref string, options ...dockerClient.ImageInspectOption) (dockerClient.ImageInspectResult, error) {
			api.inspected = append(api.inspected, ref)

			if inspectErr != nil {
				return dockerClient.ImageInspectResult{}, inspectErr
			}

			image, ok := images[ref]
			if !ok {
				return dockerClient.ImageInspectResult{}, cerrdefs.ErrNotFound
			}

			if len(options) == 0 {
				image.Manifests = nil
			}

			return dockerClient.ImageInspectResult{InspectResponse: image}, nil
		}

		api.EXPECT().ImageInspect(mock.Anything, mock.Anything).RunAndReturn(inspect).Maybe()
		api.EXPECT().ImageInspect(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(inspect).Maybe()
		api.EXPECT().ContainerCreate(mock.Anything, mock.Anything).RunAndReturn(
			func(_ context.Context, options dockerClient.ContainerCreateOptions) (dockerClient.ContainerCreateResult, error) {
				api.created = options.Config

				return dockerClient.ContainerCreateResult{ID: testTargetID}, nil
			},
		)
		api.EXPECT().ContainerRename(mock.Anything, testTargetID, mock.Anything).
			Return(dockerClient.ContainerRenameResult{}, nil)

		return api
	}

	// create runs CreateTarget at the given API version and
	// returns the label value sent to ContainerCreate.
	create := func(t *testing.T, log *zerolog.Logger, api *labelAPI, source *container.Container, clientVersion string) string {
		t.Helper()

		networkConfig := &dockerNetwork.NetworkingConfig{EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{}}

		_, err := CreateTarget(log, t.Context(), api, source, networkConfig,
			clientVersion, false, "auto", false,
		)
		require.NoError(t, err)
		require.NotNil(t, api.created)

		return api.created.Labels["com.docker.compose.image"]
	}

	t.Run("should leave containers without the label untouched", func(t *testing.T) {
		t.Parallel()

		log, _ := logcapture.New(zerolog.DebugLevel)
		api := newLabelAPI(t, nil)

		assert.Empty(t, create(t, log, api, modelfixture.Container(containerfixture.WithImageName(imageRef)), "1.48"))
		assert.NotContains(t, api.created.Labels, "com.docker.compose.image")
		assert.Empty(t, api.inspected)
	})

	t.Run("should create the container with the label pointing at the new image", func(t *testing.T) {
		t.Parallel()

		log, logOutput := logcapture.New(zerolog.DebugLevel)

		assert.Equal(t, newImageID, create(t, log, newLabelAPI(t, nil), composeContainer(oldImageID), "1.44"))
		assert.Contains(t, logOutput.String(), "Updated Compose image label")
	})

	t.Run("should pass the client API version through for manifest digest labels", func(t *testing.T) {
		t.Parallel()

		log, _ := logcapture.New(zerolog.DebugLevel)
		api := newLabelAPI(t, nil)

		assert.Equal(t, newAmd64, create(t, log, api, composeContainer(oldAmd64), "1.48"))
		assert.Equal(t, oldAmd64, create(t, log, api, composeContainer(oldAmd64), "1.47"))
	})

	t.Run("should keep a label with no matching value for the new image", func(t *testing.T) {
		t.Parallel()

		log, logOutput := logcapture.New(zerolog.DebugLevel)

		assert.Equal(t, "sha256:unknown", create(t, log, newLabelAPI(t, nil), composeContainer("sha256:unknown"), "1.48"))
		assert.Contains(t, logOutput.String(), "Found no matching value for Compose image label, keeping it unchanged")
	})

	t.Run("should keep the label and still create the container when the image inspect fails", func(t *testing.T) {
		t.Parallel()

		log, logOutput := logcapture.New(zerolog.DebugLevel)

		assert.Equal(t, oldImageID, create(t, log, newLabelAPI(t, cerrdefs.ErrNotFound), composeContainer(oldImageID), "1.48"))
		assert.Contains(t, logOutput.String(), "Failed to resolve Compose image label, keeping it unchanged")
	})

	t.Run("should not modify the source container's labels", func(t *testing.T) {
		t.Parallel()

		log, _ := logcapture.New(zerolog.DebugLevel)
		source := composeContainer(oldImageID, func(_ *dockerContainer.InspectResponse, i *dockerImage.InspectResponse) {
			i.Config = nil
		})

		assert.Equal(t, newImageID, create(t, log, newLabelAPI(t, nil), source, "1.48"))
		assert.Equal(t, oldImageID, source.ContainerInfo().Config.Labels["com.docker.compose.image"])
	})
}

// testTargetID is the ID the mocked daemon gives a created target container.
const testTargetID = "new_container_id"

// expectForceRemove expects the target container to be force-removed with a
// context that is not canceled, so cleanup survives a canceled parent.
//
// Parameters:
//   - api: Mocked Docker API.
func expectForceRemove(api *mockContainer.MockAPIClient) {
	api.EXPECT().
		ContainerRemove(
			mock.MatchedBy(func(ctx context.Context) bool { return ctx.Err() == nil }),
			testTargetID,
			dockerClient.ContainerRemoveOptions{Force: true},
		).
		Return(dockerClient.ContainerRemoveResult{}, nil).Once()
}

// newRunningSource returns a running source container on the bridge
// network with a MAC address and a memory swappiness setting.
func newRunningSource() *container.Container {
	var defaultMem int64 = 60

	return modelfixture.Container(
		containerfixture.WithNetworkMode("bridge"),
		containerfixture.WithContainerState(dockerContainer.State{Running: true, Status: "running"}),
		containerfixture.WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
			"bridge": {
				NetworkID:  "network_bridge_id",
				IPAddress:  netip.MustParseAddr("172.17.0.2"),
				MacAddress: dockerNetwork.HardwareAddr("aa:bb:cc:dd:ee:ff"),
				Aliases:    []string{"test-watchtower"},
			},
		}),
		func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
			if c.HostConfig == nil {
				c.HostConfig = &dockerContainer.HostConfig{}
			}

			c.HostConfig.MemorySwappiness = &defaultMem
		},
	)
}

// newMultiNetworkSource returns a running source container attached
// to two networks.
func newMultiNetworkSource() *container.Container {
	return modelfixture.Container(
		containerfixture.WithNetworkMode("bridge"),
		containerfixture.WithContainerState(dockerContainer.State{Running: true, Status: "running"}),
		containerfixture.WithNetworks("network1", "network2"),
	)
}

// TestKeepRunningImage verifies that a new container is created from the
// image the source container runs when its image reference names another
// image or none. It also verifies that the reference is kept in the Zodiac
// original-image label so later scans still check it against its registry.
func TestKeepRunningImage(t *testing.T) {
	t.Parallel()

	const (
		running  = "sha256:running"
		imageRef = "example/app:latest"
	)

	tests := []struct {
		name string
		// named is the image the reference names, empty when it names none.
		named string
		want  string
		// wantLabel is the Zodiac original-image label, empty for none.
		wantLabel string
		// withoutImageInfo builds the source as when its image inspect failed,
		// which leaves only the container inspect to name the running image.
		withoutImageInfo bool
	}{
		{name: "reference names the running image", named: running, want: imageRef, wantLabel: ""},
		{name: "reference names a newer image", named: "sha256:newer", want: running, wantLabel: imageRef},
		{name: "reference names no image", named: "", want: running, wantLabel: imageRef},
		{
			name:             "running image known only from the container",
			named:            "sha256:newer",
			want:             running,
			wantLabel:        imageRef,
			withoutImageInfo: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			build := modelfixture.Container
			if tt.withoutImageInfo {
				build = modelfixture.ContainerWithoutImageInfo
			}

			source := build(
				containerfixture.WithImageName(imageRef),
				containerfixture.WithImageID(running),
			)

			api := mockContainer.NewMockAPIClient(t)

			call := api.EXPECT().ImageInspect(mock.Anything, imageRef)
			if tt.named == "" {
				call.Return(dockerClient.ImageInspectResult{}, cerrdefs.ErrNotFound).Once()
			} else {
				call.Return(dockerClient.ImageInspectResult{ID: tt.named}, nil).Once()
			}

			config := source.GetCreateConfig()
			log, _ := logcapture.New(zerolog.DebugLevel)

			keepRunningImage(t.Context(), api, log, source, config)

			assert.Equal(t, tt.want, config.Image)
			assert.Equal(t, tt.wantLabel, config.Labels[labels.ZodiacOriginalImage])
			assert.NotContains(t, source.ContainerInfo().Config.Labels, labels.ZodiacOriginalImage,
				"the inspect data is not changed")
		})
	}
}

// TestKeepAnonymousVolumes verifies that the new container mounts the source
// container's anonymous volumes, including those declared as volume mounts
// without a source. It keeps its binds and named volume mounts as they are.
// A bind and a mount target are written with a trailing slash, which Docker
// keeps in the host config but not in the inspect mount points. A bind that
// gives only a destination names its anonymous volume rather than gaining a
// second mount at the same path. Every kept volume is mounted without copying
// the image's files into it. The new container's labels record each kept
// volume, and restoring them declares each one as the source container did.
func TestKeepAnonymousVolumes(t *testing.T) {
	t.Parallel()

	source := modelfixture.Container(
		containerfixture.WithBinds([]string{"config:/config/", "/srv/media:/media:ro", "/state"}),
		containerfixture.WithMounts([]dockerMount.Mount{
			{Type: dockerMount.TypeVolume, Source: "logs", Target: "/logs"},
			{
				Type:          dockerMount.TypeVolume,
				Target:        "/scratch/",
				VolumeOptions: &dockerMount.VolumeOptions{Labels: map[string]string{"tier": "cache"}},
			},
		}),
		containerfixture.WithInspectMounts([]dockerContainer.MountPoint{
			{Type: dockerMount.TypeVolume, Name: "anonymous-data", Destination: "/data", RW: true},
			{Type: dockerMount.TypeVolume, Name: "anonymous-cache", Destination: "/cache", RW: false},
			{Type: dockerMount.TypeVolume, Name: "config", Destination: "/config", RW: true},
			{Type: dockerMount.TypeVolume, Name: "logs", Destination: "/logs", RW: true},
			{Type: dockerMount.TypeVolume, Name: "anonymous-scratch", Destination: "/scratch", RW: true},
			{Type: dockerMount.TypeBind, Source: "/srv/media", Destination: "/media", RW: true},
			{Type: dockerMount.TypeVolume, Name: "anonymous-state", Destination: "/state", RW: true},
		}),
	)

	config := source.GetCreateConfig()
	hostConfig := source.GetCreateHostConfig()
	log, _ := logcapture.New(zerolog.DebugLevel)

	keepAnonymousVolumes(log, source, config, hostConfig)

	assert.Equal(t, []dockerMount.Mount{
		{Type: dockerMount.TypeVolume, Source: "logs", Target: "/logs"},
		{
			Type:   dockerMount.TypeVolume,
			Source: "anonymous-scratch",
			Target: "/scratch/",
			VolumeOptions: &dockerMount.VolumeOptions{
				NoCopy: true,
				Labels: map[string]string{"tier": "cache"},
			},
		},
	}, hostConfig.Mounts)
	assert.Equal(t, []dockerMount.Mount{
		{Type: dockerMount.TypeVolume, Source: "logs", Target: "/logs"},
		{
			Type:          dockerMount.TypeVolume,
			Target:        "/scratch/",
			VolumeOptions: &dockerMount.VolumeOptions{Labels: map[string]string{"tier": "cache"}},
		},
	}, source.ContainerInfo().HostConfig.Mounts, "the inspect data is not changed")
	assert.Equal(t, []string{
		"config:/config/",
		"/srv/media:/media:ro",
		"anonymous-state:/state:nocopy",
		"anonymous-data:/data:nocopy",
		"anonymous-cache:/cache:ro,nocopy",
	}, hostConfig.Binds)
	assert.Equal(t, []string{"config:/config/", "/srv/media:/media:ro", "/state"},
		source.ContainerInfo().HostConfig.Binds, "the inspect data is not changed")
	assert.JSONEq(t, `[
		{"destination": "/state", "volume": "anonymous-state", "form": "bind"},
		{"destination": "/scratch/", "volume": "anonymous-scratch", "form": "mount"},
		{"destination": "/data", "volume": "anonymous-data", "form": "volume"},
		{"destination": "/cache", "volume": "anonymous-cache", "form": "volume"}
	]`, config.Labels[labels.KeptVolumes])
	assert.NotContains(t, source.ContainerInfo().Config.Labels, labels.KeptVolumes, "the inspect data is not changed")

	// A later recreation that does not keep them declares each volume as
	// the source container did, so it gets a new anonymous volume there.
	restoreAnonymousVolumes(log, config, hostConfig)

	assert.Equal(t, source.GetCreateHostConfig().Binds, hostConfig.Binds)
	assert.Equal(t, source.GetCreateHostConfig().Mounts, hostConfig.Mounts)
	assert.NotContains(t, config.Labels, labels.KeptVolumes)
}

// TestRestoreAnonymousVolumes verifies that a recreation which does not keep
// anonymous volumes mounts only the recorded ones anonymously again. A bind or
// mount that no longer names the recorded volume, such as one the user
// changed, is left as it is. A record that cannot be read changes nothing but
// is removed.
func TestRestoreAnonymousVolumes(t *testing.T) {
	t.Parallel()

	log, _ := logcapture.New(zerolog.DebugLevel)

	t.Run("leaves binds and mounts that name other volumes", func(t *testing.T) {
		t.Parallel()

		config := &dockerContainer.Config{Labels: map[string]string{
			labels.KeptVolumes: `[{"destination": "/data", "volume": "anonymous-data", "form": "bind"},
				{"destination": "/cache", "volume": "anonymous-cache", "form": "mount", "noCopy": true}]`,
		}}
		hostConfig := &dockerContainer.HostConfig{
			Binds:  []string{"user-data:/data", "config:/config"},
			Mounts: []dockerMount.Mount{{Type: dockerMount.TypeVolume, Source: "anonymous-cache", Target: "/cache"}},
		}

		restoreAnonymousVolumes(log, config, hostConfig)

		assert.Equal(t, []string{"user-data:/data", "config:/config"}, hostConfig.Binds)
		assert.Equal(t, []dockerMount.Mount{{
			Type:          dockerMount.TypeVolume,
			Target:        "/cache",
			VolumeOptions: &dockerMount.VolumeOptions{NoCopy: true},
		}}, hostConfig.Mounts)
		assert.Empty(t, config.Labels)
	})

	t.Run("unreadable record", func(t *testing.T) {
		t.Parallel()

		config := &dockerContainer.Config{Labels: map[string]string{labels.KeptVolumes: "not json"}}
		hostConfig := &dockerContainer.HostConfig{Binds: []string{"anonymous-data:/data:nocopy"}}

		restoreAnonymousVolumes(log, config, hostConfig)

		assert.Equal(t, []string{"anonymous-data:/data:nocopy"}, hostConfig.Binds)
		assert.Empty(t, config.Labels)
	})
}

// TestKeepAnonymousVolumes_volumesFrom verifies that a volume the source
// container got from a volumes-from container is not mounted by name. Docker
// takes it from that container again, which may have a new volume there since
// it was recreated. The volumes-from destinations are those recorded when the
// source container was listed, so a volumes-from container recreated with
// other volumes since then changes nothing. The source container's own
// anonymous volume is still kept.
func TestKeepAnonymousVolumes_volumesFrom(t *testing.T) {
	t.Parallel()

	source := modelfixture.Container(
		containerfixture.WithVolumesFrom([]string{"vpn:ro"}),
		containerfixture.WithInspectMounts([]dockerContainer.MountPoint{
			{Type: dockerMount.TypeVolume, Name: "anonymous-vpn-state", Destination: "/state", RW: false},
			{Type: dockerMount.TypeVolume, Name: "anonymous-data", Destination: "/data", RW: true},
		}),
	)
	source.SetVolumesFromDestinations([]string{"/state"})

	config := source.GetCreateConfig()
	hostConfig := source.GetCreateHostConfig()
	log, _ := logcapture.New(zerolog.DebugLevel)

	keepAnonymousVolumes(log, source, config, hostConfig)

	assert.Empty(t, hostConfig.Mounts)
	assert.Equal(t, []string{"anonymous-data:/data:nocopy"}, hostConfig.Binds)
	assert.Equal(t, []string{"vpn:ro"}, hostConfig.VolumesFrom)
}
