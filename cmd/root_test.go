package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/docker/docker/api/types/container"

	"github.com/nicholas-fedor/watchtower/internal/flags"
	"github.com/nicholas-fedor/watchtower/pkg/api/update"
	containerMock "github.com/nicholas-fedor/watchtower/pkg/container/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	typeMock "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

// TestDeriveScopeFromContainer tests the deriveScopeFromContainer function with various scenarios.
func TestDeriveScopeFromContainer(t *testing.T) {
	// Save original scope value to restore later
	originalScope := scope

	defer func() { scope = originalScope }()

	tests := []struct {
		name              string
		initialScope      string
		hostname          string
		mockSetup         func(*containerMock.MockClient, *typeMock.MockContainer)
		expectedScope     string
		expectedError     bool
		expectedErrorType error
	}{
		{
			name:              "scope already set - should return nil without derivation",
			initialScope:      "preset",
			hostname:          "test-container",
			mockSetup:         func(*containerMock.MockClient, *typeMock.MockContainer) {},
			expectedScope:     "preset",
			expectedError:     false,
			expectedErrorType: nil,
		},
		{
			name:              "no hostname - should return error",
			initialScope:      "",
			hostname:          "",
			mockSetup:         func(*containerMock.MockClient, *typeMock.MockContainer) {},
			expectedScope:     "",
			expectedError:     true,
			expectedErrorType: ErrContainerIDNotFound,
		},
		{
			name:         "container lookup fails - should return error",
			initialScope: "",
			hostname:     "test-container",
			mockSetup: func(client *containerMock.MockClient, container *typeMock.MockContainer) {
				client.EXPECT().ListAllContainers().
					Return([]types.Container{container}, nil)
				container.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
					Config: &dockerContainer.Config{Hostname: "test-container"},
				})
				container.EXPECT().ID().Return(types.ContainerID("test-container"))
				client.EXPECT().GetContainer(types.ContainerID("test-container")).
					Return(nil, errors.New("container not found"))
			},
			expectedScope:     "",
			expectedError:     true,
			expectedErrorType: nil, // Not checking specific error type for this case
		},
		{
			name:         "container has no scope label - should return nil",
			initialScope: "",
			hostname:     "test-container",
			mockSetup: func(client *containerMock.MockClient, container *typeMock.MockContainer) {
				client.EXPECT().ListAllContainers().
					Return([]types.Container{container}, nil)
				container.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
					Config: &dockerContainer.Config{Hostname: "test-container"},
				})
				container.EXPECT().ID().Return(types.ContainerID("test-container"))
				client.EXPECT().GetContainer(types.ContainerID("test-container")).
					Return(container, nil)
				container.EXPECT().Scope().Return("", false)
			},
			expectedScope:     "",
			expectedError:     false,
			expectedErrorType: nil,
		},
		{
			name:         "container has empty scope label - should return nil",
			initialScope: "",
			hostname:     "test-container",
			mockSetup: func(client *containerMock.MockClient, container *typeMock.MockContainer) {
				client.EXPECT().ListAllContainers().
					Return([]types.Container{container}, nil)
				container.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
					Config: &dockerContainer.Config{Hostname: "test-container"},
				})
				container.EXPECT().ID().Return(types.ContainerID("test-container"))
				client.EXPECT().GetContainer(types.ContainerID("test-container")).
					Return(container, nil)
				container.EXPECT().Scope().Return("", true)
			},
			expectedScope:     "",
			expectedError:     false,
			expectedErrorType: nil,
		},
		{
			name:         "container has valid scope label - should set scope and return nil",
			initialScope: "",
			hostname:     "test-container",
			mockSetup: func(client *containerMock.MockClient, container *typeMock.MockContainer) {
				client.EXPECT().ListAllContainers().
					Return([]types.Container{container}, nil)
				container.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
					Config: &dockerContainer.Config{Hostname: "test-container"},
				})
				container.EXPECT().ID().Return(types.ContainerID("test-container"))
				client.EXPECT().GetContainer(types.ContainerID("test-container")).
					Return(container, nil)
				container.EXPECT().Scope().Return("production", true)
			},
			expectedScope:     "production",
			expectedError:     false,
			expectedErrorType: nil,
		},
		{
			name:         "custom hostname with special characters - should work",
			initialScope: "",
			hostname:     "my_app.container-123",
			mockSetup: func(client *containerMock.MockClient, container *typeMock.MockContainer) {
				client.EXPECT().ListAllContainers().
					Return([]types.Container{container}, nil)
				container.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
					Config: &dockerContainer.Config{Hostname: "my_app.container-123"},
				})
				container.EXPECT().ID().Return(types.ContainerID("my_app.container-123"))
				client.EXPECT().GetContainer(types.ContainerID("my_app.container-123")).
					Return(container, nil)
				container.EXPECT().Scope().Return("staging", true)
			},
			expectedScope:     "staging",
			expectedError:     false,
			expectedErrorType: nil,
		},
		{
			name:         "custom hostname from Docker Compose - should derive scope",
			initialScope: "",
			hostname:     "watchtower_watchtower_1",
			mockSetup: func(client *containerMock.MockClient, container *typeMock.MockContainer) {
				client.EXPECT().ListAllContainers().
					Return([]types.Container{container}, nil)
				container.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
					Config: &dockerContainer.Config{Hostname: "watchtower_watchtower_1"},
				})
				container.EXPECT().ID().Return(types.ContainerID("watchtower_watchtower_1"))
				client.EXPECT().GetContainer(types.ContainerID("watchtower_watchtower_1")).
					Return(container, nil)
				container.EXPECT().Scope().Return("project-watchtower", true)
			},
			expectedScope:     "project-watchtower",
			expectedError:     false,
			expectedErrorType: nil,
		},
		{
			name:         "custom hostname lookup fails - should return error",
			initialScope: "",
			hostname:     "nonexistent-container",
			mockSetup: func(client *containerMock.MockClient, container *typeMock.MockContainer) {
				client.EXPECT().ListAllContainers().
					Return([]types.Container{container}, nil)
				container.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
					Config: &dockerContainer.Config{Hostname: "nonexistent-container"},
				})
				container.EXPECT().ID().Return(types.ContainerID("nonexistent-container"))
				client.EXPECT().GetContainer(types.ContainerID("nonexistent-container")).
					Return(nil, errors.New("container not found"))
			},
			expectedScope:     "",
			expectedError:     true,
			expectedErrorType: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset scope to initial value
			scope = tt.initialScope

			// Set up environment
			t.Setenv("HOSTNAME", tt.hostname)

			// Create mocks
			mockClient := containerMock.NewMockClient(t)
			mockContainer := typeMock.NewMockContainer(t)

			// Set up mock expectations
			tt.mockSetup(mockClient, mockContainer)

			// Execute function under test
			err := deriveScopeFromContainer(mockClient)

			// Assert results
			if tt.expectedError {
				require.Error(t, err)

				if tt.expectedErrorType != nil {
					require.ErrorIs(t, err, tt.expectedErrorType)
				}
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.expectedScope, scope)

			// Verify mock expectations
			mockClient.AssertExpectations(t)
			mockContainer.AssertExpectations(t)
		})
	}
}

