// Package auth_test provides comprehensive tests for the registry authentication
// functionality in Watchtower. It includes test suites for token retrieval,
// challenge URL generation, and authentication URL construction, ensuring
// robust coverage of the auth package's core operations.
package auth_test

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/distribution/reference"
	"github.com/docker/docker/api/types/image"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	dockerContainerType "github.com/docker/docker/api/types/container"

	"github.com/nicholas-fedor/watchtower/pkg/registry/auth"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// TestAuth executes the registry authentication test suite using the Ginkgo
// testing framework. It registers Gomega’s fail handler to report test failures
// and runs the full set of specifications defined in this file.
func TestAuth(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Registry Auth Suite")
}

// SkipIfCredentialsEmpty creates a test function that conditionally skips execution
// based on the presence of registry credentials. It checks if the username or password
// is empty, skipping the test with an appropriate message if either is missing, and
// otherwise returns the provided test function for execution.
func SkipIfCredentialsEmpty(credentials *types.RegistryCredentials, testFunc func()) func() {
	switch {
	case credentials.Username == "":
		return func() {
			ginkgo.Skip("Username missing. Skipping integration test")
		}
	case credentials.Password == "":
		return func() {
			ginkgo.Skip("Password missing. Skipping integration test")
		}
	default:
		return testFunc
	}
}

// mockContainer defines a test-specific implementation of the types.Container
// interface. It provides a minimal, controlled structure for mocking container
// behavior in authentication tests, ensuring predictable and isolated test cases.
type mockContainer struct {
	id        string // Unique identifier for the container
	name      string // Human-readable name of the container
	imageName string // Image name used by the container
}

// ID returns the container’s unique identifier as a types.ContainerID. This method
// satisfies part of the types.Container interface, allowing the mock to be used
// in authentication functions requiring an ID.
func (m mockContainer) ID() types.ContainerID {
	return types.ContainerID(m.id)
}

// Name returns the container’s name as a string. This method provides a readable
// identifier for the container, fulfilling another requirement of the types.Container
// interface, though it’s not directly used in these tests.
func (m mockContainer) Name() string {
	return m.name
}

// ImageName returns the container’s image name, such as "ghcr.io/test/image". This
// method is critical for authentication tests, as it provides the image reference
// that the auth package processes to fetch tokens or construct URLs.
func (m mockContainer) ImageName() string {
	return m.imageName
}

// Enabled indicates whether the container is enabled for Watchtower operations and
// provides a secondary status flag. This method satisfies the types.Container interface,
// returning two booleans: the first indicates enablement (true by default), and the second
// is a placeholder (false by default), as these tests do not require specific status logic.
func (m mockContainer) Enabled() (bool, bool) {
	return true, false // Minimal stub: enabled true, secondary status false
}

// ContainerInfo returns a pointer to a containertypes.InspectResponse, which contains
// detailed container metadata. For these tests, it returns nil since the auth package
// does not require this information, satisfying the interface with a minimal stub.
func (m mockContainer) ContainerInfo() *dockerContainerType.InspectResponse {
	return nil // Minimal stub, not used in these tests
}

// GetCreateConfig returns a pointer to a containertypes.Config, representing the
// container’s creation configuration. This method satisfies the types.Container interface,
// returning nil as a minimal stub since the auth package does not use this data in these tests.
func (m mockContainer) GetCreateConfig() *dockerContainerType.Config {
	return nil // Minimal stub, not used in these tests
}

// GetCreateHostConfig returns a pointer to a containertypes.HostConfig, representing the
// container’s host-specific creation configuration (e.g., port bindings, network settings).
// This method satisfies the types.Container interface, returning nil as a minimal stub since
// the auth package does not use this data in these authentication-focused tests.
func (m mockContainer) GetCreateHostConfig() *dockerContainerType.HostConfig {
	return nil // Minimal stub, not used in these tests
}

// GetLifecyclePreCheckCommand returns a string representing the command to run
// before a lifecycle check (e.g., pre-update verification). This method satisfies the
// types.Container interface, returning an empty string as a minimal stub since the auth
// package does not rely on this functionality in these authentication-focused tests.
func (m mockContainer) GetLifecyclePreCheckCommand() string {
	return "" // Minimal stub, not used in these tests
}

// GetLifecyclePostCheckCommand returns a string representing the command to run
// after a lifecycle check (e.g., post-update verification). This method satisfies the
// types.Container interface, returning an empty string as a minimal stub since the auth
// package does not rely on this functionality in these authentication-focused tests.
func (m mockContainer) GetLifecyclePostCheckCommand() string {
	return "" // Minimal stub, not used in these tests
}

// GetLifecyclePreUpdateCommand returns a string representing the command to run
// before a lifecycle update (e.g., pre-update actions). This method satisfies the
// types.Container interface, returning an empty string as a minimal stub since the auth
// package does not rely on this functionality in these authentication-focused tests.
func (m mockContainer) GetLifecyclePreUpdateCommand() string {
	return "" // Minimal stub, not used in these tests
}

// GetLifecyclePostUpdateCommand returns a string representing the command to run
// after a lifecycle update (e.g., post-update actions). This method satisfies the
// types.Container interface, returning an empty string as a minimal stub since the auth
// package does not rely on this functionality in these authentication-focused tests.
func (m mockContainer) GetLifecyclePostUpdateCommand() string {
	return "" // Minimal stub, not used in these tests
}

// ImageID returns the container's current image ID. This method satisfies the
// types.Container interface, returning an empty ImageID as a minimal stub since
// the auth package does not use this data in these authentication-focused tests.
func (m mockContainer) ImageID() types.ImageID {
	return types.ImageID("")
}

// SafeImageID returns a safe version of the container's image ID. This method satisfies
// the types.Container interface, returning an empty ImageID as a minimal stub since
// the auth package does not use this data in these authentication-focused tests.
func (m mockContainer) SafeImageID() types.ImageID {
	return types.ImageID("")
}

// IsRunning indicates whether the container is currently running. This method satisfies
// the types.Container interface, returning true as a minimal stub since the auth package
// does not rely on this state in these authentication-focused tests.
func (m mockContainer) IsRunning() bool {
	return true // Minimal stub, not used in these tests
}

// IsMonitorOnly indicates if the container is in monitor-only mode based on update parameters.
// This method satisfies the types.Container interface, returning false as a minimal stub
// since the auth package does not use this logic in these authentication-focused tests.
func (m mockContainer) IsMonitorOnly(_ types.UpdateParams) bool {
	return false // Minimal stub, not used in these tests
}

// Scope returns the container's scope and a boolean flag. This method satisfies the
// types.Container interface, returning an empty string and false as a minimal stub
// since the auth package does not use this data in these authentication-focused tests.
func (m mockContainer) Scope() (string, bool) {
	return "", false // Minimal stub, not used in these tests
}

