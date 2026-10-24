package container

import (
	"encoding/json"
	"net/netip"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"
	dockerImage "github.com/moby/moby/api/types/image"
	dockerNetwork "github.com/moby/moby/api/types/network"

	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// testShortID is the short ID of the container whose endpoint aliases the
// network tests filter.
const testShortID = "abc123def456"

// Test_getNetworkConfig verifies the networking config built for a replacement
// container across network modes, client versions, and missing or partial
// network settings.
func Test_getNetworkConfig(t *testing.T) {
	t.Parallel()

	t.Run("with bridge network mode", func(t *testing.T) {
		t.Parallel()

		t.Run("should return network config with processed endpoints", func(t *testing.T) {
			t.Parallel()

			container := MockContainer(
				containerfixture.WithNetworkMode("bridge"),
				containerfixture.WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
					"bridge": {
						NetworkID:  "bridge_network_id",
						MacAddress: dockerNetwork.HardwareAddr("aa:bb:cc:dd:ee:ff"),
						IPAddress:  netip.MustParseAddr("172.17.0.2"),
						Aliases:    []string{"container_id", "test-alias"},
						DNSNames:   []string{"test.example.com"},
						IPAMConfig: &dockerNetwork.EndpointIPAMConfig{
							IPv4Address: netip.MustParseAddr("172.17.0.2"),
						},
					},
				}),
			)

			config := getNetworkConfig(newTestLogger(t), container, "1.50")

			require.NotNil(t, config)
			assert.Contains(t, config.EndpointsConfig, "bridge")
			endpoint := config.EndpointsConfig["bridge"]
			assert.Equal(t, "bridge_network_id", endpoint.NetworkID)
			assert.Equal(t, dockerNetwork.HardwareAddr("aa:bb:cc:dd:ee:ff"), endpoint.MacAddress)
			assert.Equal(t, netip.MustParseAddr("172.17.0.2"), endpoint.IPAddress)
			assert.ElementsMatch(t, []string{"test.example.com"}, endpoint.DNSNames)
			require.NotNil(t, endpoint.IPAMConfig)
			assert.Equal(t, netip.MustParseAddr("172.17.0.2"), endpoint.IPAMConfig.IPv4Address)
			// Aliases should be filtered to remove container short ID
			assert.ElementsMatch(t, []string{"test-alias"}, endpoint.Aliases)
		})

		t.Run("should handle multiple networks", func(t *testing.T) {
			t.Parallel()

			container := MockContainer(
				containerfixture.WithNetworks("bridge", "custom_network"),
			)

			config := getNetworkConfig(newTestLogger(t), container, "1.50")

			require.Len(t, config.EndpointsConfig, 2)
			assert.Contains(t, config.EndpointsConfig, "bridge")
			assert.Contains(t, config.EndpointsConfig, "custom_network")
		})

		t.Run("should clear engine-generated MAC for running container without validation error", func(t *testing.T) {
			t.Parallel()

			container := MockContainer(
				containerfixture.WithNetworkMode("bridge"),
				containerfixture.WithContainerState(dockerContainer.State{Running: true, Status: "running"}),
				containerfixture.WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
					"bridge": {
						NetworkID:  "bridge_network_id",
						MacAddress: mustParseMAC(t, "02:42:ac:11:00:02"),
						IPAddress:  netip.MustParseAddr("172.17.0.2"),
					},
				}),
			)

			config := getNetworkConfig(newTestLogger(t), container, "1.50")

			require.NotNil(t, config)
			assert.Contains(t, config.EndpointsConfig, "bridge")
			endpoint := config.EndpointsConfig["bridge"]
			assert.Equal(t, dockerNetwork.HardwareAddr{}, endpoint.MacAddress)
		})
	})

	t.Run("with host network mode", func(t *testing.T) {
		t.Parallel()

		t.Run("should return network config with cleared host-specific settings", func(t *testing.T) {
			t.Parallel()

			container := MockContainer(
				containerfixture.WithNetworkMode("host"),
				containerfixture.WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
					"host": {
						MacAddress: dockerNetwork.HardwareAddr("02:42:ac:11:00:02"),
						IPAddress:  netip.MustParseAddr("192.168.1.100"),
						Aliases:    []string{"container_id", "host-alias"},
						DNSNames:   []string{"host.example.com"},
						IPAMConfig: &dockerNetwork.EndpointIPAMConfig{
							IPv4Address: netip.MustParseAddr("192.168.1.100"),
						},
					},
				}),
			)

			config := getNetworkConfig(newTestLogger(t), container, "1.50")

			assert.Contains(t, config.EndpointsConfig, "host")
			endpoint := config.EndpointsConfig["host"]
			assert.Equal(t, dockerNetwork.HardwareAddr{}, endpoint.MacAddress)
			assert.Equal(t, netip.Addr{}, endpoint.IPAddress)
			assert.Nil(t, endpoint.DNSNames)
			assert.Nil(t, endpoint.IPAMConfig)
			assert.Nil(t, endpoint.Aliases)
		})
	})

	t.Run("with legacy client version (< 1.44)", func(t *testing.T) {
		t.Parallel()

		t.Run("should clear MAC addresses and IP info", func(t *testing.T) {
			t.Parallel()

			container := MockContainer(
				containerfixture.WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
					"bridge": {
						MacAddress: dockerNetwork.HardwareAddr("02:42:ac:11:00:02"),
						IPAddress:  netip.MustParseAddr("172.17.0.2"),
						DNSNames:   []string{"test.example.com"},
					},
				}),
			)

			config := getNetworkConfig(newTestLogger(t), container, "1.40")

			endpoint := config.EndpointsConfig["bridge"]
			assert.Equal(t, dockerNetwork.HardwareAddr{}, endpoint.MacAddress)
			assert.Equal(t, netip.Addr{}, endpoint.IPAddress)
			assert.Nil(t, endpoint.DNSNames)
		})
	})

	t.Run("with nil network settings", func(t *testing.T) {
		t.Parallel()

		t.Run("should return empty network config", func(t *testing.T) {
			t.Parallel()

			container := MockContainer(
				func(container *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
					container.NetworkSettings = nil
				},
			)

			config := getNetworkConfig(newTestLogger(t), container, "1.50")

			require.NotNil(t, config)
			assert.Empty(t, config.EndpointsConfig)
		})
	})

	t.Run("with stopped container (exited state)", func(t *testing.T) {
		t.Parallel()

		t.Run("should handle MAC address validation for non-running containers", func(t *testing.T) {
			t.Parallel()

			container := MockContainer(
				containerfixture.WithContainerState(dockerContainer.State{Running: false, Status: "exited"}),
				containerfixture.WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
					"bridge": {
						// No MAC address for stopped container - this is expected
					},
				}),
			)

			config := getNetworkConfig(newTestLogger(t), container, "1.50")

			require.NotNil(t, config)
			assert.Contains(t, config.EndpointsConfig, "bridge")
		})
	})

	t.Run("with custom network settings", func(t *testing.T) {
		t.Parallel()

		t.Run("should preserve custom network configurations", func(t *testing.T) {
			t.Parallel()

			container := MockContainer(
				containerfixture.WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
					"my_custom_network": {
						NetworkID:  "custom_net_id",
						MacAddress: dockerNetwork.HardwareAddr("aa:bb:cc:dd:ee:ff"),
						IPAddress:  netip.MustParseAddr("10.0.0.5"),
						Aliases:    []string{"container_id", "custom-alias"},
						Links:      []string{"other_container:alias"},
					},
				}),
			)

			config := getNetworkConfig(newTestLogger(t), container, "1.50")

			endpoint := config.EndpointsConfig["my_custom_network"]
			assert.Equal(t, "custom_net_id", endpoint.NetworkID)
			assert.Equal(t, dockerNetwork.HardwareAddr("aa:bb:cc:dd:ee:ff"), endpoint.MacAddress)
			assert.Equal(t, netip.MustParseAddr("10.0.0.5"), endpoint.IPAddress)
			assert.ElementsMatch(t, []string{"custom-alias"}, endpoint.Aliases)
			assert.ElementsMatch(t, []string{"other_container:alias"}, endpoint.Links)
		})
	})

	t.Run("with Podman-style client version", func(t *testing.T) {
		t.Parallel()

		t.Run("should handle Podman API version strings", func(t *testing.T) {
			t.Parallel()

			container := MockContainer(
				containerfixture.WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
					"bridge": {
						MacAddress: dockerNetwork.HardwareAddr("02:42:ac:11:00:02"),
					},
				}),
			)

			// Test with Podman version format
			config := getNetworkConfig(newTestLogger(t), container, "4.0.0")

			endpoint := config.EndpointsConfig["bridge"]
			// Should preserve MAC for modern versions
			assert.Equal(t, dockerNetwork.HardwareAddr("02:42:ac:11:00:02"), endpoint.MacAddress)
		})
	})

	t.Run("with nil endpoint in network settings", func(t *testing.T) {
		t.Parallel()

		t.Run("should skip nil endpoints", func(t *testing.T) {
			t.Parallel()

			container := MockContainer(
				containerfixture.WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
					"bridge":    nil,
					"valid_net": {NetworkID: "valid_id"},
				}),
			)

			config := getNetworkConfig(newTestLogger(t), container, "1.50")

			assert.Contains(t, config.EndpointsConfig, "valid_net")
			assert.NotContains(t, config.EndpointsConfig, "bridge")
		})
	})

	// A source MAC on a host network or a legacy API version is cleared for
	// the new container, which is expected, so nothing is logged above debug.
	t.Run("clears host and legacy MAC addresses without warning", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name          string
			networkMode   string
			network       string
			clientVersion string
		}{
			{name: "host network on a modern API", networkMode: "host", network: "host", clientVersion: "1.50"},
			{name: "host network on a legacy API", networkMode: "host", network: "host", clientVersion: "1.43"},
			{name: "bridge network on a legacy API", networkMode: "bridge", network: "bridge", clientVersion: "1.43"},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				log, logbuf := newCapturedLogger(zerolog.DebugLevel)
				container := MockContainer(
					containerfixture.WithNetworkMode(tc.networkMode),
					containerfixture.WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
						tc.network: {
							NetworkID:  "network_id",
							MacAddress: dockerNetwork.HardwareAddr("02:42:ac:11:00:99"),
						},
					}),
				)

				config := getNetworkConfig(log, container, tc.clientVersion)

				assert.Empty(t, config.EndpointsConfig[tc.network].MacAddress)
				assert.Contains(t, logbuf.String(), "Skipping MAC address validation for host network or legacy API")
				assert.NotContains(t, logbuf.String(), "level=warn")
			})
		}
	})
}

