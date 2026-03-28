package scheduling_test

import (
	"context"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/docker/docker/api/types/container"
	dockerNat "github.com/docker/go-connections/nat"

	mockActions "github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/internal/scheduling"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/filters"
	"github.com/nicholas-fedor/watchtower/pkg/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// testContainerOption configures optional fields on the InspectResponse for testing.
type testContainerOption func(*dockerContainer.InspectResponse)

// withPortBindings adds host-bound port bindings to the container, making
// HasExposedPorts() return true.
func withPortBindings() testContainerOption {
	return func(ir *dockerContainer.InspectResponse) {
		ir.HostConfig = &dockerContainer.HostConfig{
			PortBindings: map[dockerNat.Port][]dockerNat.PortBinding{
				dockerNat.Port("8080/tcp"): {{HostIP: "0.0.0.0", HostPort: "8080"}},
			},
		}
	}
}

// createTestContainer creates a *container.Container with specified chain label for testing.
func createTestContainer(chain string, opts ...testContainerOption) *container.Container {
	labels := make(map[string]string)
	if chain != "" {
		labels[container.ContainerChainLabel] = chain
	}

	inspectResponse := &dockerContainer.InspectResponse{
		ContainerJSONBase: &dockerContainer.ContainerJSONBase{
			ID: "test-container-id",
		},
		Config: &dockerContainer.Config{
			Hostname: "test-container",
			Image:    "test-image",
			Labels:   labels,
		},
	}

	for _, opt := range opts {
		opt(inspectResponse)
	}

	return container.NewContainer(inspectResponse, nil)
}

func TestWaitForRunningUpdate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		lock := make(chan bool, 1) // lock is taken (no value in channel)

		start := time.Now()
		done := make(chan struct{})

		go func() {
			scheduling.WaitForRunningUpdate(ctx, lock)

			elapsed := time.Since(start)
			// Should have waited for the timeout
			if elapsed < 40*time.Millisecond {
				t.Errorf("expected elapsed >= 40ms, got %v", elapsed)
			}

			close(done)
		}()

		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		synctest.Wait()
		<-done
	})
}

func TestRunUpgradesOnSchedule_EmptySchedule(t *testing.T) {
	cmd := &cobra.Command{}
	client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

	ctx := t.Context()

	cmd.Flags().Bool("update-on-start", false, "")

	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
		return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
	}

	writeStartupMessage := func(*cobra.Command, time.Time, string, string, container.Client, types.Notifier, string, *bool) {}

	// Use timeout to avoid hanging
	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer timeoutCancel()

	err := scheduling.RunUpgradesOnSchedule(
		timeoutCtx,
		cmd,
		filters.NoFilter,
		"test filter",
		nil,   // no lock
		false, // cleanup
		"",    // empty schedule
		writeStartupMessage,
		runUpdatesWithNotifications,
		client,
		"",  // scope
		nil, // no notifier
		"v1.0.0",
		false, // monitorOnly
		false, // updateOnStart
		false, // skipFirstRun
		nil,   // currentWatchtowerContainer
		false, // startupMessageSent
		false, // ephemeralSelfUpdate
	)
	// Should complete without error when context times out (clean cancellation)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestRunUpgradesOnSchedule_StartupMessageSuppressed(t *testing.T) {
	cmd := &cobra.Command{}
	client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

	ctx := t.Context()

	cmd.Flags().Bool("update-on-start", false, "")

	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
		return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
	}

	// Spy closure to detect if writeStartupMessage is called
	startupMessageCalled := false
	writeStartupMessage := func(*cobra.Command, time.Time, string, string, container.Client, types.Notifier, string, *bool) {
		startupMessageCalled = true
	}

	// Use timeout to avoid hanging
	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer timeoutCancel()

	err := scheduling.RunUpgradesOnSchedule(
		timeoutCtx,
		cmd,
		filters.NoFilter,
		"test filter",
		nil,   // no lock
		false, // cleanup
		"",    // empty schedule
		writeStartupMessage,
		runUpdatesWithNotifications,
		client,
		"",  // scope
		nil, // no notifier
		"v1.0.0",
		false, // monitorOnly
		false, // updateOnStart
		false, // skipFirstRun
		nil,   // currentWatchtowerContainer
		true,  // startupMessageSent - suppress the startup message
		false, // ephemeralSelfUpdate
	)
	// Should complete without error when context times out (clean cancellation)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Verify writeStartupMessage was NOT called when startupMessageSent=true
	if startupMessageCalled {
		t.Error("writeStartupMessage should not be called when startupMessageSent is true")
	}
}

