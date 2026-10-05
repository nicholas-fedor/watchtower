package scheduling_test

import (
	"context"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"
	dockerNetwork "github.com/moby/moby/api/types/network"

	mockActions "github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/internal/logging"
	"github.com/nicholas-fedor/watchtower/internal/metrics"
	"github.com/nicholas-fedor/watchtower/internal/scheduling"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/filters"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// testLogger returns a discarded zerolog logger for tests that do not assert on logs.
func testLogger() *zerolog.Logger {
	nop := zerolog.Nop()

	return &nop
}

// testContainerOption configures optional fields on the InspectResponse for testing.
type testContainerOption func(*dockerContainer.InspectResponse)

// withName sets the container runtime name (stored in InspectResponse.Name).
func withName(name string) testContainerOption {
	return func(ir *dockerContainer.InspectResponse) {
		ir.Name = name
	}
}

// withLabel sets an arbitrary container label on the test container config.
func withLabel(key, value string) testContainerOption {
	return func(ir *dockerContainer.InspectResponse) {
		if ir.Config.Labels == nil {
			ir.Config.Labels = make(map[string]string)
		}

		ir.Config.Labels[key] = value
	}
}

// withPortBindings adds host-bound port bindings to the container, making
// HasExposedPorts() return true.
func withPortBindings() testContainerOption {
	return func(ir *dockerContainer.InspectResponse) {
		ir.HostConfig = &dockerContainer.HostConfig{
			PortBindings: dockerNetwork.PortMap{
				dockerNetwork.MustParsePort("8080/tcp"): {
					{
						HostIP:   netip.MustParseAddr("0.0.0.0"),
						HostPort: dockerNetwork.MustParsePort("8080/tcp").Port(),
					},
				},
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
		ID: "test-container-id",
		Config: &dockerContainer.Config{
			Hostname: "test-container",
			Image:    "test-image",
			Labels:   labels,
		},
	}

	for _, opt := range opts {
		opt(inspectResponse)
	}

	return container.NewContainer(nil, inspectResponse, nil)
}

// testDeps returns ScheduleDeps with common test defaults.
// Callers override ScheduleSpec, UpdateOnStart, SkipFirstRun, containers, and BaseParams as needed.
func testDeps(
	client container.Client,
	runUpdate func(context.Context, types.Filter, types.UpdateParams) *metrics.Metric,
	writeStartup func(logging.StartupParams),
) scheduling.ScheduleDeps {
	return scheduling.ScheduleDeps{
		Logger:              testLogger(),
		Filter:              filters.NoFilter,
		FilterDesc:          "test filter",
		WriteStartupMessage: writeStartup,
		RunUpdate:           runUpdate,
		Client:              client,
		MetaVersion:         "v1.0.0",
		BaseParams:          types.UpdateParams{},
	}
}

func TestWaitForRunningUpdate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		lock := make(chan bool, 1) // lock is taken (no value in channel)

		start := time.Now()
		done := make(chan struct{})

		go func() {
			scheduling.WaitForRunningUpdate(testLogger(), ctx, lock)

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
	client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

	ctx := t.Context()

	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
		return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
	}

	writeStartupMessage := func(logging.StartupParams) {}

	// Use timeout to avoid hanging
	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer timeoutCancel()

	deps := testDeps(client, runUpdatesWithNotifications, writeStartupMessage)
	err := scheduling.RunUpgradesOnSchedule(timeoutCtx, deps)
	// Should complete without error when context times out (clean cancellation)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestRunUpgradesOnSchedule_StartupMessageSuppressed(t *testing.T) {
	client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

	ctx := t.Context()

	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
		return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
	}

	// Spy closure to detect if writeStartupMessage is called
	startupMessageCalled := false
	writeStartupMessage := func(logging.StartupParams) {
		startupMessageCalled = true
	}

	// Use timeout to avoid hanging
	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer timeoutCancel()

	deps := testDeps(client, runUpdatesWithNotifications, writeStartupMessage)
	deps.StartupMessageSent = true
	err := scheduling.RunUpgradesOnSchedule(timeoutCtx, deps)
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
	client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

	ctx := t.Context()

	updateCalled := false
	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
		updateCalled = true

		return &metrics.Metric{Scanned: 1, Updated: 1, Failed: 0}
	}

	writeStartupMessage := func(logging.StartupParams) {}

	// Use timeout to avoid hanging
	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer timeoutCancel()

	deps := testDeps(client, runUpdatesWithNotifications, writeStartupMessage)
	deps.UpdateOnStart = true

	err := scheduling.RunUpgradesOnSchedule(timeoutCtx, deps)
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

	scheduling.WaitForRunningUpdate(testLogger(), ctx, lock)

	elapsed := time.Since(start)

	if elapsed >= 10*time.Millisecond {
		t.Errorf("expected elapsed < 10ms, got %v", elapsed)
	}
}