// Test_newEmptyNetworkConfig verifies that an empty networking config has a
// usable, empty endpoints map.
func Test_newEmptyNetworkConfig(t *testing.T) {
	t.Parallel()

	t.Run("should create a non-nil network config", func(t *testing.T) {
		t.Parallel()

		config := newEmptyNetworkConfig()
		require.NotNil(t, config)
	})

	t.Run("should initialize EndpointsConfig as an empty map", func(t *testing.T) {
		t.Parallel()

		config := newEmptyNetworkConfig()
		require.NotNil(t, config.EndpointsConfig)
		assert.Empty(t, config.EndpointsConfig)
		assert.Equal(t, make(map[string]*dockerNetwork.EndpointSettings), config.EndpointsConfig)
	})

	t.Run("should return a properly structured NetworkingConfig", func(t *testing.T) {
		t.Parallel()

		config := newEmptyNetworkConfig()
		assert.IsType(t, &dockerNetwork.NetworkingConfig{}, config)
		assert.IsType(t, make(map[string]*dockerNetwork.EndpointSettings), config.EndpointsConfig)
	})

	t.Run("should be ready for use - can add endpoints to the map", func(t *testing.T) {
		t.Parallel()

		config := newEmptyNetworkConfig()
		endpoint := &dockerNetwork.EndpointSettings{
			NetworkID: "test-network",
		}
		config.EndpointsConfig["test"] = endpoint
		require.Len(t, config.EndpointsConfig, 1)
		assert.Equal(t, endpoint, config.EndpointsConfig["test"])
	})
}