// TestDeriveScopeFromContainer_Logging tests logging behavior in deriveScopeFromContainer.

func TestDeriveScopeFromContainer_Logging(t *testing.T) {
	// Save original scope value to restore later
	originalScope := scope

	defer func() { scope = originalScope }()

	// Save original log level
	originalLevel := logrus.GetLevel()
	defer logrus.SetLevel(originalLevel)

	// Set log level to debug to capture debug logs
	logrus.SetLevel(logrus.DebugLevel)

	// Set up environment
	t.Setenv("HOSTNAME", "test-container")

	// Reset scope
	scope = ""

	// Create mocks
	mockClient := containerMock.NewMockClient(t)
	mockContainer := typeMock.NewMockContainer(t)

	// Set up successful derivation
	mockClient.On("ListAllContainers").Return([]types.Container{mockContainer}, nil)
	mockContainer.On("ContainerInfo").Return(&dockerContainer.InspectResponse{
		Config: &dockerContainer.Config{Hostname: "test-container"},
	})
	mockContainer.On("ID").Return(types.ContainerID("test-container"))
	mockClient.On("GetContainer", types.ContainerID("test-container")).Return(mockContainer, nil)
	mockContainer.On("Scope").Return("derived-scope", true)

	// Capture log output
	var logOutput []byte

	hook := &testLogHook{logs: &logOutput}

	logrus.AddHook(hook)
	defer logrus.StandardLogger().ReplaceHooks(make(map[logrus.Level][]logrus.Hook))

	// Execute function
	err := deriveScopeFromContainer(mockClient)

	// Assert no error and scope was set
	require.NoError(t, err)
	assert.Equal(t, "derived-scope", scope)

	// Verify log contains expected message
	logStr := string(logOutput)
	assert.Contains(t, logStr, "Derived operational scope from current container's scope label")
	assert.Contains(t, logStr, "container_id=test-container")
	assert.Contains(t, logStr, "derived_scope=derived-scope")

	// Verify mock expectations
	mockClient.AssertExpectations(t)
	mockContainer.AssertExpectations(t)
}

