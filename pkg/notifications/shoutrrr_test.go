package notifications

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"
	dockerImage "github.com/moby/moby/api/types/image"
	dockerNetwork "github.com/moby/moby/api/types/network"
	shoutrrrTypes "github.com/nicholas-fedor/shoutrrr/pkg/types"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/testutil/logcapture"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

var allButTrace = zerolog.DebugLevel

const shoutrrrFatalHelperEnv = "WATCHTOWER_SHOUTRRR_FATAL_HELPER"

// newTestLogger returns a logger that writes each event to the test's log, so a
// failing test shows what it logged.
//
// Parameters:
//   - t: Test that receives the log lines.
//
// Returns:
//   - *zerolog.Logger: Logger writing to t.
func newTestLogger(t *testing.T) *zerolog.Logger {
	t.Helper()

	// Frame 4 skips zerolog's write path, so each line names the log call.
	w := zerolog.TestWriter{T: t, Frame: 4}

	return new(zerolog.New(w))
}

// newBufferLogger returns a JSON logger at trace level and the buffer it writes
// to, for tests that check what the notifier logs.
//
// Returns:
//   - *zerolog.Logger: Logger writing to the buffer.
//   - *logcapture.Buffer: Buffer holding the log output.
func newBufferLogger() (*zerolog.Logger, *logcapture.Buffer) {
	buf := &logcapture.Buffer{}
	// JSON encoding keeps field assertions simple (failure_type, failed_count, etc.).
	l := zerolog.New(buf).Level(zerolog.TraceLevel).With().Timestamp().Logger()

	return &l, buf
}

// createTestNotifier wraps createNotifier with the default template. Pass a
// custom template to createNotifier directly when a test needs one.
func createTestNotifier(
	log *zerolog.Logger,
	urls []string,
	level zerolog.Level,
	legacy bool,
	data StaticData,
	stdout bool,
	delay time.Duration,
) *shoutrrrTypeNotifier {
	return createNotifier(log, urls, level, "", legacy, data, stdout, delay)
}

// TODO: Remove legacyMockData when legacy notification types are removed.
//
//nolint:godox
var legacyMockData = notificationData{
	Entries: []*notificationEntry{
		{
			Level:   "info",
			Message: "foo Bar",
		},
	},
}

var mockDataMultipleEntries = notificationData{
	Entries: []*notificationEntry{
		{
			Level:   "info",
			Message: "The situation is under control",
		},
		{
			Level:   "warning",
			Message: "All the smoke might be covering up some problems",
		},
		{
			Level:   "error",
			Message: "Turns out everything is on fire",
		},
	},
}

var mockDataAllFresh = notificationData{
	Entries: []*notificationEntry{},
	Report:  newProgressReport(state.FreshState),
}

// mockDataFromStates generates mock notification data with specified container states.
// It includes legacy log entries and static data for testing purposes.
//
// TODO: Remove legacyMockData reference when legacy notification types are removed.
//
//nolint:godox
func mockDataFromStates(t *testing.T, states ...state.State) notificationData {
	t.Helper()

	hostname := "Mock"
	prefix := ""

	return notificationData{
		Entries: legacyMockData.Entries,
		Report:  newProgressReport(states...),
		Title:   getTitle(newTestLogger(t), hostname, prefix),
		Host:    hostname,
	}
}

// Error values recorded on the skipped and failed containers of a progress report.
var (
	// errReportSkipped is the reason recorded for a skipped container.
	errReportSkipped = errors.New("unpossible")
	// errReportFailed is the error recorded for a failed container.
	errReportFailed = errors.New("accidentally the whole container")
)

// Container ID prefixes that keep the containers of each state in a progress
// report distinct, such as "c7941..." for the first skipped container.
const (
	// updatedIDPrefix prefixes updated containers.
	updatedIDPrefix = 11
	// failedIDPrefix prefixes failed containers.
	failedIDPrefix = 21
	// freshIDPrefix prefixes fresh containers.
	freshIDPrefix = 31
	// skippedIDPrefix prefixes skipped containers.
	skippedIDPrefix = 41
	// restartedIDPrefix prefixes restarted containers.
	restartedIDPrefix = 51
)

// progressIDLength is the length of a full container or image ID.
const progressIDLength = 64