// Test_processEndpoint verifies which endpoint settings are copied to a
// replacement container, including when an engine-generated MAC address is
// cleared, for modern, legacy, and host-network configurations.
func Test_processEndpoint(t *testing.T) {
	t.Parallel()

	t.Run("with modern API version (>= 1.44)", func(t *testing.T) {
		t.Parallel()

		clientVersion := "1.50"

		t.Run("and non-host network mode", func(t *testing.T) {
			t.Parallel()

			isHostNetwork := false

			t.Run("should preserve MAC address, IP address, and DNS names", func(t *testing.T) {
				t.Parallel()

				sourceEndpoint := &dockerNetwork.EndpointSettings{
					NetworkID:  "bridge_network_id",
					MacAddress: dockerNetwork.HardwareAddr("aa:bb:cc:dd:ee:ff"),
					IPAddress:  netip.MustParseAddr("172.17.0.2"),
					DNSNames:   []string{"test.example.com"},
					Aliases:    []string{"container_id", "test-alias"},
					IPAMConfig: &dockerNetwork.EndpointIPAMConfig{
						IPv4Address: netip.MustParseAddr("172.17.0.2"),
					},
				}
				containerID := types.ContainerID("container_id")

				result, err := processEndpoint(newTestLogger(t), sourceEndpoint,
					containerID,
					clientVersion,
					isHostNetwork,
				)
				require.NoError(t, err)

				assert.Equal(t, "bridge_network_id", result.NetworkID)
				assert.Equal(t, dockerNetwork.HardwareAddr("aa:bb:cc:dd:ee:ff"), result.MacAddress)
				assert.Equal(t, netip.MustParseAddr("172.17.0.2"), result.IPAddress)
				assert.ElementsMatch(t, []string{"test.example.com"}, result.DNSNames)
				require.NotNil(t, result.IPAMConfig)
				assert.Equal(t, netip.MustParseAddr("172.17.0.2"), result.IPAMConfig.IPv4Address)
				// Aliases should be filtered to remove container short ID
				assert.ElementsMatch(t, []string{"test-alias"}, result.Aliases)
			})

			t.Run("should clear engine-generated MAC when it matches the endpoint IP", func(t *testing.T) {
				t.Parallel()

				sourceEndpoint := &dockerNetwork.EndpointSettings{
					NetworkID:  "bridge_network_id",
					MacAddress: mustParseMAC(t, "02:42:ac:11:00:02"),
					IPAddress:  netip.MustParseAddr("172.17.0.2"),
				}
				containerID := types.ContainerID("container_id")

				result, err := processEndpoint(newTestLogger(t), sourceEndpoint,
					containerID,
					clientVersion,
					isHostNetwork,
				)
				require.NoError(t, err)

				assert.Equal(t, dockerNetwork.HardwareAddr{}, result.MacAddress)
			})

			t.Run("should preserve user-configured MAC that does not match the endpoint IP", func(t *testing.T) {
				t.Parallel()

				sourceEndpoint := &dockerNetwork.EndpointSettings{
					NetworkID:  "bridge_network_id",
					MacAddress: dockerNetwork.HardwareAddr("aa:bb:cc:dd:ee:ff"),
					IPAddress:  netip.MustParseAddr("172.17.0.2"),
				}
				containerID := types.ContainerID("container_id")

				result, err := processEndpoint(newTestLogger(t), sourceEndpoint,
					containerID,
					clientVersion,
					isHostNetwork,
				)
				require.NoError(t, err)

				assert.Equal(t, dockerNetwork.HardwareAddr("aa:bb:cc:dd:ee:ff"), result.MacAddress)
			})

			t.Run("should preserve engine-generated MAC when endpoint IP is empty", func(t *testing.T) {
				t.Parallel()

				sourceEndpoint := &dockerNetwork.EndpointSettings{
					NetworkID:  "bridge_network_id",
					MacAddress: mustParseMAC(t, "02:42:ac:11:00:02"),
					IPAddress:  netip.Addr{},
				}
				containerID := types.ContainerID("container_id")

				result, err := processEndpoint(newTestLogger(t), sourceEndpoint,
					containerID,
					clientVersion,
					isHostNetwork,
				)
				require.NoError(t, err)

				assert.Equal(t, mustParseMAC(t, "02:42:ac:11:00:02"), result.MacAddress)
			})

			t.Run("should preserve engine-generated MAC when endpoint IP is IPv6", func(t *testing.T) {
				t.Parallel()

				sourceEndpoint := &dockerNetwork.EndpointSettings{
					NetworkID:  "bridge_network_id",
					MacAddress: mustParseMAC(t, "02:42:ac:11:00:02"),
					IPAddress:  netip.MustParseAddr("::1"),
				}
				containerID := types.ContainerID("container_id")

				result, err := processEndpoint(newTestLogger(t), sourceEndpoint,
					containerID,
					clientVersion,
					isHostNetwork,
				)
				require.NoError(t, err)

				assert.Equal(t, mustParseMAC(t, "02:42:ac:11:00:02"), result.MacAddress)
			})

			t.Run("should clear empty MAC without error", func(t *testing.T) {
				t.Parallel()

				sourceEndpoint := &dockerNetwork.EndpointSettings{
					NetworkID:  "bridge_network_id",
					MacAddress: dockerNetwork.HardwareAddr{},
					IPAddress:  netip.MustParseAddr("172.17.0.2"),
				}
				containerID := types.ContainerID("container_id")

				result, err := processEndpoint(newTestLogger(t), sourceEndpoint,
					containerID,
					clientVersion,
					isHostNetwork,
				)
				require.NoError(t, err)

				assert.Equal(t, dockerNetwork.HardwareAddr{}, result.MacAddress)
			})

			t.Run("should preserve user-configured MAC matching engine-generated prefix with different IP", func(t *testing.T) {
				t.Parallel()

				// A user-configured MAC that happens to start with 02:42 but whose last
				// four bytes do not match the endpoint's IP must not be cleared.
				sourceEndpoint := &dockerNetwork.EndpointSettings{
					NetworkID:  "bridge_network_id",
					MacAddress: mustParseMAC(t, "02:42:de:ad:be:ef"),
					IPAddress:  netip.MustParseAddr("172.17.0.2"),
				}
				containerID := types.ContainerID("container_id")

				result, err := processEndpoint(newTestLogger(t), sourceEndpoint,
					containerID,
					clientVersion,
					isHostNetwork,
				)
				require.NoError(t, err)

				assert.Equal(t, mustParseMAC(t, "02:42:de:ad:be:ef"), result.MacAddress)
			})

			t.Run("should handle multiple aliases correctly", func(t *testing.T) {
				t.Parallel()

				sourceEndpoint := &dockerNetwork.EndpointSettings{
					Aliases: []string{"container_id", "alias1", "alias2", "other_id"},
				}
				containerID := types.ContainerID("container_id")

				result, err := processEndpoint(newTestLogger(t), sourceEndpoint,
					containerID,
					clientVersion,
					isHostNetwork,
				)
				require.NoError(t, err)

				assert.ElementsMatch(t, []string{"alias1", "alias2", "other_id"}, result.Aliases)
			})

			t.Run("should copy all IPAM config fields", func(t *testing.T) {
				t.Parallel()

				sourceEndpoint := &dockerNetwork.EndpointSettings{
					IPAMConfig: &dockerNetwork.EndpointIPAMConfig{
						IPv4Address: netip.MustParseAddr("192.168.1.100"),
						IPv6Address: netip.MustParseAddr("2001:db8::1"),
						LinkLocalIPs: []netip.Addr{
							netip.MustParseAddr("169.254.1.1"),
							netip.MustParseAddr("169.254.1.2"),
						},
					},
				}
				containerID := types.ContainerID("container_id")

				result, err := processEndpoint(newTestLogger(t), sourceEndpoint,
					containerID,
					clientVersion,
					isHostNetwork,
				)
				require.NoError(t, err)

				require.NotNil(t, result.IPAMConfig)
				assert.Equal(t, netip.MustParseAddr("192.168.1.100"), result.IPAMConfig.IPv4Address)
				assert.Equal(t, netip.MustParseAddr("2001:db8::1"), result.IPAMConfig.IPv6Address)
				assert.ElementsMatch(t, []netip.Addr{netip.MustParseAddr("169.254.1.1"), netip.MustParseAddr("169.254.1.2")}, result.IPAMConfig.LinkLocalIPs)
			})

			t.Run("should preserve empty aliases list", func(t *testing.T) {
				t.Parallel()

				sourceEndpoint := &dockerNetwork.EndpointSettings{
					Aliases: []string{},
				}
				containerID := types.ContainerID("test_container_id")

				result, err := processEndpoint(newTestLogger(t), sourceEndpoint,
					containerID,
					clientVersion,
					isHostNetwork,
				)
				require.NoError(t, err)

				assert.Empty(t, result.Aliases)
			})

			t.Run("should preserve other endpoint fields", func(t *testing.T) {
				t.Parallel()

				sourceEndpoint := &dockerNetwork.EndpointSettings{
					Gateway:    netip.MustParseAddr("172.17.0.1"),
					Links:      []string{"other_container:alias"},
					EndpointID: "endpoint_id",
				}
				containerID := types.ContainerID("test_container_id")

				result, err := processEndpoint(newTestLogger(t), sourceEndpoint,
					containerID,
					clientVersion,
					isHostNetwork,
				)
				require.NoError(t, err)

				assert.Equal(t, netip.MustParseAddr("172.17.0.1"), result.Gateway)
				assert.ElementsMatch(t, []string{"other_container:alias"}, result.Links)
				assert.Equal(t, "endpoint_id", result.EndpointID)
			})
		})
	})

	t.Run("with legacy API version (< 1.44)", func(t *testing.T) {
		t.Parallel()

		clientVersion := "1.40"

		t.Run("and non-host network mode", func(t *testing.T) {
			t.Parallel()

			isHostNetwork := false

			t.Run("should clear MAC address, IP address, and DNS names", func(t *testing.T) {
				t.Parallel()

				sourceEndpoint := &dockerNetwork.EndpointSettings{
					MacAddress: dockerNetwork.HardwareAddr("02:42:ac:11:00:02"),
					IPAddress:  netip.MustParseAddr("172.17.0.2"),
					DNSNames:   []string{"test.example.com"},
					Aliases:    []string{"container_id", "test-alias"},
					IPAMConfig: &dockerNetwork.EndpointIPAMConfig{
						IPv4Address: netip.MustParseAddr("172.17.0.2"),
					},
				}
				containerID := types.ContainerID("container_id")

				result, err := processEndpoint(newTestLogger(t), sourceEndpoint,
					containerID,
					clientVersion,
					isHostNetwork,
				)
				require.NoError(t, err)

				assert.Equal(t, dockerNetwork.HardwareAddr{}, result.MacAddress)
				assert.Equal(t, netip.Addr{}, result.IPAddress)
				assert.Nil(t, result.DNSNames)
				require.NotNil(t, result.IPAMConfig)
				assert.Equal(t, netip.MustParseAddr("172.17.0.2"), result.IPAMConfig.IPv4Address)
				// Aliases should still be filtered
				assert.ElementsMatch(t, []string{"test-alias"}, result.Aliases)
			})
		})
	})

	t.Run("with host network mode", func(t *testing.T) {
		t.Parallel()

		isHostNetwork := true

		t.Run("should clear aliases regardless of API version", func(t *testing.T) {
			t.Parallel()

			sourceEndpoint := &dockerNetwork.EndpointSettings{
				Aliases: []string{"container_id", "test-alias"},
			}
			containerID := types.ContainerID("test_container_id")

			result, err := processEndpoint(newTestLogger(t), sourceEndpoint,
				containerID,
				"1.50",
				isHostNetwork,
			)
			require.NoError(t, err)

			assert.Nil(t, result.Aliases)
		})

		t.Run("should clear IPAM config regardless of API version", func(t *testing.T) {
			t.Parallel()

			sourceEndpoint := &dockerNetwork.EndpointSettings{
				IPAMConfig: &dockerNetwork.EndpointIPAMConfig{
					IPv4Address: netip.MustParseAddr("192.168.1.100"),
				},
			}
			containerID := types.ContainerID("test_container_id")

			result, err := processEndpoint(newTestLogger(t), sourceEndpoint,
				containerID,
				"1.50",
				isHostNetwork,
			)
			require.NoError(t, err)

			assert.Nil(t, result.IPAMConfig)
		})

		t.Run("should clear MAC and IP addresses even with modern API", func(t *testing.T) {
			t.Parallel()

			sourceEndpoint := &dockerNetwork.EndpointSettings{
				MacAddress: dockerNetwork.HardwareAddr("02:42:ac:11:00:02"),
				IPAddress:  netip.MustParseAddr("192.168.1.100"),
				DNSNames:   []string{"host.example.com"},
			}
			containerID := types.ContainerID("test_container_id")

			result, err := processEndpoint(newTestLogger(t), sourceEndpoint,
				containerID,
				"1.50",
				isHostNetwork,
			)
			require.NoError(t, err)

			assert.Equal(t, dockerNetwork.HardwareAddr{}, result.MacAddress)
			assert.Equal(t, netip.Addr{}, result.IPAddress)
			assert.Nil(t, result.DNSNames)
		})
	})

	t.Run("with edge cases", func(t *testing.T) {
		t.Parallel()

		t.Run("should handle nil IPAM config gracefully", func(t *testing.T) {
			t.Parallel()

			sourceEndpoint := &dockerNetwork.EndpointSettings{
				IPAMConfig: nil,
			}
			containerID := types.ContainerID("test_container_id")

			result, err := processEndpoint(newTestLogger(t), sourceEndpoint,
				containerID,
				"1.50",
				false,
			)
			require.NoError(t, err)

			assert.Nil(t, result.IPAMConfig)
		})

		t.Run("should handle empty DNS names", func(t *testing.T) {
			t.Parallel()

			sourceEndpoint := &dockerNetwork.EndpointSettings{
				DNSNames: []string{},
			}
			containerID := types.ContainerID("test_container_id")

			result, err := processEndpoint(newTestLogger(t), sourceEndpoint,
				containerID,
				"1.50",
				false,
			)
			require.NoError(t, err)

			assert.Empty(t, result.DNSNames)
		})

		t.Run("with nil source endpoint", func(t *testing.T) {
			t.Parallel()

			t.Run("should return ErrNilSourceEndpoint when sourceEndpoint is nil", func(t *testing.T) {
				t.Parallel()

				containerID := types.ContainerID("test_container_id")

				result, err := processEndpoint(newTestLogger(t), nil,
					containerID,
					"1.50",
					false,
				)
				require.Error(t, err)
				require.ErrorIs(t, err, errNilSourceEndpoint)
				assert.Nil(t, result)
			})
		})
	})
}

