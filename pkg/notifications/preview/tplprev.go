// Package preview provides the core functionality for rendering notification template previews in Watchtower.
// This package is responsible for processing user-defined Go templates with simulated data to generate preview output
// for the template preview tool, accessible via the Watchtower documentation website. The tool allows users to test
// notification templates interactively without a live Docker environment, rendering results in both text and JSON formats.
//
// The primary component of this package is the Render function, which takes a template string, container states, and log
// levels as input, and produces a formatted string output. It leverages the text/template package with custom functions
// defined in notifications/templates/funcs.go (e.g., ToJSON, ToUpper) to process templates defined in
// notifications/common_templates.go or user inputs from the web interface (docs/notifications/template-preview/index.md).
// The Render function uses simulated data from the data package (data.go, logs.go, preview_strings.go, report.go, status.go),
// which generates realistic container statuses (e.g., scanned, updated, skipped) and log entries (e.g., info, warning, error)
// to mimic real Watchtower operational logs and notification reports.
//
// Integration with Watchtower:
// This package is consumed by the WebAssembly (WASM) module (tplprev.wasm, built from build/tplprev/main_wasm.go), which is
// loaded in the web interface (docs/notifications/template-preview/script.js) to dynamically render previews. The simulated
// data conforms to the notifications.Data model (model.go), which includes a Report (types.Report from report.go) and log
// Entries (logrus.Entry from logs.go). The web interface allows users to adjust container counts and log levels, triggering
// the Render function to generate updated previews displayed in styled tabs (styles.css) for text and JSON output.
//
// Usage:
// The Render function is the main entry point, called with a template string and arrays of states and log levels:
//   - input: A Go template string (e.g., from index.md or common_templates.go).
//   - states: Container states (e.g., ScannedState, UpdatedState from report.go).
//   - loglevels: Log levels (e.g., InfoLevel, ErrorLevel from logs.go).
//
// It initializes a PreviewData struct (data.go), populates it with simulated container statuses and log entries, and executes
// the template to produce a string output. The output is then displayed in the web interface or used for JSON marshaling
// (json.go) in the JSON tab.
//
// Example:
// To render a preview with 3 scanned containers and 2 error logs:
//
//	result, err := preview.Render(templateString, []data.State{"scanned", "scanned", "scanned"}, []data.LogLevel{"error", "error"})
//	if err != nil { ... }
//	fmt.Println(result) // Outputs formatted notification text
//
// Dependencies:
//   - github.com/nicholas-fedor/watchtower/pkg/notifications/preview/data: Provides simulated data (PreviewData, states, logs).
//   - github.com/nicholas-fedor/watchtower/pkg/notifications/templates: Provides template functions (e.g., ToJSON).
//   - github.com/nicholas-fedor/watchtower/pkg/types: Provides Report and ContainerReport interfaces.
//   - github.com/sirupsen/logrus: Used for log entry structures.
//   - text/template, strings: Standard library packages for template parsing and string building.
//
// Notes:
//   - The package is designed for preview purposes only and does not interact with live Docker environments.
//   - Simulated data is generated with a fixed random seed for deterministic output, ensuring consistent previews.
//   - Log messages and container names (preview_strings.go) are designed to mimic real Watchtower logs (e.g., "Found new image",
//     "Stopping container") for realistic simulation, as seen in provided example logs.
//   - The package integrates with the web interface via WASM, requiring recompilation (scripts/build-tplprev.sh) after changes.
//   - Future enhancements could include support for additional template functions or more dynamic data generation.
//
// For more details, see the Watchtower documentation at https://pkg.go.dev/github.com/nicholas-fedor/watchtower.
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