// testLogHook captures log output for testing.
type testLogHook struct {
	logs *[]byte
}

func (h *testLogHook) Fire(entry *logrus.Entry) error {
	// Format the log entry similar to how logrus does it
	formatted := fmt.Sprintf("time=\"%s\" level=%s msg=\"%s\"",
		entry.Time.Format("2006-01-02T15:04:05-07:00"),
		entry.Level.String(),
		entry.Message)

	// Add fields
	for k, v := range entry.Data {
		formatted += fmt.Sprintf(" %s=%v", k, v)
	}

	formatted += "\n"

	*h.logs = append(*h.logs, []byte(formatted)...)

	return nil
}

func (h *testLogHook) Levels() []logrus.Level {
	// TestFormatDuration tests the formatDuration function with various time durations.
	return []logrus.Level{logrus.DebugLevel}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{
			name:     "zero duration",
			duration: 0,
			expected: "0 seconds",
		},
		{
			name:     "only seconds",
			duration: 45 * time.Second,
			expected: "45 seconds",
		},
		{
			name:     "minutes and seconds",
			duration: 2*time.Minute + 30*time.Second,
			expected: "2 minutes, 30 seconds",
		},
		{
			name:     "hours, minutes, seconds",
			duration: 1*time.Hour + 30*time.Minute + 45*time.Second,
			expected: "1 hour, 30 minutes, 45 seconds",
		},
		{
			name:     "single hour",
			duration: 1 * time.Hour,
			expected: "1 hour",
		},
		{
			name:     "single minute",
			duration: 1 * time.Minute,
			expected: "1 minute",
		},
		{
			name:     "single second",
			duration: 1 * time.Second,
			expected: "1 second",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatDuration(tt.duration)
			assert.Equal(t, tt.expected, result)
			// TestFormatTimeUnit tests the formatTimeUnit function with different values and options.
		})
	}
}

