package assert

import (
	"encoding/json"
	"fmt"
)

// PorcelainContainer is one container row in porcelain JSON.
type PorcelainContainer struct {
	Name            string `json:"name"`
	Image           string `json:"image"`
	ImageID         string `json:"image_id"`
	LatestImageID   string `json:"latest_image_id"`
	State           string `json:"state"`
	UpdateAvailable bool   `json:"update_available"`
	Error           string `json:"error,omitempty"`
}

// PorcelainReport is the top-level porcelain JSON document.
type PorcelainReport struct {
	Containers []PorcelainContainer `json:"containers"`
}

// ParsePorcelain decodes porcelain JSON and checks the documented field set.
//
// Parameters:
//   - raw: stdout captured from a run-once porcelain session.
//
// Returns:
//   - PorcelainReport: Parsed report.
//   - error: JSON or contract error.
func ParsePorcelain(raw []byte) (PorcelainReport, error) {
	var report PorcelainReport

	err := json.Unmarshal(raw, &report)
	if err != nil {
		return PorcelainReport{}, fmt.Errorf("parse porcelain: %w", err)
	}

	for idx, row := range report.Containers {
		if row.Name == "" {
			return report, fmt.Errorf("%w: index %d", ErrPorcelainMissingName, idx)
		}

		if row.Image == "" {
			return report, fmt.Errorf("%w: index %d", ErrPorcelainMissingImage, idx)
		}
	}

	return report, nil
}

// RequireUpdated asserts at least one container reports an update.
//
// Parameters:
//   - report: Parsed porcelain.
//
// Returns:
//   - error: When no container has update_available or a state of updated.
func RequireUpdated(report PorcelainReport) error {
	for _, row := range report.Containers {
		if row.UpdateAvailable || row.State == "Updated" || row.State == "updated" {
			return nil
		}
	}

	return fmt.Errorf("%w: rows %d", ErrPorcelainNoUpdate, len(report.Containers))
}