func TestRunUpgradesOnSchedule_UpdateOnStart(t *testing.T) {
	cmd := &cobra.Command{}
	client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

	ctx := t.Context()

	cmd.PersistentFlags().Bool("update-on-start", true, "")

	updateCalled := false
	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
		updateCalled = true

		return &metrics.Metric{Scanned: 1, Updated: 1, Failed: 0}
	}

	writeStartupMessage := func(*cobra.Command, time.Time, string, string, container.Client, types.Notifier, string, *bool) {}

	// Use timeout to avoid hanging
	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer timeoutCancel()

	err := scheduling.RunUpgradesOnSchedule(
		timeoutCtx,
		cmd,
		filters.NoFilter,
		"test filter",
		nil,
		false,
		"", // no schedule
		writeStartupMessage,
		runUpdatesWithNotifications,
		client,
		"",
		nil,
		"v1.0.0",
		false, // monitorOnly
		true,  // updateOnStart
		false, // skipFirstRun
		nil,   // currentWatchtowerContainer
		false, // startupMessageSent
		false, // ephemeralSelfUpdate
	)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if !updateCalled {
		t.Error("expected updateCalled to be true")
	}
}

func TestWaitForRunningUpdate_NoUpdateRunning(t *testing.T) {
	ctx := context.Background()

	lock := make(chan bool, 1)
	lock <- true // lock is available

	start := time.Now()

	scheduling.WaitForRunningUpdate(ctx, lock)

	elapsed := time.Since(start)

	if elapsed >= 10*time.Millisecond {
		t.Errorf("expected elapsed < 10ms, got %v", elapsed)
	}
}

func TestRunUpgradesOnSchedule_InvalidCronSpec(t *testing.T) {
	cmd := &cobra.Command{}
	client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

	ctx := t.Context()

	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
		return &metrics.Metric{Scanned: 0, Updated: 0, Failed: 0}
	}

	writeStartupMessage := func(*cobra.Command, time.Time, string, string, container.Client, types.Notifier, string, *bool) {}

	err := scheduling.RunUpgradesOnSchedule(
		ctx,
		cmd,
		filters.NoFilter,
		"test filter",
		nil,
		false,
		"invalid cron spec",
		writeStartupMessage,
		runUpdatesWithNotifications,
		client,
		"",
		nil,
		"v1.0.0",
		false, // monitorOnly
		false, // updateOnStart
		false, // skipFirstRun
		nil,   // currentWatchtowerContainer
		false, // startupMessageSent
		false, // ephemeralSelfUpdate
	)
	if err == nil {
		t.Error("expected error")
	}

	if err != nil && !strings.Contains(err.Error(), "failed to schedule updates") {
		t.Errorf("expected error to contain 'failed to schedule updates', got %v", err)
	}
}