// Test_isEngineGeneratedMAC verifies that only a MAC address derived from the
// endpoint's IPv4 address counts as generated by the engine.
func Test_isEngineGeneratedMAC(t *testing.T) {
	t.Parallel()

	t.Run("should return true for MAC derived from the endpoint IPv4 address", func(t *testing.T) {
		t.Parallel()

		mac := mustParseMAC(t, "02:42:ac:11:00:02")
		ip := netip.MustParseAddr("172.17.0.2")

		assert.True(t, isEngineGeneratedMAC(mac, ip))
	})

	t.Run("should return true for a MAC unmarshaled from Docker inspect JSON", func(t *testing.T) {
		t.Parallel()

		var endpoint dockerNetwork.EndpointSettings

		err := json.Unmarshal(
			[]byte(`{"MacAddress":"02:42:ac:11:00:02","IPAddress":"172.17.0.2"}`),
			&endpoint,
		)
		require.NoError(t, err)
		require.Len(t, endpoint.MacAddress, 6)
		assert.True(t, isEngineGeneratedMAC(endpoint.MacAddress, endpoint.IPAddress))
	})

	t.Run("should return false for colon-separated ASCII bytes that are not a parsed HardwareAddr", func(t *testing.T) {
		t.Parallel()

		mac := dockerNetwork.HardwareAddr("02:42:ac:11:00:02")
		ip := netip.MustParseAddr("172.17.0.2")

		require.Len(t, mac, 17)
		assert.False(t, isEngineGeneratedMAC(mac, ip))
	})

	t.Run("should return false for user-configured MAC with matching prefix but different IP", func(t *testing.T) {
		t.Parallel()

		mac := mustParseMAC(t, "02:42:de:ad:be:ef")
		ip := netip.MustParseAddr("172.17.0.2")

		assert.False(t, isEngineGeneratedMAC(mac, ip))
	})

	t.Run("should return false when MAC is empty", func(t *testing.T) {
		t.Parallel()

		mac := dockerNetwork.HardwareAddr{}
		ip := netip.MustParseAddr("172.17.0.2")

		assert.False(t, isEngineGeneratedMAC(mac, ip))
	})

	t.Run("should return false when IP is empty", func(t *testing.T) {
		t.Parallel()

		mac := mustParseMAC(t, "02:42:ac:11:00:02")
		ip := netip.Addr{}

		assert.False(t, isEngineGeneratedMAC(mac, ip))
	})

	t.Run("should return false when IP is IPv6", func(t *testing.T) {
		t.Parallel()

		mac := mustParseMAC(t, "02:42:ac:11:00:02")
		ip := netip.MustParseAddr("::1")

		assert.False(t, isEngineGeneratedMAC(mac, ip))
	})

	t.Run("should return false when MAC length is not 6", func(t *testing.T) {
		t.Parallel()

		mac := dockerNetwork.HardwareAddr("02:42:ac:11")
		ip := netip.MustParseAddr("172.17.0.2")

		assert.False(t, isEngineGeneratedMAC(mac, ip))
	})

	t.Run("should return true for the reproduction case from issue #2104", func(t *testing.T) {
		t.Parallel()

		mac := mustParseMAC(t, "02:42:0a:63:00:02")
		ip := netip.MustParseAddr("10.99.0.2")

		assert.True(t, isEngineGeneratedMAC(mac, ip))
	})
}

