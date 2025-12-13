package data

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// PreviewData represents a generator for preview data, including container statuses and log entries.
// It maintains state for random generation, container counts, and report data.
type PreviewData struct {
	rand           *rand.Rand  // Random number generator for deterministic output
	lastTime       time.Time   // Last timestamp used for log entries, for sequential generation
	report         *report     // Report containing categorized container statuses
	containerCount int         // Counter for generating unique container names
	Entries        []*logEntry // List of simulated log entries
	StaticData     staticData  // Static data for notification templates (Title, Host)
}

// staticData holds static fields for the notification template, set during initialization.
type staticData struct {
	Title string // Title field for the notification
	Host  string // Host field for the notification
}

// Errors for preview data generation.
var (
	// errExecutionFailed indicates a simulated failure in container execution for FailedState.
	errExecutionFailed = errors.New("execution failed")
	// errSkipped indicates a simulated skip of a container for SkippedState.
	errSkipped = errors.New("container skipped")
)

// New initializes a new PreviewData struct with seeded random generation and default static data.
// The random seed is fixed at 1 for deterministic output in previews.
func New() *PreviewData {
	// Initialize random number generator with a fixed seed for consistent previews
	// Note: Using math/rand intentionally for deterministic output (not cryptographically secure)
	//nolint:gosec
	return &PreviewData{
		rand:           rand.New(rand.NewSource(1)),
		lastTime:       time.Now().Add(-30 * time.Minute), // Start timestamps 30 minutes ago
		report:         &report{},                         // Initialize empty report
		containerCount: 0,                                 // Start container count at 0
		Entries:        []*logEntry{},                     // Initialize empty log entries
		StaticData: staticData{
			Title: "Title", // Default title for notifications
			Host:  "Host",  // Default host for notifications
		},
	}
}

// AddFromState adds a container status entry to the report with the specified state.
// It generates a random container ID, image IDs, name, and image name based on the state.
// For FailedState or SkippedState, it includes a contextual error message.
func (p *PreviewData) AddFromState(state State) {
	// Generate unique IDs for container and images
	cid := types.ContainerID(p.generateID())
	oldImageID := types.ImageID(p.generateID())
	newImageID := types.ImageID(p.generateID())
	name := p.generateName()
	image := p.generateImageName(name)

	var err error

	// Handle state-specific logic for errors
	// Switch statement avoids exhaustive check to reduce verbosity
	//nolint:exhaustive
	switch state {
	case FailedState:
		// Simulate a failure with a realistic error message
		err = fmt.Errorf("%w: %s", errExecutionFailed, p.randomEntry(errorMessages))
	case SkippedState:
		// Simulate a skip with a realistic skip reason
		err = fmt.Errorf("%w: %s", errSkipped, p.randomEntry(skippedMessages))
	}

	// Create a container status with the generated data
	status := &containerStatus{
		containerID:    cid,
		oldImage:       oldImageID,
		newImage:       newImageID,
		containerName:  name,
		imageName:      image,
		containerError: err,
		state:          state,
	}

	// Append the status to the appropriate report category

	switch state {
	case ScannedState:
		p.report.scanned = append(p.report.scanned, status)
	case UpdatedState:
		p.report.updated = append(p.report.updated, status)
	case FailedState:
		p.report.failed = append(p.report.failed, status)
	case SkippedState:
		p.report.skipped = append(p.report.skipped, status)
	case RestartedState:
		p.report.restarted = append(p.report.restarted, status)
	case StaleState:
		p.report.stale = append(p.report.stale, status)
	case FreshState:
		p.report.fresh = append(p.report.fresh, status)
	}

	// Increment container count for unique name generation
	p.containerCount++
}

// AddLogEntry adds a preview log entry with the specified level.
// It generates a message based on the log level, using errorMessages for Error/Warn, warningMessages for Warn,
// and infoMessages for Info/Debug, with a timestamp advancing randomly.
func (p *PreviewData) AddLogEntry(level LogLevel) {
	var msg string

	// Select message based on log level
	switch level {
	case FatalLevel, ErrorLevel:
		msg = p.randomEntry(errorMessages) // Use error messages for fatal/error
	case WarnLevel:
		msg = p.randomEntry(warningMessages) // Use warning messages
	case TraceLevel, DebugLevel, InfoLevel, PanicLevel:
		msg = p.randomEntry(infoMessages) // Use info messages for others
	default:
		msg = p.randomEntry(infoMessages) // Fallback to info messages
	}

	// Append a new log entry with the generated message and timestamp
	p.Entries = append(p.Entries, &logEntry{
		Message: msg,
		Data:    map[string]any{}, // Empty data map (no key-value pairs in preview)
		Time:    p.generateTime(),
		Level:   level,
	})
}

// Report returns the current preview report containing categorized container statuses.
// It provides a snapshot of the simulated data for notification previews.
func (p *PreviewData) Report() types.Report {
	return p.report // Return the current report instance
}

// Constants for ID generation and time increments.
const (
	idLength                = 32 // Length of generated IDs in bytes (for container and image IDs)
	maxTimeIncrementSeconds = 30 // Maximum seconds to increment time between log entries
)

// generateID creates a random hexadecimal ID string of idLength bytes.
// Used for container and image IDs in the preview.
func (p *PreviewData) generateID() string {
	buf := make([]byte, idLength) // Create buffer for ID
	_, _ = p.rand.Read(buf)       // Fill with random bytes

	return hex.EncodeToString(buf) // Convert to hexadecimal string
}

// generateTime generates a new timestamp by advancing the last time by a random increment.
// Ensures sequential timestamps for realistic log simulation.
func (p *PreviewData) generateTime() time.Time {
	p.lastTime = p.lastTime.Add(
		time.Duration(p.rand.Intn(maxTimeIncrementSeconds)) * time.Second,
	) // Increment by up to 30 seconds

	return p.lastTime
}

// randomEntry selects a random string from the provided array.
// Used to pick messages or errors for logs and statuses.
func (p *PreviewData) randomEntry(arr []string) string {
	return arr[p.rand.Intn(len(arr))] // Select random index
}

// generateName creates a unique container name for the preview.
// Uses containerNames array with a suffix for uniqueness if needed.
func (p *PreviewData) generateName() string {
	index := p.containerCount
	if index < len(containerNames) {
		return containerNames[index] // Use base name
	}

	suffix := index / len(
		containerNames,
	) // Calculate suffix for uniqueness
	index %= len(containerNames) // Wrap around array

	return containerNames[index] + strconv.FormatInt(int64(suffix), 10) // Append suffix
}

// generateImageName creates an image name based on a container name.
// Uses organizationNames array for realistic image references.
func (p *PreviewData) generateImageName(name string) string {
	index := p.containerCount % len(organizationNames) // Select organization

	return organizationNames[index] + name + ":latest" // Format as org/name:latest
}