func TestRunUpgradesOnSchedule_InvalidCronSpec(t *testing.T) {
	client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

	ctx := t.Context()

	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
		return &metrics.Metric{Scanned: 0, Updated: 0, Failed: 0}
	}

	writeStartupMessage := func(logging.StartupParams) {}

	deps := testDeps(client, runUpdatesWithNotifications, writeStartupMessage)
	deps.ScheduleSpec = "invalid cron spec"

	err := scheduling.RunUpgradesOnSchedule(ctx, deps)
	if err == nil {
		t.Error("expected error")
	}

	if err != nil && !strings.Contains(err.Error(), "failed to schedule updates") {
		t.Errorf("expected error to contain 'failed to schedule updates', got %v", err)
	}
}

func TestRunUpgradesOnSchedule_QuotedScheduleSpec(t *testing.T) {
	tests := []struct {
		name         string
		scheduleSpec string
		expectError  bool
	}{
		{
			name:         "double-quoted cron spec from Docker Compose",
			scheduleSpec: `"0 0 2 * * *"`,
			expectError:  false,
		},
		{
			name:         "single-quoted cron spec",
			scheduleSpec: `'0 0 2 * * *'`,
			expectError:  false,
		},
		{
			name:         "double-quoted descriptor",
			scheduleSpec: `"@hourly"`,
			expectError:  false,
		},
		{
			name:         "unquoted cron spec",
			scheduleSpec: `0 0 2 * * *`,
			expectError:  false,
		},
		{
			name:         "unquoted descriptor",
			scheduleSpec: `@hourly`,
			expectError:  false,
		},
		{
			name:         "double-quoted invalid spec still errors",
			scheduleSpec: `"invalid cron spec"`,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

			ctx := t.Context()

			runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
				return &metrics.Metric{Scanned: 0, Updated: 0, Failed: 0}
			}

			writeStartupMessage := func(logging.StartupParams) {}

			timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 10*time.Millisecond)
			defer timeoutCancel()

			deps := testDeps(client, runUpdatesWithNotifications, writeStartupMessage)
			deps.ScheduleSpec = tt.scheduleSpec
			deps.SkipFirstRun = true
			err := scheduling.RunUpgradesOnSchedule(timeoutCtx, deps)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			}
		})
	}
}