// Test_onlyGeneratedMacs verifies that a container counts as having only
// engine-generated MAC addresses when every original endpoint is covered and
// none has a user-configured MAC.
func Test_onlyGeneratedMacs(t *testing.T) {
	t.Parallel()

	t.Run("should return true when all original MACs are engine-generated and all endpoints are covered", func(t *testing.T) {
		t.Parallel()

		container := MockContainer(
			containerfixture.WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
				"bridge": {
					NetworkID:  "bridge_network_id",
					MacAddress: mustParseMAC(t, "02:42:ac:11:00:02"),
					IPAddress:  netip.MustParseAddr("172.17.0.2"),
				},
			}),
		)
		processedConfig := &dockerNetwork.NetworkingConfig{
			EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
				"bridge": {
					NetworkID: "bridge_network_id",
				},
			},
		}

		assert.True(t, onlyGeneratedMacs(container, processedConfig))
	})

	t.Run("should return false when a non-nil original endpoint is missing from the processed config", func(t *testing.T) {
		t.Parallel()

		container := MockContainer(
			containerfixture.WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
				"bridge": {
					NetworkID:  "bridge_network_id",
					MacAddress: mustParseMAC(t, "02:42:ac:11:00:02"),
					IPAddress:  netip.MustParseAddr("172.17.0.2"),
				},
				"custom": {
					NetworkID:  "custom_network_id",
					MacAddress: mustParseMAC(t, "02:42:0a:00:00:05"),
					IPAddress:  netip.MustParseAddr("10.0.0.5"),
				},
			}),
		)
		processedConfig := &dockerNetwork.NetworkingConfig{
			EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
				"bridge": {
					NetworkID: "bridge_network_id",
				},
			},
		}

		assert.False(t, onlyGeneratedMacs(container, processedConfig))
	})

	t.Run("should return false when there is a user-configured MAC", func(t *testing.T) {
		t.Parallel()

		container := MockContainer(
			containerfixture.WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
				"bridge": {
					NetworkID:  "bridge_network_id",
					MacAddress: dockerNetwork.HardwareAddr("aa:bb:cc:dd:ee:ff"),
					IPAddress:  netip.MustParseAddr("172.17.0.2"),
				},
			}),
		)
		processedConfig := &dockerNetwork.NetworkingConfig{
			EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
				"bridge": {
					NetworkID: "bridge_network_id",
				},
			},
		}

		assert.False(t, onlyGeneratedMacs(container, processedConfig))
	})

	t.Run("should return false when there are no MACs at all", func(t *testing.T) {
		t.Parallel()

		container := MockContainer(
			containerfixture.WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
				"bridge": {
					NetworkID: "bridge_network_id",
					IPAddress: netip.MustParseAddr("172.17.0.2"),
				},
			}),
		)
		processedConfig := &dockerNetwork.NetworkingConfig{
			EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
				"bridge": {
					NetworkID: "bridge_network_id",
				},
			},
		}

		assert.False(t, onlyGeneratedMacs(container, processedConfig))
	})

	t.Run("should return false when container info is nil", func(t *testing.T) {
		t.Parallel()

		processedConfig := &dockerNetwork.NetworkingConfig{}

		assert.False(t, onlyGeneratedMacs(nil, processedConfig))
	})
}