func TestRunUpgradesOnSchedule_ContextCancellation(t *testing.T) {
	cmd := &cobra.Command{}
	client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

	ctx := t.Context()

	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
		return &metrics.Metric{Scanned: 0, Updated: 0, Failed: 0}
	}

	writeStartupMessage := func(*cobra.Command, time.Time, string, string, container.Client, types.Notifier, string, *bool) {}

	// Cancel immediately
	canceledCtx, cancelFunc := context.WithCancel(ctx)
	cancelFunc()

	err := scheduling.RunUpgradesOnSchedule(
		canceledCtx,
		cmd,
		filters.NoFilter,
		"test filter",
		nil,
		false,
		"",
		writeStartupMessage,
		runUpdatesWithNotifications,
		client,
		"",
		nil,
		"v1.0.0",
		false, // monitorOnly
		false, // updateOnStart
		false, // skipFirstRun
		nil,   // currentWatchtowerContainer
		false, // startupMessageSent
		false, // ephemeralSelfUpdate
	)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestRunUpgradesOnSchedule_MonitorOnlyParameter(t *testing.T) {
	tests := []struct {
		name              string
		monitorOnly       bool
		expectMonitorOnly bool
	}{
		{
			name:              "monitorOnly false",
			monitorOnly:       false,
			expectMonitorOnly: false,
		},
		{
			name:              "monitorOnly true",
			monitorOnly:       true,
			expectMonitorOnly: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

			ctx := t.Context()

			var capturedParams types.UpdateParams

			runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, params types.UpdateParams) *metrics.Metric {
				capturedParams = params

				return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
			}

			writeStartupMessage := func(*cobra.Command, time.Time, string, string, container.Client, types.Notifier, string, *bool) {}

			// Use timeout to avoid hanging
			timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 10*time.Millisecond)
			defer timeoutCancel()

			err := scheduling.RunUpgradesOnSchedule(
				timeoutCtx,
				cmd,
				filters.NoFilter,
				"test filter",
				nil,   // no lock
				false, // cleanup
				"",    // empty schedule
				writeStartupMessage,
				runUpdatesWithNotifications,
				client,
				"",  // scope
				nil, // no notifier
				"v1.0.0",
				tt.monitorOnly, // monitorOnly parameter
				true,           // updateOnStart - trigger immediate update
				false,          // skipFirstRun
				nil,            // currentWatchtowerContainer
				false,          // startupMessageSent
				false,          // ephemeralSelfUpdate
			)
			if err != nil {
				t.Errorf("expected no error, got %v", err)
			}

			if capturedParams.MonitorOnly != tt.expectMonitorOnly {
				t.Errorf(
					"expected MonitorOnly=%v, got %v",
					tt.expectMonitorOnly,
					capturedParams.MonitorOnly,
				)
			}
		})
	}
}

// TestShouldExitDueToInvalidRestart verifies the ShouldExitDueToInvalidRestart function
// handles various scenarios correctly.
func TestShouldExitDueToInvalidRestart(t *testing.T) {
	tests := []struct {
		name         string
		container    types.Container
		runOnceFlag  bool
		expectedExit bool
	}{
		{
			name:         "no container",
			container:    nil,
			runOnceFlag:  false,
			expectedExit: false,
		},
		{
			name:         "not a watchtower parent",
			container:    createTestContainer("other-id"),
			runOnceFlag:  false,
			expectedExit: false,
		},
		{
			name:         "is watchtower parent but run-once is true",
			container:    createTestContainer("test-container-id,parent-id"),
			runOnceFlag:  true,
			expectedExit: false,
		},
		{
			name:         "is watchtower parent and run-once is false",
			container:    createTestContainer("test-container-id,parent-id"),
			runOnceFlag:  false,
			expectedExit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create flag set
			flagSet := &pflag.FlagSet{}
			flagSet.Bool("run-once", tt.runOnceFlag, "")

			// Test the function
			shouldExit := scheduling.ShouldExitDueToInvalidRestart(
				tt.container,
				flagSet,
			)

			assert.Equal(t, tt.expectedExit, shouldExit)
		})
	}
}

func TestRunUpgradesOnSchedule_CronWithSeconds(t *testing.T) {
	cmd := &cobra.Command{}
	client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

	ctx := t.Context()

	updateCallCount := 0
	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
		updateCallCount++

		return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
	}

	writeStartupMessage := func(*cobra.Command, time.Time, string, string, container.Client, types.Notifier, string, *bool) {}

	// Use a 6-field cron spec that includes seconds (every 2 seconds)
	scheduleSpec := "*/2 * * * * *"

	// Use timeout to avoid hanging - should execute at least once within 5 seconds
	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 5*time.Second)
	defer timeoutCancel()

	err := scheduling.RunUpgradesOnSchedule(
		timeoutCtx,
		cmd,
		filters.NoFilter,
		"test filter",
		nil,   // no lock
		false, // cleanup
		scheduleSpec,
		writeStartupMessage,
		runUpdatesWithNotifications,
		client,
		"",  // scope
		nil, // no notifier
		"v1.0.0",
		false, // monitorOnly
		false, // updateOnStart
		false, // skipFirstRun
		nil,   // currentWatchtowerContainer
		false, // startupMessageSent
		false, // ephemeralSelfUpdate
	)
	// Should complete without error when context times out (clean cancellation)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Should have executed at least once (depending on timing)
	if updateCallCount == 0 {
		t.Error("expected at least one update call")
	}
}

