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

type jsonMap = map[string]any

// MarshalJSON implements json.Marshaler for the Data type.
// It converts the notification data into a JSON structure, including report details and log entries.
func (d Data) MarshalJSON() ([]byte, error) {
	clog := logrus.WithFields(logrus.Fields{
		"title":   d.Title,
		"host":    d.Host,
		"entries": len(d.Entries),
	})
	clog.Debug("Marshaling notification data to JSON")

	entries := make([]jsonMap, len(d.Entries))
	for i, entry := range d.Entries {
		entries[i] = jsonMap{
			"level":   entry.Level,
			"message": entry.Message,
			"data":    entry.Data,
			"time":    entry.Time,
		}
	}

	var report jsonMap

	if d.Report != nil {
		clog.WithField("report_entries", fmt.Sprintf("%d scanned, %d updated", len(d.Report.Scanned()), len(d.Report.Updated()))).
			Debug("Including report in JSON")

		report = jsonMap{
			"scanned": marshalReports(d.Report.Scanned()),
			"updated": marshalReports(d.Report.Updated()),
			"failed":  marshalReports(d.Report.Failed()),
			"skipped": marshalReports(d.Report.Skipped()),
			"stale":   marshalReports(d.Report.Stale()),
			"fresh":   marshalReports(d.Report.Fresh()),
		}
	}

	data := jsonMap{
		"report":  report,
		"title":   d.Title,
		"host":    d.Host,
		"entries": entries,
	}

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

// marshalReports converts a slice of ContainerReport into a JSON-compatible structure.
// It includes key fields like ID, name, image IDs, and state, adding an error field if present.
func marshalReports(reports []types.ContainerReport) []jsonMap {
	jsonReports := make([]jsonMap, len(reports))
	for i, report := range reports {
		jsonReports[i] = jsonMap{
			"id":             report.ID().ShortID(),
			"name":           report.Name(),
			"currentImageId": report.CurrentImageID().ShortID(),
			"latestImageId":  report.LatestImageID().ShortID(),
			"imageName":      report.ImageName(),
			"state":          report.State(),
		}
		if errorMessage := report.Error(); errorMessage != "" {
			jsonReports[i]["error"] = errorMessage
		}
	}

	return jsonReports
}