// Test_validateMacAddresses verifies where a MAC address in the networking
// config is expected or an error, by client version, network mode, and
// container state.
func Test_validateMacAddresses(t *testing.T) {
	t.Parallel()

	// processEndpoint clears MAC addresses for legacy API versions and host
	// networks, so the validator expects none there and reports no error.
	t.Run("with legacy API version (< 1.44) or host network mode", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name          string
			clientVersion string
			isHostNetwork bool
		}{
			{name: "legacy API with bridge network", clientVersion: "1.40", isHostNetwork: false},
			{name: "legacy API with host network", clientVersion: "1.40", isHostNetwork: true},
			{name: "modern API with host network", clientVersion: "1.50", isHostNetwork: true},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				log, logbuf := newCapturedLogger(zerolog.DebugLevel)
				container := MockContainer(containerfixture.WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
					"bridge": {NetworkID: "bridge_network_id"},
				}))
				config := &dockerNetwork.NetworkingConfig{
					EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
						"bridge": {NetworkID: "bridge_network_id"},
					},
				}

				err := validateMacAddresses(log, config, container.ID(), tc.clientVersion, tc.isHostNetwork, container)
				require.NoError(t, err)
				assert.Contains(t, logbuf.String(), "Skipping MAC address validation for host network or legacy API")
				assert.Contains(t, logbuf.String(), "container="+container.Name())
				assert.Contains(t, logbuf.String(), "id="+container.ID().ShortID())
			})
		}
	})

	t.Run("with modern API version (>= 1.44)", func(t *testing.T) {
		t.Parallel()

		clientVersion := "1.50"

		t.Run("and non-host network mode", func(t *testing.T) {
			t.Parallel()

			isHostNetwork := false

			t.Run("and running container", func(t *testing.T) {
				t.Parallel()

				t.Run("should succeed when MAC address is present", func(t *testing.T) {
					t.Parallel()

					container := MockContainer(
						containerfixture.WithContainerState(dockerContainer.State{Running: true, Status: "running"}),
						containerfixture.WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
							"bridge": {
								NetworkID:  "bridge_network_id",
								MacAddress: dockerNetwork.HardwareAddr("02:42:ac:11:00:02"),
								IPAddress:  netip.MustParseAddr("172.17.0.2"),
							},
						}),
					)
					config := &dockerNetwork.NetworkingConfig{
						EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
							"bridge": {
								NetworkID:  "bridge_network_id",
								MacAddress: dockerNetwork.HardwareAddr("02:42:ac:11:00:02"),
								IPAddress:  netip.MustParseAddr("172.17.0.2"),
							},
						},
					}

					err := validateMacAddresses(newTestLogger(t), config,
						container.ID(),
						clientVersion,
						isHostNetwork,
						container,
					)
					require.NoError(t, err)
				})

				t.Run("should return error when MAC address is missing", func(t *testing.T) {
					t.Parallel()

					container := MockContainer(
						containerfixture.WithContainerState(dockerContainer.State{Running: true, Status: "running"}),
						containerfixture.WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
							"bridge": {
								NetworkID: "bridge_network_id",
								IPAddress: netip.MustParseAddr("172.17.0.2"),
							},
						}),
					)
					config := &dockerNetwork.NetworkingConfig{
						EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
							"bridge": {
								NetworkID: "bridge_network_id",
								IPAddress: netip.MustParseAddr("172.17.0.2"),
							},
						},
					}

					err := validateMacAddresses(newTestLogger(t), config,
						container.ID(),
						clientVersion,
						isHostNetwork,
						container,
					)
					require.Error(t, err)
					require.ErrorIs(t, err, errNoMacInNonHost)
				})

				t.Run("should succeed with multiple networks all having MAC addresses", func(t *testing.T) {
					t.Parallel()

					container := MockContainer(
						containerfixture.WithContainerState(dockerContainer.State{Running: true, Status: "running"}),
						containerfixture.WithNetworks("bridge", "custom_network"),
						containerfixture.WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
							"bridge": {
								NetworkID:  "bridge_network_id",
								MacAddress: dockerNetwork.HardwareAddr("02:42:ac:11:00:02"),
								IPAddress:  netip.MustParseAddr("172.17.0.2"),
							},
							"custom_network": {
								NetworkID:  "custom_network_id",
								MacAddress: dockerNetwork.HardwareAddr("aa:bb:cc:dd:ee:ff"),
								IPAddress:  netip.MustParseAddr("10.0.0.5"),
							},
						}),
					)
					config := &dockerNetwork.NetworkingConfig{
						EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
							"bridge": {
								NetworkID:  "bridge_network_id",
								MacAddress: dockerNetwork.HardwareAddr("02:42:ac:11:00:02"),
								IPAddress:  netip.MustParseAddr("172.17.0.2"),
							},
							"custom_network": {
								NetworkID:  "custom_network_id",
								MacAddress: dockerNetwork.HardwareAddr("aa:bb:cc:dd:ee:ff"),
								IPAddress:  netip.MustParseAddr("10.0.0.5"),
							},
						},
					}

					err := validateMacAddresses(newTestLogger(t), config,
						container.ID(),
						clientVersion,
						isHostNetwork,
						container,
					)
					require.NoError(t, err)
				})

				t.Run("should succeed when at least one of multiple networks has MAC address", func(t *testing.T) {
					t.Parallel()

					container := MockContainer(
						containerfixture.WithContainerState(
							dockerContainer.State{Running: true, Status: "running"},
						),
						containerfixture.WithNetworks("bridge", "custom_network"),
						containerfixture.WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
							"bridge": {
								NetworkID:  "bridge_network_id",
								MacAddress: dockerNetwork.HardwareAddr("02:42:ac:11:00:02"),
								IPAddress:  netip.MustParseAddr("172.17.0.2"),
							},
							"custom_network": {
								NetworkID: "custom_network_id",
								IPAddress: netip.MustParseAddr("10.0.0.5"),
							},
						}),
					)
					config := &dockerNetwork.NetworkingConfig{
						EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
							"bridge": {
								NetworkID:  "bridge_network_id",
								MacAddress: dockerNetwork.HardwareAddr("02:42:ac:11:00:02"),
								IPAddress:  netip.MustParseAddr("172.17.0.2"),
							},
							"custom_network": {
								NetworkID: "custom_network_id",
								IPAddress: netip.MustParseAddr("10.0.0.5"),
							},
						},
					}

					err := validateMacAddresses(newTestLogger(t), config,
						container.ID(),
						clientVersion,
						isHostNetwork,
						container,
					)
					require.NoError(t, err)
				})
			})

			t.Run("and non-running container", func(t *testing.T) {
				t.Parallel()

				t.Run("should succeed when MAC address is missing (created state)", func(t *testing.T) {
					t.Parallel()

					container := MockContainer(
						containerfixture.WithContainerState(
							dockerContainer.State{Running: false, Status: "created"},
						),
						containerfixture.WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
							"bridge": {
								NetworkID: "bridge_network_id",
								IPAddress: netip.MustParseAddr("172.17.0.2"),
							},
						}),
					)
					config := &dockerNetwork.NetworkingConfig{
						EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
							"bridge": {
								NetworkID: "bridge_network_id",
								IPAddress: netip.MustParseAddr("172.17.0.2"),
							},
						},
					}

					err := validateMacAddresses(newTestLogger(t), config,
						container.ID(),
						clientVersion,
						isHostNetwork,
						container,
					)
					require.NoError(t, err)
				})

				t.Run("should succeed when MAC address is missing (exited state)", func(t *testing.T) {
					t.Parallel()

					container := MockContainer(
						containerfixture.WithContainerState(dockerContainer.State{Running: false, Status: "exited"}),
						containerfixture.WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
							"bridge": {
								NetworkID: "bridge_network_id",
								IPAddress: netip.MustParseAddr("172.17.0.2"),
							},
						}),
					)
					config := &dockerNetwork.NetworkingConfig{
						EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{
							"bridge": {
								NetworkID: "bridge_network_id",
								IPAddress: netip.MustParseAddr("172.17.0.2"),
							},
						},
					}

					err := validateMacAddresses(newTestLogger(t), config,
						container.ID(),
						clientVersion,
						isHostNetwork,
						container,
					)
					require.NoError(t, err)
				})
			})
		})
	})

	t.Run("with edge cases", func(t *testing.T) {
		t.Parallel()

		t.Run("should handle empty network configuration", func(t *testing.T) {
			t.Parallel()

			container := MockContainer(
				containerfixture.WithContainerState(dockerContainer.State{Running: true, Status: "running"}),
			)
			config := &dockerNetwork.NetworkingConfig{
				EndpointsConfig: map[string]*dockerNetwork.EndpointSettings{},
			}

			err := validateMacAddresses(newTestLogger(t), config,
				container.ID(),
				"1.50",
				false,
				container,
			)
			require.Error(t, err)
			require.ErrorIs(t, err, errNoMacInNonHost)
		})

		t.Run("should handle nil network configuration", func(t *testing.T) {
			t.Parallel()

			container := MockContainer(
				containerfixture.WithContainerState(dockerContainer.State{Running: true, Status: "running"}),
			)

			err := validateMacAddresses(newTestLogger(t), nil,
				container.ID(),
				"1.50",
				false,
				container,
			)
			require.Error(t, err)
			require.ErrorIs(t, err, errNoMacInNonHost)
		})
	})
}

