// Package templates provides utility functions for use in Watchtower notification templates.
// This package defines a template.FuncMap with custom functions (e.g., ToJSON, ToUpper, ToLower, Title)
// that enhance the capabilities of Go templates used in the notification system, particularly for the
// template preview tool. These functions are integrated with the template rendering process in
// pkg/notifications/preview/tplprev.go and used with templates defined in
// pkg/notifications/common_templates.go, enabling dynamic formatting of notification data
// (e.g., JSON marshaling, string case conversion) for display in the web interface
// (docs/notifications/template-preview/index.md).
package templates

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// Funcs defines a set of utility functions for use in notification templates.
var Funcs = template.FuncMap{
	"ToUpper": strings.ToUpper,
	"ToLower": strings.ToLower,
	"ToJSON":  toJSON,
	"Title":   cases.Title(language.AmericanEnglish).String,
	"RFC1123": formatRFC1123,
}

// toJSON marshals a value to a formatted JSON string for use in templates.
// If marshaling fails, it logs a warning and returns an error message as the string.
func toJSON(v any) string {
	bytes, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"value": fmt.Sprintf("%v", v), // Avoid recursive marshaling issues
		}).Warn("Failed to marshal JSON in notification template")

		return fmt.Sprintf("failed to marshal JSON in notification template: %v", err)
	}

	return string(bytes)
}

// formatRFC1123 parses an RFC3339 timestamp string and formats it as RFC1123.
// If parsing fails, it logs a warning and returns the original string.
func formatRFC1123(value string) string {
	timestamp, err := time.Parse(time.RFC3339, value)
	if err != nil {
		logrus.WithError(err).WithField("value", value).
			Warn("Failed to parse RFC3339 timestamp in notification template")

		return value
	}

	return timestamp.Format(time.RFC1123)
}