func TestRunUpgradesOnSchedule_ContextCancellation(t *testing.T) {
	client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

	ctx := t.Context()

	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
		return &metrics.Metric{Scanned: 0, Updated: 0, Failed: 0}
	}

	writeStartupMessage := func(logging.StartupParams) {}

	// Cancel immediately
	canceledCtx, cancelFunc := context.WithCancel(ctx)
	cancelFunc()

	deps := testDeps(client, runUpdatesWithNotifications, writeStartupMessage)

	err := scheduling.RunUpgradesOnSchedule(canceledCtx, deps)
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
			client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

			ctx := t.Context()

			var capturedParams types.UpdateParams

			runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, params types.UpdateParams) *metrics.Metric {
				capturedParams = params

				return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
			}

			writeStartupMessage := func(logging.StartupParams) {}

			// Use timeout to avoid hanging
			timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 10*time.Millisecond)
			defer timeoutCancel()

			deps := testDeps(client, runUpdatesWithNotifications, writeStartupMessage)
			deps.UpdateOnStart = true
			deps.BaseParams.MonitorOnly = tt.monitorOnly

			err := scheduling.RunUpgradesOnSchedule(timeoutCtx, deps)
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
		{
			name:         "old container with run-once false",
			container:    createTestContainer("", withName("watchtower-old-abc123")),
			runOnceFlag:  false,
			expectedExit: true,
		},
		{
			name:         "old container with run-once true",
			container:    createTestContainer("", withName("watchtower-old-abc123")),
			runOnceFlag:  true,
			expectedExit: false,
		},
		{
			name:         "old container with leading slash",
			container:    createTestContainer("", withName("/watchtower-old-def456")),
			runOnceFlag:  false,
			expectedExit: true,
		},
		{
			name:         "non-old container continues",
			container:    createTestContainer("", withName("watchtower")),
			runOnceFlag:  false,
			expectedExit: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shouldExit := scheduling.ShouldExitDueToInvalidRestart(
				tt.container,
				tt.runOnceFlag,
			)

			assert.Equal(t, tt.expectedExit, shouldExit)
		})
	}
}

func TestRunUpgradesOnSchedule_CronWithSeconds(t *testing.T) {
	client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

	ctx := t.Context()

	updateCallCount := 0
	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
		updateCallCount++

		return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
	}

	writeStartupMessage := func(logging.StartupParams) {}

	// Use a 6-field cron spec that includes seconds (every 2 seconds)
	scheduleSpec := "*/2 * * * * *"

	// Use timeout to avoid hanging - should execute at least once within 5 seconds
	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 5*time.Second)
	defer timeoutCancel()

	deps := testDeps(client, runUpdatesWithNotifications, writeStartupMessage)
	deps.ScheduleSpec = scheduleSpec
	err := scheduling.RunUpgradesOnSchedule(timeoutCtx, deps)
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
	client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

	ctx := t.Context()

	var capturedParams []types.UpdateParams

	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, params types.UpdateParams) *metrics.Metric {
		capturedParams = append(capturedParams, params)

		return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
	}

	writeStartupMessage := func(logging.StartupParams) {}

	// Use a cron spec that runs every second
	scheduleSpec := "* * * * * *"

	// Use timeout to allow multiple executions
	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 3500*time.Millisecond)
	defer timeoutCancel()

	deps := testDeps(client, runUpdatesWithNotifications, writeStartupMessage)
	deps.ScheduleSpec = scheduleSpec
	deps.SkipFirstRun = true
	err := scheduling.RunUpgradesOnSchedule(timeoutCtx, deps)
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
	client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

	ctx := t.Context()

	updateCallCount := 0
	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
		updateCallCount++

		return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
	}

	writeStartupMessage := func(logging.StartupParams) {}

	// Create a mock Watchtower parent container
	parentContainer := createTestContainer("test-container-id,parent-id")

	// Use a cron spec that runs every second
	scheduleSpec := "* * * * * *"

	// Use timeout to allow multiple potential executions
	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer timeoutCancel()

	deps := testDeps(client, runUpdatesWithNotifications, writeStartupMessage)
	deps.ScheduleSpec = scheduleSpec
	deps.CurrentWatchtowerContainer = parentContainer
	err := scheduling.RunUpgradesOnSchedule(timeoutCtx, deps)
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
	client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

	ctx := t.Context()

	var executionTimes []time.Time

	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
		executionTimes = append(executionTimes, time.Now())

		return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
	}

	writeStartupMessage := func(logging.StartupParams) {}

	// Use a cron spec that runs every 1 second
	scheduleSpec := "*/1 * * * * *"

	startTime := time.Now()

	// Use timeout to allow multiple executions
	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer timeoutCancel()

	deps := testDeps(client, runUpdatesWithNotifications, writeStartupMessage)
	deps.ScheduleSpec = scheduleSpec
	err := scheduling.RunUpgradesOnSchedule(timeoutCtx, deps)
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

