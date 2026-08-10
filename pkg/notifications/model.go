package notifications

import (
	"time"

	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// StaticData is the part of the notification template data model set upon initialization.
type StaticData struct {
	Title string
	Host  string
}

// notificationEntry is a snapshot of a log event for notification templates.
// Field names match the shape templates expect (Message, Data, Level, Time).
type notificationEntry struct {
	Message string
	Data    map[string]any
	Time    time.Time
	Level   zerolog.Level
}

// Data is the notification template data model.
type Data struct {
	StaticData

	Entries []*notificationEntry
	Report  types.Report
}