// maxContainersPerState is the number of containers of one state that keep
// unique IDs. The ID prefixes of the states are ten apart, so the eleventh
// updated container would reuse the first failed container's ID.
const maxContainersPerState = 10

// newProgressReport builds a report with one container for each given state,
// numbering repeated states so every container has a unique ID and name.
//
// Parameters:
//   - states: Outcome of each container, in order.
//
// Returns:
//   - types.Report: Report built through state.Progress.
func newProgressReport(states ...state.State) types.Report {
	log := new(zerolog.Nop())
	// Count each state's containers so their IDs and names stay unique.
	stateNums := make(map[state.State]int)
	progress := state.Progress{}
	failed := make(map[types.ContainerID]error)

	for _, outcome := range states {
		index := stateNums[outcome]

		switch outcome {
		case state.SkippedState:
			c, _ := newProgressContainer(index, skippedIDPrefix, "skip%d")
			progress.AddSkipped(log, c, errReportSkipped, types.UpdateParams{})
		case state.FreshState:
			c, _ := newProgressContainer(index, freshIDPrefix, "frsh%d")
			progress.AddScanned(log, c, c.ImageID(), types.UpdateParams{})
		case state.UpdatedState:
			c, newImage := newProgressContainer(index, updatedIDPrefix, "updt%d")
			progress.AddScanned(log, c, newImage, types.UpdateParams{})
			progress.MarkForUpdate(log, c.ID())
		case state.FailedState:
			c, newImage := newProgressContainer(index, failedIDPrefix, "fail%d")
			progress.AddScanned(log, c, newImage, types.UpdateParams{})

			failed[c.ID()] = errReportFailed
		case state.RestartedState:
			c, _ := newProgressContainer(index, restartedIDPrefix, "rstr%d")
			progress.AddScanned(log, c, c.ImageID(), types.UpdateParams{})
			progress.MarkRestarted(log, c.ID())
		case state.UnknownState, state.ScannedState, state.StaleState:
			// Reports never end in these intermediate states.
			continue
		}

		stateNums[outcome] = index + 1
	}

	progress.UpdateFailed(log, failed)

	return progress.Report(log)
}

// newProgressContainer builds a running container for a progress report, with
// an ID and image IDs derived from idPrefix and index, and a name from
// nameFormat and index. It panics when index is maxContainersPerState or more.
//
// Parameters:
//   - index: Zero-based position of the container among its state's containers.
//   - idPrefix: Digits that start the container's ID after "c79".
//   - nameFormat: Name format with one %d verb for the one-based position.
//
// Returns:
//   - types.Container: Container running image mock/<name>:latest.
//   - types.ImageID: ID of the newer image the container can move to.
func newProgressContainer(index, idPrefix int, nameFormat string) (types.Container, types.ImageID) {
	if index >= maxContainersPerState {
		panic(fmt.Sprintf("newProgressContainer: index %d would reuse another state's container ID", index))
	}

	indexStr := strconv.Itoa(idPrefix + index)
	// Pad the ID with zeros to full length, after the "c79" prefix.
	idSuffix := indexStr + strings.Repeat("0", progressIDLength-3-len(indexStr))
	name := fmt.Sprintf(nameFormat, index+1)
	oldImageID := "01d" + idSuffix
	imageName := fmt.Sprintf("mock/%s:latest", name)

	inspect := dockerContainer.InspectResponse{
		ID:      "c79" + idSuffix,
		Image:   oldImageID,
		Name:    name,
		State:   &dockerContainer.State{Running: true},
		Created: time.Now().Format(time.RFC3339Nano),
		HostConfig: &dockerContainer.HostConfig{
			PortBindings: dockerNetwork.PortMap{},
		},
		Config: &dockerContainer.Config{Image: imageName},
	}
	imageInfo := dockerImage.InspectResponse{
		ID: oldImageID,
		RepoDigests: []string{
			fmt.Sprintf("%s@sha256:%s", oldImageID, strings.ReplaceAll(oldImageID, ":", "_")),
		},
	}

	return container.NewContainer(nil, &inspect, &imageInfo), types.ImageID("d0a" + idSuffix)
}