// filterOutcome records how a captured update filter treated a labeled and an
// unlabeled container, used to assert per-container schedule selection.
type filterOutcome struct {
	labeled   bool
	unlabeled bool
}

// recordOutcomes returns a RunUpdate hook that evaluates the received filter
// against both containers and stores the result of each invocation.
func recordOutcomes(
	labeled, unlabeled types.Container,
	outcomes *[]filterOutcome,
	mu *sync.Mutex,
) func(context.Context, types.Filter, types.UpdateParams) *metrics.Metric {
	return func(_ context.Context, f types.Filter, _ types.UpdateParams) *metrics.Metric {
		mu.Lock()
		defer mu.Unlock()

		*outcomes = append(*outcomes, filterOutcome{f(labeled), f(unlabeled)})

		return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
	}
}

// TestRunUpgradesOnSchedule_PerContainerScheduleOverride verifies that a
// container carrying the schedule label runs on its own cron schedule and that
// the override run selects only matching containers.
func TestRunUpgradesOnSchedule_PerContainerScheduleOverride(t *testing.T) {
	labeled := createTestContainer(
		"",
		withName("labeled-app"),
		withLabel(container.ScheduleLabel, "* * * * * *"),
	)
	unlabeled := createTestContainer("", withName("plain-app"))

	client := mockActions.CreateMockClient(
		&mockActions.TestData{Containers: []types.Container{labeled, unlabeled}},
		false,
		false,
	)

	ctx := t.Context()

	var (
		mu       sync.Mutex
		outcomes []filterOutcome
	)

	writeStartupMessage := func(logging.StartupParams) {}

	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer timeoutCancel()

	deps := testDeps(client, recordOutcomes(labeled, unlabeled, &outcomes, &mu), writeStartupMessage)
	// The default schedule must not fire during the test window.
	deps.ScheduleSpec = "@every 1h"

	err := scheduling.RunUpgradesOnSchedule(timeoutCtx, deps)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(outcomes) == 0 {
		t.Fatal("expected at least one update call for the schedule override")
	}

	for _, o := range outcomes {
		if !o.labeled {
			t.Error("expected override run to include the labeled container")
		}

		if o.unlabeled {
			t.Error("expected override run to exclude the unlabeled container")
		}
	}
}

// TestRunUpgradesOnSchedule_ScheduleLabelExcludedFromDefault verifies that
// containers with a schedule label are skipped by the default schedule once
// their override has been registered.
func TestRunUpgradesOnSchedule_ScheduleLabelExcludedFromDefault(t *testing.T) {
	labeled := createTestContainer(
		"",
		withName("labeled-app"),
		withLabel(container.ScheduleLabel, "@every 1h"),
	)
	unlabeled := createTestContainer("", withName("plain-app"))

	client := mockActions.CreateMockClient(
		&mockActions.TestData{Containers: []types.Container{labeled, unlabeled}},
		false,
		false,
	)

	ctx := t.Context()

	var (
		mu       sync.Mutex
		outcomes []filterOutcome
	)

	writeStartupMessage := func(logging.StartupParams) {}

	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer timeoutCancel()

	deps := testDeps(client, recordOutcomes(labeled, unlabeled, &outcomes, &mu), writeStartupMessage)
	// The default schedule fires every second; the override never does.
	deps.ScheduleSpec = "* * * * * *"

	err := scheduling.RunUpgradesOnSchedule(timeoutCtx, deps)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(outcomes) == 0 {
		t.Fatal("expected at least one default update call")
	}

	for _, o := range outcomes {
		if o.labeled {
			t.Error("expected default run to exclude the container with a schedule override")
		}

		if !o.unlabeled {
			t.Error("expected default run to include the unlabeled container")
		}
	}
}

