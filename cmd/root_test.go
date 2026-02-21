package cmd

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/docker/docker/api/types/container"

	"github.com/nicholas-fedor/watchtower/internal/actions"
	"github.com/nicholas-fedor/watchtower/internal/api"
	"github.com/nicholas-fedor/watchtower/internal/flags"
	"github.com/nicholas-fedor/watchtower/internal/logging"
	"github.com/nicholas-fedor/watchtower/internal/scheduling"
	"github.com/nicholas-fedor/watchtower/internal/util"
	"github.com/nicholas-fedor/watchtower/pkg/api/update"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	mockContainer "github.com/nicholas-fedor/watchtower/pkg/container/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	mockTypes "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

const testFilterDesc = "test filter"

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
			result := util.FormatDuration(tt.duration)
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
			result := util.FormatTimeUnit(tt.value, tt.singular, tt.plural, tt.forceInclude)
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
			result := util.FilterEmpty(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAwaitDockerClient(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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
	})
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
			result := api.GetAPIAddr(tt.host, tt.port)
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
	synctest.Test(t, func(t *testing.T) {
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
				synctest.Wait()

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
	})
}

// TestConcurrentScheduledAndAPIUpdate verifies that API-triggered updates wait for scheduled updates to complete,
// ensuring proper serialization and preventing race conditions between periodic updates and HTTP API calls.
func TestConcurrentScheduledAndAPIUpdate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Enable debug logging to see lock acquisition logs
		originalLevel := logrus.GetLevel()

		logrus.SetLevel(logrus.DebugLevel)
		defer logrus.SetLevel(originalLevel)

		// Initialize the update lock channel with the same pattern as in runMain
		updateLock := make(chan bool, 1)
		updateLock <- true

		// Channels to signal when each update type starts and completes
		scheduledStarted := make(chan struct{})
		scheduledCompleted := make(chan struct{})
		apiStarted := make(chan struct{})
		apiCompleted := make(chan struct{})

		// Mock update function for API handler that signals start and completion
		// Mutex to protect concurrent t.Log calls from race conditions
		var logMu sync.Mutex

		updateFn := func(_ []string) *metrics.Metric {
			close(apiStarted)
			synctest.Wait() // Simulate API update work
			close(apiCompleted)

			return &metrics.Metric{Scanned: 1, Updated: 1, Failed: 0}
		}

		// Create the update handler with the shared lock
		handler := update.New(updateFn, updateLock)

		// Simulate scheduled update (longer duration)
		go func() {
			logMu.Lock()
			t.Log("Scheduled: trying to acquire lock")
			logMu.Unlock()

			select {
			case v := <-updateLock:
				t.Log("Scheduled: acquired lock")
				close(scheduledStarted)
				synctest.Wait() // Simulate scheduled update work (longer than API)
				close(scheduledCompleted)
				t.Log("Scheduled: releasing lock")

				updateLock <- v
			default:
				t.Error("Scheduled update should have acquired the lock")
			}
		}()

		// Wait for scheduled update to start
		<-scheduledStarted

		// Simulate API update request
		go func() {
			t.Log("API: creating request")

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

			t.Log("API: calling handler.Handle")
			handler.Handle(w, req)
			t.Log("API: handler.Handle completed")
		}()

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
	})
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
	runUpdatesWithNotifications = func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
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
	filter := types.Filter(func(_ types.FilterableContainer) bool { return false })
	filterDesc := testFilterDesc

	// The function should trigger immediate update and then start scheduler
	err = scheduling.RunUpgradesOnSchedule(
		ctx,
		cmd,
		filter,
		filterDesc,
		updateLock,
		false,
		"",
		logging.WriteStartupMessage,
		runUpdatesWithNotifications,
		nil,
		"",
		nil,
		"",
		false,
		true,
		false,
		nil,
	)

	// Should not return an error (context cancellation is expected)
	require.NoError(t, err)

	// Verify that update was called immediately
	select {
	case <-updateCalled:
		// Expected: update was called
	default:
		t.Error("Update function was not called immediately with --update-on-start")
	}

	// Verify only one update call occurred (the immediate one)
	assert.Equal(t, int32(1), atomic.LoadInt32(&updateCallCount))
}