func TestFormatTimeUnit(t *testing.T) {
	tests := []struct {
		name         string
		value        int64
		singular     string
		plural       string
		forceInclude bool
		expected     string
	}{
		{
			name:         "zero value not forced",
			value:        0,
			singular:     "second",
			plural:       "seconds",
			forceInclude: false,
			expected:     "",
		},
		{
			name:         "zero value forced",
			value:        0,
			singular:     "second",
			plural:       "seconds",
			forceInclude: true,
			expected:     "0 seconds",
		},
		{
			name:         "singular value",
			value:        1,
			singular:     "hour",
			plural:       "hours",
			forceInclude: false,
			expected:     "1 hour",
		},
		{
			name:         "plural value",
			value:        5,
			singular:     "minute",
			plural:       "minutes",
			forceInclude: false,
			expected:     "5 minutes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatTimeUnit(struct {
				value    int64
				singular string
				plural   string
			}{tt.value, tt.singular, tt.plural}, tt.forceInclude)
			// TestFilterEmpty tests the filterEmpty function with various string slices.
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFilterEmpty(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "all empty",
			input:    []string{"", "", ""},
			expected: nil,
		},
		{
			name:     "all non-empty",
			input:    []string{"a", "b", "c"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "mixed empty and non-empty",
			input:    []string{"", "a", "", "b", ""},
			expected: []string{"a", "b"},
		},
		{
			name:     "empty slice",
			input:    []string{},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// TestAwaitDockerClient tests that awaitDockerClient sleeps for the expected duration.
			result := filterEmpty(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAwaitDockerClient(t *testing.T) {
	// This function just sleeps for 1 second, so we test that it doesn't panic
	// and completes within a reasonable time
	start := time.Now()

	awaitDockerClient()

	// TestLifecycleFlags tests reading lifecycle UID and GID flags.
	elapsed := time.Since(start)

	// Should take at least 900ms but not more than 3 seconds (to account for CI timing variations)
	assert.GreaterOrEqual(
		t,
		elapsed,
		900*time.Millisecond,
		"Should sleep for at least 900 milliseconds",
	)
	assert.Less(t, elapsed, 3*time.Second, "Should not sleep for more than 3 seconds")
}

func TestLifecycleFlags(t *testing.T) {
	// Test that lifecycle UID and GID flags are properly read
	originalLifecycleUID := lifecycleUID
	originalLifecycleGID := lifecycleGID

	defer func() {
		lifecycleUID = originalLifecycleUID
		lifecycleGID = originalLifecycleGID
	}()

	// Reset to defaults
	lifecycleUID = 0
	lifecycleGID = 0

	// Test setting flags
	cmd := &cobra.Command{}
	flags.RegisterSystemFlags(cmd)

	err := cmd.ParseFlags([]string{"--lifecycle-uid", "1000", "--lifecycle-gid", "1001"})
	require.NoError(t, err)

	// Simulate preRun flag reading
	uid, err := cmd.Flags().GetInt("lifecycle-uid")
	require.NoError(t, err)

	lifecycleUID = uid

	gid, err := cmd.Flags().GetInt("lifecycle-gid")
	require.NoError(t, err)

	lifecycleGID = gid

	assert.Equal(t, 1000, lifecycleUID, "lifecycleUID should be set to 1000")
	assert.Equal(t, 1001, lifecycleGID, "lifecycleGID should be set to 1001")
}

func TestGetAPIAddr(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		port     string
		expected string
	}{
		{
			name:     "empty host",
			host:     "",
			port:     "8080",
			expected: ":8080",
		},
		{
			name:     "IPv4 host",
			host:     "127.0.0.1",
			port:     "8080",
			expected: "127.0.0.1:8080",
		},
		{
			name:     "IPv6 host",
			host:     "::1",
			port:     "8080",
			expected: "[::1]:8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getAPIAddr(tt.host, tt.port)
			assert.Equal(t, tt.expected, result)

			// Verify the formatted address is a valid TCP address
			_, err := net.ResolveTCPAddr("tcp", result)
			assert.NoError(t, err, "formatted address should be a valid TCP address")
		})
	}
}

// TestUpdateLockSerialization verifies that the updateLock mechanism properly serializes updates,
// preventing concurrent access to the Docker client. This test simulates multiple update operations
// running simultaneously, ensuring only one update executes at a time, mimicking the behavior
// of updateOnStart and scheduled updates in the main application.
func TestUpdateLockSerialization(t *testing.T) {
	// Initialize the update lock channel with the same pattern as in runMain
	updateLock := make(chan bool, 1)
	updateLock <- true

	// Atomic counters to track concurrent execution and completion
	var (
		running   int32
		started   int32
		completed int32
	)

	// WaitGroup to synchronize test completion
	var wg sync.WaitGroup

	// Simulate the update function used in runMain and runUpgradesOnSchedule
	updateFunc := func(id int) {
		select {
		case v := <-updateLock:
			// Acquired lock, perform update
			defer func() { updateLock <- v }()

			// Track that only one update is running at a time
			current := atomic.AddInt32(&running, 1)
			require.Equal(
				t,
				int32(1),
				current,
				"Only one update should be running at a time, but %d are running",
				current,
			)

			atomic.AddInt32(&started, 1)

			// Simulate update work with a delay
			time.Sleep(100 * time.Millisecond)

			atomic.AddInt32(&running, -1)
			atomic.AddInt32(&completed, 1)

		default:
			// Lock not available, skip update (same as in the actual code)
			t.Logf("Update %d skipped due to concurrent update in progress", id)
		}
	}

	// Simulate concurrent updateOnStart and scheduled updates
	numUpdates := 2
	for i := range numUpdates {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			updateFunc(id)
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Verify that only one update executed due to lock serialization
	assert.Equal(t, int32(1), started, "Only one update should have started due to lock")
	assert.Equal(t, int32(1), completed, "Only one update should have completed")
	assert.Equal(t, int32(0), running, "No updates should be running after completion")
}

// TestConcurrentScheduledAndAPIUpdate verifies that API-triggered updates wait for scheduled updates to complete,
// ensuring proper serialization and preventing race conditions between periodic updates and HTTP API calls.
func TestConcurrentScheduledAndAPIUpdate(t *testing.T) {
	// Initialize the update lock channel with the same pattern as in runMain
	updateLock := make(chan bool, 1)
	updateLock <- true

	// Channels to signal when each update type starts and completes
	scheduledStarted := make(chan struct{})
	scheduledCompleted := make(chan struct{})
	apiStarted := make(chan struct{})
	apiCompleted := make(chan struct{})

	// Mock update function for API handler that signals start and completion
	updateFn := func(_ []string) *metrics.Metric {
		close(apiStarted)
		time.Sleep(100 * time.Millisecond) // Simulate API update work
		close(apiCompleted)

		return &metrics.Metric{Scanned: 1, Updated: 1, Failed: 0}
	}

	// Create the update handler with the shared lock
	handler := update.New(updateFn, updateLock)

	// Simulate scheduled update (longer duration)
	go func() {
		select {
		case v := <-updateLock:
			close(scheduledStarted)
			time.Sleep(200 * time.Millisecond) // Simulate scheduled update work (longer than API)
			close(scheduledCompleted)

			updateLock <- v
		default:
			t.Error("Scheduled update should have acquired the lock")
		}
	}()

	// Simulate API update request
	go func() {
		req, err := http.NewRequestWithContext(
			context.Background(),
			http.MethodPost,
			"/v1/update",
			http.NoBody,
		)
		if err != nil {
			t.Errorf("Failed to create request: %v", err)

			return
		}

		w := httptest.NewRecorder()
		handler.Handle(w, req)
	}()

	// Wait for scheduled update to start
	<-scheduledStarted

	// Verify API update has not started yet (should be blocked by lock)
	select {
	case <-apiStarted:
		t.Error("API update should not have started while scheduled update is running")
	default:
		// Expected: API is blocked
	}

	// Wait for scheduled update to complete
	<-scheduledCompleted

	// Now API update should start and complete
	<-apiStarted
	<-apiCompleted

	// Verify the API response is successful
	// Note: In a real scenario, we'd check the response body, but for this test,
	// the completion signals are sufficient to verify serialization
}