// TestRunUpgradesOnSchedule_InvalidScheduleLabel verifies that a container with
// an unparseable schedule label keeps following the default schedule.
func TestRunUpgradesOnSchedule_InvalidScheduleLabel(t *testing.T) {
	labeled := createTestContainer(
		"",
		withName("bad-label-app"),
		withLabel(container.ScheduleLabel, "not a cron"),
	)
	unlabeled := createTestContainer("", withName("plain-app"))

	client := mockActions.CreateMockClient(
		&mockActions.TestData{Containers: []types.Container{labeled, unlabeled}},
		false,
		false,
	)

	ctx := t.Context()

	var (
		mu       sync.Mutex
		outcomes []filterOutcome
	)

	writeStartupMessage := func(logging.StartupParams) {}

	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer timeoutCancel()

	deps := testDeps(client, recordOutcomes(labeled, unlabeled, &outcomes, &mu), writeStartupMessage)
	deps.ScheduleSpec = "* * * * * *"

	err := scheduling.RunUpgradesOnSchedule(timeoutCtx, deps)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(outcomes) == 0 {
		t.Fatal("expected at least one default update call")
	}

	for _, o := range outcomes {
		if !o.labeled {
			t.Error("expected default run to include the container with an invalid schedule label")
		}

		if !o.unlabeled {
			t.Error("expected default run to include the unlabeled container")
		}
	}
}

// TestRunUpgradesOnSchedule_SkipFirstRun_AppliesToOverrideJob verifies that the
// first-run self-update skip applies to whichever scheduled job fires first:
// a per-container override firing before the default schedule must still skip
// the Watchtower self-update.
func TestRunUpgradesOnSchedule_SkipFirstRun_AppliesToOverrideJob(t *testing.T) {
	labeled := createTestContainer(
		"",
		withName("labeled-app"),
		withLabel(container.ScheduleLabel, "* * * * * *"),
	)

	client := mockActions.CreateMockClient(
		&mockActions.TestData{Containers: []types.Container{labeled}},
		false,
		false,
	)

	ctx := t.Context()

	var capturedParams []types.UpdateParams

	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, params types.UpdateParams) *metrics.Metric {
		capturedParams = append(capturedParams, params)

		return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
	}

	writeStartupMessage := func(logging.StartupParams) {}

	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer timeoutCancel()

	deps := testDeps(client, runUpdatesWithNotifications, writeStartupMessage)
	// The default schedule never fires inside the test window; only the
	// per-container override job runs.
	deps.ScheduleSpec = "@every 1h"
	deps.SkipFirstRun = true

	err := scheduling.RunUpgradesOnSchedule(timeoutCtx, deps)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if len(capturedParams) < 2 {
		t.Fatalf("expected at least 2 update calls, got %d", len(capturedParams))
	}

	if !capturedParams[0].SkipSelfUpdate {
		t.Error("expected first run (override job) to skip Watchtower self-update")
	}

	if capturedParams[1].SkipSelfUpdate {
		t.Error("expected second run to not skip Watchtower self-update")
	}
}