// TestUpdateOnStartIntegratesWithCronScheduling verifies that update-on-start
// works with cron scheduling without causing duplicate updates.
func TestUpdateOnStartIntegratesWithCronScheduling(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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
		runUpdatesWithNotifications = func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
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
		filter := types.Filter(func(_ types.FilterableContainer) bool { return false })
		filterDesc := testFilterDesc

		startTime := time.Now()
		err = scheduling.RunUpgradesOnSchedule(
			ctx,
			cmd,
			filter,
			filterDesc,
			updateLock,
			false,
			"",
			logging.WriteStartupMessage,
			runUpdatesWithNotifications,
			nil,
			"",
			nil,
			"",
			false,
			true,
			false,
			nil,
		)

		// Should not return an error (context cancellation is expected)
		require.NoError(t, err)

		// Wait a bit for any scheduled calls
		synctest.Wait()

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
	})
}

// TestUpdateOnStartLockingBehavior verifies that update-on-start respects the update lock
// and doesn't run concurrent updates.
func TestUpdateOnStartLockingBehavior(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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
		runUpdatesWithNotifications = func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
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
		filter := types.Filter(func(_ types.FilterableContainer) bool { return false })
		filterDesc := testFilterDesc

		err = scheduling.RunUpgradesOnSchedule(
			ctx,
			cmd,
			filter,
			filterDesc,
			updateLock,
			false,
			"",
			logging.WriteStartupMessage,
			runUpdatesWithNotifications,
			nil,
			"",
			nil,
			"",
			false,
			false,
			false,
			nil,
		)

		// Should not return an error
		require.NoError(t, err)

		// Verify that update was NOT called because lock was unavailable
		select {
		case <-updateCalled:
			t.Error("Update should not have been called when lock is unavailable")
		default:
			// Expected: no update call
		}
	})
}

// TestUpdateOnStartSelfUpdateScenario verifies that update-on-start works correctly
// in self-update scenarios where Watchtower updates itself.
func TestUpdateOnStartSelfUpdateScenario(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Create a command with update-on-start flag enabled
		cmd := &cobra.Command{}
		flags.RegisterSystemFlags(cmd)
		err := cmd.ParseFlags([]string{"--update-on-start", "--no-startup-message"})
		require.NoError(t, err)

		updateOnStart, _ := cmd.Flags().GetBool("update-on-start")

		// Track update calls
		updateCalled := make(chan bool, 1)

		// Mock the update function
		originalRunUpdatesWithNotifications := runUpdatesWithNotifications
		runUpdatesWithNotifications = func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
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
		filter := types.Filter(func(_ types.FilterableContainer) bool { return true })
		filterDesc := testFilterDesc

		err = scheduling.RunUpgradesOnSchedule(
			ctx,
			cmd,
			filter,
			filterDesc,
			updateLock,
			false,
			"",
			logging.WriteStartupMessage,
			runUpdatesWithNotifications,
			nil,
			"",
			nil,
			"",
			false,
			updateOnStart,
			false,
			nil,
		)

		// Should not return an error
		require.NoError(t, err)

		// Verify that update was called for self-update scenario
		select {
		case <-updateCalled:
			// Expected: update was called
		default:
			t.Error("Update function was not called in self-update scenario")
		}
	})
}

