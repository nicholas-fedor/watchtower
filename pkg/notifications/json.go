package notifications

import (
	"encoding/json"
	"fmt"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

type jsonMap = map[string]any

// MarshalJSON implements json.Marshaler.
func (d Data) MarshalJSON() ([]byte, error) {
	entries := make([]jsonMap, len(d.Entries))
	for i, entry := range d.Entries {
		entries[i] = jsonMap{
			`level`:   entry.Level,
			`message`: entry.Message,
			`data`:    entry.Data,
			`time`:    entry.Time,
		}
	}

	var report jsonMap
	if d.Report != nil {
		report = jsonMap{
			`scanned`: marshalReports(d.Report.Scanned()),
			`updated`: marshalReports(d.Report.Updated()),
			`failed`:  marshalReports(d.Report.Failed()),
			`skipped`: marshalReports(d.Report.Skipped()),
			`stale`:   marshalReports(d.Report.Stale()),
			`fresh`:   marshalReports(d.Report.Fresh()),
		}
	}

	data := jsonMap{
		`report`:  report,
		`title`:   d.Title,
		`host`:    d.Host,
		`entries`: entries,
	}

	bytes, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal notification data: %w", err)
	}

	return bytes, nil
}

func marshalReports(reports []types.ContainerReport) []jsonMap {
	jsonReports := make([]jsonMap, len(reports))
	for i, report := range reports {
		jsonReports[i] = jsonMap{
			`id`:             report.ID().ShortID(),
			`name`:           report.Name(),
			`currentImageId`: report.CurrentImageID().ShortID(),
			`latestImageId`:  report.LatestImageID().ShortID(),
			`imageName`:      report.ImageName(),
			`state`:          report.State(),
		}
		if errorMessage := report.Error(); errorMessage != "" {
			jsonReports[i][`error`] = errorMessage
		}
	}

	return jsonReports
}

var _ json.Marshaler = &Data{}
