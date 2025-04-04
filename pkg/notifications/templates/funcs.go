package templates

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

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