func TestRunUpgradesOnSchedule_SkipFirstRun_True(t *testing.T) {
	cmd := &cobra.Command{}
	client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

	ctx := t.Context()

	var capturedParams []types.UpdateParams

	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, params types.UpdateParams) *metrics.Metric {
		capturedParams = append(capturedParams, params)

		return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
	}

	writeStartupMessage := func(*cobra.Command, time.Time, string, string, container.Client, types.Notifier, string, *bool) {}

	// Use a cron spec that runs every second
	scheduleSpec := "* * * * * *"

	// Use timeout to allow multiple executions
	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 3500*time.Millisecond)
	defer timeoutCancel()

	err := scheduling.RunUpgradesOnSchedule(
		timeoutCtx,
		cmd,
		filters.NoFilter,
		"test filter",
		nil,   // no lock
		false, // cleanup
		scheduleSpec,
		writeStartupMessage,
		runUpdatesWithNotifications,
		client,
		"",  // scope
		nil, // no notifier
		"v1.0.0",
		false, // monitorOnly
		false, // updateOnStart
		true,  // skipFirstRun - should skip Watchtower self-update on first run
		nil,   // currentWatchtowerContainer
		false, // startupMessageSent
		false, // ephemeralSelfUpdate
	)
	// Should complete without error when context times out (clean cancellation)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Should have executed at least twice (first run skips self-update, second doesn't)
	if len(capturedParams) < 2 {
		t.Fatalf("expected at least 2 update calls, got %d", len(capturedParams))
	}

	// First run should have SkipSelfUpdate=true
	if !capturedParams[0].SkipSelfUpdate {
		t.Error("expected first run to skip Watchtower self-update")
	}

	// Second run should have SkipSelfUpdate=false
	if capturedParams[1].SkipSelfUpdate {
		t.Error("expected second run to not skip Watchtower self-update")
	}
}

func TestRunUpgradesOnSchedule_WatchtowerParent_Skipping(t *testing.T) {
	cmd := &cobra.Command{}
	client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

	ctx := t.Context()

	updateCallCount := 0
	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
		updateCallCount++

		return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
	}

	writeStartupMessage := func(*cobra.Command, time.Time, string, string, container.Client, types.Notifier, string, *bool) {}

	// Create a mock Watchtower parent container
	parentContainer := createTestContainer("test-container-id,parent-id")

	// Use a cron spec that runs every second
	scheduleSpec := "* * * * * *"

	// Use timeout to allow multiple potential executions
	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer timeoutCancel()

	err := scheduling.RunUpgradesOnSchedule(
		timeoutCtx,
		cmd,
		filters.NoFilter,
		"test filter",
		nil,   // no lock
		false, // cleanup
		scheduleSpec,
		writeStartupMessage,
		runUpdatesWithNotifications,
		client,
		"",  // scope
		nil, // no notifier
		"v1.0.0",
		false,           // monitorOnly
		false,           // updateOnStart
		false,           // skipFirstRun
		parentContainer, // currentWatchtowerContainer - should skip updates
		false,           // startupMessageSent
		false,           // ephemeralSelfUpdate
	)
	// Should complete without error when context times out (clean cancellation)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Should not have executed any updates (parent container skips scheduled updates)
	if updateCallCount > 0 {
		t.Errorf(
			"expected no update calls for Watchtower parent container, got %d",
			updateCallCount,
		)
	}
}

