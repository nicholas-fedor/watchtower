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

const testFilterDesc = "test filter"

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

// TestUpdateOnStartTriggersImmediateUpdate verifies that the --update-on-start flag
// triggers an immediate update before scheduling periodic updates.
func TestUpdateOnStartTriggersImmediateUpdate(t *testing.T) {
	// Create a command with update-on-start flag enabled
	cmd := &cobra.Command{}
	flags.RegisterSystemFlags(cmd)
	err := cmd.ParseFlags([]string{"--update-on-start", "--no-startup-message"})
	require.NoError(t, err)

	// Track if update function was called
	updateCalled := make(chan bool, 1)
	updateCallCount := int32(0)

	// Mock the update function to signal when called
	originalRunUpdatesWithNotifications := runUpdatesWithNotifications
	runUpdatesWithNotifications = func(_ types.Filter, _ bool) *metrics.Metric {
		atomic.AddInt32(&updateCallCount, 1)

		select {
		case updateCalled <- true:
		default:
		}

		return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
	}

	defer func() { runUpdatesWithNotifications = originalRunUpdatesWithNotifications }()

	// Create a context that will cancel quickly to avoid running the scheduler
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	// Create update lock
	updateLock := make(chan bool, 1)
	updateLock <- true

	// Call runUpgradesOnSchedule with a filter that matches no containers
	filter := func(_ types.FilterableContainer) bool { return false }
	filterDesc := testFilterDesc

	// The function should trigger immediate update and then start scheduler
	err = runUpgradesOnSchedule(ctx, cmd, filter, filterDesc, updateLock, false)

	// Should not return an error (context cancellation is expected)
	require.NoError(t, err)

	// Verify that update was called immediately
	select {
	case <-updateCalled:
		// Expected: update was called
	case <-time.After(100 * time.Millisecond):
		t.Error("Update function was not called immediately with --update-on-start")
	}

	// Verify only one update call occurred (the immediate one)
	assert.Equal(t, int32(1), atomic.LoadInt32(&updateCallCount))
}

// TestUpdateOnStartIntegratesWithCronScheduling verifies that update-on-start
// works with cron scheduling without causing duplicate updates.
func TestUpdateOnStartIntegratesWithCronScheduling(t *testing.T) {
	// Create a command with update-on-start flag enabled and a schedule
	cmd := &cobra.Command{}
	flags.RegisterSystemFlags(cmd)
	err := cmd.ParseFlags(
		[]string{"--update-on-start", "--schedule", "@every 1h", "--no-startup-message"},
	)
	require.NoError(t, err)

	// Save original scheduleSpec and restore after test
	originalScheduleSpec := scheduleSpec

	defer func() { scheduleSpec = originalScheduleSpec }()

	scheduleSpec = "@every 1h" // Set the schedule spec that was parsed

	// Track update calls
	updateCallCount := int32(0)
	updateCalls := make(chan time.Time, 10)

	// Mock the update function
	originalRunUpdatesWithNotifications := runUpdatesWithNotifications
	runUpdatesWithNotifications = func(_ types.Filter, _ bool) *metrics.Metric {
		callTime := time.Now()

		atomic.AddInt32(&updateCallCount, 1)

		select {
		case updateCalls <- callTime:
		default:
		}

		return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
	}

	defer func() { runUpdatesWithNotifications = originalRunUpdatesWithNotifications }()

	// Create a context that allows some time for scheduler to start but not run updates
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Create update lock
	updateLock := make(chan bool, 1)
	updateLock <- true

	// Call runUpgradesOnSchedule
	filter := func(_ types.FilterableContainer) bool { return false }
	filterDesc := testFilterDesc

	startTime := time.Now()
	err = runUpgradesOnSchedule(ctx, cmd, filter, filterDesc, updateLock, false)

	// Should not return an error (context cancellation is expected)
	require.NoError(t, err)

	// Wait a bit for any scheduled calls
	time.Sleep(50 * time.Millisecond)

	// Verify that at least one update was called (the immediate one)
	callCount := atomic.LoadInt32(&updateCallCount)
	assert.GreaterOrEqual(t, callCount, int32(1), "At least one update should have been called")

	// Verify that the first call happened immediately (within 10ms of start)
	select {
	case callTime := <-updateCalls:
		timeSinceStart := callTime.Sub(startTime)
		assert.Less(
			t,
			timeSinceStart,
			10*time.Millisecond,
			"First update should happen immediately",
		)
	default:
		t.Error("No update calls were recorded")
	}

	// Verify no duplicate immediate calls occurred
	assert.LessOrEqual(
		t,
		callCount,
		int32(2),
		"Should not have more than 2 update calls in short test period",
	)
}

