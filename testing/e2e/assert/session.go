package assert

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	// msgStopping is Watchtower's stop log line.
	msgStopping = "Stopping container"
	// msgStarted is Watchtower's start log line.
	msgStarted = "Started new container"
	// msgFoundNew is Watchtower's new-image log line.
	msgFoundNew = "Found new image"
	// msgSession is Watchtower's session-complete log line.
	msgSession = "Update session completed"
	// minDependencyNodes is the smallest graph that has a stop/start order.
	minDependencyNodes = 2
)

// SessionEvent is one Watchtower JSON log line that names a container.
type SessionEvent struct {
	// Message is the log message.
	Message string `json:"message"`
	// Container is the container name when present.
	Container string `json:"container"`
}

// ParseSession extracts Stopping / Started / Found new image events from JSON logs.
//
// Parameters:
//   - logs: Demultiplexed Watchtower stdout.
//
// Returns:
//   - []SessionEvent: Ordered events.
func ParseSession(logs string) []SessionEvent {
	events := make([]SessionEvent, 0)
	scanner := bufio.NewScanner(strings.NewReader(logs))

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(strings.TrimSpace(line), "{") {
			continue
		}

		var event SessionEvent

		err := json.Unmarshal([]byte(line), &event)
		if err != nil {
			continue
		}

		switch event.Message {
		case msgStopping, msgStarted, msgFoundNew, msgSession:
			events = append(events, event)
		}
	}

	return events
}

// StopNames returns container names in the order Watchtower stopped them.
//
// Parameters:
//   - events: Parsed session.
//
// Returns:
//   - []string: Stop order.
func StopNames(events []SessionEvent) []string {
	return namesFor(events, msgStopping)
}

// StartNames returns container names in the order Watchtower started them.
//
// Parameters:
//   - events: Parsed session.
//
// Returns:
//   - []string: Start order.
func StartNames(events []SessionEvent) []string {
	return namesFor(events, msgStarted)
}

// AssertDependencyOrder checks stop-dependents-first and start-dependencies-first.
//
// Parameters:
//   - events: Parsed session.
//   - dependencyOrder: Names from dependency to dependent (A, B, C, D).
//
// Returns:
//   - error: Wrong order or missing container.
func AssertDependencyOrder(events []SessionEvent, dependencyOrder []string) error {
	if len(dependencyOrder) < minDependencyNodes {
		return nil
	}

	wantStop := reverseNames(dependencyOrder)
	wantStart := dependencyOrder

	gotStop := filterKnown(StopNames(events), dependencyOrder)
	gotStart := filterKnown(StartNames(events), dependencyOrder)

	if !namesEqual(gotStop, wantStop) {
		return fmt.Errorf("%w: stop got %v want %v", ErrDependencyOrder, gotStop, wantStop)
	}

	if !namesEqual(gotStart, wantStart) {
		return fmt.Errorf("%w: start got %v want %v", ErrDependencyOrder, gotStart, wantStart)
	}

	return nil
}

// namesFor collects container names for one log message type.
//
// Parameters:
//   - events: Parsed session.
//   - message: Log message to keep.
//
// Returns:
//   - []string: Container names in log order.
func namesFor(events []SessionEvent, message string) []string {
	names := make([]string, 0)

	for _, event := range events {
		if event.Message == message && event.Container != "" {
			names = append(names, event.Container)
		}
	}

	return names
}

// reverseNames returns a new slice in reverse order.
//
// Parameters:
//   - names: Original names.
//
// Returns:
//   - []string: Reversed copy.
func reverseNames(names []string) []string {
	out := make([]string, len(names))
	for idx, name := range names {
		out[len(names)-1-idx] = name
	}

	return out
}

// filterKnown keeps log names that belong to the fixture.
//
// Parameters:
//   - got: Names from logs.
//   - known: Fixture names.
//
// Returns:
//   - []string: Filtered names.
func filterKnown(got, known []string) []string {
	allow := make(map[string]struct{}, len(known))
	for _, name := range known {
		allow[name] = struct{}{}
	}

	out := make([]string, 0, len(got))
	for _, name := range got {
		if _, ok := allow[name]; ok {
			out = append(out, name)
		}
	}

	return out
}

// namesEqual reports whether got and want are the same sequence.
//
// Parameters:
//   - got: Actual names.
//   - want: Expected names.
//
// Returns:
//   - bool: True when equal.
func namesEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}

	for idx := range want {
		if got[idx] != want[idx] {
			return false
		}
	}

	return true
}
