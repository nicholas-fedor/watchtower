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

	dockerContainer "github.com/docker/docker/api/types/container"

	mockActions "github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/internal/scheduling"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/filters"
	"github.com/nicholas-fedor/watchtower/pkg/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// createTestContainer creates a *container.Container with specified chain label for testing.
func createTestContainer(chain string) *container.Container {
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
	)
	// Should complete without error when context times out (clean cancellation)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
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
	cancelledCtx, cancelFunc := context.WithCancel(ctx)
	cancelFunc()

	err := scheduling.RunUpgradesOnSchedule(
		cancelledCtx,
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
