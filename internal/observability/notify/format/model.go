package format

import (
	"time"
	"unicode/utf8"

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

// Limits of untrusted text in a notification.
const (
	// maxTextLength is the most characters a notification keeps of one piece of
	// text from outside Watchtower.
	maxTextLength = 1024
	// keptStart is how many characters shortened text keeps from its start.
	keptStart = 511
	// keptEnd is how many characters shortened text keeps from its end, which
	// leaves one character for the elision.
	keptEnd = maxTextLength - keptStart - 1
	// elision marks where shortened text was cut.
	elision = "…"
)

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
// outside Watchtower, such as command output and image annotations. Each string,
// also inside nested field values, is cleaned as [logging.PrintableLine]
// describes and shortened to at most 1,024 characters. The title, host, times
// and levels are Watchtower's own and stay as they are. d itself is unchanged.
//
// Returns:
//   - Data: The printable copy.
func (d Data) printable() Data {
	return Data{
		StaticData: d.StaticData,
		Report:     state.MapText(d.Report, notificationText),
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
			Message: notificationText(entry.Message),
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
		printableMap[notificationText(key)] = printableValue(value)
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
		return notificationText(typed)
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

// notificationText returns text from outside Watchtower printable on one line
// and at most 1,024 characters long.
//
// Longer text keeps its first 511 and last 512 characters with an ellipsis
// between them.
//
// Parameters:
//   - text: Text as its source wrote it.
//
// Returns:
//   - string: The printable, shortened text.
func notificationText(text string) string {
	line := logging.PrintableLine(text)

	// A line holds no more characters than bytes, so most lines need no count.
	if len(line) <= maxTextLength || utf8.RuneCountInString(line) <= maxTextLength {
		return line
	}

	// The start of an error names the failure and the end of command output
	// holds its result, so the cut drops the middle.
	start := 0
	for range keptStart {
		_, size := utf8.DecodeRuneInString(line[start:])
		start += size
	}

	end := len(line)
	for range keptEnd {
		_, size := utf8.DecodeLastRuneInString(line[:end])
		end -= size
	}

	return line[:start] + elision + line[end:]
}