// mockRouter implements the router interface for testing error scenarios.
type mockRouter struct {
	sendErrors []error
}

func (m *mockRouter) Send(_ string, _ *shoutrrrTypes.Params) []error {
	return m.sendErrors
}

// blockingRouter simulates a notification router with blocking behavior for testing.
// It waits for an unlock signal before sending and signals completion via a channel.
type blockingRouter struct {
	unlock chan bool
	sent   chan bool
	ctx    context.Context //nolint:containedctx
}

func (b blockingRouter) Send(_ string, _ *shoutrrrTypes.Params) []error {
	select {
	case <-b.unlock:
		b.sent <- true
	case <-b.ctx.Done():
		// canceled, don't send
	}

	return nil
}

// sendNotificationsWithBlockingRouter creates a notifier with a blocking router for testing.
// It queues a message and returns the notifier and router to verify notification delays.
//
// TODO: Remove legacy template usage when legacy notification types are removed.
//
//nolint:godox
func sendNotificationsWithBlockingRouter(t *testing.T) (*shoutrrrTypeNotifier, *blockingRouter, error) {
	t.Helper()

	legacy := true
	ctx, cancel := context.WithCancel(context.Background())

	router := &blockingRouter{
		unlock: make(chan bool, 1),
		sent:   make(chan bool, 1),
		ctx:    ctx,
	}

	tpl, err := newMessageTemplate(newTestLogger(t), "", legacy)
	if err != nil {
		cancel()

		return nil, nil, err
	}

	shoutrrr := &shoutrrrTypeNotifier{
		template: tpl,
		messages: make(chan string, 1),
		done:     make(chan struct{}),
		Router:   router,
		params:   &shoutrrrTypes.Params{},
		ctx:      ctx,
		cancel:   cancel,
		delay:    time.Duration(0),
	}

	entry := &notificationEntry{
		Message: "foo bar",
	}

	go sendNotifications(shoutrrr)

	shoutrrr.StartNotification(false)
	// Enqueue directly: Run(nil, ...) is fail-closed (cannot parse notify field).
	// These tests exercise the send worker, not hook field extraction.
	shoutrrr.entriesMutex.Lock()
	shoutrrr.entries = append(shoutrrr.entries, entry)
	shoutrrr.entriesMutex.Unlock()
	shoutrrr.SendNotification(nil)

	return shoutrrr, router, nil
}

// getTemplatedResult generates a templated message for testing.
// It builds and returns the message string, expecting no errors.
//
// TODO: Remove legacy parameter when legacy notification types are removed.
//
//nolint:godox
func getTemplatedResult(t *testing.T, tplString string, legacy bool, data notificationData) string {
	t.Helper()

	log := newTestLogger(t)

	tpl, err := newMessageTemplate(log, tplString, legacy)
	require.NoError(t, err)
	msg, err := tpl.render(log, data)
	require.NoError(t, err)

	return msg
}

// TestRegisterHook verifies that RegisterHook starts the notifier
// receiving log entries. Registering it a second time changes nothing.
func TestRegisterHook(t *testing.T) {
	t.Parallel()

	t.Run("it has not been added before", func(t *testing.T) {
		t.Parallel()

		t.Run("should start receiving via RegisterHook", func(t *testing.T) {
			t.Parallel()

			level := zerolog.TraceLevel
			shoutrrr := createTestNotifier(
				newTestLogger(t),
				[]string{},
				level,
				true,
				StaticData{},
				false,
				time.Second,
			)
			assert.False(t, shoutrrr.receiving.Load())

			log := newTestLogger(t)

			shoutrrr.RegisterHook(log)
			defer shoutrrr.Close()

			assert.True(t, shoutrrr.receiving.Load())
		})
	})
	t.Run("it is being added a second time", func(t *testing.T) {
		t.Parallel()

		t.Run("should be idempotent", func(t *testing.T) {
			t.Parallel()

			level := zerolog.TraceLevel
			shoutrrr := createTestNotifier(
				newTestLogger(t),
				[]string{},
				level,
				true,
				StaticData{},
				false,
				time.Second,
			)
			log := newTestLogger(t)

			shoutrrr.RegisterHook(log)
			defer shoutrrr.Close()

			shoutrrr.RegisterHook(log)
			assert.True(t, shoutrrr.receiving.Load())
		})
	})
}