// TestUpdateOnStartLockingBehavior verifies that update-on-start respects the update lock
// and doesn't run concurrent updates.
func TestUpdateOnStartLockingBehavior(t *testing.T) {
	// Create a command with update-on-start flag enabled
	cmd := &cobra.Command{}
	flags.RegisterSystemFlags(cmd)
	err := cmd.ParseFlags([]string{"--update-on-start", "--no-startup-message"})
	require.NoError(t, err)

	// Create update lock that's initially unavailable (simulating another update in progress)
	updateLock := make(chan bool, 1)
	// Don't put anything in the lock initially

	// Track if update function was called
	updateCalled := make(chan bool, 1)

	// Mock the update function
	originalRunUpdatesWithNotifications := runUpdatesWithNotifications
	runUpdatesWithNotifications = func(_ types.Filter, _ bool) *metrics.Metric {
		select {
		case updateCalled <- true:
		default:
		}

		return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
	}

	defer func() { runUpdatesWithNotifications = originalRunUpdatesWithNotifications }()

	// Create a short context
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Call runUpgradesOnSchedule
	filter := func(_ types.FilterableContainer) bool { return false }
	filterDesc := testFilterDesc

	err = runUpgradesOnSchedule(ctx, cmd, filter, filterDesc, updateLock, false)

	// Should not return an error
	require.NoError(t, err)

	// Verify that update was NOT called because lock was unavailable
	select {
	case <-updateCalled:
		t.Error("Update should not have been called when lock is unavailable")
	case <-time.After(10 * time.Millisecond):
		// Expected: no update call
	}
}

// TestUpdateOnStartSelfUpdateScenario verifies that update-on-start works correctly
// in self-update scenarios where Watchtower updates itself.
func TestUpdateOnStartSelfUpdateScenario(t *testing.T) {
	// Create a command with update-on-start flag enabled
	cmd := &cobra.Command{}
	flags.RegisterSystemFlags(cmd)
	err := cmd.ParseFlags([]string{"--update-on-start", "--no-startup-message"})
	require.NoError(t, err)

	// Track update calls
	updateCalled := make(chan bool, 1)

	// Mock the update function
	originalRunUpdatesWithNotifications := runUpdatesWithNotifications
	runUpdatesWithNotifications = func(_ types.Filter, _ bool) *metrics.Metric {
		select {
		case updateCalled <- true:
		default:
		}

		return &metrics.Metric{Scanned: 1, Updated: 1, Failed: 0}
	}

	defer func() { runUpdatesWithNotifications = originalRunUpdatesWithNotifications }()

	// Create a short context
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	// Create update lock
	updateLock := make(chan bool, 1)
	updateLock <- true

	// Call runUpgradesOnSchedule with a filter that includes containers
	filter := func(_ types.FilterableContainer) bool { return true }
	filterDesc := testFilterDesc

	err = runUpgradesOnSchedule(ctx, cmd, filter, filterDesc, updateLock, false)

	// Should not return an error
	require.NoError(t, err)

	// Verify that update was called for self-update scenario
	select {
	case <-updateCalled:
		// Expected: update was called
	case <-time.After(50 * time.Millisecond):
		t.Error("Update function was not called in self-update scenario")
	}
}

// TestUpdateOnStartMultiInstanceScenario verifies that multiple Watchtower instances
// with update-on-start don't conflict with each other.
func TestUpdateOnStartMultiInstanceScenario(t *testing.T) {
	// This test simulates two Watchtower instances both with --update-on-start
	// They should not conflict due to proper locking

	// Create commands with update-on-start flag enabled
	cmd1 := &cobra.Command{}
	flags.RegisterSystemFlags(cmd1)
	err := cmd1.ParseFlags([]string{"--update-on-start", "--no-startup-message"})
	require.NoError(t, err)

	cmd2 := &cobra.Command{}
	flags.RegisterSystemFlags(cmd2)
	err = cmd2.ParseFlags([]string{"--update-on-start", "--no-startup-message"})
	require.NoError(t, err)

	// Shared update lock (simulating shared resource)
	updateLock := make(chan bool, 1)
	updateLock <- true

	// Track update calls from both instances
	updateCallCount := int32(0)

	var completed int32

	instance1Called := make(chan bool, 1)
	instance2Called := make(chan bool, 1)

	// Mock the update function
	originalRunUpdatesWithNotifications := runUpdatesWithNotifications
	runUpdatesWithNotifications = func(_ types.Filter, _ bool) *metrics.Metric {
		atomic.AddInt32(&updateCallCount, 1)
		time.Sleep(50 * time.Millisecond) // Simulate update work

		return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
	}

	defer func() { runUpdatesWithNotifications = originalRunUpdatesWithNotifications }()

	// Start both instances concurrently
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		filter := func(_ types.FilterableContainer) bool { return false }
		filterDesc := "instance1"

		err := runUpgradesOnSchedule(ctx, cmd1, filter, filterDesc, updateLock, false)
		assert.NoError(t, err)
		atomic.AddInt32(&completed, 1)
		close(instance1Called)
	}()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		filter := func(_ types.FilterableContainer) bool { return false }
		filterDesc := "instance2"

		err := runUpgradesOnSchedule(ctx, cmd2, filter, filterDesc, updateLock, false)
		assert.NoError(t, err)
		atomic.AddInt32(&completed, 1)
		close(instance2Called)
	}()

	// Wait for both instances to complete
	<-instance1Called
	<-instance2Called

	// Verify that both instances shut down properly
	assert.Equal(
		t,
		int32(2),
		atomic.LoadInt32(&completed),
		"Both instances should have shut down properly",
	)

	// Verify that only one update occurred due to locking (one instance gets the lock first)
	callCount := atomic.LoadInt32(&updateCallCount)
	assert.Equal(t, int32(1), callCount, "Only one update should occur due to lock serialization")
	// Verify the lock is properly released after the test
	lockAvailable := false

	select {
	case v := <-updateLock:
		lockAvailable = true
		// Lock was available, put it back for cleanup
		updateLock <- v
	default:
		// Lock not available
	}

	assert.True(t, lockAvailable, "Lock should be available after test completion")
}

