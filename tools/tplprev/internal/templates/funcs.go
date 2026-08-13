// Package templates provides template functions and named builtin catalogs.
package templates

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/template"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/nicholas-fedor/tplprev/internal/report"
)

// Funcs defines a set of utility functions for use in notification templates.
var Funcs = template.FuncMap{
	"ToUpper":         strings.ToUpper,
	"ToLower":         strings.ToLower,
	"ToJSON":          toJSON,
	"ToPorcelainJSON": toPorcelainJSON,
	"Title":           cases.Title(language.AmericanEnglish).String,
	"RFC1123":         formatRFC1123,
}

// toJSON marshals a value to a formatted JSON string for use in templates.
// If marshaling fails, it returns an error message as the string.
func toJSON(v any) string {
	bytes, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("failed to marshal JSON in notification template: %v", err)
	}

	return string(bytes)
}

// porcelainContainer represents a single container in the porcelain JSON report.
type porcelainContainer struct {
	Name            string `json:"name"`
	Image           string `json:"image"`
	ImageID         string `json:"image_id"`
	LatestImageID   string `json:"latest_image_id"`
	State           string `json:"state"`
	UpdateAvailable bool   `json:"update_available"`
	Error           string `json:"error,omitempty"`
}

// porcelainReport is the top-level JSON structure for porcelain JSON output.
type porcelainReport struct {
	Containers []porcelainContainer `json:"containers"`
}

// toPorcelainJSON marshals a report.Report to an indented JSON string for templates.
// If marshaling fails, it returns an error message as the string.
func toPorcelainJSON(v any) string {
	if v == nil {
		return "{\n  \"containers\": []\n}"
	}

	r, ok := v.(report.Report)
	if !ok {
		return "failed to marshal porcelain JSON: input is not a report.Report"
	}

	pr := porcelainReport{
		Containers: make([]porcelainContainer, 0, len(r.All())),
	}

	for _, cr := range r.All() {
		container := porcelainContainer{
			Name:            cr.Name(),
			Image:           cr.ImageName(),
			ImageID:         cr.CurrentImageID().ShortID(),
			LatestImageID:   cr.LatestImageID().ShortID(),
			State:           cr.State(),
			UpdateAvailable: cr.CurrentImageID() != cr.LatestImageID(),
		}
		if err := cr.Error(); err != "" {
			container.Error = err
		}
		pr.Containers = append(pr.Containers, container)
	}

	bytes, err := json.MarshalIndent(pr, "", "  ")
	if err != nil {
		return fmt.Sprintf("failed to marshal porcelain JSON: %v", err)
	}

	return string(bytes)
}

// formatRFC1123 parses an RFC3339 timestamp string and formats it as RFC1123.
// If parsing fails, it returns the original string.
func formatRFC1123(value string) string {
	timestamp, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return value
	}

	return timestamp.Format(time.RFC1123)
}