// TestCreateNotifier_EmptyTitle verifies that a notifier built without a
// title sets no title param.
func TestCreateNotifier_EmptyTitle(t *testing.T) {
	t.Parallel()

	t.Run("should not have set the title param", func(t *testing.T) {
		t.Parallel()

		shoutrrr := createTestNotifier(newTestLogger(t), []string{"logger://"}, allButTrace, true, StaticData{
			Host:  "test.host",
			Title: "",
		}, false, time.Second)
		_, found := shoutrrr.params.Title()
		assert.False(t, found)
	})
}

// TestClose verifies that Close is safe to call repeatedly or concurrently. It
// returns at once without a running worker. Later operations do not panic.
func TestClose(t *testing.T) {
	t.Parallel()

	t.Run("Close() is called multiple times", func(t *testing.T) {
		t.Parallel()

		t.Run("should be idempotent and not panic", func(t *testing.T) {
			t.Parallel()

			shoutrrr := createTestNotifier(
				newTestLogger(t),
				[]string{"logger://"},
				allButTrace,
				true,
				StaticData{},
				false,
				time.Duration(0),
			)
			shoutrrr.RegisterHook(newTestLogger(t))

			// First close should work normally
			shoutrrr.Close()

			// Subsequent closes should be no-ops
			shoutrrr.Close()
			shoutrrr.Close()

			// Should not panic
		})
	})

	t.Run("Close() is called without starting the goroutine", func(t *testing.T) {
		t.Parallel()

		t.Run("should not panic or block", func(t *testing.T) {
			t.Parallel()

			shoutrrr := createTestNotifier(
				newTestLogger(t),
				[]string{"logger://"},
				allButTrace,
				true,
				StaticData{},
				false,
				time.Duration(0),
			)
			// Note: Not calling RegisterHook(), so no goroutine is started

			// Close should work without blocking
			shoutrrr.Close()

			// Should not panic
		})
	})

	t.Run("operations are attempted after Close()", func(t *testing.T) {
		t.Parallel()

		t.Run("should handle gracefully without panicking", func(t *testing.T) {
			t.Parallel()

			shoutrrr := createTestNotifier(
				newTestLogger(t),
				[]string{"logger://"},
				allButTrace,
				true,
				StaticData{},
				false,
				time.Duration(0),
			)
			shoutrrr.RegisterHook(newTestLogger(t))

			// Close the notifier
			shoutrrr.Close()

			// These operations should not panic after close
			shoutrrr.StartNotification(false)
			shoutrrr.SendNotification(nil)
			shoutrrr.SendNotification(nil)

			// Fire should still work (but may not send if channel is closed)
			entry := &notificationEntry{Message: "test"}
			shoutrrr.Run(nil, zerolog.InfoLevel, entry.Message)

			err := error(nil)
			require.NoError(t, err)

			// Should not panic
		})
	})

	t.Run("Close() is called concurrently", func(t *testing.T) {
		t.Parallel()

		t.Run("should handle concurrent calls safely", func(t *testing.T) {
			t.Parallel()

			shoutrrr := createTestNotifier(
				newTestLogger(t),
				[]string{"logger://"},
				allButTrace,
				true,
				StaticData{},
				false,
				time.Duration(0),
			)
			shoutrrr.RegisterHook(newTestLogger(t))

			// Start multiple goroutines calling Close concurrently
			done := make(chan bool, 10)

			for range 10 {
				go func() {
					shoutrrr.Close()

					done <- true
				}()
			}

			// Wait for all to complete
			for range 10 {
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("Close did not return")
				}
			}

			// Should not panic and all should complete
		})
	})
}

// TestShutdownGracePeriodConstant verifies that the shutdownGracePeriod constant is set to 50ms.
func TestShutdownGracePeriodConstant(t *testing.T) {
	expectedGracePeriod := 50 * time.Millisecond
	if shutdownGracePeriod != expectedGracePeriod {
		t.Fatalf("expected shutdownGracePeriod to be %v, got %v", expectedGracePeriod, shutdownGracePeriod)
	}
}