// TestWaitForRunningUpdate_NoUpdateRunning verifies that waitForRunningUpdate returns immediately
// when no update is currently running (lock channel has a value).
func TestWaitForRunningUpdate_NoUpdateRunning(t *testing.T) {
	lock := make(chan bool, 1)
	lock <- true // Lock is available, no update running

	ctx := context.Background()
	start := time.Now()

	waitForRunningUpdate(ctx, lock)

	elapsed := time.Since(start)

	// Should return immediately without blocking
	assert.Less(t, elapsed, 10*time.Millisecond, "Should not block when no update is running")
}

// TestWaitForRunningUpdate_UpdateRunning verifies that waitForRunningUpdate blocks
// and waits for an update to complete when one is running (lock channel is empty).
func TestWaitForRunningUpdate_UpdateRunning(t *testing.T) {
	lock := make(chan bool, 1)
	// Don't put anything in lock initially - simulating update in progress

	ctx := context.Background()
	waitCompleted := make(chan bool, 1)

	go func() {
		waitForRunningUpdate(ctx, lock)

		waitCompleted <- true
	}()

	// Wait a bit to ensure waitForRunningUpdate is blocking
	time.Sleep(50 * time.Millisecond)

	// Verify it's still waiting
	select {
	case <-waitCompleted:
		t.Error("waitForRunningUpdate should still be waiting")
	default:
		// Expected: still waiting
	}

	// Now complete the "update" by putting value back in lock
	lock <- true

	// Wait for waitForRunningUpdate to complete
	select {
	case <-waitCompleted:
		// Expected: completed after lock was released
	case <-time.After(100 * time.Millisecond):
		t.Error("waitForRunningUpdate should have completed after lock was released")
	}
}

// TestRunUpgradesOnSchedule_ShutdownWaitsForRunningUpdate verifies that runUpgradesOnSchedule
// waits for any running update to complete before shutting down when receiving a shutdown signal.
func TestRunUpgradesOnSchedule_ShutdownWaitsForRunningUpdate(t *testing.T) {
	// Create a command without scheduling to keep test simple
	cmd := &cobra.Command{}
	flags.RegisterSystemFlags(cmd)
	err := cmd.ParseFlags([]string{"--no-startup-message"})
	require.NoError(t, err)

	// Create update lock
	updateLock := make(chan bool, 1)
	updateLock <- true

	// Track when shutdown completes
	shutdownCompleted := make(chan bool, 1)

	// Mock runUpdatesWithNotifications to simulate a long-running update
	originalRunUpdatesWithNotifications := runUpdatesWithNotifications
	runUpdatesWithNotifications = func(_ types.Filter, _ bool) *metrics.Metric {
		// Signal that we're in the update
		time.Sleep(100 * time.Millisecond) // Simulate update work

		return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
	}

	defer func() { runUpdatesWithNotifications = originalRunUpdatesWithNotifications }()

	// Create a cancellable context for shutdown
	ctx, cancel := context.WithCancel(context.Background())

	// Start runUpgradesOnSchedule in a goroutine
	go func() {
		filter := func(_ types.FilterableContainer) bool { return false }
		filterDesc := testFilterDesc

		// This should start and wait for context cancellation
		err := runUpgradesOnSchedule(ctx, cmd, filter, filterDesc, updateLock, false)
		assert.NoError(t, err)

		shutdownCompleted <- true
	}()

	// Start an update manually to simulate one running
	go func() {
		select {
		case v := <-updateLock:
			defer func() { updateLock <- v }()

			time.Sleep(200 * time.Millisecond) // Longer than the shutdown delay
		default:
			// Lock not available
		}
	}()

	// Give the update time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context to trigger shutdown
	cancel()

	// Wait for shutdown to complete
	select {
	case <-shutdownCompleted:
		// Expected: shutdown completed after waiting for update
	case <-time.After(500 * time.Millisecond):
		t.Error("Shutdown should have completed after update finished")
	}
}
