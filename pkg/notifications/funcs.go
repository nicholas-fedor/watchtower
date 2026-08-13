package notifications

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/template"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// Funcs defines utility functions for notification templates.
var Funcs = template.FuncMap{
	"ToUpper":         strings.ToUpper,
	"ToLower":         strings.ToLower,
	"ToJSON":          toJSON,
	"ToPorcelainJSON": ToPorcelainJSON,
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

// formatRFC1123 parses an RFC3339 timestamp string and formats it as RFC1123.
// If parsing fails, it returns the original string.
func formatRFC1123(value string) string {
	timestamp, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return value
	}

	return timestamp.Format(time.RFC1123)
}