// TestRunUpgradesOnSchedule_ScheduleLabelDiscoveredAtTick verifies that
// discovery runs inside each update run: a container that gains a schedule
// label after startup is excluded from the default schedule on the very next
// run, without waiting for the periodic rescan.
func TestRunUpgradesOnSchedule_ScheduleLabelDiscoveredAtTick(t *testing.T) {
	labeled := createTestContainer(
		"",
		withName("labeled-app"),
		withLabel(container.ScheduleLabel, "@every 1h"),
	)
	unlabeled := createTestContainer("", withName("plain-app"))

	// Only the unlabeled container exists at startup.
	data := &mockActions.TestData{Containers: []types.Container{unlabeled}}
	client := mockActions.CreateMockClient(data, false, false)

	ctx := t.Context()

	var (
		mu       sync.Mutex
		outcomes []filterOutcome
	)

	hook := recordOutcomes(labeled, unlabeled, &outcomes, &mu)
	swapped := false

	// The labeled container appears after the first update run completes; the
	// next default run must already exclude it.
	runUpdatesWithNotifications := func(ctx context.Context, f types.Filter, p types.UpdateParams) *metrics.Metric {
		m := hook(ctx, f, p)

		mu.Lock()
		defer mu.Unlock()

		if !swapped {
			swapped = true
			data.Containers = []types.Container{unlabeled, labeled}
		}

		return m
	}

	writeStartupMessage := func(logging.StartupParams) {}

	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer timeoutCancel()

	deps := testDeps(client, runUpdatesWithNotifications, writeStartupMessage)
	// The default schedule fires every second; the override never does.
	deps.ScheduleSpec = "* * * * * *"

	err := scheduling.RunUpgradesOnSchedule(timeoutCtx, deps)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(outcomes) < 2 {
		t.Fatalf("expected at least 2 update calls, got %d", len(outcomes))
	}

	// The first run predates discovery: the not-yet-registered spec cannot
	// exclude the container yet.
	if !outcomes[0].labeled {
		t.Error("expected first run to include the labeled container before its spec registered")
	}

	for i, o := range outcomes[1:] {
		if o.labeled {
			t.Errorf("expected run %d to exclude the container once its override registered", i+2)
		}

		if !o.unlabeled {
			t.Errorf("expected run %d to include the unlabeled container", i+2)
		}
	}
}

// TestRunUpgradesOnSchedule_EphemeralSelfUpdateWithExposedPorts verifies that when
// ephemeralSelfUpdate=true, the port-conflict guard is bypassed even when the
// Watchtower container has exposed ports. This allows ephemeral self-updates to
// proceed because they remove the old container before creating the new one,
// avoiding port conflicts.
func TestRunUpgradesOnSchedule_EphemeralSelfUpdateWithExposedPorts(t *testing.T) {
	client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

	ctx := t.Context()

	var capturedParams types.UpdateParams

	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, params types.UpdateParams) *metrics.Metric {
		capturedParams = params

		return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
	}

	writeStartupMessage := func(logging.StartupParams) {}

	// Create a Watchtower container with exposed ports (host-bound port mappings).
	containerWithPorts := createTestContainer("", withPortBindings())

	require.True(t, containerWithPorts.HasExposedPorts(),
		"test container should have exposed ports",
	)

	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer timeoutCancel()

	deps := testDeps(client, runUpdatesWithNotifications, writeStartupMessage)
	deps.UpdateOnStart = true
	deps.CurrentWatchtowerContainer = containerWithPorts
	deps.BaseParams.EphemeralSelfUpdate = true
	err := scheduling.RunUpgradesOnSchedule(timeoutCtx, deps)
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
	client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

	ctx := t.Context()

	var capturedParams types.UpdateParams

	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, params types.UpdateParams) *metrics.Metric {
		capturedParams = params

		return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
	}

	writeStartupMessage := func(logging.StartupParams) {}

	// Create a Watchtower container with exposed ports (host-bound port mappings).
	containerWithPorts := createTestContainer("", withPortBindings())

	require.True(t, containerWithPorts.HasExposedPorts(),
		"test container should have exposed ports",
	)

	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer timeoutCancel()

	deps := testDeps(client, runUpdatesWithNotifications, writeStartupMessage)
	deps.UpdateOnStart = true
	deps.CurrentWatchtowerContainer = containerWithPorts
	err := scheduling.RunUpgradesOnSchedule(timeoutCtx, deps)
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
	tests := []struct {
		name                string
		ephemeralSelfUpdate bool
	}{
		{
			name:                "ephemeralSelfUpdate=true",
			ephemeralSelfUpdate: true,
		},
		{
			name:                "ephemeralSelfUpdate=false",
			ephemeralSelfUpdate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

			ctx := t.Context()

			var capturedParams types.UpdateParams

			runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, params types.UpdateParams) *metrics.Metric {
				capturedParams = params

				return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
			}

			writeStartupMessage := func(logging.StartupParams) {}

			// Create a Watchtower container WITHOUT exposed ports.
			containerNoPorts := createTestContainer("")

			require.False(t, containerNoPorts.HasExposedPorts(),
				"test container should not have exposed ports",
			)

			timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 10*time.Millisecond)
			defer timeoutCancel()

			deps := testDeps(client, runUpdatesWithNotifications, writeStartupMessage)
			deps.UpdateOnStart = true
			deps.CurrentWatchtowerContainer = containerNoPorts
			deps.BaseParams.EphemeralSelfUpdate = tt.ephemeralSelfUpdate
			err := scheduling.RunUpgradesOnSchedule(timeoutCtx, deps)
			require.NoError(t, err)

			// Without exposed ports, the port-conflict guard should not trigger,
			// so SkipSelfUpdate should remain false regardless of ephemeralSelfUpdate.
			assert.False(t, capturedParams.SkipSelfUpdate,
				"self-update should NOT be skipped when container has no exposed ports",
			)
		})
	}
}