func TestRunUpgradesOnSchedule_ScheduledRuns_Execution(t *testing.T) {
	cmd := &cobra.Command{}
	client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

	ctx := t.Context()

	var executionTimes []time.Time

	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
		executionTimes = append(executionTimes, time.Now())

		return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
	}

	writeStartupMessage := func(*cobra.Command, time.Time, string, string, container.Client, types.Notifier, string, *bool) {}

	// Use a cron spec that runs every 1 second
	scheduleSpec := "*/1 * * * * *"

	startTime := time.Now()

	// Use timeout to allow multiple executions
	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer timeoutCancel()

	err := scheduling.RunUpgradesOnSchedule(
		timeoutCtx,
		cmd,
		filters.NoFilter,
		"test filter",
		nil,   // no lock
		false, // cleanup
		scheduleSpec,
		writeStartupMessage,
		runUpdatesWithNotifications,
		client,
		"",  // scope
		nil, // no notifier
		"v1.0.0",
		false, // monitorOnly
		false, // updateOnStart
		false, // skipFirstRun
		nil,   // currentWatchtowerContainer
		false, // startupMessageSent
		false, // ephemeralSelfUpdate
	)
	// Should complete without error when context times out (clean cancellation)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Should have executed multiple times
	if len(executionTimes) < 2 {
		t.Fatalf("expected at least 2 executions, got %d", len(executionTimes))
	}

	// Verify intervals are approximately correct (within 200ms tolerance for 1-second cron)
	for i := 1; i < len(executionTimes); i++ {
		interval := executionTimes[i].Sub(executionTimes[i-1])
		if interval < 800*time.Millisecond || interval > 1200*time.Millisecond {
			t.Errorf("execution interval %v is not within expected range [800ms, 1200ms]", interval)
		}
	}

	// Verify executions happened after start time
	for _, execTime := range executionTimes {
		if execTime.Before(startTime) {
			t.Errorf("execution time %v is before start time %v", execTime, startTime)
		}
	}
}

// TestRunUpgradesOnSchedule_EphemeralSelfUpdateWithExposedPorts verifies that when
// ephemeralSelfUpdate=true, the port-conflict guard is bypassed even when the
// Watchtower container has exposed ports. This allows ephemeral self-updates to
// proceed because they remove the old container before creating the new one,
// avoiding port conflicts.
func TestRunUpgradesOnSchedule_EphemeralSelfUpdateWithExposedPorts(t *testing.T) {
	cmd := &cobra.Command{}
	client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

	ctx := t.Context()

	var capturedParams types.UpdateParams

	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, params types.UpdateParams) *metrics.Metric {
		capturedParams = params

		return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
	}

	writeStartupMessage := func(*cobra.Command, time.Time, string, string, container.Client, types.Notifier, string, *bool) {}

	cmd.PersistentFlags().Bool("update-on-start", true, "")

	// Create a Watchtower container with exposed ports (host-bound port mappings).
	containerWithPorts := createTestContainer("", withPortBindings())

	require.True(t, containerWithPorts.HasExposedPorts(),
		"test container should have exposed ports",
	)

	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer timeoutCancel()

	err := scheduling.RunUpgradesOnSchedule(
		timeoutCtx,
		cmd,
		filters.NoFilter,
		"test filter",
		nil,   // lock (auto-created)
		false, // cleanup
		"",    // empty schedule
		writeStartupMessage,
		runUpdatesWithNotifications,
		client,
		"",  // scope
		nil, // no notifier
		"v1.0.0",
		false,              // monitorOnly
		true,               // updateOnStart - triggers immediate update
		false,              // skipFirstRun
		containerWithPorts, // currentWatchtowerContainer with exposed ports
		false,              // startupMessageSent
		true,               // ephemeralSelfUpdate - bypasses port-conflict guard
	)
	require.NoError(t, err)

	// With ephemeralSelfUpdate=true, the port-conflict guard should be bypassed,
	// so SkipSelfUpdate should remain false (self-update is allowed).
	assert.False(t, capturedParams.SkipSelfUpdate,
		"self-update should NOT be skipped when ephemeralSelfUpdate=true, "+
			"even with exposed ports",
	)
}