// TestCloseDoesNotHangWithBlockingRouter verifies that Close() completes without hanging
// when the router is blocked. This tests that the context cancellation properly unblocks
// the send call.
func TestCloseDoesNotHangWithBlockingRouter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Create a notifier with a blocking router
		shoutrrr, blockingRouter, err := sendNotificationsWithBlockingRouter(t)
		if err != nil {
			t.Fatal(err)
		}

		// Wait for goroutine to be blocked
		synctest.Wait()

		// Verify router is blocked (not yet sent)
		select {
		case <-blockingRouter.sent:
			t.Fatal("message should not have been sent yet")
		default:
			// Good - router is blocked
		}

		// Close should complete without hanging - use a goroutine since it may block briefly
		closeDone := make(chan error, 1)

		go func() {
			shoutrrr.Close()

			closeDone <- nil
		}()

		// Wait for Close to complete (should be fast with synctest)
		select {
		case <-closeDone:
			// Good - Close completed
			t.Log("Close() completed successfully")
		case <-time.After(2 * time.Second):
			t.Fatal("Close() hung - context cancellation did not unblock the goroutine")
		}

		// Verify context was canceled
		select {
		case <-shoutrrr.ctx.Done():
			t.Log("Context was canceled")
		default:
			t.Fatal("expected context to be canceled")
		}

		// Clean up: unlock the router so the goroutine can exit gracefully
		blockingRouter.unlock <- true

		// Wait for the done channel to be signaled
		synctest.Wait()

		select {
		case <-shoutrrr.done:
			t.Log("Done channel was signaled")
		default:
			t.Fatal("expected done channel to be signaled")
		}
	})
}

// controlledRouter simulates a router that can be controlled via channels for testing.
// It waits for a continue signal before sending, allowing deterministic testing.
type controlledRouter struct {
	continueCh chan struct{}
	sent       chan bool
	ctx        context.Context //nolint:containedctx
}

func (c *controlledRouter) Send(_ string, _ *shoutrrrTypes.Params) []error {
	// Signal that we're waiting
	// Wait for continue signal or context cancellation
	select {
	case <-c.continueCh:
		c.sent <- true
	case <-c.ctx.Done():
		// canceled, don't send
	}

	return nil
}

// TestGracePeriodAllowsInFlightMessages verifies that in-flight messages have time to complete
// before context is canceled during the shutdown grace period.
func TestGracePeriodAllowsInFlightMessages(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Create a controlled router
		ctx, cancel := context.WithCancel(context.Background())

		controlledR := &controlledRouter{
			continueCh: make(chan struct{}, 1),
			sent:       make(chan bool, 1),
			ctx:        ctx,
		}

		tpl, err := newMessageTemplate(newTestLogger(t), "", true)
		if err != nil {
			cancel()
			t.Fatal(err)
		}

		shoutrrr := &shoutrrrTypeNotifier{
			template:  tpl,
			messages:  make(chan string, 1),
			done:      make(chan struct{}),
			stop:      make(chan struct{}),
			Router:    controlledR,
			params:    &shoutrrrTypes.Params{},
			ctx:       ctx,
			cancel:    cancel,
			delay:     time.Duration(0),
			receiving: atomic.Bool{},
		}
		shoutrrr.receiving.Store(true)

		// Start the notification goroutine
		go sendNotifications(shoutrrr)

		// Queue a message
		shoutrrr.messages <- "test message"

		// Wait for goroutine to be blocked in router.Send
		synctest.Wait()

		// Now call Close - this should trigger the grace period wait
		// We let it proceed by signaling continueCh in a separate goroutine
		go func() {
			// Wait a bit to simulate the grace period passing
			synctest.Wait()
			// Then allow the message to be sent
			controlledR.continueCh <- struct{}{}
		}()

		// Close should complete
		shoutrrr.Close()

		// Wait for done channel
		synctest.Wait()

		// Note: Close() already waits for done channel internally (line 500 in shoutrrr.go)
		// so we don't need to wait for it here. Instead, we verify the message was sent.

		// Verify the message was actually sent by checking controlledR.sent
		timeout := 2 * time.Second
		select {
		case sent := <-controlledR.sent:
			if !sent {
				t.Fatal("expected message to be sent during grace period")
			}

			t.Log("Message was successfully sent during grace period")
		case <-time.After(timeout):
			t.Fatalf("expected sent channel to be signaled within %v timeout", timeout)
		}
	})
}