// TestRunUpgradesOnSchedule_ReviveStoppedPropagation verifies that RunUpgradesOnSchedule
// passes the ReviveStopped flag through UpdateParams to the update function.
func TestRunUpgradesOnSchedule_ReviveStoppedPropagation(t *testing.T) {
	tests := []struct {
		name           string
		reviveStopped  bool
		expectSelected bool
	}{
		{
			name:           "reviveStopped=true",
			reviveStopped:  true,
			expectSelected: true,
		},
		{
			name:           "reviveStopped=false",
			reviveStopped:  false,
			expectSelected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

			ctx := t.Context()

			var capturedParams types.UpdateParams

			runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, params types.UpdateParams) *metrics.Metric {
				capturedParams = params

				return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
			}

			writeStartupMessage := func(logging.StartupParams) {}

			timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 10*time.Millisecond)
			defer timeoutCancel()

			deps := testDeps(client, runUpdatesWithNotifications, writeStartupMessage)
			deps.UpdateOnStart = true
			deps.BaseParams.ReviveStopped = tt.reviveStopped
			err := scheduling.RunUpgradesOnSchedule(timeoutCtx, deps)
			require.NoError(t, err)

			assert.Equal(t, tt.expectSelected, capturedParams.ReviveStopped,
				"ReviveStopped should be %v in UpdateParams", tt.expectSelected)
		})
	}
}

// TestRunUpgradesOnSchedule_UseComposeDependsOnPropagation verifies that
// RunUpgradesOnSchedule passes UseComposeDependsOn through UpdateParams so
// scheduled cycles honor Compose depends_on the same way as --run-once.
func TestRunUpgradesOnSchedule_UseComposeDependsOnPropagation(t *testing.T) {
	tests := []struct {
		name                string
		useComposeDependsOn bool
		expectSelected      bool
	}{
		{
			name:                "useComposeDependsOn=true",
			useComposeDependsOn: true,
			expectSelected:      true,
		},
		{
			name:                "useComposeDependsOn=false",
			useComposeDependsOn: false,
			expectSelected:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

			ctx := t.Context()

			var capturedParams types.UpdateParams

			runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, params types.UpdateParams) *metrics.Metric {
				capturedParams = params

				return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
			}

			writeStartupMessage := func(logging.StartupParams) {}

			timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 10*time.Millisecond)
			defer timeoutCancel()

			deps := testDeps(client, runUpdatesWithNotifications, writeStartupMessage)
			deps.UpdateOnStart = true
			deps.BaseParams.UseComposeDependsOn = tt.useComposeDependsOn
			err := scheduling.RunUpgradesOnSchedule(timeoutCtx, deps)
			require.NoError(t, err)

			assert.Equal(t, tt.expectSelected, capturedParams.UseComposeDependsOn,
				"UseComposeDependsOn should be %v in UpdateParams", tt.expectSelected)
			assert.False(t, capturedParams.RunOnce,
				"scheduled updates must keep RunOnce=false")
		})
	}
}