// TestRunUpgradesOnSchedule_PortConflictGuard_SkipsSelfUpdate verifies that when
// the Watchtower container has exposed ports and ephemeralSelfUpdate=false, the
// port-conflict guard forces SkipSelfUpdate to true to prevent the old container
// from holding the port while the new container tries to bind it.
func TestRunUpgradesOnSchedule_PortConflictGuard_SkipsSelfUpdate(t *testing.T) {
	cmd := &cobra.Command{}
	client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

	ctx := t.Context()

	var capturedParams types.UpdateParams

	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, params types.UpdateParams) *metrics.Metric {
		capturedParams = params

		return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
	}

	writeStartupMessage := func(*cobra.Command, time.Time, string, string, container.Client, types.Notifier, string, *bool) {}

	cmd.PersistentFlags().Bool("update-on-start", true, "")

	// Create a Watchtower container with exposed ports (host-bound port mappings).
	containerWithPorts := createTestContainer("", withPortBindings())

	require.True(t, containerWithPorts.HasExposedPorts(),
		"test container should have exposed ports",
	)

	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer timeoutCancel()

	err := scheduling.RunUpgradesOnSchedule(
		timeoutCtx,
		cmd,
		filters.NoFilter,
		"test filter",
		nil,   // lock (auto-created)
		false, // cleanup
		"",    // empty schedule
		writeStartupMessage,
		runUpdatesWithNotifications,
		client,
		"",  // scope
		nil, // no notifier
		"v1.0.0",
		false,              // monitorOnly
		true,               // updateOnStart - triggers immediate update
		false,              // skipFirstRun
		containerWithPorts, // currentWatchtowerContainer with exposed ports
		false,              // startupMessageSent
		false,              // ephemeralSelfUpdate - port-conflict guard is active
	)
	require.NoError(t, err)

	// With ephemeralSelfUpdate=false and exposed ports, the port-conflict guard
	// should force SkipSelfUpdate to true to prevent port conflicts.
	assert.True(t, capturedParams.SkipSelfUpdate,
		"self-update should be skipped when container has exposed ports "+
			"and ephemeralSelfUpdate=false",
	)
}

// TestRunUpgradesOnSchedule_NoExposedPorts_AllowsSelfUpdate verifies that when
// the Watchtower container has no exposed ports, the port-conflict guard does
// not interfere regardless of the ephemeralSelfUpdate setting.
func TestRunUpgradesOnSchedule_NoExposedPorts_AllowsSelfUpdate(t *testing.T) {
	cmd := &cobra.Command{}
	client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

	ctx := t.Context()

	var capturedParams types.UpdateParams

	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, params types.UpdateParams) *metrics.Metric {
		capturedParams = params

		return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
	}

	writeStartupMessage := func(*cobra.Command, time.Time, string, string, container.Client, types.Notifier, string, *bool) {}

	cmd.PersistentFlags().Bool("update-on-start", true, "")

	// Create a Watchtower container WITHOUT exposed ports.
	containerNoPorts := createTestContainer("")

	require.False(t, containerNoPorts.HasExposedPorts(),
		"test container should not have exposed ports",
	)

	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer timeoutCancel()

	err := scheduling.RunUpgradesOnSchedule(
		timeoutCtx,
		cmd,
		filters.NoFilter,
		"test filter",
		nil,   // lock (auto-created)
		false, // cleanup
		"",    // empty schedule
		writeStartupMessage,
		runUpdatesWithNotifications,
		client,
		"",  // scope
		nil, // no notifier
		"v1.0.0",
		false,            // monitorOnly
		true,             // updateOnStart - triggers immediate update
		false,            // skipFirstRun
		containerNoPorts, // currentWatchtowerContainer without exposed ports
		false,            // startupMessageSent
		false,            // ephemeralSelfUpdate
	)
	require.NoError(t, err)

	// Without exposed ports, the port-conflict guard should not trigger,
	// so SkipSelfUpdate should remain false.
	assert.False(t, capturedParams.SkipSelfUpdate,
		"self-update should NOT be skipped when container has no exposed ports",
	)
}
