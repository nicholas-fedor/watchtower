package preview

import (
	"fmt"
	"strings"
	"text/template"

	"github.com/nicholas-fedor/watchtower/pkg/notifications/preview/data"
	"github.com/nicholas-fedor/watchtower/pkg/notifications/templates"
)

// Render generates a preview string from a template, states, and log levels.
//
// Parameters:
//   - input: Template string to render.
//   - states: List of container states to include.
//   - loglevels: List of log levels to include.
//
// Returns:
//   - string: Rendered preview string.
//   - error: Non-nil if parsing or execution fails, nil on success.
func Render(input string, states []data.State, loglevels []data.LogLevel) (string, error) {
	// Initialize data structure for template.
	data := data.New()

	// Parse template with custom functions.
	tpl, err := template.New("").Funcs(templates.Funcs).Parse(input)
	if err != nil {
		return "", fmt.Errorf("failed to parse %w", err)
	}

	// Add state data to preview.
	for _, state := range states {
		data.AddFromState(state)
	}

	// Add log level data to preview.
	for _, level := range loglevels {
		data.AddLogEntry(level)
	}

	// Execute template into buffer.
	var buf strings.Builder

	err = tpl.Execute(&buf, data)
	if err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}
