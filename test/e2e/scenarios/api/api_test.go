// Package api provides end-to-end tests for Watchtower's HTTP API functionality.
package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"

	"github.com/nicholas-fedor/watchtower/test/e2e/framework"
)

// TestHTTPAPIBasic tests basic HTTP API functionality.
func TestHTTPAPIBasic(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Create a test container
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable": "true",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Start Watchtower with HTTP API enabled
		_, err = fw.CreateWatchtowerContainer([]string{
			"--http-api-update",
			"--http-api-metrics",
			"--http-api-token=test-token",
			"--http-api-port=8080",
			"--no-startup-message",
		})
		require.NoError(t, err)

		// Wait for API to be ready
		err = fw.WaitForAPIReady("http://localhost:8080", "test-token", 30*time.Second)
		require.NoError(t, err)

		// Wait for API to be ready
		err = fw.WaitForAPIReady("http://localhost:8080", "test-token", 30*time.Second)
		require.NoError(t, err)

		// Test health endpoint
		client := framework.NewAPIClient("http://localhost:8080", "test-token")
		resp, err := client.GetHealth()
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)
		resp.Body.Close()

		// Test metrics endpoint
		resp, err = client.GetMetrics()
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)
		resp.Body.Close()

		return nil
	})
}

// TestHTTPAPIUpdateTrigger tests triggering updates via HTTP API.
func TestHTTPAPIUpdateTrigger(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Create a test container
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable": "true",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Start Watchtower with HTTP API enabled
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--http-api-update",
			"--http-api-token=test-token",
			"--http-api-port=8080",
			"--no-startup-message",
		})
		require.NoError(t, err)

		// Wait for API to be ready
		err = fw.WaitForAPIReady("http://localhost:8080", "test-token", 30*time.Second)
		require.NoError(t, err)

		// Trigger update via API
		err = fw.TriggerAPIUpdate("test-token", []string{"nginx"})
		require.NoError(t, err)

		// Verify Watchtower processed the API request
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")

		return nil
	})
}

// TestHTTPAPIAuthentication tests API authentication.
func TestHTTPAPIAuthentication(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Start Watchtower with HTTP API and authentication
		_, err = fw.CreateWatchtowerContainer([]string{
			"--http-api-update",
			"--http-api-token=secret-token",
			"--http-api-port=8080",
			"--no-startup-message",
		})
		require.NoError(t, err)

		// Wait for API to be ready
		err = fw.WaitForAPIReady("http://localhost:8080", "secret-token", 30*time.Second)
		require.NoError(t, err)

		// Test with correct token
		client := framework.NewAPIClient("http://localhost:8080", "secret-token")
		resp, err := client.GetHealth()
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)
		resp.Body.Close()

		// Test with incorrect token
		clientBad := framework.NewAPIClient("http://localhost:8080", "wrong-token")
		resp, err = clientBad.GetHealth()
		require.NoError(t, err)
		require.Equal(t, 401, resp.StatusCode) // Should be unauthorized
		resp.Body.Close()

		// Test without token
		clientNoAuth := framework.NewAPIClient("http://localhost:8080", "")
		resp, err = clientNoAuth.GetHealth()
		require.NoError(t, err)
		require.Equal(t, 401, resp.StatusCode) // Should be unauthorized
		resp.Body.Close()

		return nil
	})
}

// TestHTTPAPIMetrics tests retrieving metrics via HTTP API.
func TestHTTPAPIMetrics(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Create a test container
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable": "true",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Start Watchtower with HTTP API and metrics enabled
		_, err = fw.CreateWatchtowerContainer([]string{
			"--http-api-metrics",
			"--http-api-token=test-token",
			"--http-api-port=8080",
			"--no-startup-message",
		})
		require.NoError(t, err)

		// Wait for API to be ready
		err = fw.WaitForAPIReady("http://localhost:8080", "test-token", 30*time.Second)
		require.NoError(t, err)

		// Get metrics
		metrics, err := fw.GetAPIMetrics("test-token")
		require.NoError(t, err)
		require.NotNil(t, metrics)

		// Verify metrics structure
		require.Contains(t, metrics, "scanned")
		require.Contains(t, metrics, "updated")
		require.Contains(t, metrics, "failed")

		return nil
	})
}

// TestHTTPAPIPeriodicPolls tests HTTP API with periodic polling disabled.
func TestHTTPAPIPeriodicPolls(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Create a test container
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable": "true",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Start Watchtower with HTTP API and periodic polls disabled
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--http-api-update",
			"--http-api-periodic-polls",
			"--http-api-token=test-token",
			"--http-api-port=8080",
			"--no-startup-message",
		})
		require.NoError(t, err)

		// Wait for API to be ready (should not start periodic updates)
		err = fw.WaitForAPIReady("http://localhost:8080", "test-token", 30*time.Second)
		require.NoError(t, err)

		// Verify no periodic updates are running
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "HTTP API is enabled")
		// Should not contain periodic scheduling messages
		require.NotContains(t, logs, "Scheduling first run")

		return nil
	})
}

// TestHTTPAPIMockServer tests API client against mock server.
func TestHTTPAPIMockServer(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Create mock API server
		mockServer := framework.NewMockAPIServer()
		defer mockServer.Close(context.Background())

		// Create API client pointing to mock server
		client := framework.NewAPIClient(mockServer.URL(), "test-token")

		// Test health endpoint
		resp, err := client.GetHealth()
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)
		resp.Body.Close()

		// Test metrics endpoint
		resp, err = client.GetMetrics()
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)
		resp.Body.Close()

		// Test update trigger
		resp, err = client.TriggerUpdate([]string{"nginx", "redis"})
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)
		resp.Body.Close()

		// Verify requests were captured
		requests := mockServer.GetRequests()
		require.GreaterOrEqual(t, len(requests), 3) // health, metrics, update

		// Check that update request contained the images
		updateFound := false
		for _, req := range requests {
			if req.Path == "/v1/update" && req.Method == http.MethodPost {
				require.Contains(t, req.Body, "nginx")
				require.Contains(t, req.Body, "redis")
				require.Contains(t, req.Headers, "Authorization")
				updateFound = true

				break
			}
		}
		require.True(t, updateFound, "Update request not found in captured requests")

		return nil
	})
}
