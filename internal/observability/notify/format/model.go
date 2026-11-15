package format

import (
	"time"

	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
)

// StaticData is the part of the notification template data model set upon initialization.
type StaticData struct {
	Title string
	Host  string
}

// Entry is a snapshot of a log event for notification templates.
// Field names match the shape templates expect (Message, Data, Level, Time).
type Entry struct {
	Message string
	Data    map[string]any
	Time    time.Time
	Level   string
}

// Data is the notification template data model.
type Data struct {
	StaticData

	Report  *state.Report
	Entries []*Entry
}

// LevelString converts a zerolog level to the legacy template string.
// WarnLevel maps to "warning" for backwards compatibility with templates
// that compare .Level against "warning". All other levels use zerolog's
// standard string representation.
func LevelString(level zerolog.Level) string {
	if level == zerolog.WarnLevel {
		return "warning"
	}

	return level.String()
}

// printable returns a copy of the data whose untrusted text is printable on
// one line.
//
// Log messages, log fields and the report's container text can hold text from
// outside Watchtower, such as command output and image annotations. Each string
// is cleaned as [logging.PrintableLine] describes, also inside nested field
// values. The title, host, times and levels are Watchtower's own and stay as
// they are. d itself is unchanged.
//
// Returns:
//   - Data: The printable copy.
func (d Data) printable() Data {
	return Data{
		StaticData: d.StaticData,
		Report:     state.MapText(d.Report, logging.PrintableLine),
		Entries:    printableEntries(d.Entries),
	}
}

// printableEntries returns copies of log entries whose messages and fields are
// printable on one line.
//
// Parameters:
//   - entries: The entries to copy.
//
// Returns:
//   - []*Entry: The printable copies, in the same order.
func printableEntries(entries []*Entry) []*Entry {
	printableList := make([]*Entry, len(entries))
	for i, entry := range entries {
		printableList[i] = &Entry{
			Message: logging.PrintableLine(entry.Message),
			Data:    printableFields(entry.Data),
			Time:    entry.Time,
			Level:   entry.Level,
		}
	}

	return printableList
}

// printableFields returns a copy of log fields whose keys and string values are
// printable on one line, also inside nested maps and lists.
//
// Parameters:
//   - fields: Fields decoded from a log event, or nil.
//
// Returns:
//   - map[string]any: The printable copy, or nil when fields is nil.
func printableFields(fields map[string]any) map[string]any {
	if fields == nil {
		return nil
	}

	printableMap := make(map[string]any, len(fields))
	for key, value := range fields {
		printableMap[logging.PrintableLine(key)] = printableValue(value)
	}

	return printableMap
}

// printableValue returns a log field value with its strings printable on one
// line. Other values, such as numbers and booleans, are returned unchanged.
//
// Parameters:
//   - value: A value decoded from a log event's JSON.
//
// Returns:
//   - any: The printable value.
func printableValue(value any) any {
	switch typed := value.(type) {
	case string:
		return logging.PrintableLine(typed)
	case map[string]any:
		return printableFields(typed)
	case []any:
		values := make([]any, len(typed))
		for i, item := range typed {
			values[i] = printableValue(item)
		}

		return values
	default:
		return value
	}
}