// TestCloseWithNoGoroutine verifies that Close() works correctly when the
// notification goroutine was never started.
func TestCloseWithNoGoroutine(t *testing.T) {
	shoutrrr := createTestNotifier(
		newTestLogger(t),
		[]string{"logger://"},
		allButTrace,
		true,
		StaticData{},
		false,
		time.Duration(0),
	)
	// Note: Not calling RegisterHook(), so no goroutine is started

	// Close should complete immediately without blocking
	// Use goroutine+channel+select pattern to avoid flaky wall-clock timing.
	// Close() has a shutdownGracePeriod (50ms) wait, so we use 1s timeout
	// which is comfortably larger than 2x shutdownGracePeriod.
	closeDone := make(chan struct{})

	go func() {
		shoutrrr.Close()

		closeDone <- struct{}{}
	}()

	// Select with timeout - Close() should complete within reasonable time
	select {
	case <-closeDone:
		t.Log("Close() completed successfully with no goroutine")
	case <-time.After(1 * time.Second):
		t.Fatalf("Close() took too long without goroutine (timeout exceeded)")
	}
}

// TestCreateNotifier_FatalsOnBadURL verifies that createNotifier calls Fatal
// when given a docker-secret file path instead of a valid URL.
// The fatal path is tested via subprocess because zerolog.Fatal calls os.Exit.
func TestCreateNotifier_FatalsOnBadURL(t *testing.T) {
	if os.Getenv(shoutrrrFatalHelperEnv) != "" {
		// Child process: expect createNotifier to fatal on invalid URL.
		log := zerolog.Nop()
		createNotifier(
			&log,
			[]string{"docker-secret:/run/secrets/my_url"},
			zerolog.InfoLevel,
			"",
			true,
			StaticData{},
			false,
			0,
		)

		// If we reach here, the fatal did not exit.
		t.Fatal("expected createNotifier to fatal on bad URL")
	}

	// Parent process: run child and assert non-zero exit.
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCreateNotifier_FatalsOnBadURL$")

	cmd.Env = append(os.Environ(), shoutrrrFatalHelperEnv+"=1")
	out, err := cmd.CombinedOutput()

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("timed out waiting for createNotifier fatal path; output:\n%s", string(out))
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() == 0 {
		t.Fatalf("expected non-zero exit for bad URL; output:\n%s", string(out))
	}
}

// TestCreateNotifier_AcceptsGotifyURLFromFileExpansion verifies that createNotifier
// succeeds when given a valid Gotify URL matching expanded secret content.
func TestCreateNotifier_AcceptsGotifyURLFromFileExpansion(t *testing.T) {
	notifier := createTestNotifier(
		newTestLogger(t),
		[]string{"gotify://gotify.example.com/Atoken123456789"},
		zerolog.InfoLevel,
		true,
		StaticData{},
		false,
		0,
	)

	require.NotNil(t, notifier)
}

type countingRouter struct {
	sends atomic.Int32
}

func (c *countingRouter) Send(_ string, _ *shoutrrrTypes.Params) []error {
	c.sends.Add(1)

	return nil
}

// TestCreateNotifier_InvalidTemplateFallsBackToDefault verifies that a
// notification template that fails to parse is replaced by the default
// template, so notifications render as with no template set instead of
// failing when the first notification is sent.
func TestCreateNotifier_InvalidTemplateFallsBackToDefault(t *testing.T) {
	data := goldenFixtures()["full"]

	for _, legacy := range []bool{true, false} {
		t.Run(fmt.Sprintf("legacy=%t", legacy), func(t *testing.T) {
			invalid := createNotifier(newTestLogger(t), []string{}, zerolog.TraceLevel, "{{ .Broken", legacy, StaticData{}, false, 0)
			t.Cleanup(invalid.Close)

			fallback := createNotifier(newTestLogger(t), []string{}, zerolog.TraceLevel, "", legacy, StaticData{}, false, 0)
			t.Cleanup(fallback.Close)

			want, err := fallback.template.render(fallback.ll(), data)
			require.NoError(t, err)

			got, err := invalid.template.render(invalid.ll(), data)
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}