// Links returns a slice of container links. This method satisfies the types.Container
// interface, returning an empty slice as a minimal stub since the auth package does not
// use this data in these authentication-focused tests.
func (m mockContainer) Links() []string {
	return []string{} // Minimal stub, not used in these tests
}

// ToRestart indicates whether the container should be restarted. This method satisfies
// the types.Container interface, returning false as a minimal stub since the auth package
// does not use this logic in these authentication-focused tests.
func (m mockContainer) ToRestart() bool {
	return false // Minimal stub, not used in these tests
}

// IsWatchtower indicates whether the container is a Watchtower instance. This method
// satisfies the types.Container interface, returning false as a minimal stub since
// the auth package does not use this check in these authentication-focused tests.
func (m mockContainer) IsWatchtower() bool {
	return false // Minimal stub, not used in these tests
}

// StopSignal returns the signal used to stop the container. This method satisfies
// the types.Container interface, returning an empty string as a minimal stub since
// the auth package does not use this data in these authentication-focused tests.
func (m mockContainer) StopSignal() string {
	return "" // Minimal stub, not used in these tests
}

// HasImageInfo indicates whether the container has associated image info. This method
// satisfies the types.Container interface, returning false as a minimal stub since
// the auth package does not use this check in these authentication-focused tests.
func (m mockContainer) HasImageInfo() bool {
	return false // Minimal stub, not used in these tests
}

// ImageInfo returns a pointer to an image.InspectResponse, providing image-specific metadata.
// This method satisfies the types.Container interface, returning nil as a minimal stub
// since the auth package does not use this data in these authentication-focused tests.
func (m mockContainer) ImageInfo() *image.InspectResponse {
	return nil // Minimal stub, not used in these tests
}

// VerifyConfiguration verifies the container's configuration. This method satisfies
// the types.Container interface, returning nil (no error) as a minimal stub since
// the auth package does not use this validation in these authentication-focused tests.
func (m mockContainer) VerifyConfiguration() error {
	return nil // Minimal stub, not used in these tests
}

// SetStale sets the container's stale status. This method satisfies the types.Container
// interface and is implemented as a no-op since the auth package does not use this
// state in these authentication-focused tests.
func (m mockContainer) SetStale(_ bool) {
	// Minimal stub, not used in these tests
}

// IsStale indicates whether the container is stale. This method satisfies the
// types.Container interface, returning false as a minimal stub since the auth package
// does not use this state in these authentication-focused tests.
func (m mockContainer) IsStale() bool {
	return false // Minimal stub, not used in these tests
}

// IsNoPull indicates whether the container should skip pulling based on update parameters.
// This method satisfies the types.Container interface, returning false as a minimal stub
// since the auth package does not use this logic in these authentication-focused tests.
func (m mockContainer) IsNoPull(_ types.UpdateParams) bool {
	return false // Minimal stub, not used in these tests
}

// SetLinkedToRestarting sets the container's linked-to-restarting status. This method
// satisfies the types.Container interface and is implemented as a no-op since the auth
// package does not use this state in these authentication-focused tests.
func (m mockContainer) SetLinkedToRestarting(_ bool) {
	// Minimal stub, not used in these tests
}

// IsLinkedToRestarting indicates whether the container is linked to a restarting container.
// This method satisfies the types.Container interface, returning false as a minimal stub
// since the auth package does not use this state in these authentication-focused tests.
func (m mockContainer) IsLinkedToRestarting() bool {
	return false // Minimal stub, not used in these tests
}

// PreUpdateTimeout returns the timeout duration before an update. This method satisfies
// the types.Container interface, returning 0 as a minimal stub since the auth package
// does not use this value in these authentication-focused tests.
func (m mockContainer) PreUpdateTimeout() int {
	return 0 // Minimal stub, not used in these tests
}

// PostUpdateTimeout returns the timeout duration after an update. This method satisfies
// the types.Container interface, returning 0 as a minimal stub since the auth package
// does not use this value in these authentication-focused tests.
func (m mockContainer) PostUpdateTimeout() int {
	return 0 // Minimal stub, not used in these tests
}

// IsRestarting indicates whether the container is currently restarting. This method
// satisfies the types.Container interface, returning false as a minimal stub since
// the auth package does not use this state in these authentication-focused tests.
func (m mockContainer) IsRestarting() bool {
	return false // Minimal stub, not used in these tests
}

// GetLifecycleUID returns the UID for lifecycle hooks. This method satisfies the
// types.Container interface, returning 0 and false as a minimal stub since the auth
// package does not use this data in these authentication-focused tests.
func (m mockContainer) GetLifecycleUID() (int, bool) {
	return 0, false // Minimal stub, not used in these tests
}

// GetLifecycleGID returns the GID for lifecycle hooks. This method satisfies the
// types.Container interface, returning 0 and false as a minimal stub since the auth
// package does not use this data in these authentication-focused tests.
func (m mockContainer) GetLifecycleGID() (int, bool) {
	return 0, false // Minimal stub, not used in these tests
}

// testAuthClient is a custom implementation of the AuthClient interface for testing.
// It wraps an HTTP client with configurable TLS settings to bypass certificate
// verification in test scenarios involving mock TLS servers.
type testAuthClient struct {
	client *http.Client // The underlying HTTP client for making requests.
}

// Do executes an HTTP request using the underlying HTTP client.
//
// This method satisfies the AuthClient interface, delegating the request execution
// to the embedded HTTP client.
//
// Parameters:
//   - req: The HTTP request to execute.
//
// Returns:
//   - *http.Response: The HTTP response from the registry, if successful.
//   - error: Non-nil if the request fails, nil otherwise.
func (t *testAuthClient) Do(req *http.Request) (*http.Response, error) {
	return t.client.Do(req)
}

var GHCRCredentials = &types.RegistryCredentials{
	Username: os.Getenv("CI_INTEGRATION_TEST_REGISTRY_GH_USERNAME"),
	Password: os.Getenv("CI_INTEGRATION_TEST_REGISTRY_GH_PASSWORD"),
}

var _ = ginkgo.BeforeSuite(func() {
	// Reset Viper configuration to ensure a clean state for tests.
	viper.Reset()
	viper.AutomaticEnv()
	// Set logrus to Debug level for all tests
	logrus.SetLevel(logrus.DebugLevel)
	logrus.WithField("event", "BeforeSuite").Debug("Initialized logrus to Debug level")
})

