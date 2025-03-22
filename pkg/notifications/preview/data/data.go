// Package data provides utilities for generating preview data for Watchtower notifications.
// It includes mechanisms to simulate container statuses and log entries for testing purposes.
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
type PreviewData struct {
	rand           *rand.Rand
	lastTime       time.Time
	report         *report
	containerCount int
	Entries        []*logEntry
	StaticData     staticData
}

type staticData struct {
	Title string
	Host  string
}

// New initializes a new PreviewData struct with seeded random generation and default static data.
// The random seed is fixed at 1 for deterministic output in previews.
//
//nolint:redefines-builtin-id // Constructor naming convention
func New() *PreviewData {
	return &PreviewData{
		rand:           rand.New(rand.NewSource(1)), //nolint:gosec // Preview data, not security-critical
		lastTime:       time.Now().Add(-30 * time.Minute),
		report:         nil,
		containerCount: 0,
		Entries:        []*logEntry{},
		StaticData: staticData{
			Title: "Title",
			Host:  "Host",
		},
	}
}

// AddFromState adds a container status entry to the report with the specified state.
// It generates a random container ID, image IDs, name, and image name based on the state.
// For FailedState or SkippedState, it includes a contextual error message.
func (p *PreviewData) AddFromState(state State) {
	cid := types.ContainerID(p.generateID())
	old := types.ImageID(p.generateID())
	new := types.ImageID(p.generateID()) //nolint
	name := p.generateName()
	image := p.generateImageName(name)

	var err error

	//nolint
	switch state {
	case FailedState:
		err = fmt.Errorf("execution failed: %w", errors.New(p.randomEntry(errorMessages)))
	case SkippedState:
		err = fmt.Errorf("skipped: %w", errors.New(p.randomEntry(skippedMessages)))
	}

	p.addContainer(containerStatus{
		containerID:   cid,
		oldImage:      old,
		newImage:      new,
		containerName: name,
		imageName:     image,
		error:         err,
		state:         state,
	})
}

// addContainer appends a container status to the appropriate report category based on its state.
// It initializes the report if it doesn’t exist and increments the container count.
func (p *PreviewData) addContainer(c containerStatus) {
	if p.report == nil {
		p.report = &report{}
	}

	switch c.state {
	case ScannedState:
		p.report.scanned = append(p.report.scanned, &c)
	case UpdatedState:
		p.report.updated = append(p.report.updated, &c)
	case FailedState:
		p.report.failed = append(p.report.failed, &c)
	case SkippedState:
		p.report.skipped = append(p.report.skipped, &c)
	case StaleState:
		p.report.stale = append(p.report.stale, &c)
	case FreshState:
		p.report.fresh = append(p.report.fresh, &c)
	default:
		return
	}

	p.containerCount++
}

// AddLogEntry adds a preview log entry with the specified level.
// It generates a message based on the log level, using error messages for Fatal, Error, and Warn levels,
// and general messages for others, with a timestamp advancing randomly.
func (p *PreviewData) AddLogEntry(level LogLevel) {
	var msg string

	switch level {
	case FatalLevel, ErrorLevel, WarnLevel:
		msg = p.randomEntry(logErrors)
	case TraceLevel, DebugLevel, InfoLevel, PanicLevel:
		msg = p.randomEntry(logMessages)
	default:
		msg = p.randomEntry(logMessages) // Fallback for unhandled levels
	}

	p.Entries = append(p.Entries, &logEntry{
		Message: msg,
		Data:    map[string]any{},
		Time:    p.generateTime(),
		Level:   level,
	})
}

// Report returns the current preview report containing categorized container statuses.
// It provides a snapshot of the simulated data for notification previews.
func (p *PreviewData) Report() types.Report {
	return p.report
}

// Constants for ID generation and time increments.
const (
	idLength                = 32 // Length of generated IDs in bytes
	maxTimeIncrementSeconds = 30 // Maximum seconds to increment time
)

func (p *PreviewData) generateID() string {
	buf := make([]byte, idLength)
	_, _ = p.rand.Read(buf)

	return hex.EncodeToString(buf)
}

func (p *PreviewData) generateTime() time.Time {
	p.lastTime = p.lastTime.Add(time.Duration(p.rand.Intn(maxTimeIncrementSeconds)) * time.Second)

	return p.lastTime
}

func (p *PreviewData) randomEntry(arr []string) string {
	return arr[p.rand.Intn(len(arr))]
}

func (p *PreviewData) generateName() string {
	index := p.containerCount
	if index <= len(containerNames) {
		return "/" + containerNames[index]
	}

	suffix := index / len(containerNames)
	index %= len(containerNames)

	return "/" + containerNames[index] + strconv.FormatInt(int64(suffix), 10)
}

func (p *PreviewData) generateImageName(name string) string {
	index := p.containerCount % len(organizationNames)

	return organizationNames[index] + name + ":latest"
}