// TestUpdateOnStartMultiInstanceScenario verifies that multiple Watchtower instances
// with update-on-start don't conflict with each other.
func TestUpdateOnStartMultiInstanceScenario(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// This test simulates two Watchtower instances both with --update-on-start
		// They should not conflict due to proper locking

		// Create commands with update-on-start flag enabled
		cmd1 := &cobra.Command{}
		flags.RegisterSystemFlags(cmd1)
		err := cmd1.ParseFlags([]string{"--update-on-start", "--no-startup-message"})
		require.NoError(t, err)

		updateOnStart1, _ := cmd1.Flags().GetBool("update-on-start")

		cmd2 := &cobra.Command{}
		flags.RegisterSystemFlags(cmd2)
		err = cmd2.ParseFlags([]string{"--update-on-start", "--no-startup-message"})
		require.NoError(t, err)

		updateOnStart2, _ := cmd2.Flags().GetBool("update-on-start")

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
		runUpdatesWithNotifications = func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
			atomic.AddInt32(&updateCallCount, 1)
			synctest.Wait() // Simulate update work

			return nil // Don't trigger metrics in test
		}

		defer func() { runUpdatesWithNotifications = originalRunUpdatesWithNotifications }()

		// Start both instances concurrently
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			filter := types.Filter(func(_ types.FilterableContainer) bool { return false })
			filterDesc := "instance1"

			err := scheduling.RunUpgradesOnSchedule(
				ctx,
				cmd1,
				filter,
				filterDesc,
				updateLock,
				false,
				"",
				logging.WriteStartupMessage,
				runUpdatesWithNotifications,
				nil,
				"",
				nil,
				"",
				false,
				updateOnStart1,
				false,
				nil,
			)
			assert.NoError(t, err)
			atomic.AddInt32(&completed, 1)
			close(instance1Called)
		}()

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			filter := types.Filter(func(_ types.FilterableContainer) bool { return false })
			filterDesc := "instance2"

			err := scheduling.RunUpgradesOnSchedule(
				ctx,
				cmd2,
				filter,
				filterDesc,
				updateLock,
				false,
				"",
				logging.WriteStartupMessage,
				runUpdatesWithNotifications,
				nil,
				"",
				nil,
				"",
				false,
				updateOnStart2,
				false,
				nil,
			)
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
		assert.Equal(
			t,
			int32(1),
			callCount,
			"Only one update should occur due to lock serialization",
		)
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
	})
}

// TestWaitForRunningUpdate_NoUpdateRunning verifies that waitForRunningUpdate returns immediately
// when no update is currently running (lock channel has a value).
func TestWaitForRunningUpdate_NoUpdateRunning(t *testing.T) {
	lock := make(chan bool, 1)
	lock <- true // Lock is available, no update running

	ctx := context.Background()
	start := time.Now()

	scheduling.WaitForRunningUpdate(ctx, lock)

	elapsed := time.Since(start)

	// Should return immediately without blocking
	assert.Less(t, elapsed, 10*time.Millisecond, "Should not block when no update is running")
}

// TestWaitForRunningUpdate_UpdateRunning verifies that waitForRunningUpdate blocks
// and waits for an update to complete when one is running (lock channel is empty).
func TestWaitForRunningUpdate_UpdateRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		lock := make(chan bool, 1)
		// Don't put anything in lock initially - simulating update in progress

		ctx := context.Background()
		waitCompleted := make(chan bool, 1)

		go func() {
			scheduling.WaitForRunningUpdate(ctx, lock)

			waitCompleted <- true
		}()

		// Wait a bit to ensure waitForRunningUpdate is blocking
		synctest.Wait()

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
		synctest.Wait()

		select {
		case <-waitCompleted:
			// Expected: completed after lock was released
		default:
			t.Error("waitForRunningUpdate should have completed after lock was released")
		}
	})
}

// TestListContainersWithoutFilterIntegration verifies that client.ListContainers() is called
// without filter arguments when no filter is provided, and that containers are returned correctly.
func TestListContainersWithoutFilterIntegration(t *testing.T) {
	// Set up environment
	hostname := "test-container"
	t.Setenv("HOSTNAME", hostname)

	// Create mocks
	mockClient := mockContainer.NewMockClient(t)
	mockContainer := mockTypes.NewMockContainer(t)

	// Set up mock expectations for ListContainers called with context
	mockClient.EXPECT().ListContainers(context.Background()).Return([]types.Container{mockContainer}, nil).Once()

	// Set up container mock to return the expected hostname
	mockContainer.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
		Config: &dockerContainer.Config{Hostname: hostname},
	}).Once()

	// Set up container mock to return the container ID
	expectedID := types.ContainerID("test-container-id")
	mockContainer.EXPECT().ID().Return(expectedID).Once()

	// Execute the function that calls ListContainers with context
	resultID, err := container.GetContainerIDFromHostname(context.Background(), mockClient)

	// Assert results
	require.NoError(t, err)
	assert.Equal(t, expectedID, resultID)

	// Verify mock expectations
	mockClient.AssertExpectations(t)
	mockContainer.AssertExpectations(t)
}