// Test_filterAliases verifies that only exact matches of the container's short
// ID are removed from its network aliases.
func Test_filterAliases(t *testing.T) {
	t.Parallel()

	t.Run("removes container short ID from aliases", func(t *testing.T) {
		t.Parallel()

		shortID := testShortID
		aliases := []string{testShortID, "custom-alias", "another-alias"}
		result := filterAliases(aliases, shortID)
		assert.ElementsMatch(t, []string{"custom-alias", "another-alias"}, result)
	})

	t.Run("preserves all aliases when short ID is not present", func(t *testing.T) {
		t.Parallel()

		shortID := testShortID
		aliases := []string{"custom-alias", "another-alias", "third-alias"}
		result := filterAliases(aliases, shortID)
		assert.ElementsMatch(t, []string{"custom-alias", "another-alias", "third-alias"}, result)
	})

	t.Run("handles empty alias list", func(t *testing.T) {
		t.Parallel()

		shortID := testShortID
		aliases := []string{}
		result := filterAliases(aliases, shortID)
		assert.Empty(t, result)
	})

	t.Run("handles nil alias list", func(t *testing.T) {
		t.Parallel()

		shortID := testShortID

		var aliases []string

		result := filterAliases(aliases, shortID)
		assert.Empty(t, result)
	})

	t.Run("removes multiple instances of short ID", func(t *testing.T) {
		t.Parallel()

		shortID := testShortID
		aliases := []string{testShortID, "custom-alias", testShortID, "another-alias"}
		result := filterAliases(aliases, shortID)
		assert.ElementsMatch(t, []string{"custom-alias", "another-alias"}, result)
	})

	t.Run("returns empty list when all aliases are the short ID", func(t *testing.T) {
		t.Parallel()

		shortID := testShortID
		aliases := []string{testShortID, testShortID}
		result := filterAliases(aliases, shortID)
		assert.Empty(t, result)
	})

	t.Run("does not remove partial matches", func(t *testing.T) {
		t.Parallel()

		shortID := testShortID
		aliases := []string{"abc123def4567", "abc123def45", testShortID, "custom-alias"}
		result := filterAliases(aliases, shortID)
		assert.ElementsMatch(t, []string{"abc123def4567", "abc123def45", "custom-alias"}, result)
	})

	t.Run("handles case-sensitive matching", func(t *testing.T) {
		t.Parallel()

		shortID := testShortID
		aliases := []string{"ABC123DEF456", testShortID, "Abc123Def456"}
		result := filterAliases(aliases, shortID)
		assert.ElementsMatch(t, []string{"ABC123DEF456", "Abc123Def456"}, result)
	})

	t.Run("preserves order of remaining aliases", func(t *testing.T) {
		t.Parallel()

		shortID := testShortID
		aliases := []string{"first", testShortID, "second", "third", testShortID}
		result := filterAliases(aliases, shortID)
		assert.Equal(t, []string{"first", "second", "third"}, result)
	})
}
