package notifications

import (
	"encoding/json"
	"fmt"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// PorcelainContainer represents a single container in the porcelain JSON report.
type PorcelainContainer struct {
	Name            string `json:"name"`
	Image           string `json:"image"`
	ImageID         string `json:"image_id"`
	LatestImageID   string `json:"latest_image_id"`
	State           string `json:"state"`
	UpdateAvailable bool   `json:"update_available"`
	Error           string `json:"error,omitempty"`
}

// PorcelainReport is the top-level JSON structure for --porcelain json.
type PorcelainReport struct {
	Containers []PorcelainContainer `json:"containers"`
}

// ToPorcelainReport converts a types.Report into a PorcelainReport.
//
// Parameters:
//   - sourceReport: Source report.
//
// Returns:
//   - PorcelainReport: JSON-ready report.
func ToPorcelainReport(sourceReport types.Report) PorcelainReport {
	if sourceReport == nil {
		return PorcelainReport{Containers: make([]PorcelainContainer, 0)}
	}

	allContainers := sourceReport.All()
	report := PorcelainReport{
		Containers: make([]PorcelainContainer, 0, len(allContainers)),
	}

	for _, containerReport := range allContainers {
		container := PorcelainContainer{
			Name:            containerReport.Name(),
			Image:           containerReport.ImageName(),
			ImageID:         containerReport.CurrentImageID().ShortID(),
			LatestImageID:   containerReport.LatestImageID().ShortID(),
			State:           containerReport.State(),
			UpdateAvailable: containerReport.CurrentImageID() != containerReport.LatestImageID(),
		}
		if err := containerReport.Error(); err != "" {
			container.Error = err
		}

		report.Containers = append(report.Containers, container)
	}

	return report
}

// ToPorcelainJSON marshals a types.Report to an indented JSON string for templates.
//
// Parameters:
//   - sourceReport: Source report.
//
// Returns:
//   - string: Indented JSON or error string if marshaling fails.
func ToPorcelainJSON(sourceReport types.Report) string {
	report := ToPorcelainReport(sourceReport)

	bytes, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Sprintf("failed to marshal porcelain JSON: %v", err)
	}

	return string(bytes)
}
