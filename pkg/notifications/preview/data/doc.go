// Package data provides utilities for generating simulated data used in the Watchtower template preview tool.
// The template preview tool, accessible via the Watchtower documentation website, allows users to test
// notification templates by rendering them with mock container statuses and log entries, without requiring
// a live Docker environment. This package is responsible for creating realistic placeholder data that mimics
// real Watchtower operational logs and container reports, enabling accurate testing of notification templates.
//
// The primary functionality of this package is to simulate:
//   - Container statuses (e.g., scanned, updated, failed, skipped, stale, fresh) with realistic names,
//     image references, and IDs, as defined in status.go and report.go.
//   - Log entries with timestamps, levels (e.g., info, warning, error), and messages, as defined in logs.go.
//   - Static data fields like Title and Host for notification templates, as integrated with model.go.
//
// Key Components:
//   - PreviewData (data.go): The main struct for generating and managing preview data, including a random
//     number generator for deterministic output, a report of container statuses, and a list of log entries.
//   - report (report.go): Manages categorized container statuses (e.g., Scanned, Updated) for the template.
//   - containerStatus (status.go): Represents a single container's status with ID, name, image, and state.
//   - logEntry (logs.go): Represents a single log entry with message, level, timestamp, and data fields.
//   - Placeholder data (preview_strings.go): Arrays of realistic container names, organization names, and
//     log messages (info, warning, error, skipped) to simulate Watchtower's operational output.
//
// Integration with Watchtower:
// This package is used by the template preview tool (tplprev.go) to render templates defined in
// notifications/common_templates.go. The PreviewData struct generates data that conforms to the
// notifications.Data model (model.go), which includes a Report (types.Report) and log Entries.
// The generated data is processed by the WASM module (tplprev.wasm, built from tools/tplprev/main_wasm.go)
// and displayed in the web interface (docs/notifications/template-preview/index.md, script.js, styles.css).
//
// Usage:
// The package is primarily consumed by the WASM-based template preview tool, where:
//   - PreviewData.New() initializes a generator with a fixed random seed for deterministic output.
//   - AddFromState(state) adds a container status with a realistic name, image, and optional error.
//   - AddLogEntry(level) adds a log entry with a realistic message and timestamp, based on the level.
//   - Report() provides the aggregated container statuses for template rendering.
//
// The simulated data is rendered using the template defined in index.md, styled by styles.css, and updated
// dynamically via script.js in response to user inputs (e.g., number of containers or log entries).
//
// Example:
// To simulate a preview with 3 scanned containers, 1 updated container, and 2 error logs:
//
//	data := New()
//	data.AddFromState(ScannedState)
//	data.AddFromState(ScannedState)
//	data.AddFromState(ScannedState)
//	data.AddFromState(UpdatedState)
//	data.AddLogEntry(ErrorLevel)
//	data.AddLogEntry(ErrorLevel)
//	result, _ := preview.Render(template, data.Entries, data.Report())
//
// Dependencies:
//   - github.com/nicholas-fedor/watchtower/pkg/types: Provides Report and ContainerReport interfaces.
//   - github.com/sirupsen/logrus: Used for log entry levels and data structures.
//   - math/rand: Used for deterministic random generation of IDs, names, and timestamps.
//   - encoding/hex, strconv, time: Standard library packages for ID generation and time handling.
//
// Notes:
//   - The random number generator uses a fixed seed (1) to ensure consistent preview output across runs.
//   - Messages in preview_strings.go are designed to mimic real Watchtower logs (e.g., "Found new image",
//     "Stopping container") for realistic simulation, as seen in the example logs provided.
//   - The package is designed for preview purposes only and does not interact with live Docker environments.
//   - Future enhancements could include more dynamic log message generation or integration with real log data.
//
// For more details, see the Watchtower documentation at https://pkg.go.dev/github.com/nicholas-fedor/watchtower.
package data