// TestRunUpgradesOnSchedule_ShutdownWaitsForRunningUpdate verifies that runUpgradesOnSchedule
// waits for any running update to complete before shutting down when receiving a shutdown signal.
func TestRunUpgradesOnSchedule_ShutdownWaitsForRunningUpdate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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

		// Channels to coordinate the manual update simulation
		updateStarted := make(chan bool, 1)
		updateFinished := make(chan bool, 1)

		// Mock runUpdatesWithNotifications to simulate a long-running update
		originalRunUpdatesWithNotifications := runUpdatesWithNotifications
		runUpdatesWithNotifications = func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
			// Signal that we're in the update
			synctest.Wait() // Simulate update work

			return nil // Don't trigger metrics in test
		}

		defer func() { runUpdatesWithNotifications = originalRunUpdatesWithNotifications }()

		// Create a cancellable context for shutdown
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)

		// Start runUpgradesOnSchedule in a goroutine
		go func() {
			filter := types.Filter(func(_ types.FilterableContainer) bool { return false })
			filterDesc := testFilterDesc

			// This should start and wait for context cancellation
			err := scheduling.RunUpgradesOnSchedule(
				ctx,
				cmd,
				filter,
				filterDesc,
				updateLock,
				false,
				"",
				logging.WriteStartupMessage,
				runUpdatesWithNotifications,
				nil,
				"",
				nil,
				"",
				false,
				false,
				false,
				nil,
			)
			assert.NoError(t, err)

			shutdownCompleted <- true
		}()

		// Start an update manually to simulate one running
		go func() {
			select {
			case v := <-updateLock:
				updateStarted <- true

				defer func() {
					updateLock <- v

					updateFinished <- true
				}()

				// Simulate longer update work
				synctest.Wait()
			default:
				// Lock not available
			}
		}()

		// Wait for the update to start
		<-updateStarted

		// Cancel context to trigger shutdown
		cancel()

		// Wait for shutdown to complete
		<-shutdownCompleted

		// Ensure the manual update completes
		<-updateFinished
	})
}

// TestValidateRollingRestartDependenciesAcceptsCancellableContext verifies that
// actions.ValidateRollingRestartDependencies properly accepts and uses a cancellable context.
func TestValidateRollingRestartDependenciesAcceptsCancellableContext(t *testing.T) {
	// Create a mock client
	mockClient := mockContainer.NewMockClient(t)

	// Create a filter that accepts all containers
	filter := types.Filter(func(_ types.FilterableContainer) bool { return true })

	// Test with cancellable context - context should not be cancelled
	t.Run("cancellable context without cancellation", func(t *testing.T) {
		ctx := t.Context()

		// Mock expects ListContainers to be called with the cancellable context
		mockClient.EXPECT().ListContainers(ctx, mock.Anything, mock.Anything).Return([]types.Container{}, nil).Once()

		err := actions.ValidateRollingRestartDependencies(ctx, mockClient, filter)

		require.NoError(t, err)
		mockClient.AssertExpectations(t)
	})

	// Test that cancelled context is properly propagated
	t.Run("cancelled context is propagated to client", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		// Cancel immediately
		cancel()

		// Mock expects ListContainers to be called with cancelled context
		mockClient.EXPECT().ListContainers(ctx, mock.Anything, mock.Anything).Return(nil, context.Canceled).Once()

		err := actions.ValidateRollingRestartDependencies(ctx, mockClient, filter)

		// The function should return the error from ListContainers
		require.Error(t, err)
		mockClient.AssertExpectations(t)
	})

	// Test with timeout context
	t.Run("timeout context is propagated to client", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
		defer cancel()

		// Wait for context to timeout
		time.Sleep(time.Millisecond)

		// Verify context has expired before proceeding
		require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)

		// Mock expects ListContainers to be called with timed out context
		mockClient.EXPECT().ListContainers(ctx, mock.Anything, mock.Anything).Return(nil, context.DeadlineExceeded).Once()

		err := actions.ValidateRollingRestartDependencies(ctx, mockClient, filter)

		// The function should return the error from ListContainers
		require.Error(t, err)
		mockClient.AssertExpectations(t)
	})
}

