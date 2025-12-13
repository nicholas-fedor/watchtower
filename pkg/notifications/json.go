// Package notifications provides mechanisms for sending notifications via various services.
// This file implements JSON marshaling for notification data.
package notifications

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

var _ json.Marshaler = &Data{}

// Errors for JSON marshaling.
var (
	// errMarshalFailed indicates a failure to marshal notification data to JSON.
	errMarshalFailed = errors.New("failed to marshal notification data")
)

// jsonMap is a type alias for a JSON-compatible map.
type jsonMap = map[string]any

// MarshalJSON implements json.Marshaler for Data.
//
// Returns:
//   - []byte: JSON-encoded data.
//   - error: Non-nil if marshaling fails, nil on success.
func (d Data) MarshalJSON() ([]byte, error) {
	clog := logrus.WithFields(logrus.Fields{
		"title":   d.Title,
		"host":    d.Host,
		"entries": len(d.Entries),
	})
	clog.Debug("Marshaling notification data to JSON")

	// Convert log entries to JSON maps.
	entries := make([]jsonMap, len(d.Entries))
	for i, entry := range d.Entries {
		entries[i] = jsonMap{
			"level":   entry.Level,
			"message": entry.Message,
			"data":    entry.Data,
			"time":    entry.Time,
		}
	}

	// Include report data if present.
	var report jsonMap

	if d.Report != nil {
		clog.WithField("report_entries", fmt.Sprintf("%d scanned, %d updated", len(d.Report.Scanned()), len(d.Report.Updated()))).
			Debug("Including report in JSON")

		report = jsonMap{
			"scanned":   marshalReports(d.Report.Scanned()),
			"updated":   marshalReports(d.Report.Updated()),
			"restarted": marshalReports(d.Report.Restarted()),
			"failed":    marshalReports(d.Report.Failed()),
			"skipped":   marshalReports(d.Report.Skipped()),
			"stale":     marshalReports(d.Report.Stale()),
			"fresh":     marshalReports(d.Report.Fresh()),
		}
	}

	// Build final JSON structure.
	data := jsonMap{
		"report":  report,
		"title":   d.Title,
		"host":    d.Host,
		"entries": entries,
	}

	// Marshal to JSON bytes.
	bytes, err := json.Marshal(data)
	if err != nil {
		clog.WithError(err).
			WithField("data", fmt.Sprintf("%v", data)).
			Error("Failed to marshal notification data to JSON")

		return nil, fmt.Errorf("%w: %w", errMarshalFailed, err)
	}

	clog.WithField("size", len(bytes)).Debug("Successfully marshaled notification data to JSON")

	return bytes, nil
}

// marshalReports converts ContainerReport slice to JSON-compatible maps.
//
// Parameters:
//   - reports: List of container reports.
//
// Returns:
//   - []jsonMap: JSON maps of report data.
func marshalReports(reports []types.ContainerReport) []jsonMap {
	jsonReports := make([]jsonMap, len(reports))
	for i, report := range reports {
		containerID := report.ID().ShortID()

		// Populate base report fields.
		jsonReports[i] = jsonMap{
			"id":             containerID,
			"name":           report.Name(),
			"currentImageId": report.CurrentImageID().ShortID(),
			"latestImageId":  report.LatestImageID().ShortID(),
			"imageName":      report.ImageName(),
			"state":          report.State(),
		}
		// Add error if present.
		if errorMessage := report.Error(); errorMessage != "" {
			jsonReports[i]["error"] = errorMessage
		}
	}

	return jsonReports
}