var _ = ginkgo.Describe("the auth module", func() {
	// mockID is a constant identifier used across test cases to represent a container’s
	// unique ID. It ensures consistency in mock container creation.
	const mockID = "mock-id"

	// mockName is a constant name used for mock containers in tests. It provides a
	// human-readable identifier, though it’s not critical for auth functionality.
	const mockName = "mock-container"

	// mockImage is the default image name for the initial mock container, representing
	// a real-world registry image used in the bearer token test.
	const mockImage = "ghcr.io/k6io/operator:latest"

	// mockContainerInstance is a pre-configured instance of mockContainer used for
	// the initial bearer token test with GHCR credentials. It avoids redundancy in
	// test setup while providing a baseline for authentication testing.
	mockContainerInstance := mockContainer{
		id:        mockID,
		name:      mockName,
		imageName: mockImage,
	}

	// runBasicAuthTest is a helper function to reduce duplication in GetToken tests
	// that use a mock HTTPS server to simulate basic auth challenges.
	runBasicAuthTest := func(challengeHeader, creds, expectedToken, expectedErr string) {
		// Create a TLS test server to simulate the registry.
		testServer := httptest.NewTLSServer(
			http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set(auth.ChallengeHeader, challengeHeader)
				w.WriteHeader(http.StatusUnauthorized)
			}),
		)
		defer testServer.Close()

		// Configure the container with the test server’s address.
		serverURL, _ := url.Parse(testServer.URL)
		containerInstance := mockContainer{
			id:        mockID,
			name:      mockName,
			imageName: serverURL.Host + "/test/image",
		}

		// Create an authentication client with TLS verification disabled for the mock server.
		client := &testAuthClient{
			client: &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{
						InsecureSkipVerify: true,
						MinVersion:         tls.VersionTLS12,
					},
				},
			},
		}

		// Temporarily disable WATCHTOWER_REGISTRY_TLS_SKIP to ensure HTTPS scheme.
		originalTLSSkip := viper.GetBool("WATCHTOWER_REGISTRY_TLS_SKIP")
		viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
		defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", originalTLSSkip)

		// Execute GetToken and verify the result.
		token, _, _, err := auth.GetToken(context.Background(), containerInstance, creds, client)
		if expectedErr != "" {
			gomega.Expect(err).
				To(gomega.HaveOccurred(), fmt.Sprintf("Expected an error for challenge '%s'", challengeHeader))
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring(expectedErr), fmt.Sprintf("Expected error to contain '%s'", expectedErr))
			gomega.Expect(token).To(gomega.Equal(""), "Expected empty token on failure")
		} else {
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "Expected no error when fetching basic auth token")
			gomega.Expect(token).To(gomega.Equal(expectedToken), fmt.Sprintf("Expected token to match '%s'", expectedToken))
		}
	}

	// runBearerHeaderTest is a helper function to reduce duplication in GetBearerHeader tests
	// that use a mock HTTPS server to simulate bearer token retrieval.
	runBearerHeaderTest := func(creds, expectedToken string, expectAuthFailure bool) {
		// Create a TLS test server to simulate the registry.
		testServer := httptest.NewTLSServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if expectAuthFailure {
					auth := r.Header.Get("Authorization")
					if auth != "Basic user:pass" {
						w.WriteHeader(http.StatusUnauthorized)

						return
					}
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"token": "%s"}`, expectedToken)
			}),
		)
		defer testServer.Close()

		// Create an authentication client with TLS verification disabled for the mock server.
		client := &testAuthClient{
			client: &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
				},
			},
		}

		// Construct the challenge header for the bearer token request.
		challenge := fmt.Sprintf(
			`bearer realm="%s",service="test-service",scope="repository:test/image:pull"`,
			testServer.URL,
		)
		ref, _ := reference.ParseNormalizedNamed("test/image")

		// Execute GetBearerHeader and verify the result.
		token, err := auth.GetBearerHeader(context.Background(), challenge, ref, creds, client)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(token).To(gomega.Equal("Bearer " + expectedToken))
	}

	ginkgo.Describe("GetToken", func() {
		// Test case: Verifies that GetToken retrieves a bearer token successfully when
		// provided with valid GHCR credentials. This is an integration test that runs
		// only if credentials are present, ensuring real-world registry interaction.
		ginkgo.It("should parse the token from a bearer response",
			SkipIfCredentialsEmpty(GHCRCredentials, func() {
				creds := fmt.Sprintf("%s:%s", GHCRCredentials.Username, GHCRCredentials.Password)
				client := auth.NewAuthClient()
				token, _, _, err := auth.GetToken(
					context.Background(),
					mockContainerInstance,
					creds,
					client,
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(token).NotTo(gomega.Equal(""))
			}),
		)

		// Test case: Ensures GetToken returns a basic auth token when the registry
		// responds with a "Basic" challenge.
		ginkgo.It("should return basic auth token when challenged with basic", func() {
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
			runBasicAuthTest("Basic realm=\"test\"", "user:pass", "Basic user:pass", "")
		})

		// Test case: Verifies that GetToken fails when no credentials are provided for
		// a basic auth challenge.
		ginkgo.It("should fail with no credentials for basic auth", func() {
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
			runBasicAuthTest("Basic realm=\"test\"", "", "", "no credentials available")
		})

		// Test case: Ensures GetToken returns an error for an unsupported challenge type
		// (e.g., "Digest").
		ginkgo.It("should fail with unsupported challenge type", func() {
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
			runBasicAuthTest(
				"Digest realm=\"test\"",
				"user:pass",
				"",
				"unsupported challenge type from registry",
			)
		})

		// Test case: Tests GetToken’s behavior when an HTTP request fails (e.g., due to an
		// unreachable host). Uses a non-existent URL to trigger a network error, ensuring
		// the function handles such failures gracefully.
		ginkgo.It("should handle HTTP request failure", func() {
			// Use a valid image name with an unreachable host
			containerInstance := mockContainer{
				id:        mockID,
				name:      mockName,
				imageName: "nonexistent.local/test/image",
			}

			client := auth.NewAuthClient()
			token, _, _, err := auth.GetToken(
				context.Background(),
				containerInstance,
				"user:pass",
				client,
			)
			gomega.Expect(err).
				To(gomega.HaveOccurred(), "Expected error due to HTTP request failure")
			gomega.Expect(token).To(gomega.Equal(""), "Expected empty token on failure")
		})

		// Test case: Verifies that GetToken returns an empty token for an unauthenticated
		// local HTTP registry responding with 200 OK when TLS verification is skipped.
		ginkgo.It(
			"should return empty token for local HTTP registry (200 OK) with TLS skip",
			func() {
				// Create an HTTP test server to simulate the registry.
				mux := http.NewServeMux()
				server := httptest.NewServer(mux)
				defer server.Close()

				// Parse the server URL to extract the host for the container’s image name.
				serverURL, _ := url.Parse(server.URL)
				containerInstance := mockContainer{
					id:        mockID,
					name:      mockName,
					imageName: serverURL.Host + "/test/image:latest",
				}

				// Configure the server to return 200 OK, indicating no authentication required.
				mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusOK)
				})

				// Simulate WATCHTOWER_REGISTRY_TLS_SKIP=true to disable TLS verification.
				viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
				defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)

				// Create a custom authentication client with HTTP-only transport.
				client := &testAuthClient{
					client: &http.Client{
						Transport: &http.Transport{
							TLSClientConfig: nil, // Disable TLS for HTTP requests
						},
					},
				}

				// Execute GetToken and verify the result.
				token, _, _, err := auth.GetToken(
					context.Background(),
					containerInstance,
					"",
					client,
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(token).To(gomega.Equal(""))
			},
		)

		// Test case: Ensures GetToken fails when attempting to connect to an HTTP registry
		// using HTTPS without TLS verification skipped, resulting in a connection error.
		ginkgo.It("should fail for HTTPS to HTTP registry without TLS skip", func() {
			// Create an HTTP test server to simulate the registry.
			mux := http.NewServeMux()
			server := httptest.NewServer(mux)
			defer server.Close()

			// Parse the server URL to extract the host for the container’s image name.
			serverURL, _ := url.Parse(server.URL)
			containerInstance := mockContainer{
				id:        mockID,
				name:      mockName,
				imageName: serverURL.Host + "/test/image:latest",
			}

			// Configure the server to return 200 OK, but it’s unreachable due to TLS mismatch.
			mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			// Ensure TLS verification is enabled.
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)

			// Create a new authentication client with default TLS settings.
			client := auth.NewAuthClient()

			// Execute GetToken and verify the expected failure.
			token, _, _, err := auth.GetToken(context.Background(), containerInstance, "", client)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring("http: server gave HTTP response to HTTPS client"))
			gomega.Expect(token).To(gomega.Equal(""))
		})

		// Test case: Verifies that GetToken handles an empty WWW-Authenticate header with
		// 401 status, returning an empty token, as expected for registries requiring no auth.
		ginkgo.It("should handle empty WWW-Authenticate header with 401 status", func() {
			// Create a TLS test server to simulate the registry.
			testServer := httptest.NewTLSServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusUnauthorized)
				}),
			)
			defer testServer.Close()

			// Parse the server URL to extract the host for the container’s image name.
			serverURL, _ := url.Parse(testServer.URL)
			containerInstance := mockContainer{
				id:        mockID,
				name:      mockName,
				imageName: serverURL.Host + "/test/image:latest",
			}

			// Create an authentication client with TLS verification disabled for the mock server.
			client := &testAuthClient{
				client: &http.Client{
					Transport: &http.Transport{
						TLSClientConfig: &tls.Config{
							InsecureSkipVerify: true,
						},
					},
				},
			}

			// Simulate WATCHTOWER_REGISTRY_TLS_SKIP=true to allow HTTP scheme.
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)

			// Execute GetToken and verify the result.
			token, _, _, err := auth.GetToken(context.Background(), containerInstance, "", client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(token).To(gomega.Equal(""))
		})

		// Test case: Verifies that GetToken returns an empty token for an HTTPS registry
		// responding with 200 OK without requiring authentication, even without TLS skip.
		ginkgo.It("should handle HTTPS registry with 200 OK without TLS skip", func() {
			// Create a TLS test server to simulate a secure registry.
			mux := http.NewServeMux()
			server := httptest.NewTLSServer(mux)
			defer server.Close()

			// Parse the server URL to extract the host for the container’s image name.
			serverURL, _ := url.Parse(server.URL)
			containerInstance := mockContainer{
				id:        mockID,
				name:      mockName,
				imageName: serverURL.Host + "/test/image:latest",
			}

			// Configure the server to return 200 OK, indicating no authentication required.
			mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			// Create an authentication client with TLS verification disabled for the mock server.
			client := &testAuthClient{
				client: &http.Client{
					Transport: &http.Transport{
						TLSClientConfig: &tls.Config{
							InsecureSkipVerify: true,
						},
					},
				},
			}

			// Ensure TLS verification is enabled.
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)

			// Execute GetToken and verify the result.
			token, _, _, err := auth.GetToken(context.Background(), containerInstance, "", client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(token).To(gomega.Equal(""))
		})

		// Test case: Verifies that GetToken handles an invalid TLS minimum version by
		// defaulting to TLS 1.2, successfully connecting to a TLS-enabled registry.
		ginkgo.It("should handle invalid TLS min version", func() {
			// Create a TLS test server to simulate a secure registry.
			mux := http.NewServeMux()
			server := httptest.NewTLSServer(mux)
			defer server.Close()

			// Parse the server URL to extract the host for the container’s image name.
			serverURL, _ := url.Parse(server.URL)
			containerInstance := mockContainer{
				id:        mockID,
				name:      mockName,
				imageName: serverURL.Host + "/test/image:latest",
			}

			// Configure the server to return 200 OK, indicating no authentication required.
			mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			// Simulate an invalid TLS minimum version.
			viper.Set("WATCHTOWER_REGISTRY_TLS_MIN_VERSION", "TLS9.9")
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_MIN_VERSION", "")

			// Create a new authentication client with TLS verification disabled.
			client := &testAuthClient{
				client: &http.Client{
					Transport: &http.Transport{
						TLSClientConfig: &tls.Config{
							MinVersion:         tls.VersionTLS12,
							InsecureSkipVerify: true,
						},
					},
				},
			}

			// Execute GetToken and verify the result.
			token, _, _, err := auth.GetToken(context.Background(), containerInstance, "", client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(token).To(gomega.Equal(""))
		})

		// Test case: Ensures GetToken fails when the TLS minimum version is set to an
		// incompatible value (e.g., TLS 1.3) for a registry supporting a lower version.
		ginkgo.It("should fail with TLS version mismatch", func() {
			// Create a TLS test server to simulate a secure registry.
			mux := http.NewServeMux()
			server := httptest.NewTLSServer(mux)
			defer server.Close()

			// Parse the server URL to extract the host for the container’s image name.
			serverURL, _ := url.Parse(server.URL)
			containerInstance := mockContainer{
				id:        mockID,
				name:      mockName,
				imageName: serverURL.Host + "/test/image:latest",
			}

			// Configure the server to return 200 OK, but it’s unreachable due to TLS mismatch.
			mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			// Simulate TLS 1.3, which is incompatible with the test server’s TLS version.
			viper.Set("WATCHTOWER_REGISTRY_TLS_MIN_VERSION", "TLS1.3")
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_MIN_VERSION", "")

			// Create a new authentication client with the specified TLS settings.
			client := auth.NewAuthClient()

			// Execute GetToken and verify the expected failure.
			token, _, _, err := auth.GetToken(context.Background(), containerInstance, "", client)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring("failed to execute challenge request"))
			gomega.Expect(token).To(gomega.Equal(""))
		})

		ginkgo.It("should extract the challenge host for bearer token", func() {
			defer ginkgo.GinkgoRecover()
			mux := http.NewServeMux()
			server := httptest.NewTLSServer(mux)
			defer server.Close()

			redirectMux := http.NewServeMux()
			redirectServer := httptest.NewTLSServer(redirectMux)
			defer redirectServer.Close()

			serverAddr := server.Listener.Addr().String()
			redirectAddr := redirectServer.Listener.Addr().String()
			containerInstance := mockContainer{
				id:        mockID,
				name:      mockName,
				imageName: serverAddr + "/test/image:latest",
			}

			mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /v2/ request")
				w.Header().Set(
					"WWW-Authenticate",
					fmt.Sprintf(
						`Bearer realm="https://%s/token",service="test-service",scope="repository:test/image:pull"`,
						redirectAddr,
					),
				)
				w.WriteHeader(http.StatusUnauthorized)
			})
			redirectMux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /token request")
				w.Write([]byte(`{"token": "mock-token"}`))
			})

			client := &testAuthClient{
				client: &http.Client{
					Transport: &http.Transport{
						TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
					},
				},
			}

			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)

			token, challengeHost, redirected, err := auth.GetToken(
				context.Background(),
				containerInstance,
				"",
				client,
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(token).To(gomega.Equal("Bearer mock-token"))
			gomega.Expect(challengeHost).To(gomega.Equal(redirectAddr))
			gomega.Expect(redirected).To(gomega.BeFalse()) // No HTTP redirect occurred
		})
		// Test case: Verifies that GetToken returns redirect=false when the challenge request is not redirected.
		ginkgo.It("should return redirect=false when challenge request is not redirected", func() {
			defer ginkgo.GinkgoRecover()
			mux := http.NewServeMux()
			server := httptest.NewTLSServer(mux)
			defer server.Close()

			serverAddr := server.Listener.Addr().String()
			containerInstance := mockContainer{
				id:        mockID,
				name:      mockName,
				imageName: serverAddr + "/test/image:latest",
			}

			mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /v2/ request - no redirect")
				w.Header().Set(
					"WWW-Authenticate",
					fmt.Sprintf(
						`Bearer realm="https://%s/token",service="test-service",scope="repository:test/image:pull"`,
						serverAddr,
					),
				)
				w.WriteHeader(http.StatusUnauthorized)
			})
			mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /token request")
				w.Write([]byte(`{"token": "mock-token"}`))
			})

			client := &testAuthClient{
				client: &http.Client{
					Transport: &http.Transport{
						TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
					},
				},
			}

			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)

			token, challengeHost, redirected, err := auth.GetToken(
				context.Background(),
				containerInstance,
				"",
				client,
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(token).To(gomega.Equal("Bearer mock-token"))
			gomega.Expect(challengeHost).To(gomega.Equal(serverAddr))
			gomega.Expect(redirected).To(gomega.BeFalse())
		})

		// Test case: Verifies that GetToken returns redirect=true when the challenge request is redirected.
		ginkgo.It("should return redirect=true when challenge request is redirected", func() {
			defer ginkgo.GinkgoRecover()
			mux := http.NewServeMux()
			server := httptest.NewTLSServer(mux)
			defer server.Close()

			redirectMux := http.NewServeMux()
			redirectServer := httptest.NewTLSServer(redirectMux)
			defer redirectServer.Close()

			serverAddr := server.Listener.Addr().String()
			redirectAddr := redirectServer.Listener.Addr().String()
			containerInstance := mockContainer{
				id:        mockID,
				name:      mockName,
				imageName: serverAddr + "/test/image:latest",
			}

			mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /v2/ request - redirecting")
				// Redirect to the redirect server's /v2/
				redirectURL := fmt.Sprintf("https://%s/v2/", redirectAddr)
				w.Header().Set("Location", redirectURL)
				w.WriteHeader(http.StatusFound)
			})
			redirectMux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /v2/ request on redirect server")
				w.Header().Set(
					"WWW-Authenticate",
					fmt.Sprintf(
						`Bearer realm="https://%s/token",service="test-service",scope="repository:test/image:pull"`,
						redirectAddr,
					),
				)
				w.WriteHeader(http.StatusUnauthorized)
			})
			redirectMux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /token request")
				w.Write([]byte(`{"token": "mock-token"}`))
			})

			client := &testAuthClient{
				client: &http.Client{
					Transport: &http.Transport{
						TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
					},
				},
			}

			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)

			token, challengeHost, redirected, err := auth.GetToken(
				context.Background(),
				containerInstance,
				"",
				client,
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(token).To(gomega.Equal("Bearer mock-token"))
			gomega.Expect(challengeHost).To(gomega.Equal(redirectAddr))
			gomega.Expect(redirected).To(gomega.BeTrue()) // HTTP redirect occurred
		})

		// Test case: Ensures GetToken fails with an error when the HTTP client exceeds
		// the maximum number of redirects, verifying proper handling of redirect limits.
		ginkgo.It("should fail when exceeding maximum redirects", func() {
			// Create a chain of test servers to simulate multiple redirects
			redirectCount := auth.DefaultMaxRedirects + 1 // Exceed limit (3 + 1 = 4 redirects)
			servers := make([]*httptest.Server, redirectCount)
			for i := range redirectCount {
				// Capture range variable
				mux := http.NewServeMux()
				mux.HandleFunc("/v2/", func(w http.ResponseWriter, r *http.Request) {
					logrus.WithField("server", i).
						WithField("path", r.URL.Path).
						Debug("Handling /v2/ request")
					if i < redirectCount-1 {
						// Redirect to the next server’s /v2/
						nextURL := servers[i+1].URL + "/v2/"
						w.Header().Set("Location", nextURL)
						w.WriteHeader(http.StatusTemporaryRedirect)
						logrus.WithField("server", i).Debug("Sent redirect response")
					} else {
						// Final server returns a token response
						w.Header().Set("Content-Type", "application/json")
						w.Write([]byte(`{"token": "final-token"}`))
						logrus.WithField("server", i).Debug("Sent token response")
					}
				})
				servers[i] = httptest.NewServer(mux)
				defer servers[i].Close()
			}

			// Configure container with the first server’s address
			serverURL, _ := url.Parse(servers[0].URL)
			containerInstance := mockContainer{
				id:        mockID,
				name:      mockName,
				imageName: serverURL.Host + "/test/image:latest",
			}

			// Create a custom client to enforce redirect limit
			client := &testAuthClient{
				client: &http.Client{
					Transport: &http.Transport{
						TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
					},
					CheckRedirect: func(_ *http.Request, via []*http.Request) error {
						if len(via) >= auth.DefaultMaxRedirects {
							return fmt.Errorf(
								"stopped after %d redirects",
								auth.DefaultMaxRedirects,
							)
						}

						return nil
					},
				},
			}

			// Use HTTP scheme to match non-TLS servers
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)

			// Execute GetToken and verify failure due to too many redirects
			token, _, _, err := auth.GetToken(context.Background(), containerInstance, "", client)
			gomega.Expect(err).
				To(gomega.HaveOccurred(), "Expected error due to exceeding redirect limit")
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring("stopped after 3 redirects"), "Expected error indicating redirect limit exceeded")
			gomega.Expect(token).To(gomega.Equal(""), "Expected empty token on redirect failure")
		})
	})

	ginkgo.Describe("GetChallengeRequest", func() {
		// Test case: Verifies that GetChallengeRequest constructs a valid HTTP GET request
		// with the expected headers and URL. Ensures the request is properly formed for
		// registry challenges.
		ginkgo.It("should create a valid HTTP request", func() {
			url := url.URL{
				Scheme: "https",
				Host:   "example.com",
				Path:   "/v2/",
			}
			req, err := auth.GetChallengeRequest(context.Background(), url)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(req.Method).To(gomega.Equal(http.MethodGet))
			gomega.Expect(req.URL.String()).To(gomega.Equal("https://example.com/v2/"))
			gomega.Expect(req.Header.Get("Accept")).To(gomega.Equal("*/*"))
			gomega.Expect(req.Header.Get("User-Agent")).To(gomega.Equal("Watchtower (Docker)"))
			gomega.Expect(req.Context()).To(gomega.Equal(context.Background()))
		})

		// Test case: Ensures GetChallengeRequest returns an error when given an invalid URL.
		// Tests error handling for malformed inputs, such as an invalid scheme.
		ginkgo.It("should return an error for invalid URL", func() {
			url := url.URL{
				Scheme: "://", // Invalid scheme
				Host:   "example.com",
			}
			req, err := auth.GetChallengeRequest(context.Background(), url)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(req).To(gomega.BeNil())
		})
	})

	ginkgo.Describe("GetBearerHeader", func() {
		// Test case: Verifies that GetBearerHeader fetches a bearer token successfully from
		// a mock registry response without credentials.
		ginkgo.It("should fetch a bearer token successfully", func() {
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
			runBearerHeaderTest("", "test-token", false)
		})

		// Test case: Ensures GetBearerHeader fetches a bearer token when credentials are
		// provided, validating the Authorization header.
		ginkgo.It("should fetch a bearer token with credentials", func() {
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
			runBearerHeaderTest("user:pass", "auth-token", true)
		})

		// Test case: Tests GetBearerHeader’s error handling when the HTTP request fails
		// (e.g., due to an unreachable host). Ensures proper error propagation.
		ginkgo.It("should fail on HTTP request error", func() {
			client := auth.NewAuthClient()
			challenge := `bearer realm="http://nonexistent.local/token",service="test-service",scope="repository:test/image:pull"`
			ref, _ := reference.ParseNormalizedNamed("test/image")
			token, err := auth.GetBearerHeader(context.Background(), challenge, ref, "", client)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(token).To(gomega.Equal(""))
		})

		// Test case: Verifies GetBearerHeader fails when the registry returns invalid JSON.
		ginkgo.It("should fail on invalid JSON response", func() {
			// Create a TLS test server to simulate the registry.
			testServer := httptest.NewTLSServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprintf(w, `{"invalid": "json"`) // Missing token field
				}),
			)
			defer testServer.Close()

			// Create an authentication client with TLS verification disabled.
			client := &testAuthClient{
				client: &http.Client{
					Transport: &http.Transport{
						TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
					},
				},
			}

			// Construct the challenge header.
			challenge := fmt.Sprintf(
				`bearer realm="%s",service="test-service",scope="repository:test/image:pull"`,
				testServer.URL,
			)
			ref, _ := reference.ParseNormalizedNamed("test/image")

			// Execute GetBearerHeader and verify the failure.
			token, err := auth.GetBearerHeader(context.Background(), challenge, ref, "", client)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(token).To(gomega.Equal(""))
		})

		ginkgo.It("should handle missing scope in WWW-Authenticate header gracefully", func() {
			// Create a ServeMux to handle multiple paths
			mux := http.NewServeMux()
			// Create a TLS test server
			testServer := httptest.NewTLSServer(mux)
			defer testServer.Close()

			// Set up the mock reference
			ref, err := reference.ParseNormalizedNamed("test/image:latest")
			gomega.Expect(err).
				NotTo(gomega.HaveOccurred(), "Expected no error parsing image reference")

			// Configure the server to handle /v2/ and /token paths
			mux.HandleFunc("/v2/", func(w http.ResponseWriter, r *http.Request) {
				logrus.WithField("path", r.URL.Path).Debug("Mock server received request")
				w.Header().
					Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="test-service"`, testServer.URL))
				w.WriteHeader(http.StatusUnauthorized)
				logrus.Debug("Mock server sent WWW-Authenticate header")
			})
			mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
				logrus.WithField("path", r.URL.Path).Debug("Mock server received request")
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"token": "mock-token"}`))
				logrus.Debug("Mock server sent token response")
			})

			// Create a client with TLS skip
			client := &testAuthClient{
				client: &http.Client{
					Transport: &http.Transport{
						TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
					},
				},
			}

			// Set TLS skip
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)

			// Construct the challenge header
			challenge := fmt.Sprintf(
				`bearer realm="%s/token",service="test-service"`,
				testServer.URL,
			)

			// Call GetBearerHeader directly to test processChallenge
			token, err := auth.GetBearerHeader(context.Background(), challenge, ref, "", client)
			gomega.Expect(err).
				NotTo(gomega.HaveOccurred(), "Expected no error from GetBearerHeader")
			gomega.Expect(token).
				To(gomega.Equal("Bearer mock-token"), "Expected token to be 'Bearer mock-token'")
		})
	})

	ginkgo.Describe("GetAuthURL", func() {
		// Test case: Ensures GetAuthURL constructs a valid URL from a bearer challenge
		// header, including realm, service, and scope parameters, for a given image reference.
		ginkgo.It(
			"should create a valid auth url object based on the challenge header supplied",
			func() {
				challenge := `bearer realm="https://ghcr.io/token",service="ghcr.io",scope="repository:user/image:pull"`
				imageRef, err := reference.ParseNormalizedNamed("nicholas-fedor/watchtower")
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				expected := &url.URL{
					Host:     "ghcr.io",
					Scheme:   "https",
					Path:     "/token",
					RawQuery: "scope=repository%3Anicholas-fedor%2Fwatchtower%3Apull&service=ghcr.io",
				}

				URL, err := auth.GetAuthURL(challenge, imageRef)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(URL).To(gomega.Equal(expected))
			},
		)

		ginkgo.When("given an invalid challenge header", func() {
			// Test case: Verifies GetAuthURL returns an error when the challenge header lacks
			// required fields (e.g., service). Ensures robust error handling for malformed inputs.
			ginkgo.It("should return an error", func() {
				challenge := `bearer realm="https://ghcr.io/token"`
				imageRef, err := reference.ParseNormalizedNamed("nicholas-fedor/watchtower")
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				URL, err := auth.GetAuthURL(challenge, imageRef)
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(URL).To(gomega.BeNil())
			})
		})

		ginkgo.When("deriving the auth scope from an image name", func() {
			// Test case: Ensures GetAuthURL prepends "library/" to official Docker Hub images,
			// validating correct scope derivation for standard images.
			ginkgo.It("should prepend official dockerhub images with \"library/\"", func() {
				gomega.Expect(getScopeFromImageAuthURL("registry")).
					To(gomega.Equal("library/registry"))
				gomega.Expect(getScopeFromImageAuthURL("docker.io/registry")).
					To(gomega.Equal("library/registry"))
				gomega.Expect(getScopeFromImageAuthURL("index.docker.io/registry")).
					To(gomega.Equal("library/registry"))
			})

			// Test case: Verifies GetAuthURL excludes vanity hosts (e.g., "docker.io") from the
			// scope, ensuring clean repository paths for Docker Hub images.
			ginkgo.It("should not include vanity hosts", func() {
				gomega.Expect(getScopeFromImageAuthURL("docker.io/nickfedor/watchtower")).
					To(gomega.Equal("nickfedor/watchtower"))
				gomega.Expect(getScopeFromImageAuthURL("index.docker.io/nickfedor/watchtower")).
					To(gomega.Equal("nickfedor/watchtower"))
			})

			// Test case: Confirms GetAuthURL handles non-Docker Hub images correctly, extracting
			// the repository path without additional prefixes for registries like GHCR.
			ginkgo.It("should handle non-Docker Hub images correctly", func() {
				gomega.Expect(getScopeFromImageAuthURL("ghcr.io/watchtower")).
					To(gomega.Equal("watchtower"))
				gomega.Expect(getScopeFromImageAuthURL("ghcr.io/nicholas-fedor/watchtower")).
					To(gomega.Equal("nicholas-fedor/watchtower"))
			})
		})

		// Test case: Ensures GetAuthURL does not panic when the challenge header includes an
		// empty field, testing robustness against incomplete but valid inputs.
		ginkgo.It("should not crash when an empty field is received", func() {
			input := `bearer realm="https://ghcr.io/token",service="ghcr.io",scope="repository:user/image:pull",`
			imageRef, err := reference.ParseNormalizedNamed("nicholas-fedor/watchtower")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			res, err := auth.GetAuthURL(input, imageRef)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(res).NotTo(gomega.BeNil())
		})

		// Test case: Verifies GetAuthURL handles a valueless key in the challenge header
		// without crashing, ensuring stability with unusual but parsable inputs.
		ginkgo.It("should not crash when a field without a value is received", func() {
			input := `bearer realm="https://ghcr.io/token",service="ghcr.io",scope="repository:user/image:pull",valuelesskey`
			imageRef, err := reference.ParseNormalizedNamed("nicholas-fedor/watchtower")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			res, err := auth.GetAuthURL(input, imageRef)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(res).NotTo(gomega.BeNil())
		})
	})

	ginkgo.Describe("GetChallengeURL", func() {
		// Test case: Ensures GetChallengeURL constructs a correct challenge URL for a
		// GHCR-hosted image, validating registry address extraction and URL formatting.
		ginkgo.It(
			"should create a valid challenge url object based on the image ref supplied",
			func() {
				expected := url.URL{Host: "ghcr.io", Scheme: "https", Path: "/v2/"}
				imageRef, _ := reference.ParseNormalizedNamed(
					"ghcr.io/nicholas-fedor/watchtower:latest",
				)
				gomega.Expect(auth.GetChallengeURL(imageRef)).To(gomega.Equal(expected))
			},
		)

		// Test case: Verifies GetChallengeURL defaults to Docker Hub (index.docker.io) for
		// images without an explicit registry, ensuring correct fallback behavior.
		ginkgo.It("should assume Docker Hub for image refs with no explicit registry", func() {
			expected := url.URL{Host: "index.docker.io", Scheme: "https", Path: "/v2/"}
			imageRef, _ := reference.ParseNormalizedNamed("nickfedor/watchtower:latest")
			gomega.Expect(auth.GetChallengeURL(imageRef)).To(gomega.Equal(expected))
		})

		// Test case: Confirms GetChallengeURL uses "index.docker.io" for "docker.io" registry
		// references, validating consistent handling of Docker Hub vanity URLs.
		ginkgo.It("should use index.docker.io if the image ref specifies docker.io", func() {
			expected := url.URL{Host: "index.docker.io", Scheme: "https", Path: "/v2/"}
			imageRef, _ := reference.ParseNormalizedNamed("docker.io/nickfedor/watchtower:latest")
			gomega.Expect(auth.GetChallengeURL(imageRef)).To(gomega.Equal(expected))
		})
	})

	ginkgo.Describe("GetRegistryAddress", func() {
		ginkgo.It("should return error if passed empty string", func() {
			_, err := auth.GetRegistryAddress("")
			gomega.Expect(err).To(gomega.HaveOccurred())
		})
		ginkgo.It("should return index.docker.io for image refs with no explicit registry", func() {
			gomega.Expect(auth.GetRegistryAddress("watchtower")).To(gomega.Equal("index.docker.io"))
			gomega.Expect(auth.GetRegistryAddress("nickfedor/watchtower")).
				To(gomega.Equal("index.docker.io"))
		})
		ginkgo.It("should return index.docker.io for image refs with docker.io domain", func() {
			gomega.Expect(auth.GetRegistryAddress("docker.io/watchtower")).
				To(gomega.Equal("index.docker.io"))
			gomega.Expect(auth.GetRegistryAddress("docker.io/nickfedor/watchtower")).
				To(gomega.Equal("index.docker.io"))
		})
		ginkgo.It("should return the host if passed an image name containing a local host", func() {
			gomega.Expect(auth.GetRegistryAddress("henk:80/watchtower")).To(gomega.Equal("henk:80"))
			gomega.Expect(auth.GetRegistryAddress("localhost/watchtower")).
				To(gomega.Equal("localhost"))
		})
		ginkgo.It(
			"should return the server address if passed a fully qualified image name",
			func() {
				gomega.Expect(auth.GetRegistryAddress("github.com/nicholas-fedor/config")).
					To(gomega.Equal("github.com"))
			},
		)
	})

	ginkgo.Describe("TransformAuth", func() {
		ginkgo.It("should transform valid credentials into base64", func() {
			creds := struct {
				Username string `json:"username"`
				Password string `json:"password"`
			}{
				Username: "testuser",
				Password: "testpass",
			}
			jsonData, _ := json.Marshal(creds)
			inputAuth := base64.StdEncoding.EncodeToString(jsonData)

			result := auth.TransformAuth(inputAuth)
			expected := base64.StdEncoding.EncodeToString([]byte("testuser:testpass"))
			gomega.Expect(result).To(gomega.Equal(expected))
		})

		ginkgo.It("should return original input if decoding fails", func() {
			inputAuth := "invalid-base64-string"
			result := auth.TransformAuth(inputAuth)
			gomega.Expect(result).To(gomega.Equal(inputAuth))
		})

		ginkgo.It("should handle empty credentials", func() {
			creds := struct {
				Username string `json:"username"`
				Password string `json:"password"`
			}{
				Username: "",
				Password: "",
			}
			jsonData, _ := json.Marshal(creds)
			inputAuth := base64.StdEncoding.EncodeToString(jsonData)

			result := auth.TransformAuth(inputAuth)
			gomega.Expect(result).To(gomega.Equal(inputAuth))
		})

		ginkgo.It("should handle malformed JSON", func() {
			inputAuth := base64.StdEncoding.EncodeToString([]byte("invalid json"))
			result := auth.TransformAuth(inputAuth)
			gomega.Expect(result).To(gomega.Equal(inputAuth))
		})

		ginkgo.It("should handle missing password field", func() {
			creds := struct {
				Username string `json:"username"`
			}{
				Username: "testuser",
			}
			jsonData, _ := json.Marshal(creds)
			inputAuth := base64.StdEncoding.EncodeToString(jsonData)

			result := auth.TransformAuth(inputAuth)
			gomega.Expect(result).To(gomega.Equal(inputAuth))
		})

		ginkgo.It("should handle missing username field", func() {
			creds := struct {
				Password string `json:"password"`
			}{
				Password: "testpass",
			}
			jsonData, _ := json.Marshal(creds)
			inputAuth := base64.StdEncoding.EncodeToString(jsonData)

			result := auth.TransformAuth(inputAuth)
			gomega.Expect(result).To(gomega.Equal(inputAuth))
		})
		// Test case: Verifies that GetToken returns redirect=false when the challenge request is not redirected.
		ginkgo.It("should return redirect=false when challenge request is not redirected", func() {
			defer ginkgo.GinkgoRecover()
			mux := http.NewServeMux()
			server := httptest.NewTLSServer(mux)
			defer server.Close()

			serverAddr := server.Listener.Addr().String()
			containerInstance := mockContainer{
				id:        mockID,
				name:      mockName,
				imageName: serverAddr + "/test/image:latest",
			}

			mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /v2/ request - no redirect")
				w.Header().Set(
					"WWW-Authenticate",
					fmt.Sprintf(
						`Bearer realm="https://%s/token",service="test-service",scope="repository:test/image:pull"`,
						serverAddr,
					),
				)
				w.WriteHeader(http.StatusUnauthorized)
			})
			mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /token request")
				w.Write([]byte(`{"token": "mock-token"}`))
			})

			client := &testAuthClient{
				client: &http.Client{
					Transport: &http.Transport{
						TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
					},
				},
			}

			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)

			token, challengeHost, redirected, err := auth.GetToken(
				context.Background(),
				containerInstance,
				"",
				client,
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(token).To(gomega.Equal("Bearer mock-token"))
			gomega.Expect(challengeHost).To(gomega.Equal(serverAddr))
			gomega.Expect(redirected).To(gomega.BeFalse())
		})

		// Test case: Verifies that GetToken returns redirect=true when the challenge request is redirected.
		ginkgo.It("should return redirect=true when challenge request is redirected", func() {
			defer ginkgo.GinkgoRecover()
			mux := http.NewServeMux()
			server := httptest.NewTLSServer(mux)
			defer server.Close()

			redirectMux := http.NewServeMux()
			redirectServer := httptest.NewTLSServer(redirectMux)
			defer redirectServer.Close()

			serverAddr := server.Listener.Addr().String()
			redirectAddr := redirectServer.Listener.Addr().String()
			containerInstance := mockContainer{
				id:        mockID,
				name:      mockName,
				imageName: serverAddr + "/test/image:latest",
			}

			mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /v2/ request - redirecting")
				// Redirect to the redirect server's /v2/
				redirectURL := fmt.Sprintf("https://%s/v2/", redirectAddr)
				w.Header().Set("Location", redirectURL)
				w.WriteHeader(http.StatusFound)
			})
			redirectMux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /v2/ request on redirect server")
				w.Header().Set(
					"WWW-Authenticate",
					fmt.Sprintf(
						`Bearer realm="https://%s/token",service="test-service",scope="repository:test/image:pull"`,
						redirectAddr,
					),
				)
				w.WriteHeader(http.StatusUnauthorized)
			})
			redirectMux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /token request")
				w.Write([]byte(`{"token": "mock-token"}`))
			})

			client := &testAuthClient{
				client: &http.Client{
					Transport: &http.Transport{
						TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
					},
				},
			}

			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)

			token, challengeHost, redirected, err := auth.GetToken(
				context.Background(),
				containerInstance,
				"",
				client,
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(token).To(gomega.Equal("Bearer mock-token"))
			gomega.Expect(challengeHost).To(gomega.Equal(redirectAddr))
			gomega.Expect(redirected).To(gomega.BeTrue()) // HTTP redirect occurred
		})
	})
})

// getScopeFromImageAuthURL extracts and returns the repository path from an auth URL’s
// scope parameter for a given image name. It constructs a mock challenge header, builds
// the auth URL, and strips the "repository:" prefix and ":pull" suffix, providing the
// clean path used in registry authentication.
func getScopeFromImageAuthURL(imageName string) string {
	normalizedRef, err := reference.ParseNormalizedNamed(imageName)
	if err != nil {
		return "" // Return empty string on parse failure to avoid panic
	}

	challenge := `bearer realm="https://dummy.host/token",service="dummy.host",scope="repository:user/image:pull"`

	URL, err := auth.GetAuthURL(challenge, normalizedRef)
	if err != nil {
		return "" // Return empty string on auth URL failure
	}

	scope := URL.Query().Get("scope")

	return strings.TrimSuffix(strings.TrimPrefix(scope, "repository:"), ":pull")
}