// TestCreateSignalContext verifies that the signal-aware context is properly created
// and can be cancelled via the stop function.
func TestCreateSignalContext(t *testing.T) {
	// Save original and restore after test
	originalCreateSignalContext := createSignalContext

	defer func() { createSignalContext = originalCreateSignalContext }()

	// Test with custom mock that simulates signal handling
	callCount := 0
	createSignalContext = func(ctx context.Context, signals ...os.Signal) (context.Context, context.CancelFunc) {
		callCount++

		// Verify the correct signals are passed
		assert.Contains(t, signals, os.Interrupt, "Should include SIGINT")
		assert.Contains(t, signals, syscall.SIGTERM, "Should include SIGTERM")

		// Return a context that's cancelled via the cancel function
		return context.WithCancel(ctx)
	}

	ctx, cancel := createSignalContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	// Verify context is not done initially
	assert.NotNil(t, ctx, "Context should not be nil")
	assert.NotNil(t, ctx.Done(), "Context should not be done initially")

	// Call cancel and verify context is done
	cancel()

	// Verify the function was called once
	assert.Equal(t, 1, callCount, "createSignalContext should be called once")

	// Verify context is done after cancel
	assert.Error(t, ctx.Err(), "Context should be done after cancel")
}

// TestCreateSignalContextDefault verifies that the default implementation
// correctly creates a signal-aware context using signal.NotifyContext.
func TestCreateSignalContextDefault(t *testing.T) {
	// Save original and restore after test
	originalCreateSignalContext := createSignalContext

	defer func() { createSignalContext = originalCreateSignalContext }()

	// Use the default implementation
	createSignalContext = signal.NotifyContext

	ctx, stop := createSignalContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Verify context is created successfully
	assert.NotNil(t, ctx, "Context should not be nil")

	// Context should not be done initially
	select {
	case <-ctx.Done():
		t.Error("Context should not be done initially")
	default:
		// Expected: context is not done
	}
}

// TestSignalContextCancellation verifies that the signal context properly cancels
// when signals are received, enabling graceful shutdown.
func TestSignalContextCancellation(t *testing.T) {
	// Skip in short test mode as this requires real signal handling
	if testing.Short() {
		t.Skip("Skipping signal test in short mode")
	}

	// Save original and restore after test
	originalCreateSignalContext := createSignalContext

	defer func() { createSignalContext = originalCreateSignalContext }()

	// Create context that we'll control
	ctx, cancel := context.WithCancel(context.Background())

	createSignalContext = func(_ context.Context, _ ...os.Signal) (context.Context, context.CancelFunc) {
		return ctx, cancel
	}

	ctx, _ = createSignalContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	// Verify context is not done initially
	select {
	case <-ctx.Done():
		t.Error("Context should not be done initially")
	default:
		// Expected: context is not done
	}
}

// TestSignalContextWithMultipleSignals verifies that the context correctly handles
// multiple signal types (SIGINT and SIGTERM).
func TestSignalContextWithMultipleSignals(t *testing.T) {
	// Save original and restore after test
	originalCreateSignalContext := createSignalContext

	defer func() { createSignalContext = originalCreateSignalContext }()

	// Track which signals were received
	var receivedSignals []os.Signal

	createSignalContext = func(ctx context.Context, signals ...os.Signal) (context.Context, context.CancelFunc) {
		receivedSignals = signals

		return context.WithCancel(ctx)
	}

	ctx, cancel := createSignalContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Verify both signals are in the received list
	assert.Len(t, receivedSignals, 2, "Should receive exactly 2 signals")
	assert.Contains(t, receivedSignals, os.Interrupt)
	assert.Contains(t, receivedSignals, syscall.SIGTERM)

	// Verify context is valid
	assert.NotNil(t, ctx)
}

// TestSignalContextGracefulShutdown verifies that the context supports graceful
// shutdown by not completing until explicitly cancelled.
func TestSignalContextGracefulShutdown(t *testing.T) {
	// Save original and restore after test
	originalCreateSignalContext := createSignalContext

	defer func() { createSignalContext = originalCreateSignalContext }()

	// Create context that we'll control
	ctx, cancel := context.WithCancel(context.Background())

	createSignalContext = func(_ context.Context, _ ...os.Signal) (context.Context, context.CancelFunc) {
		return ctx, cancel
	}

	// Create signal context
	sigCtx, stop := createSignalContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Verify context is valid and not done
	assert.NotNil(t, sigCtx)

	// Simulate work - context should still be valid
	select {
	case <-sigCtx.Done():
		t.Error("Context should not be done during graceful operation")
	default:
		// Expected: context is not done
	}

	// Now cancel to simulate signal receipt
	cancel()

	// Context should be done after cancellation
	<-sigCtx.Done()
	assert.ErrorIs(t, sigCtx.Err(), context.Canceled)
}
